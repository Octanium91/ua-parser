package core

// The correction layer: a declarative override config (corrections.yaml) that
// patches known detection gaps after uap-core regexes and Client Hints have
// run. The file is embedded at build time and hot-swappable at runtime via
// ApplyCorrectionsYAML (used by the background updater, the c-shared
// UpdateCorrections export, the wasip1 host-push export, and the js/wasm
// fetch-at-init path). Design: docs/correction-layer.md.
//
// This file intentionally imports no networking packages: it must compile
// unchanged for every target including wasm (transport lives elsewhere).

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed resources/corrections.yaml
var defaultCorrections []byte

const (
	// correctionsSchemaVersion is the highest schema_version this binary
	// understands; files above it are rejected whole (keep-last-good).
	correctionsSchemaVersion = 1

	maxCorrectionRules  = 64
	maxVendorPrefixes   = 24
	maxCorrectionRegex  = 512 // bytes of regex source per rule
	maxCorrectionValue  = 128 // bytes per set value
	maxCorrectionsBytes = 1 << 20

	// Self-test cost bounds. ApplyCorrectionsYAML runs every inline test
	// through the full parse pipeline BEFORE swapping (and, in the Node/WASM
	// hosts, synchronously). Real UAs are well under 1 KB; these caps keep a
	// crafted-but-valid file from stalling the swap on giant test strings.
	maxTestUALen      = 4 << 10  // per inline-test UA
	maxTestsPerRule   = 16       // inline tests per rule
	maxTotalTestBytes = 64 << 10 // cumulative inline-test UA bytes per file
)

// stringList accepts a YAML scalar or sequence, so rules can write
// `ua_contains: "tesla/"` and `ua_contains: [a, b]` interchangeably.
type stringList []string

func (s *stringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var v string
		if err := node.Decode(&v); err != nil {
			return err
		}
		*s = []string{v}
		return nil
	case yaml.SequenceNode:
		var v []string
		if err := node.Decode(&v); err != nil {
			return err
		}
		*s = v
		return nil
	}
	return fmt.Errorf("expected string or list of strings")
}

type correctionMatch struct {
	// UAContains: every anchor must be present (AND); UAContainsAny: at least
	// one (OR). One of the two is mandatory — it is the perf prefilter that
	// gates the regex and field checks.
	UAContains    stringList `yaml:"ua_contains"`
	UAContainsAny stringList `yaml:"ua_contains_any"`
	UARegex       string     `yaml:"ua_regex"`

	// Equals-any conditions on the parsed (post-Client-Hints) result. An
	// empty string in the list matches an empty field — the idiom for
	// "fill the gap, never overwrite".
	BrowserName  stringList `yaml:"browser_name"`
	BrowserType  stringList `yaml:"browser_type"`
	OSName       stringList `yaml:"os_name"`
	DeviceType   stringList `yaml:"device_type"`
	DeviceVendor stringList `yaml:"device_vendor"`
	IsBot        *bool      `yaml:"is_bot"`
	IsAICrawler  *bool      `yaml:"is_ai_crawler"`

	// XRequestedWith matches the X-Requested-With request header (equals-any,
	// case-insensitive): Android WebView carries the embedding app's package
	// id there — the strongest in-app browser signal a backend gets for free.
	XRequestedWith stringList `yaml:"x_requested_with"`
}

// correctionSet uses pointers so an explicit "" clears a field while an
// absent key leaves it untouched. Values may reference ua_regex capture
// groups with $1 / ${name} (regexp.Expand syntax).
type correctionSet struct {
	BrowserName    *string `yaml:"browser_name"`
	BrowserVersion *string `yaml:"browser_version"`
	BrowserType    *string `yaml:"browser_type"`
	OSName         *string `yaml:"os_name"`
	OSVersion      *string `yaml:"os_version"`
	DeviceVendor   *string `yaml:"device_vendor"`
	DeviceModel    *string `yaml:"device_model"`
	DeviceType     *string `yaml:"device_type"`
	Category       *string `yaml:"category"`
	IsBot          *bool   `yaml:"is_bot"`
	IsAICrawler    *bool   `yaml:"is_ai_crawler"`
}

