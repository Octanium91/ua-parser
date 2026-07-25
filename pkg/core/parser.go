package core

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ua-parser/uap-go/uaparser"
	"gopkg.in/yaml.v3"
)

// The upstream regex database is embedded directly as YAML so the module is
// self-contained on the Go proxy (no build-time generation step required).
// The snapshot is refreshed by .github/workflows/sync-uap-core.yml; the exact
// upstream commit is recorded in resources/UAP_CORE_SHA.
//
//go:embed resources/regexes.yaml
var defaultRegexes []byte

// aiBots lists lowercase UA substrings of known AI-related agents (training
// crawlers, AI-search indexers, and on-demand user fetchers). Sourced from
// vendor docs and the ai-robots-txt project; keep vendor-grouped for reviewable
// diffs. Tags: training | search | user-fetch | agent | other.
var aiBots = []string{
	// OpenAI — https://developers.openai.com/api/docs/bots
	"gptbot",        // training
	"oai-searchbot", // search (ChatGPT search)
	"oai-adsbot",    // other: ads landing-page QA
	"chatgpt-user",  // user-fetch (also agent-mode fetches)

	// Anthropic — https://support.claude.com/en/articles/8896518
	"claudebot",        // training
	"claude-user",      // user-fetch
	"claude-searchbot", // search
	"claude-web",       // legacy pre-2024 crawler
	"anthropic-ai",     // legacy, undocumented

	// Google — https://developers.google.com/search/docs/crawling-indexing/google-common-crawlers
	"googleother",           // training/R&D (covers GoogleOther-Image/-Video)
	"google-cloudvertexbot", // Vertex AI site-owner-requested crawls
	"google-agent",          // agent: Google-hosted AI agents
	"googleagent-",          // agent: legacy GoogleAgent-Mariner / -URLContext
	"gemini-deep-research",  // user-fetch: Gemini Deep Research
	"google-gemininotebook", // user-fetch: NotebookLM
	"google-notebooklm",     // user-fetch: NotebookLM legacy token

	// Meta — https://developers.facebook.com/docs/sharing/webmasters/web-crawlers/
	"meta-externalagent",   // training
	"meta-externalfetcher", // user-fetch
	"meta-webindexer",      // search: Meta AI search index
	"facebookbot",          // legacy

	// Perplexity — https://docs.perplexity.ai/guides/bots
	"perplexitybot",   // search
	"perplexity-user", // user-fetch

	// Apple — https://support.apple.com/en-us/119829
	"applebot-extended", // training (Apple Intelligence opt-out agent)

	// Amazon — https://developer.amazon.com/en/amazonbot
	"amazonbot",      // training/service improvement
	"amzn-searchbot", // search
	"amzn-user",      // user-fetch (Alexa)
	"bedrockbot",     // AWS Bedrock web-crawler data source
	"novaact",        // agent: Amazon Nova Act

	// ByteDance (community-verified)
	"bytespider",   // training
	"tiktokspider", // training

	// Common Crawl
	"ccbot", // training (open corpus widely used for LLMs)

	// Cohere
	"cohere-training-data-crawler", // training
	"cohere-ai",                    // legacy user-fetch

	// Mistral — https://docs.mistral.ai/robots
	"mistralai-user",  // user-fetch (Le Chat)
	"mistralai-index", // search

	// DuckDuckGo — https://duckduckgo.com/duckduckgo-help-pages/results/duckassistbot/
	"duckassistbot", // user-fetch: DuckAssist answers

	// Allen Institute for AI — https://allenai.org/crawler
	"ai2bot", // training (covers Ai2Bot-Dolma)

	// Huawei
	"pangubot", // training (PanGu LLM)

	// Webz.io (ex-Omgili)
	"webzio-extended", // training (crawl data sold for LLM training)
	"omgili",          // training; also covers legacy "omgilibot"

	// Yandex
	"yandexadditional", // training (YandexGPT; covers YandexAdditionalBot)

	// Independent AI data collectors / assistants
	"diffbot",         // AI-powered extraction/knowledge graph
	"imagesiftbot",    // training: image scraping (Hive)
	"timpibot",        // training (Timpi)
	"youbot",          // search+training (You.com)
	"deepseekbot",     // training (community-reported)
	"kimi-user",       // user-fetch (Moonshot Kimi)
	"manus-user",      // agent (Manus)
	"semrushbot-ocob", // Semrush ContentShake AI
}

