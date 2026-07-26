package core

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

// newTestParser is shared with regression_test.go.

// The embedded corrections file must compile cleanly with zero skipped rules:
// a skip in the file we ship means a schema drift bug, not forward compat.
func TestEmbeddedCorrectionsCompile(t *testing.T) {
	cc, err := compileCorrections(defaultCorrections)
	if err != nil {
		t.Fatalf("embedded corrections do not compile: %v", err)
	}
	if cc.skippedRules != 0 {
		t.Fatalf("embedded corrections skipped %d rules — schema drift in our own file", cc.skippedRules)
	}
	if len(cc.rules) == 0 {
		t.Fatal("embedded corrections contain no rules")
	}
}

// Every rule's inline tests must pass through the FULL parse pipeline — the
// same check the runtime updater performs before swapping a downloaded file.
func TestCorrectionsInlineTests(t *testing.T) {
	p := newTestParser(t, 0)
	if err := p.runCorrectionTests(p.corrections.Load()); err != nil {
		t.Fatal(err)
	}
}

// Dead-rule lint: a non-permanent rule whose test UAs parse identically with
// and without the correction layer no longer changes anything — upstream has
// caught up and the rule must be deleted (typically in the weekly sync PR).
func TestCorrectionsNoOpLint(t *testing.T) {
	p := newTestParser(t, 0)
	active := p.corrections.Load()
	empty := &compiledCorrections{}

	for _, rule := range active.rules {
		if rule.permanent {
			continue
		}
		changed := false
		for _, tc := range rule.tests {
			with := p.computeResult(tc.UA, normalizeHeaders(tc.Headers), active)
			without := p.computeResult(tc.UA, normalizeHeaders(tc.Headers), empty)
			if !reflect.DeepEqual(with, without) {
				changed = true
				break
			}
		}
		if !changed {
			t.Errorf("rule %q is a no-op — upstream likely caught up; retire the rule", rule.id)
		}
	}
}

// A corrections swap must bump the generation and purge the cache so stale
// results cannot be served or re-cached (mirror of TestUpdaterSwapAndPurge).
func TestCorrectionsSwapPurgesCache(t *testing.T) {
	p := newTestParser(t, 10)

	ua := "Mozilla/5.0 (TestCorrections) SwapProbe/1.0"
	before := p.Parse(ua, nil)
	if before.Browser.Name == "SwapProbe" {
		t.Fatal("embedded rules unexpectedly know SwapProbe")
	}

	payload := `
schema_version: 1
version: "test-swap"
rules:
  - id: swap-probe
    match:
      ua_contains: "swapprobe/"
      ua_regex: 'SwapProbe/([\d.]+)'
    set:
      browser_name: "SwapProbe"
      browser_version: "$1"
    tests:
      - ua: "Mozilla/5.0 (TestCorrections) SwapProbe/1.0"
        expect:
          browser.name: "SwapProbe"
          browser.version: "1.0"
`
	genBefore := p.gen.Load()
	if err := p.ApplyCorrectionsYAML([]byte(payload)); err != nil {
		t.Fatalf("ApplyCorrectionsYAML: %v", err)
	}
	if p.gen.Load() != genBefore+1 {
		t.Errorf("generation not bumped on corrections swap: %d -> %d", genBefore, p.gen.Load())
	}

	after := p.Parse(ua, nil)
	if after.Browser.Name != "SwapProbe" {
		t.Errorf("cache/rules not refreshed after swap: browser %q (stale cache entry?)", after.Browser.Name)
	}

	version, rules := p.CorrectionsInfo()
	if version != "test-swap" || rules != 1 {
		t.Errorf("CorrectionsInfo = (%q, %d), want (test-swap, 1)", version, rules)
	}
}