type correctionTest struct {
	UA      string            `yaml:"ua"`
	Headers map[string]string `yaml:"headers"`
	// Expect maps dotted result paths (browser.name, device.type, category,
	// is_bot, ...) to expected values; booleans compare as "true"/"false".
	Expect map[string]string `yaml:"expect"`
}

type correctionRule struct {
	ID          string           `yaml:"id"`
	Description string           `yaml:"description"`
	Upstream    string           `yaml:"upstream"`
	Permanent   bool             `yaml:"permanent"`
	Match       correctionMatch  `yaml:"match"`
	Set         correctionSet    `yaml:"set"`
	Tests       []correctionTest `yaml:"tests"`
}

type vendorPrefix struct {
	ModelRegex string `yaml:"model_regex"`
	Vendor     string `yaml:"vendor"`
}

// correctionsFile is the top level. Rules are decoded per-node so a rule
// using a match/set key this binary does not know is SKIPPED (logged), not
// fatal — old binaries keep applying every rule they fully understand.
// Unknown top-level sections are ignored (additive evolution); structural
// errors reject the whole file (keep-last-good).
type correctionsFile struct {
	SchemaVersion  int            `yaml:"schema_version"`
	Version        string         `yaml:"version"`
	Rules          []yaml.Node    `yaml:"rules"`
	VendorPrefixes []vendorPrefix `yaml:"vendor_prefixes"`
}

type compiledRule struct {
	id         string
	permanent  bool
	anchorsAll []string // lowercased
	anchorsAny []string // lowercased
	re         *regexp.Regexp

	matchBrowserName  []string
	matchBrowserType  []string
	matchOSName       []string
	matchDeviceType   []string
	matchDeviceVendor []string
	matchIsBot        *bool
	matchIsAI         *bool
	matchXRW          []string

	set   correctionSet
	tests []correctionTest
}

type compiledVendorPrefix struct {
	re     *regexp.Regexp
	vendor string
}

type compiledCorrections struct {
	version        string
	rules          []compiledRule
	vendorPrefixes []compiledVendorPrefix
	skippedRules   int
}

// compileCorrections parses, validates, and compiles a corrections.yaml
// payload. Any returned error means "reject the whole file"; individual rules
// with unknown keys are skipped and counted instead (forward compatibility).
func compileCorrections(data []byte) (*compiledCorrections, error) {
	if len(data) > maxCorrectionsBytes {
		return nil, fmt.Errorf("corrections file exceeds %d bytes", maxCorrectionsBytes)
	}

	var file correctionsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("corrections YAML: %w", err)
	}
	if file.SchemaVersion > correctionsSchemaVersion {
		return nil, fmt.Errorf("corrections schema_version %d newer than supported %d", file.SchemaVersion, correctionsSchemaVersion)
	}
	if len(file.Rules) > maxCorrectionRules {
		return nil, fmt.Errorf("corrections file has %d rules, cap is %d", len(file.Rules), maxCorrectionRules)
	}
	if len(file.VendorPrefixes) > maxVendorPrefixes {
		return nil, fmt.Errorf("corrections file has %d vendor prefixes, cap is %d", len(file.VendorPrefixes), maxVendorPrefixes)
	}

	cc := &compiledCorrections{version: file.Version}
	seen := make(map[string]bool)
	totalTestBytes := 0

	for i := range file.Rules {
		rule, skip, err := decodeRuleStrict(&file.Rules[i])
		if err != nil {
			// A structural error (type mismatch, duplicate key) is an author
			// bug, not a newer-engine field — reject the whole file so the
			// mistake is loud, not a silently-vanished rule.
			return nil, fmt.Errorf("rule #%d: %w", i+1, err)
		}
		if skip {
			// A rule written for a newer engine (unknown field): skip it,
			// keep the rest.
			cc.skippedRules++
			continue
		}
		compiled, err := compileRule(rule)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.ID, err)
		}
		if compiled.id == "" {
			return nil, fmt.Errorf("rule #%d: missing id", i+1)
		}
		if seen[compiled.id] {
			return nil, fmt.Errorf("duplicate rule id %q", compiled.id)
		}
		seen[compiled.id] = true
		for _, tc := range compiled.tests {
			totalTestBytes += len(tc.UA)
		}
		if totalTestBytes > maxTotalTestBytes {
			return nil, fmt.Errorf("cumulative inline-test UA bytes exceed %d", maxTotalTestBytes)
		}
		cc.rules = append(cc.rules, *compiled)
	}

	for _, vp := range file.VendorPrefixes {
		if vp.Vendor == "" || vp.ModelRegex == "" {
			return nil, fmt.Errorf("vendor prefix entry needs both model_regex and vendor")
		}
		if len(vp.ModelRegex) > maxCorrectionRegex {
			return nil, fmt.Errorf("vendor prefix regex for %q exceeds %d bytes", vp.Vendor, maxCorrectionRegex)
		}
		re, err := regexp.Compile(vp.ModelRegex)
		if err != nil {
			return nil, fmt.Errorf("vendor prefix regex for %q: %w", vp.Vendor, err)
		}
		cc.vendorPrefixes = append(cc.vendorPrefixes, compiledVendorPrefix{re: re, vendor: vp.Vendor})
	}

	return cc, nil
}