// botFalsePositiveTokens are real product names that contain a "bot"-like
// substring; they are blanked out of the UA before the generic bot patterns run
// (mirrors uap-core's own "not CUBOT" guard).
var botFalsePositiveTokens = []string{"cubot"}

// cacheKeyHeaders is the canonical list of Client Hints headers consumed by
// applyClientHints. Every consumed header MUST be part of the cache key,
// otherwise requests differing only in that header would collide in the cache.
var cacheKeyHeaders = []string{
	"sec-ch-ua",
	"sec-ch-ua-mobile",
	"sec-ch-ua-platform",
	"sec-ch-ua-platform-version",
	"sec-ch-ua-model",
	"sec-ch-ua-arch",
	"sec-ch-ua-bitness",
	"sec-ch-ua-full-version-list",
	"sec-ch-ua-form-factors",
}

type Parser struct {
	mu     sync.RWMutex
	uap    *uaparser.Parser
	cache  *lru.Cache[string, *Result]
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	// gen increments on every regex hot-swap; Parse skips caching results that
	// were computed against a superseded database (see updateRegexes).
	gen atomic.Uint64
	// lastETag is the ETag of the last downloaded regexes.yaml; accessed only
	// from the single updater goroutine.
	lastETag string
}

func New(cfg Config) (*Parser, error) {
	def := uaparser.RegexDefinitions{}
	if err := yaml.Unmarshal(defaultRegexes, &def); err != nil {
		return nil, err
	}

	uap, err := uaparser.New(uaparser.WithRegexDefinitions(def))
	if err != nil {
		return nil, err
	}

	var cache *lru.Cache[string, *Result]
	if cfg.LRUCacheSize > 0 {
		cache, err = lru.New[string, *Result](cfg.LRUCacheSize)
		if err != nil {
			return nil, fmt.Errorf("failed to create LRU cache: %w", err)
		}
	}

	parentCtx := cfg.Ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)

	p := &Parser{
		uap:    uap,
		cache:  cache,
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	if !cfg.DisableAutoUpdate {
		go p.startUpdater()
	}

	return p, nil
}

func (p *Parser) Close() {
	p.cancel()
}

func (p *Parser) Parse(ua string, headers map[string]string) *Result {
	// Normalize headers once
	normalizedHeaders := make(map[string]string, len(headers))
	for k, v := range headers {
		normalizedHeaders[strings.ToLower(k)] = v
	}

	cacheKey := ""
	if p.cache != nil {
		cacheKey = buildCacheKey(ua, normalizedHeaders)
		if res, ok := p.cache.Get(cacheKey); ok {
			cp := *res // copy-on-return: callers must never share the cached struct
			return &cp
		}
	}

	gen := p.gen.Load()

	p.mu.RLock()
	client := p.uap.Parse(ua)
	p.mu.RUnlock()

	res := &Result{
		UA: ua,
		Browser: BrowserInfo{
			Name:    client.UserAgent.Family,
			Version: joinVersion(client.UserAgent.Major, client.UserAgent.Minor, client.UserAgent.Patch, ""),
			Major:   client.UserAgent.Major,
			Type:    "browser", // Default
		},
		OS: OSInfo{
			Name:    client.Os.Family,
			Version: joinVersion(client.Os.Major, client.Os.Minor, client.Os.Patch, client.Os.PatchMinor),
		},
		Device: DeviceInfo{
			Model:  client.Device.Model,
			Vendor: client.Device.Brand,
			Type:   "desktop", // Default
		},
	}

	// Infer additional info
	p.inferInfo(res)

	// Apply Client Hints (overrides) using already-normalized headers
	p.applyClientHints(res, normalizedHeaders)

	// Post-process category
	switch {
	case res.IsBot:
		res.Category = "bot"
	case res.Device.Type == "mobile" || res.Device.Type == "tablet":
		res.Category = "mobile"
	case res.Device.Type == "tv" || res.Device.Type == "console" ||
		res.Device.Type == "wearable" || res.Device.Type == "xr" ||
		res.Device.Type == "automotive":
		res.Category = res.Device.Type
	default:
		res.Category = "desktop"
	}

	// Skip caching when the regex DB was hot-swapped mid-parse: the result was
	// computed against the old database and must not outlive the purge.
	if p.cache != nil && p.gen.Load() == gen {
		p.cache.Add(cacheKey, res)
	}

	cp := *res // copy-on-return (Result contains only value types)
	return &cp
}