// Invalid payloads must be rejected whole, keeping the last good rule set.
func TestCorrectionsRejectsBadPayloads(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"garbage", "!!! not yaml ["},
		{"future schema", "schema_version: 99\nrules: []"},
		{"missing anchor", `
schema_version: 1
rules:
  - id: r
    match: {browser_name: "X"}
    set: {browser_name: "Y"}
    tests: [{ua: "x", expect: {browser.name: "Y"}}]
`},
		{"bad regex", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x", ua_regex: "([unclosed"}
    set: {browser_name: "Y"}
    tests: [{ua: "x", expect: {browser.name: "Y"}}]
`},
		{"capture without regex", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x"}
    set: {browser_name: "$1"}
    tests: [{ua: "x", expect: {browser.name: "x"}}]
`},
		{"capture group out of range", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x", ua_regex: 'x(\d)'}
    set: {browser_name: "cost $5"}
    tests: [{ua: "x9", expect: {browser.name: "cost "}}]
`},
		{"duplicate ids", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x"}
    set: {browser_name: "Y"}
    tests: [{ua: "x", expect: {browser.name: "Y"}}]
  - id: r
    match: {ua_contains: "x"}
    set: {browser_name: "Z"}
    tests: [{ua: "x", expect: {browser.name: "Z"}}]
`},
		{"rule without tests", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x"}
    set: {browser_name: "Y"}
`},
		{"oversized inline-test UA", "schema_version: 1\nrules:\n  - id: r\n    match: {ua_contains: \"x\"}\n    set: {browser_name: \"Y\"}\n    tests: [{ua: \"x" + strings.Repeat("A", maxTestUALen) + "\", expect: {browser.name: \"Y\"}}]\n"},
		{"duplicate set key is an author bug, not a skip", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x"}
    set:
      browser_name: "First"
      browser_name: "Second"
    tests: [{ua: "x", expect: {browser.name: "Second"}}]
`},
		{"type mismatch is an author bug, not a skip", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "x"}
    set: {browser_name: {nested: map}}
    tests: [{ua: "x", expect: {browser.name: "Y"}}]
`},
		{"failing self-test", `
schema_version: 1
rules:
  - id: r
    match: {ua_contains: "zzz-no-such-token"}
    set: {browser_name: "Y"}
    tests: [{ua: "something else entirely", expect: {browser.name: "Y"}}]
`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestParser(t, 0)
			versionBefore, rulesBefore := p.CorrectionsInfo()
			gen := p.gen.Load()

			if err := p.ApplyCorrectionsYAML([]byte(tc.payload)); err == nil {
				t.Fatal("bad payload accepted")
			}
			if p.gen.Load() != gen {
				t.Error("rejected payload must not bump the generation")
			}
			version, rules := p.CorrectionsInfo()
			if version != versionBefore || rules != rulesBefore {
				t.Error("rejected payload must keep the last good rule set")
			}
		})
	}
}

// A rule using match/set keys from a newer engine is skipped (logged), while
// the rest of the file still applies — old binaries degrade gracefully.
func TestCorrectionsUnknownRuleKeysSkipped(t *testing.T) {
	payload := `
schema_version: 1
version: "mixed"
rules:
  - id: from-the-future
    match:
      ua_contains: "x"
      quantum_entanglement: true
    set: {browser_name: "Y"}
    tests: [{ua: "x", expect: {browser.name: "Y"}}]
  - id: understood
    match: {ua_contains: "knowntoken/"}
    set: {browser_name: "Known"}
    tests:
      - ua: "Mozilla/5.0 knowntoken/1.0"
        expect: {browser.name: "Known"}
`
	cc, err := compileCorrections([]byte(payload))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if cc.skippedRules != 1 {
		t.Errorf("skippedRules = %d, want 1", cc.skippedRules)
	}
	if len(cc.rules) != 1 || cc.rules[0].id != "understood" {
		t.Errorf("surviving rules = %+v, want exactly [understood]", cc.rules)
	}
}

// Vendor resolution: fill-gap from model codes (including the CH-model path
// and the Android Build/ extraction), never overwriting a resolved vendor.
func TestVendorPrefixResolution(t *testing.T) {
	p := newTestParser(t, 0)

	t.Run("ua model SM tablet", func(t *testing.T) {
		res := p.Parse("Mozilla/5.0 (Linux; Android 14; SM-X910) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
		if res.Device.Vendor != "Samsung" || res.Device.Model != "SM-X910" {
			t.Errorf("got vendor=%q model=%q, want Samsung/SM-X910", res.Device.Vendor, res.Device.Model)
		}
	})

	t.Run("model extracted from Build token", func(t *testing.T) {
		res := p.Parse("Mozilla/5.0 (Linux; U; Android 13; ru-ru; 2201116SG Build/TKQ1.220829.002) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/112.0.5615.136 Mobile Safari/537.36 XiaoMi/MiuiBrowser/14.28.0-gn", nil)
		if res.Device.Vendor != "Xiaomi" {
			t.Errorf("got vendor=%q, want Xiaomi (model %q)", res.Device.Vendor, res.Device.Model)
		}
	})

	t.Run("CH model resolves vendor", func(t *testing.T) {
		headers := map[string]string{
			"Sec-CH-UA-Platform": `"Android"`,
			"Sec-CH-UA-Model":    `"Pixel 8 Pro"`,
			"Sec-CH-UA-Mobile":   "?1",
		}
		res := p.Parse("Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36", headers)
		if res.Device.Vendor != "Google" || res.Device.Model != "Pixel 8 Pro" {
			t.Errorf("got vendor=%q model=%q, want Google/Pixel 8 Pro", res.Device.Vendor, res.Device.Model)
		}
	})

	t.Run("resolved vendor is never overwritten", func(t *testing.T) {
		res := p.Parse("Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36", nil)
		if res.Device.Vendor != "Samsung" {
			t.Errorf("got vendor=%q, want Samsung (from uap-core, untouched)", res.Device.Vendor)
		}
	})
}

// The X-Requested-With rules must fire on the header, not the UA.
func TestCorrectionsXRequestedWith(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/126.0.0.0 Mobile Safari/537.36"

	plain := p.Parse(ua, nil)
	if plain.Browser.Name == "WeChat" {
		t.Fatal("WeChat detected without the header")
	}

	res := p.Parse(ua, map[string]string{"X-Requested-With": "com.tencent.mm"})
	if res.Browser.Name != "WeChat" || res.Browser.Type != "inapp" {
		t.Errorf("got %q/%q, want WeChat/inapp", res.Browser.Name, res.Browser.Type)
	}
}

// Corrections are terminal: an in-app rule must survive the Client Hints
// brand rewrite (Chromium WebViews send Sec-CH-UA with an "Android WebView"
// brand, which would clobber a pre-CH correction — the reason the layer runs
// after applyClientHints).
func TestCorrectionsSurviveClientHintsBrands(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (Linux; Android 13; 22081212C Build/TKQ1.220829.002; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/116.0.0.0 Mobile Safari/537.36 XWEB/1160065 MMWEBSDK/20231202 MMWEBID/1234 MicroMessenger/8.0.47.2560(0x28002F35) WeChat/arm64"
	headers := map[string]string{
		"Sec-CH-UA":          `"Android WebView";v="116", "Chromium";v="116", "Not)A;Brand";v="24"`,
		"Sec-CH-UA-Mobile":   "?1",
		"Sec-CH-UA-Platform": `"Android"`,
	}

	res := p.Parse(ua, headers)
	if res.Browser.Name != "WeChat" || res.Browser.Type != "inapp" {
		t.Errorf("got %q/%q, want WeChat/inapp (correction must win over CH brands)", res.Browser.Name, res.Browser.Type)
	}
}

// The substring prefilter must keep the layer near-free on unrelated traffic:
// corrections must not change a plain desktop Chrome result at all.
func TestCorrectionsNoEffectOnMainstreamUA(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	with := p.computeResult(ua, nil, p.corrections.Load())
	without := p.computeResult(ua, nil, &compiledCorrections{})
	if !reflect.DeepEqual(with, without) {
		t.Errorf("corrections changed a mainstream UA:\nwith:    %+v\nwithout: %+v", with, without)
	}
}

// Header keys in the cache must include x-requested-with once rules consume
// it — two requests differing only in that header must not collide.
func TestCacheKeyIncludesXRequestedWith(t *testing.T) {
	p := newTestParser(t, 10)
	ua := "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/126.0.0.0 Mobile Safari/537.36"

	first := p.Parse(ua, nil)
	second := p.Parse(ua, map[string]string{"X-Requested-With": "com.tencent.mm"})
	if first.Browser.Name == second.Browser.Name {
		t.Errorf("cache collision: %q == %q despite differing X-Requested-With", first.Browser.Name, second.Browser.Name)
	}
}

// Concurrency gate: many Parse() calls hammer the atomic rule-set pointer and
// the LRU cache while the rule set is hot-swapped underneath them. Run under
// `go test -race` (CI does) — the design relies on atomic.Pointer + the gen
// counter to make this safe with no lock on the parse path.
func TestCorrectionsConcurrentParseAndSwap(t *testing.T) {
	p := newTestParser(t, 256)

	uas := []string{
		"Mozilla/5.0 (Linux; Android 13; 22081212C) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36 MicroMessenger/8.0.47.2560",
		"Mozilla/5.0 (compatible; GPTBot/1.2; +https://openai.com/gptbot)",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; GNU/Linux) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.130 Safari/537.36 Tesla/2024.14.6-x",
	}
	swaps := [][]byte{
		defaultCorrections,
		[]byte("schema_version: 1\nversion: \"empty\"\nrules: []\n"),
	}

	stop := make(chan struct{})
	var swapper sync.WaitGroup
	swapper.Add(1)
	go func() {
		defer swapper.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				_ = p.ApplyCorrectionsYAML(swaps[i%len(swaps)])
			}
		}
	}()

	var parsers sync.WaitGroup
	for w := 0; w < 8; w++ {
		parsers.Add(1)
		go func(seed int) {
			defer parsers.Done()
			for n := 0; n < 5000; n++ {
				res := p.ParseFull(uas[(seed+n)%len(uas)], nil, &Signals{MaxTouchPoints: n % 3})
				if res == nil || res.Category == "" {
					t.Errorf("nil/empty result under concurrency")
					return
				}
			}
		}(w)
	}

	parsers.Wait() // let parsers finish their fixed work...
	close(stop)    // ...then wind down the swapper.
	swapper.Wait()
}

// A rule using a genuinely unknown field (newer engine) is still skipped, not
// rejected — forward compat must survive the stricter decode.
func TestCorrectionsUnknownFieldStillSkips(t *testing.T) {
	payload := `
schema_version: 1
rules:
  - id: future
    match:
      ua_contains: "x"
      quantum_field: true
    set: {browser_name: "Y"}
    tests: [{ua: "x", expect: {browser.name: "Y"}}]
  - id: ok
    match: {ua_contains: "knowntoken/"}
    set: {browser_name: "Known"}
    tests: [{ua: "x knowntoken/1", expect: {browser.name: "Known"}}]
`
	cc, err := compileCorrections([]byte(payload))
	if err != nil {
		t.Fatalf("unknown-field rule must be skipped, not reject the file: %v", err)
	}
	if cc.skippedRules != 1 || len(cc.rules) != 1 || cc.rules[0].id != "ok" {
		t.Errorf("skipped=%d rules=%d, want 1 skipped + [ok]", cc.skippedRules, len(cc.rules))
	}
}

// buildCacheKey must be injective: no two distinct (ua, headers, signals)
// tuples may collide, even with NUL or ':' bytes crafted into values.
func TestCacheKeyInjective(t *testing.T) {
	k := func(ua string, h map[string]string, s *Signals) string { return buildCacheKey(ua, h, s) }
	pairs := [][2]string{
		{k("a", map[string]string{"sec-ch-ua": "X\x00Y"}, nil), k("a\x00X", map[string]string{"sec-ch-ua": "Y"}, nil)},
		{k("1:a", nil, nil), k("", map[string]string{"sec-ch-ua": "a"}, nil)},
		{k("a", map[string]string{"sec-ch-ua": "5:hello"}, nil), k("a", map[string]string{"sec-ch-ua": "", "sec-ch-ua-mobile": "hello"}, nil)},
		{k("a", nil, nil), k("a", nil, &Signals{})},
	}
	for i, p := range pairs {
		if p[0] == p[1] {
			t.Errorf("case %d: cache-key collision between distinct inputs", i)
		}
	}
}

func TestStringListScalarAndSequence(t *testing.T) {
	payload := `
schema_version: 1
rules:
  - id: scalar-anchor
    match: {ua_contains: "single/"}
    set: {browser_name: "Single"}
    tests: [{ua: "x single/1", expect: {browser.name: "Single"}}]
`
	cc, err := compileCorrections([]byte(payload))
	if err != nil {
		t.Fatalf("scalar ua_contains failed to compile: %v", err)
	}
	if !strings.EqualFold(cc.rules[0].anchorsAll[0], "single/") {
		t.Errorf("anchor = %q", cc.rules[0].anchorsAll[0])
	}
}