// decodeRuleStrict decodes one rule node with strict field checking.
//   - skip=true: the node uses ONLY unknown fields this binary does not
//     understand (a rule authored for a newer engine) → caller skips it.
//   - err!=nil: a structural error (type mismatch, duplicate key, bad value)
//     → caller rejects the whole file, so an author typo is loud rather than
//     a rule that silently vanishes under the "newer engine" banner.
func decodeRuleStrict(node *yaml.Node) (rule correctionRule, skip bool, err error) {
	raw, err := yaml.Marshal(node)
	if err != nil {
		return correctionRule{}, false, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if decErr := dec.Decode(&rule); decErr != nil {
		if isUnknownFieldOnly(decErr) {
			return correctionRule{}, true, nil
		}
		return correctionRule{}, false, decErr
	}
	return rule, false, nil
}

// isUnknownFieldOnly reports whether a yaml decode error consists solely of
// KnownFields "field not found" errors (forward-compat), as opposed to a
// structural error (type mismatch, duplicate key) that must reject the file.
func isUnknownFieldOnly(err error) bool {
	te, ok := err.(*yaml.TypeError)
	if !ok || len(te.Errors) == 0 {
		return false
	}
	for _, e := range te.Errors {
		if !strings.Contains(e, "not found in type") {
			return false
		}
	}
	return true
}

func compileRule(rule correctionRule) (*compiledRule, error) {
	c := &compiledRule{
		id:                rule.ID,
		permanent:         rule.Permanent,
		matchBrowserName:  rule.Match.BrowserName,
		matchBrowserType:  rule.Match.BrowserType,
		matchOSName:       rule.Match.OSName,
		matchDeviceType:   rule.Match.DeviceType,
		matchDeviceVendor: rule.Match.DeviceVendor,
		matchIsBot:        rule.Match.IsBot,
		matchIsAI:         rule.Match.IsAICrawler,
		matchXRW:          rule.Match.XRequestedWith,
		set:               rule.Set,
		tests:             rule.Tests,
	}

	for _, a := range rule.Match.UAContains {
		if a == "" {
			return nil, fmt.Errorf("empty ua_contains anchor")
		}
		c.anchorsAll = append(c.anchorsAll, strings.ToLower(a))
	}
	for _, a := range rule.Match.UAContainsAny {
		if a == "" {
			return nil, fmt.Errorf("empty ua_contains_any anchor")
		}
		c.anchorsAny = append(c.anchorsAny, strings.ToLower(a))
	}
	if len(c.anchorsAll) == 0 && len(c.anchorsAny) == 0 {
		return nil, fmt.Errorf("a rule must declare ua_contains or ua_contains_any (the perf prefilter)")
	}

	if rule.Match.UARegex != "" {
		if len(rule.Match.UARegex) > maxCorrectionRegex {
			return nil, fmt.Errorf("ua_regex exceeds %d bytes", maxCorrectionRegex)
		}
		re, err := regexp.Compile(rule.Match.UARegex)
		if err != nil {
			return nil, fmt.Errorf("ua_regex: %w", err)
		}
		c.re = re
	}

	expands := false
	for _, v := range []*string{
		rule.Set.BrowserName, rule.Set.BrowserVersion, rule.Set.BrowserType,
		rule.Set.OSName, rule.Set.OSVersion,
		rule.Set.DeviceVendor, rule.Set.DeviceModel, rule.Set.DeviceType,
		rule.Set.Category,
	} {
		if v == nil {
			continue
		}
		if len(*v) > maxCorrectionValue {
			return nil, fmt.Errorf("set value %q exceeds %d bytes", *v, maxCorrectionValue)
		}
		if strings.Contains(*v, "$") {
			expands = true
		}
	}
	if expands && c.re == nil {
		return nil, fmt.Errorf("set value references capture groups but the rule has no ua_regex")
	}
	if c.re != nil {
		// Catch the common authoring foot-gun: a $N referencing a group the
		// regex does not have (regexp.Expand would silently blank it). A
		// literal '$' must be written '$$'.
		groups := c.re.NumSubexp()
		for _, v := range []*string{
			rule.Set.BrowserName, rule.Set.BrowserVersion, rule.Set.BrowserType,
			rule.Set.OSName, rule.Set.OSVersion,
			rule.Set.DeviceVendor, rule.Set.DeviceModel, rule.Set.DeviceType,
			rule.Set.Category,
		} {
			if v == nil {
				continue
			}
			if n, bad := referencesMissingGroup(*v, groups); bad {
				return nil, fmt.Errorf("set value %q references capture group $%d but the regex has %d (write a literal '$' as '$$')", *v, n, groups)
			}
		}
	}
	if !hasAnySet(rule.Set) {
		return nil, fmt.Errorf("rule sets nothing")
	}
	if len(rule.Tests) == 0 {
		return nil, fmt.Errorf("every rule must carry at least one inline test")
	}
	if len(rule.Tests) > maxTestsPerRule {
		return nil, fmt.Errorf("rule has %d inline tests, cap is %d", len(rule.Tests), maxTestsPerRule)
	}
	for _, tc := range rule.Tests {
		if tc.UA == "" || len(tc.Expect) == 0 {
			return nil, fmt.Errorf("inline test needs ua and expect")
		}
		if len(tc.UA) > maxTestUALen {
			return nil, fmt.Errorf("inline test UA exceeds %d bytes", maxTestUALen)
		}
	}

	return c, nil
}

// referencesMissingGroup scans a set value for a numeric $N / ${N} capture
// reference whose index exceeds numGroups (regexp.Expand semantics: $$ is a
// literal dollar and is skipped; $0 is the whole match and is always valid).
// Named references are left to the rule's inline self-test to validate.
func referencesMissingGroup(value string, numGroups int) (int, bool) {
	for i := 0; i < len(value); i++ {
		if value[i] != '$' {
			continue
		}
		if i+1 < len(value) && value[i+1] == '$' {
			i++ // literal $$
			continue
		}
		j := i + 1
		if j < len(value) && value[j] == '{' {
			j++
		}
		start := j
		for j < len(value) && value[j] >= '0' && value[j] <= '9' {
			j++
		}
		if j > start {
			if n, err := strconv.Atoi(value[start:j]); err == nil && n > numGroups {
				return n, true
			}
		}
	}
	return 0, false
}

func hasAnySet(s correctionSet) bool {
	return s.BrowserName != nil || s.BrowserVersion != nil || s.BrowserType != nil ||
		s.OSName != nil || s.OSVersion != nil ||
		s.DeviceVendor != nil || s.DeviceModel != nil || s.DeviceType != nil ||
		s.Category != nil || s.IsBot != nil || s.IsAICrawler != nil
}

// genericVendors / genericModels are the placeholder values uap-core emits
// when it cannot resolve a device; correction rules and the vendor-prefix
// table only ever fill these, never a real value.
var genericVendors = map[string]bool{
	"":                       true,
	"Generic":                true,
	"Generic_Android":        true,
	"Generic_Android_Tablet": true,
}

var genericModels = map[string]bool{
	"":                   true,
	"K":                  true,
	"Smartphone":         true,
	"Tablet":             true,
	"Generic":            true,
	"Generic Smartphone": true,
	"Generic Tablet":     true,
}

// modelFromBuildRE extracts the device model from the "; <model> Build/"
// segment Android UAs carry, used only when uap-core produced a generic model.
var modelFromBuildRE = regexp.MustCompile(`;\s*([^;()]+?)\s+Build/`)

// applyCorrections runs the rule set against a parsed result. It executes
// after applyClientHints (corrections are terminal — see the design doc) and
// before the category switch, so overridden device types feed the category
// derivation. An explicit set.category is returned to the caller and applied
// after the switch.
func applyCorrections(res *Result, ua, uaLower string, headers map[string]string, cc *compiledCorrections) (categoryOverride string) {
	if cc == nil {
		return ""
	}

rules:
	for i := range cc.rules {
		rule := &cc.rules[i]

		// Substring prefilter — the only work done for non-matching traffic.
		for _, a := range rule.anchorsAll {
			if !strings.Contains(uaLower, a) {
				continue rules
			}
		}
		if len(rule.anchorsAny) > 0 {
			hit := false
			for _, a := range rule.anchorsAny {
				if strings.Contains(uaLower, a) {
					hit = true
					break
				}
			}
			if !hit {
				continue rules
			}
		}

		// Parsed-result conditions.
		if !matchesAny(rule.matchBrowserName, res.Browser.Name) ||
			!matchesAny(rule.matchBrowserType, res.Browser.Type) ||
			!matchesAny(rule.matchOSName, res.OS.Name) ||
			!matchesAny(rule.matchDeviceType, res.Device.Type) ||
			!matchesAny(rule.matchDeviceVendor, res.Device.Vendor) {
			continue
		}
		if rule.matchIsBot != nil && *rule.matchIsBot != res.IsBot {
			continue
		}
		if rule.matchIsAI != nil && *rule.matchIsAI != res.IsAICrawler {
			continue
		}
		if len(rule.matchXRW) > 0 && !matchesAny(rule.matchXRW, headers["x-requested-with"]) {
			continue
		}

		// Regex + capture groups run only after every cheap check passed.
		var matchIdx []int
		if rule.re != nil {
			matchIdx = rule.re.FindStringSubmatchIndex(ua)
			if matchIdx == nil {
				continue
			}
		}

		expand := func(v string) string {
			if !strings.Contains(v, "$") || rule.re == nil {
				return v
			}
			return string(rule.re.ExpandString(nil, v, ua, matchIdx))
		}

		if rule.set.BrowserName != nil {
			res.Browser.Name = expand(*rule.set.BrowserName)
		}
		if rule.set.BrowserVersion != nil {
			res.Browser.Version = expand(*rule.set.BrowserVersion)
			res.Browser.Major = majorOf(res.Browser.Version)
		}
		if rule.set.BrowserType != nil {
			res.Browser.Type = expand(*rule.set.BrowserType)
		}
		if rule.set.OSName != nil {
			res.OS.Name = expand(*rule.set.OSName)
		}
		if rule.set.OSVersion != nil {
			res.OS.Version = expand(*rule.set.OSVersion)
		}
		if rule.set.DeviceVendor != nil {
			res.Device.Vendor = expand(*rule.set.DeviceVendor)
		}
		if rule.set.DeviceModel != nil {
			res.Device.Model = expand(*rule.set.DeviceModel)
		}
		if rule.set.DeviceType != nil {
			res.Device.Type = expand(*rule.set.DeviceType)
		}
		if rule.set.IsBot != nil {
			res.IsBot = *rule.set.IsBot
		}
		if rule.set.IsAICrawler != nil {
			res.IsAICrawler = *rule.set.IsAICrawler
		}
		if rule.set.Category != nil {
			categoryOverride = expand(*rule.set.Category)
		}
	}

	resolveDeviceVendor(res, ua, uaLower, cc)
	return categoryOverride
}

// matchesAny reports whether value equals (case-insensitively) any entry;
// an empty condition list matches everything.
func matchesAny(want []string, value string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if strings.EqualFold(w, value) {
			return true
		}
	}
	return false
}