// buildCacheKey joins the UA with every consumed CH header, separated by a byte
// that is illegal inside header values, so crafted values cannot alias keys.
func buildCacheKey(ua string, headers map[string]string) string {
	var b strings.Builder
	b.Grow(len(ua) + 160)
	b.WriteString(ua)
	for _, k := range cacheKeyHeaders {
		b.WriteByte(0)
		b.WriteString(headers[k])
	}
	return b.String()
}

func joinVersion(major, minor, patch, patchMinor string) string {
	parts := []string{}
	if major != "" {
		parts = append(parts, major)
	}
	if minor != "" {
		parts = append(parts, minor)
	}
	if patch != "" {
		parts = append(parts, patch)
	}
	if patchMinor != "" {
		parts = append(parts, patchMinor)
	}
	return strings.Join(parts, ".")
}

func majorOf(version string) string {
	if i := strings.IndexByte(version, '.'); i != -1 {
		return version[:i]
	}
	return version
}

func (p *Parser) inferInfo(res *Result) {
	uaLower := strings.ToLower(res.UA)
	nameLower := strings.ToLower(res.Browser.Name)

	// Bot detection. Known false-positive tokens (e.g. the Cubot phone brand)
	// are blanked before the generic patterns run.
	scan := uaLower
	for _, t := range botFalsePositiveTokens {
		if strings.Contains(scan, t) {
			scan = strings.ReplaceAll(scan, t, " ")
		}
	}
	res.IsBot = isBot(nameLower, scan)
	res.IsAICrawler = isAICrawler(uaLower)
	if res.IsAICrawler {
		// Every AI agent (training crawler, AI-search indexer, or on-demand
		// fetcher) is automated traffic; many of their tokens ("Claude-User",
		// "meta-externalagent") contain no generic bot marker.
		res.IsBot = true
	}

	// Browser Type
	if res.IsBot {
		res.Browser.Type = "bot"
	} else if strings.Contains(uaLower, "email") || strings.Contains(uaLower, "thunderbird") || nameLower == "airmail" {
		res.Browser.Type = "email"
	} else if strings.Contains(uaLower, "library") || strings.Contains(uaLower, "curl") ||
		strings.Contains(uaLower, "wget") || strings.Contains(uaLower, "http-client") ||
		strings.Contains(uaLower, "python-requests") || strings.Contains(uaLower, "python-urllib") ||
		strings.Contains(uaLower, "postmanruntime") || strings.Contains(uaLower, "okhttp") ||
		strings.Contains(uaLower, "axios/") || strings.Contains(uaLower, "libwww") {
		res.Browser.Type = "library"
	}

	// On iOS every browser is WebKit by platform policy; Chromium/Gecko tokens
	// there (CriOS, EdgiOS, FxiOS) name the app, not the engine.
	isIOS := strings.Contains(uaLower, "iphone") || strings.Contains(uaLower, "ipad") ||
		strings.Contains(uaLower, "ipod") || strings.Contains(uaLower, "crios") ||
		strings.Contains(uaLower, "fxios") || strings.Contains(uaLower, "edgios")

	// Engine detection (order matters: check specific engines before generic ones)
	if strings.Contains(uaLower, "edge/") && !strings.Contains(uaLower, "edg/") {
		res.Engine.Name = "EdgeHTML"
	} else if strings.Contains(uaLower, "trident") {
		res.Engine.Name = "Trident"
	} else if strings.Contains(uaLower, "webkit") {
		res.Engine.Name = "WebKit"
		if !isIOS && (strings.Contains(uaLower, "chrome/") || strings.Contains(uaLower, "chromium/") ||
			strings.Contains(uaLower, "edg/") || strings.Contains(uaLower, "opr/") ||
			strings.Contains(uaLower, "samsungbrowser/") || strings.Contains(uaLower, "yabrowser/")) {
			res.Engine.Name = "Blink"
		}
	} else if strings.Contains(uaLower, "gecko") {
		res.Engine.Name = "Gecko"
	} else if strings.Contains(uaLower, "presto") {
		res.Engine.Name = "Presto"
	}

	// Engine Version. For Blink the true engine version is the "Chrome/x.y.z.w"
	// token, which every Chromium-based UA carries (Edge/Opera/Samsung ship
	// their own product version elsewhere).
	if res.Engine.Name == "Blink" {
		if v := extractVersionAfter(uaLower, "chrome/"); v != "" {
			res.Engine.Version = v
		} else {
			res.Engine.Version = res.Browser.Version
		}
	} else if res.Engine.Name != "" {
		res.Engine.Version = extractEngineVersion(res.Engine.Name, uaLower)
	}

	// Device Type. Wearables are checked before TVs: Tizen/webOS run on both
	// watches and TVs, so bare platform tokens must not force "tv".
	if strings.Contains(uaLower, "watch") {
		res.Device.Type = "wearable"
	} else if strings.Contains(uaLower, "smart-tv") || strings.Contains(uaLower, "smarttv") ||
		strings.Contains(uaLower, "appletv") || strings.Contains(uaLower, "roku") ||
		strings.Contains(uaLower, "crkey") || strings.Contains(uaLower, "firetv") ||
		strings.Contains(uaLower, "googletv") || strings.Contains(uaLower, "hbbtv") ||
		strings.Contains(uaLower, "web0s") ||
		(strings.Contains(uaLower, "tizen") && strings.Contains(uaLower, "tv")) ||
		(strings.Contains(uaLower, "webos") && strings.Contains(uaLower, "tv")) {
		res.Device.Type = "tv"
	} else if strings.Contains(uaLower, "playstation") || strings.Contains(uaLower, "xbox") ||
		strings.Contains(uaLower, "nintendo") {
		res.Device.Type = "console"
	} else if strings.Contains(uaLower, "iphone") || strings.Contains(uaLower, "ipod") {
		res.Device.Type = "mobile"
	} else if strings.Contains(uaLower, "ipad") {
		res.Device.Type = "tablet"
	} else if strings.Contains(uaLower, "android") {
		if strings.Contains(uaLower, "mobi") {
			res.Device.Type = "mobile"
		} else {
			res.Device.Type = "tablet"
		}
	} else if strings.Contains(uaLower, "mobi") {
		res.Device.Type = "mobile"
	}

	// The reduced Chromium UA freezes the Android model to the placeholder "K";
	// it is never a real device model (the CH model header carries the truth).
	if res.Device.Model == "K" && strings.Contains(uaLower, "android 10; k") {
		res.Device.Model = ""
	}

	// CPU architecture (best effort from UA)
	if strings.Contains(uaLower, "x86_64") || strings.Contains(uaLower, "amd64") || strings.Contains(uaLower, "win64") || strings.Contains(uaLower, "x64") {
		res.CPU.Architecture = "amd64"
	} else if strings.Contains(uaLower, "arm64") || strings.Contains(uaLower, "aarch64") {
		res.CPU.Architecture = "arm64"
	} else if strings.Contains(uaLower, "i686") || strings.Contains(uaLower, "i386") {
		res.CPU.Architecture = "x86"
	}
}