// resolveDeviceVendor fills a generic device vendor from the model string via
// the config-driven prefix table, extracting the model from the Android
// "Build/" token first when uap-core left it generic. Fill-gap only: a
// resolved vendor is never overwritten.
func resolveDeviceVendor(res *Result, ua, uaLower string, cc *compiledCorrections) {
	if !genericVendors[res.Device.Vendor] {
		return
	}

	if genericModels[res.Device.Model] && strings.Contains(uaLower, " build/") {
		if m := modelFromBuildRE.FindStringSubmatch(ua); m != nil {
			// Only replace when the capture is a real token — a "; <spaces>
			// Build/" segment would otherwise blank a generic model.
			if extracted := strings.TrimSpace(m[1]); extracted != "" {
				res.Device.Model = extracted
			}
		}
	}
	if res.Device.Model == "" {
		return
	}

	for i := range cc.vendorPrefixes {
		if cc.vendorPrefixes[i].re.MatchString(res.Device.Model) {
			res.Device.Vendor = cc.vendorPrefixes[i].vendor
			return
		}
	}
}

// ApplyCorrectionsYAML validates a corrections payload (schema, caps, regex
// compilation, and the file's own inline tests run through the full parse
// pipeline) and hot-swaps the active rule set. Whole-file semantics: any
// failure leaves the previous (last good) rules in place. Safe to call from
// any goroutine and from host FFI/WASM exports.
func (p *Parser) ApplyCorrectionsYAML(data []byte) error {
	cc, err := compileCorrections(data)
	if err != nil {
		return err
	}
	if err := p.runCorrectionTests(cc); err != nil {
		return fmt.Errorf("corrections self-test: %w", err)
	}

	p.corrections.Store(cc)

	// Bump the generation BEFORE purging (same ordering as updateRegexes):
	// any Parse that started against the old rules skips caching its result.
	p.gen.Add(1)
	if p.cache != nil {
		p.cache.Purge()
	}

	if cc.skippedRules > 0 {
		log.Printf("Corrections applied: version=%q rules=%d skipped=%d (rules for a newer engine)",
			cc.version, len(cc.rules), cc.skippedRules)
	} else {
		log.Printf("Corrections applied: version=%q rules=%d", cc.version, len(cc.rules))
	}
	return nil
}

// CorrectionsInfo reports the active correction set (for health endpoints).
func (p *Parser) CorrectionsInfo() (version string, rules int) {
	cc := p.corrections.Load()
	if cc == nil {
		return "", 0
	}
	return cc.version, len(cc.rules)
}

// runCorrectionTests executes every rule's inline test cases through the full
// parse pipeline using the CANDIDATE rule set (not the active one), without
// touching the cache. CI runs the same function over the embedded file, so
// runtime and CI can never disagree on validity.
func (p *Parser) runCorrectionTests(cc *compiledCorrections) error {
	for _, rule := range cc.rules {
		for ti, tc := range rule.tests {
			res := p.computeResult(tc.UA, normalizeHeaders(tc.Headers), cc)
			for path, want := range tc.Expect {
				got, ok := resultField(res, path)
				if !ok {
					return fmt.Errorf("rule %q test #%d: unknown expect path %q", rule.id, ti+1, path)
				}
				if got != want {
					return fmt.Errorf("rule %q test #%d: %s = %q, want %q", rule.id, ti+1, path, got, want)
				}
			}
		}
	}
	return nil
}

// resultField resolves a dotted expect path into the result's field value.
func resultField(res *Result, path string) (string, bool) {
	switch path {
	case "browser.name":
		return res.Browser.Name, true
	case "browser.version":
		return res.Browser.Version, true
	case "browser.major":
		return res.Browser.Major, true
	case "browser.type":
		return res.Browser.Type, true
	case "os.name":
		return res.OS.Name, true
	case "os.version":
		return res.OS.Version, true
	case "device.vendor":
		return res.Device.Vendor, true
	case "device.model":
		return res.Device.Model, true
	case "device.type":
		return res.Device.Type, true
	case "cpu.architecture":
		return res.CPU.Architecture, true
	case "engine.name":
		return res.Engine.Name, true
	case "engine.version":
		return res.Engine.Version, true
	case "category":
		return res.Category, true
	case "is_bot":
		return fmt.Sprintf("%t", res.IsBot), true
	case "is_ai_crawler":
		return fmt.Sprintf("%t", res.IsAICrawler), true
	case "is_frozen_ua":
		return fmt.Sprintf("%t", res.IsFrozenUA), true
	case "os.platform":
		return res.OS.Platform, true
	case "cpu.bitness":
		return res.CPU.Bitness, true
	case "device.form_factor":
		return res.Device.FormFactor, true
	case "bot.name":
		if res.Bot == nil {
			return "", true
		}
		return res.Bot.Name, true
	case "bot.category":
		if res.Bot == nil {
			return "", true
		}
		return res.Bot.Category, true
	case "bot.vendor":
		if res.Bot == nil {
			return "", true
		}
		return res.Bot.Vendor, true
	}
	return "", false
}