func isBot(nameLower, uaLower string) bool {
	// Check parsed browser name (most reliable, already isolated by uap-go)
	if strings.Contains(nameLower, "bot") || strings.Contains(nameLower, "crawler") || strings.Contains(nameLower, "spider") || strings.Contains(nameLower, "scrap") {
		return true
	}
	// Check UA string for bot patterns: word followed by non-letter or end-of-string.
	// Catches "googlebot/2.1", "my-bot", "bot" standalone, rejects "bottle", "bottom".
	if containsBotPattern(uaLower, "bot") || containsBotPattern(uaLower, "crawler") || containsBotPattern(uaLower, "spider") || strings.Contains(uaLower, "google-extended") {
		return true
	}
	return false
}

func isAICrawler(uaLower string) bool {
	for _, bot := range aiBots {
		if strings.Contains(uaLower, bot) {
			return true
		}
	}
	return false
}

// containsBotPattern checks if word appears in s followed by a non-letter char or end-of-string.
// This catches "googlebot/2.1", "my-bot", "bot" standalone, while rejecting "bottle", "bottom".
// Known false-positive product names are handled upstream via botFalsePositiveTokens.
func containsBotPattern(s, word string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], word)
		if i == -1 {
			return false
		}
		absIdx := idx + i
		rightEnd := absIdx + len(word)
		if rightEnd == len(s) || !isLetter(s[rightEnd]) {
			return true
		}
		idx = absIdx + 1
	}
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// chBrand is one entry of a Sec-CH-UA / Sec-CH-UA-Full-Version-List header.
type chBrand struct {
	name    string
	version string
}

func (p *Parser) applyClientHints(res *Result, headers map[string]string) {
	if len(headers) == 0 {
		return
	}

	// Headers are already normalized (lowercased keys) by Parse()
	platform := cleanHeader(headers["sec-ch-ua-platform"])
	platformVer := cleanHeader(headers["sec-ch-ua-platform-version"])
	model := cleanHeader(headers["sec-ch-ua-model"])
	arch := cleanHeader(headers["sec-ch-ua-arch"])
	bitness := cleanHeader(headers["sec-ch-ua-bitness"])
	mobile := cleanHeader(headers["sec-ch-ua-mobile"])
	fullVersionList := headers["sec-ch-ua-full-version-list"]
	lowEntropyBrands := headers["sec-ch-ua"]
	formFactors := headers["sec-ch-ua-form-factors"]

	// --- Platform / OS ---
	// CH values are normalized to uap-core's canonical family names so the same
	// OS reports the same name whether or not hints were present.
	switch platform {
	case "", "Unknown":
		// No reliable platform info — keep the UA-derived values.
	case "Windows":
		res.OS.Name = "Windows"
		if platformVer != "" {
			res.OS.Version = mapWindowsVersion(platformVer)
		}
	case "macOS":
		res.OS.Name = "Mac OS X" // canonical uap-core family
		if platformVer != "" {
			// The UA token is frozen at 10.15.7; the hint carries the real version.
			res.OS.Version = platformVer
		}
	case "Linux":
		// platform-version is empty on Linux by spec, and the UA-derived name
		// may be a specific distro (Ubuntu, Fedora) — keep the UA data.
	default: // Android, iOS, Chrome OS, Chromium OS, Fuchsia, ...
		name := platform
		if name == "Chromium OS" {
			name = "Chrome OS"
		}
		res.OS.Name = name
		if platformVer != "" {
			res.OS.Version = platformVer
		}
	}

	// --- Browser brand/version ---
	// Prefer the high-entropy full-version-list; fall back to the low-entropy
	// sec-ch-ua header (sent by default on every Chromium request).
	brands := parseBrandList(fullVersionList)
	fromFullList := len(brands) > 0
	if !fromFullList {
		brands = parseBrandList(lowEntropyBrands)
	}

	if len(brands) > 0 {
		if matched, ok := matchBrand(brands, res.Browser.Name); ok {
			applyBrandVersion(res, matched.version, fromFullList)
			if renameToBrand(matched.name, res.Browser.Name) {
				res.Browser.Name = matched.name
			}
		} else if specific, ok := pickSpecificBrand(brands); ok {
			// The UA-derived family matches none of the declared brands: the
			// brands header wins (e.g. Brave or Opera GX masquerading as Chrome
			// in the UA string).
			res.Browser.Name = specific.name
			applyBrandVersion(res, specific.version, fromFullList)
		}

		// The "Chromium" entry carries the true Blink version regardless of
		// which brand won above.
		if res.Engine.Name == "Blink" && fromFullList {
			for _, b := range brands {
				if strings.EqualFold(b.name, "Chromium") && b.version != "" {
					res.Engine.Version = b.version
					break
				}
			}
		}
	}

	// --- Device ---
	if model != "" {
		res.Device.Model = model
	}

	if arch != "" {
		res.CPU.Architecture = normalizeArch(arch, bitness, res.CPU.Architecture)
	}

	if mobile == "?1" {
		res.Device.Type = "mobile"
	}
	// "?0" is deliberately not acted on: tablets and desktop-mode phones
	// legitimately send ?0 with non-desktop UAs.

	// Form-Factors (high entropy, Chrome 124+) is the most specific device
	// signal and wins over the binary mobile hint.
	if formFactors != "" {
		ff := strings.ToLower(formFactors)
		switch {
		case strings.Contains(ff, "watch"):
			res.Device.Type = "wearable"
		case strings.Contains(ff, "xr"):
			res.Device.Type = "xr"
		case strings.Contains(ff, "automotive"):
			res.Device.Type = "automotive"
		case strings.Contains(ff, "tablet"):
			res.Device.Type = "tablet"
		case strings.Contains(ff, "eink"):
			// EInk describes the display, not the device class — keep the type.
		case strings.Contains(ff, "mobile"):
			res.Device.Type = "mobile"
		case strings.Contains(ff, "desktop"):
			res.Device.Type = "desktop"
		}
	}
}

// parseBrandList parses a Sec-CH-UA / Sec-CH-UA-Full-Version-List value into
// brand entries, skipping GREASE placeholders. Quotes are respected, so grease
// brands containing ';' or ',' inside quotes do not break the split.
func parseBrandList(header string) []chBrand {
	if header == "" {
		return nil
	}
	var out []chBrand
	for _, part := range splitQuoted(header, ',') {
		segs := splitQuoted(part, ';')
		if len(segs) < 2 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(segs[0]), `"`)
		if name == "" || isGreaseBrand(name) {
			continue
		}
		version := ""
		for _, seg := range segs[1:] {
			seg = strings.TrimSpace(seg)
			if strings.HasPrefix(seg, "v=") {
				version = strings.Trim(seg[2:], `"`)
				break
			}
		}
		out = append(out, chBrand{name: name, version: version})
	}
	return out
}

// splitQuoted splits s on sep, ignoring separators inside double quotes.
func splitQuoted(s string, sep byte) []string {
	var parts []string
	inQuotes := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuotes = !inQuotes
		case sep:
			if !inQuotes {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// isGreaseBrand reports whether a brand is a GREASE placeholder such as
// "Not;A=Brand", "Not_A Brand", "Not/A)Brand" — letters-only they all
// normalize to "notabrand".
func isGreaseBrand(name string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return strings.Contains(b.String(), "notabrand")
}

// chBrandAliases maps a canonical (lowercased, mobile-suffix-stripped) uap-core
// family to the CH brand strings that identify the same browser.
var chBrandAliases = map[string][]string{
	"chrome":           {"google chrome", "chromium"},
	"chromium":         {"chromium", "google chrome"},
	"edge":             {"microsoft edge", "edge"},
	"opera":            {"opera", "opera gx", "operagx", "opera air"},
	"yandex browser":   {"yabrowser", "yandex browser"},
	"samsung internet": {"samsung internet"},
	"brave":            {"brave"},
	"vivaldi":          {"vivaldi"},
}

// genericChromiumBrands never rename the uap family (they are corporate
// umbrella names, not more-specific products).
var genericChromiumBrands = map[string]bool{
	"google chrome":    true,
	"chromium":         true,
	"microsoft edge":   true,
	"edge":             true,
	"opera":            true,
	"yabrowser":        true,
	"yandex browser":   true,
	"samsung internet": true,
	"brave":            true,
	"vivaldi":          true,
}

// canonicalFamily lowercases a uap-core family and strips mobile suffixes so
// "Chrome Mobile WebView" matches the same brands as "Chrome".
func canonicalFamily(family string) string {
	f := strings.ToLower(family)
	for _, suffix := range []string{" mobile webview", " mobile ios", " mobile"} {
		f = strings.TrimSuffix(f, suffix)
	}
	return f
}

// matchBrand finds the brand entry corresponding to the UA-derived family.
// A bare "Chromium" entry only matches Chrome-family UAs when no more specific
// brand is present (otherwise Brave — which declares only Brave+Chromium —
// would be swallowed by the Chromium alias).
func matchBrand(brands []chBrand, family string) (chBrand, bool) {
	fam := canonicalFamily(family)
	want := chBrandAliases[fam]
	if want == nil {
		want = []string{fam}
	}

	var chromiumEntry *chBrand
	for i := range brands {
		bl := strings.ToLower(brands[i].name)
		if bl == "chromium" {
			chromiumEntry = &brands[i]
			continue // handled as a fallback below
		}
		for _, w := range want {
			if bl == w {
				return brands[i], true
			}
		}
	}

	// Chromium-only fallback for Chrome-family UAs (e.g. headless builds that
	// declare no branded entry).
	if chromiumEntry != nil && (fam == "chrome" || fam == "chromium") {
		if _, ok := pickSpecificBrand(brands); !ok {
			return *chromiumEntry, true
		}
	}
	return chBrand{}, false
}

// pickSpecificBrand returns the first brand that is neither GREASE (already
// filtered) nor the generic "Chromium" umbrella.
func pickSpecificBrand(brands []chBrand) (chBrand, bool) {
	for _, b := range brands {
		if !strings.EqualFold(b.name, "Chromium") {
			return b, true
		}
	}
	return chBrand{}, false
}

// renameToBrand reports whether the matched CH brand is a more specific product
// name than the uap family (e.g. "Opera GX" vs "Opera") and should replace it.
func renameToBrand(brand, family string) bool {
	bl := strings.ToLower(brand)
	if genericChromiumBrands[bl] {
		return false
	}
	return !strings.EqualFold(brand, family)
}

// applyBrandVersion sets the browser version from a CH brand entry. Versions
// from the full-version-list are complete; low-entropy sec-ch-ua carries only
// the significant (major) version, so it only corrects a mismatching major.
func applyBrandVersion(res *Result, version string, fromFullList bool) {
	if version == "" {
		return
	}
	if fromFullList {
		res.Browser.Version = version
	} else if majorOf(res.Browser.Version) != version {
		res.Browser.Version = version
	}
	res.Browser.Major = majorOf(res.Browser.Version)
}

// normalizeArch maps Sec-CH-UA-Arch / -Bitness pairs onto the parser's
// architecture vocabulary (amd64 / arm64 / x86 / arm). Unknown or ambiguous
// combinations keep the UA-derived value.
func normalizeArch(arch, bitness, fallback string) string {
	switch strings.ToLower(arch) {
	case "x86":
		switch bitness {
		case "64":
			return "amd64"
		case "32":
			return "x86"
		}
	case "arm":
		switch bitness {
		case "64":
			return "arm64"
		case "32":
			return "arm"
		}
	case "arm64", "aarch64":
		return "arm64"
	case "x64", "amd64", "x86_64":
		return "amd64"
	}
	return fallback
}

func cleanHeader(h string) string {
	return strings.Trim(h, `" `)
}

// mapWindowsVersion translates Sec-CH-UA-Platform-Version into a Windows
// marketing version, per Microsoft's guidance:
// https://learn.microsoft.com/en-us/microsoft-edge/web-platform/how-to-detect-win11
func mapWindowsVersion(ver string) string {
	parts := strings.Split(ver, ".")
	if len(parts) == 0 {
		return ver
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return ver
	}
	if major >= 13 {
		return "11"
	}
	if major > 0 {
		return "10"
	}
	// major == 0: pre-Win10 systems enumerate as 0.1 / 0.2 / 0.3
	if len(parts) >= 2 {
		switch parts[1] {
		case "1":
			return "7"
		case "2":
			return "8"
		case "3":
			return "8.1"
		}
	}
	return ver
}

// extractVersionAfter returns the version token following prefix in ua
// (terminated by space or ')').
func extractVersionAfter(uaLower, prefix string) string {
	idx := strings.Index(uaLower, prefix)
	if idx == -1 {
		return ""
	}
	version := uaLower[idx+len(prefix):]
	if end := strings.IndexAny(version, " );"); end != -1 {
		version = version[:end]
	}
	return version
}

func extractEngineVersion(engineName, uaLower string) string {
	switch engineName {
	case "WebKit":
		return extractVersionAfter(uaLower, "applewebkit/")
	case "Gecko":
		if idx := strings.Index(uaLower, "rv:"); idx != -1 {
			version := uaLower[idx+3:]
			if end := strings.IndexAny(version, ") "); end != -1 {
				return version[:end]
			}
			return version
		}
	case "EdgeHTML":
		return extractVersionAfter(uaLower, "edge/")
	}
	return ""
}
