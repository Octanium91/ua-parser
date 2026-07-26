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

// botIdentity describes a known automated agent: the lowercase UA substring
// that identifies it, the operating vendor, and a category. AI categories:
// training | search | user-fetch | agent | other; classic categories:
// search-crawler | seo | monitoring | social-preview.
type botIdentity struct {
	token    string
	vendor   string
	category string
}

// aiBots lists known AI-related agents (training crawlers, AI-search indexers,
// and on-demand user fetchers). Sourced from vendor docs and the ai-robots-txt
// project; keep vendor-grouped for reviewable diffs.
var aiBots = []botIdentity{
	// OpenAI — https://developers.openai.com/api/docs/bots
	{"gptbot", "OpenAI", "training"},
	{"oai-searchbot", "OpenAI", "search"}, // ChatGPT search
	{"oai-adsbot", "OpenAI", "other"},     // ads landing-page QA
	{"chatgpt-user", "OpenAI", "user-fetch"},

	// Anthropic — https://support.claude.com/en/articles/8896518
	{"claudebot", "Anthropic", "training"},
	{"claude-user", "Anthropic", "user-fetch"},
	{"claude-searchbot", "Anthropic", "search"},
	{"claude-web", "Anthropic", "other"},   // legacy pre-2024 crawler
	{"anthropic-ai", "Anthropic", "other"}, // legacy, undocumented

	// Google — https://developers.google.com/search/docs/crawling-indexing/google-common-crawlers
	{"googleother", "Google", "training"},        // covers GoogleOther-Image/-Video
	{"google-cloudvertexbot", "Google", "other"}, // Vertex AI site-owner-requested crawls
	{"google-agent", "Google", "agent"},          // Google-hosted AI agents
	{"googleagent-", "Google", "agent"},          // legacy GoogleAgent-Mariner / -URLContext
	{"gemini-deep-research", "Google", "user-fetch"},
	{"google-gemininotebook", "Google", "user-fetch"}, // NotebookLM
	{"google-notebooklm", "Google", "user-fetch"},     // NotebookLM legacy token

	// Meta — https://developers.facebook.com/docs/sharing/webmasters/web-crawlers/
	{"meta-externalagent", "Meta", "training"},
	{"meta-externalfetcher", "Meta", "user-fetch"},
	{"meta-webindexer", "Meta", "search"}, // Meta AI search index
	{"facebookbot", "Meta", "other"},      // legacy

	// Perplexity — https://docs.perplexity.ai/guides/bots
	{"perplexitybot", "Perplexity", "search"},
	{"perplexity-user", "Perplexity", "user-fetch"},

	// Apple — https://support.apple.com/en-us/119829
	{"applebot-extended", "Apple", "training"}, // Apple Intelligence opt-out agent

	// Amazon — https://developer.amazon.com/en/amazonbot
	{"amazonbot", "Amazon", "training"}, // training/service improvement
	{"amzn-searchbot", "Amazon", "search"},
	{"amzn-user", "Amazon", "user-fetch"}, // Alexa
	{"bedrockbot", "Amazon", "training"},  // AWS Bedrock web-crawler data source
	{"novaact", "Amazon", "agent"},        // Amazon Nova Act

	// ByteDance (community-verified)
	{"bytespider", "ByteDance", "training"},
	{"tiktokspider", "ByteDance", "training"},

	// Common Crawl
	{"ccbot", "Common Crawl", "training"}, // open corpus widely used for LLMs

	// Cohere
	{"cohere-training-data-crawler", "Cohere", "training"},
	{"cohere-ai", "Cohere", "user-fetch"}, // legacy

	// Mistral — https://docs.mistral.ai/robots
	{"mistralai-user", "Mistral", "user-fetch"}, // Le Chat
	{"mistralai-index", "Mistral", "search"},

	// DuckDuckGo — https://duckduckgo.com/duckduckgo-help-pages/results/duckassistbot/
	{"duckassistbot", "DuckDuckGo", "user-fetch"}, // DuckAssist answers

	// Allen Institute for AI — https://allenai.org/crawler
	{"ai2bot", "Allen Institute for AI", "training"}, // covers Ai2Bot-Dolma

	// Huawei
	{"pangubot", "Huawei", "training"}, // PanGu LLM

	// Webz.io (ex-Omgili)
	{"webzio-extended", "Webz.io", "training"}, // crawl data sold for LLM training
	{"omgili", "Webz.io", "training"},          // also covers legacy "omgilibot"

	// Yandex
	{"yandexadditional", "Yandex", "training"}, // YandexGPT; covers YandexAdditionalBot

	// Independent AI data collectors / assistants
	{"diffbot", "Diffbot", "other"}, // AI-powered extraction/knowledge graph
	{"imagesiftbot", "Hive", "training"},
	{"timpibot", "Timpi", "training"},
	{"youbot", "You.com", "search"},
	{"deepseekbot", "DeepSeek", "training"}, // community-reported
	{"kimi-user", "Moonshot", "user-fetch"},
	{"manus-user", "Manus", "agent"},
	{"semrushbot-ocob", "Semrush", "other"}, // Semrush ContentShake AI
}

// classicBots classifies well-known non-AI automation for the bot object.
// Checked only when no aiBots token matched (so semrushbot-ocob keeps its AI
// identity while plain semrushbot classifies as seo).
var classicBots = []botIdentity{
	{"googlebot", "Google", "search-crawler"},
	{"bingbot", "Microsoft", "search-crawler"},
	{"yandexbot", "Yandex", "search-crawler"},
	{"duckduckbot", "DuckDuckGo", "search-crawler"},
	{"baiduspider", "Baidu", "search-crawler"},
	{"applebot", "Apple", "search-crawler"},
	{"petalbot", "Huawei", "search-crawler"},
	{"seznambot", "Seznam", "search-crawler"},

	{"ahrefsbot", "Ahrefs", "seo"},
	{"semrushbot", "Semrush", "seo"},
	{"mj12bot", "Majestic", "seo"},
	{"dotbot", "Moz", "seo"},
	{"rogerbot", "Moz", "seo"},
	{"screaming frog", "Screaming Frog", "seo"},

	{"uptimerobot", "UptimeRobot", "monitoring"},
	{"pingdom", "Pingdom", "monitoring"},
	{"statuscake", "StatusCake", "monitoring"},

	{"facebookexternalhit", "Meta", "social-preview"},
	{"twitterbot", "X", "social-preview"},
	{"slackbot", "Slack", "social-preview"},
	{"discordbot", "Discord", "social-preview"},
	{"telegrambot", "Telegram", "social-preview"},
	{"whatsapp", "Meta", "social-preview"},
	{"linkedinbot", "LinkedIn", "social-preview"},
}

// botFalsePositiveTokens are real product names that contain a "bot"-like
// substring; they are blanked out of the UA before the generic bot patterns run
// (mirrors uap-core's own "not CUBOT" guard).
var botFalsePositiveTokens = []string{"cubot"}

// cacheKeyHeaders is the canonical list of headers consumed by the pipeline
// (applyClientHints + the correction layer's x_requested_with match). Every
// consumed header MUST be part of the cache key, otherwise requests differing
// only in that header would collide in the cache.
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
	"x-requested-with",
}

type Parser struct {
	mu     sync.RWMutex
	uap    *uaparser.Parser
	cache  *lru.Cache[string, *Result]
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	// gen increments on every resource hot-swap (regexes or corrections);
	// Parse skips caching results that were computed against a superseded
	// database (see updateRegexes / ApplyCorrectionsYAML).
	gen atomic.Uint64
	// corrections is the compiled correction-layer rule set (immutable once
	// built; swapped atomically so Parse never takes a lock for it).
	corrections atomic.Pointer[compiledCorrections]
	// lastETag / lastCorrectionsETag are the ETags of the last downloaded
	// regexes.yaml / corrections.yaml; accessed only from the single updater
	// goroutine.
	lastETag            string
	lastCorrectionsETag string
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

	// The embedded corrections file is CI-validated; a compile failure here is
	// a build-time bug, same posture as the embedded regexes above.
	corrections, err := compileCorrections(defaultCorrections)
	if err != nil {
		return nil, fmt.Errorf("embedded corrections: %w", err)
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
	p.corrections.Store(corrections)

	// DisableAutoUpdate is the master network switch: when set, no background
	// fetching happens at all. DisableCorrectionsUpdate is a sub-switch that
	// suppresses only corrections while regex auto-update still runs.
	if !cfg.DisableAutoUpdate {
		go p.startUpdater()
	}

	return p, nil
}

func (p *Parser) Close() {
	p.cancel()
}

func (p *Parser) Parse(ua string, headers map[string]string) *Result {
	return p.ParseFull(ua, headers, nil)
}

// ParseFull is Parse plus an optional browser-signals block (see Signals).
func (p *Parser) ParseFull(ua string, headers map[string]string, signals *Signals) *Result {
	normalizedHeaders := normalizeHeaders(headers)

	cacheKey := ""
	if p.cache != nil {
		cacheKey = buildCacheKey(ua, normalizedHeaders, signals)
		if res, ok := p.cache.Get(cacheKey); ok {
			// copy-on-return: callers must never share the cached struct
			return copyResult(res)
		}
	}

	gen := p.gen.Load()

	res := p.computeResultFull(ua, normalizedHeaders, signals, p.corrections.Load())

	// Skip caching when the regex DB or the correction set was hot-swapped
	// mid-parse: the result was computed against the old resources and must
	// not outlive the purge.
	if p.cache != nil && p.gen.Load() == gen {
		p.cache.Add(cacheKey, res)
	}

	return copyResult(res)
}

// normalizeHeaders lowercases header names once per parse.
func normalizeHeaders(headers map[string]string) map[string]string {
	normalized := make(map[string]string, len(headers))
	for k, v := range headers {
		normalized[strings.ToLower(k)] = v
	}
	return normalized
}

// computeResult runs the full detection pipeline against an explicit
// correction set: uap-core regexes → inference → Client Hints → signals →
// corrections (terminal) → category. Cache-free, so the corrections
// self-test can run candidate rule sets through it without polluting state.
func (p *Parser) computeResult(ua string, normalizedHeaders map[string]string, cc *compiledCorrections) *Result {
	return p.computeResultFull(ua, normalizedHeaders, nil, cc)
}

func (p *Parser) computeResultFull(ua string, normalizedHeaders map[string]string, signals *Signals, cc *compiledCorrections) *Result {
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

	uaLower := strings.ToLower(ua)

	// Infer additional info
	p.inferInfo(res, uaLower)

	// Apply Client Hints (overrides) using already-normalized headers
	p.applyClientHints(res, normalizedHeaders)

	// Browser-side signals: weaker than Client Hints, stronger than a frozen
	// UA string. Runs after CH so genuine CH data is never overridden.
	applySignals(res, uaLower, normalizedHeaders, signals)

	// Correction layer: terminal overrides for known detection gaps. Runs
	// after Client Hints (rules may match on the final CH-corrected state and
	// must survive CH brand rewrites, e.g. in-app browsers inside a WebView);
	// before the category switch so overridden device types feed it.
	categoryOverride := applyCorrections(res, ua, uaLower, normalizedHeaders, cc)

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
	if categoryOverride != "" {
		res.Category = categoryOverride
	}

	// Derived fields computed from the FINAL state (corrections included).
	res.OS.Platform = platformOf(res.OS.Name)
	if res.Device.FormFactor == "" && !res.IsBot {
		res.Device.FormFactor = formFactorFromType(res.Device.Type)
	}
	res.IsFrozenUA = detectFrozenUA(uaLower)
	if res.Bot != nil {
		// Keep the bot object in sync when a correction renamed the browser.
		res.Bot.Name = res.Browser.Name
	}

	return res
}

// copyResult returns an independent copy: Result contains two pointer fields
// (Bot, GPU) that must not be shared between the cache and callers.
func copyResult(res *Result) *Result {
	cp := *res
	if res.Bot != nil {
		b := *res.Bot
		cp.Bot = &b
	}
	if res.GPU != nil {
		g := *res.GPU
		cp.GPU = &g
	}
	return &cp
}

// buildCacheKey joins the UA with every consumed CH header and every consumed
// signal field. Each field is length-prefixed ("<len>:<value>") so the
// encoding is injective regardless of field contents — values arrive from
// JSON/FFI and may contain any byte (including NUL), so a plain separator
// could otherwise let one field's bytes masquerade as another's boundary.
// deviceMemory/hardwareConcurrency are NOT consumed by the pipeline and are
// deliberately excluded.
func buildCacheKey(ua string, headers map[string]string, signals *Signals) string {
	var b strings.Builder
	b.Grow(len(ua) + 220)
	writeField := func(s string) {
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
	}
	writeField(ua)
	for _, k := range cacheKeyHeaders {
		writeField(headers[k])
	}
	if signals != nil {
		b.WriteByte('S') // distinguishes "no signals" from all-zero signals
		writeField(strconv.Itoa(signals.MaxTouchPoints))
		writeField(signals.Platform)
		writeField(signals.WebGLVendor)
		writeField(signals.WebGLRenderer)
		if signals.Screen != nil {
			writeField(strconv.Itoa(signals.Screen.W))
			writeField(strconv.Itoa(signals.Screen.H))
		}
	}
	return b.String()
}

// applySignals folds browser-side evidence into the result. Every rule here
// is a verified, single-purpose inference — no scoring, no guessing:
//
//  1. iPad unmask: iPadOS Safari in desktop mode masquerades as an Intel Mac,
//     but real Macs report ≤1 touch point. Safari sends no Client Hints, so
//     this signal is the only correction path.
//  2. Apple Silicon: the frozen Mac UA always claims Intel; a WebGL renderer
//     naming an Apple M-series GPU proves arm64. Chromium-only fallback,
//     honored only when Sec-CH-UA-Arch did not already answer.
//  3. GPU passthrough: expose the renderer for consumers (Android SoC tier).
//  4. Tablet assist: a Linux-desktop UA with a multi-touch screen is a
//     desktop-mode Android tablet, not a desktop (Windows is excluded —
//     touch laptops are still desktops).
func applySignals(res *Result, uaLower string, headers map[string]string, signals *Signals) {
	if signals == nil {
		return
	}

	if signals.WebGLRenderer != "" || signals.WebGLVendor != "" {
		res.GPU = &GPUInfo{Vendor: signals.WebGLVendor, Renderer: signals.WebGLRenderer}
	}

	if signals.MaxTouchPoints > 1 && strings.Contains(uaLower, "macintosh") {
		res.OS.Name = "iPadOS"
		res.OS.Version = "" // the Mac UA token carries no real iPadOS version
		res.Device.Vendor = "Apple"
		res.Device.Model = "iPad"
		res.Device.Type = "tablet"
	} else if signals.MaxTouchPoints > 1 && res.Device.Type == "desktop" &&
		platformOf(res.OS.Name) == "linux" {
		res.Device.Type = "tablet"
	}

	// Gate on the PARSED OS family, not a raw-UA substring: iPhone/iPad UAs
	// also contain "like Mac OS X", and this rule is only meaningful for a
	// desktop Mac whose frozen UA hides Apple Silicon. CH arch, when present,
	// already answered and wins.
	if res.OS.Name == "Mac OS X" && headers["sec-ch-ua-arch"] == "" &&
		strings.Contains(signals.WebGLRenderer, "Apple M") {
		res.CPU.Architecture = "arm64"
		res.CPU.Bitness = "64"
	}
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

func (p *Parser) inferInfo(res *Result, uaLower string) {
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

	if ai := matchBotIdentity(uaLower, aiBots); ai != nil {
		// Every AI agent (training crawler, AI-search indexer, or on-demand
		// fetcher) is automated traffic; many of their tokens ("Claude-User",
		// "meta-externalagent") contain no generic bot marker.
		res.IsAICrawler = true
		res.IsBot = true

		// uap-core has no patterns for most AI agents, so its generic
		// fallbacks produce junk families ("Other", "crawler", or a URL
		// fragment like "com/bot"). Synthesize the canonical identity from
		// the UA's own token instead.
		if isJunkBotName(res.Browser.Name) {
			if name, version := extractAgentIdentity(res.UA, uaLower, ai.token); name != "" {
				res.Browser.Name = name
				if version != "" {
					res.Browser.Version = version
					res.Browser.Major = majorOf(version)
				}
			}
		}
		res.Bot = &BotInfo{Name: res.Browser.Name, Category: ai.category, Vendor: ai.vendor}
	} else if res.IsBot {
		bot := &BotInfo{Name: res.Browser.Name, Category: "other"}
		if classic := matchBotIdentity(uaLower, classicBots); classic != nil {
			bot.Category = classic.category
			bot.Vendor = classic.vendor
		}
		res.Bot = bot
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
		res.CPU.Bitness = "64"
	} else if strings.Contains(uaLower, "arm64") || strings.Contains(uaLower, "aarch64") {
		res.CPU.Architecture = "arm64"
		res.CPU.Bitness = "64"
	} else if strings.Contains(uaLower, "i686") || strings.Contains(uaLower, "i386") {
		res.CPU.Architecture = "x86"
		res.CPU.Bitness = "32"
	}
}

// platformOf maps a marketing OS name onto the canonical os.platform key.
func platformOf(osName string) string {
	n := strings.ToLower(osName)
	switch {
	case strings.Contains(n, "windows"):
		return "windows"
	case strings.Contains(n, "mac os"), strings.Contains(n, "macos"):
		return "macos"
	case n == "ios" || strings.HasPrefix(n, "ios ") || strings.Contains(n, "ipados"):
		return "ios"
	case strings.Contains(n, "android"):
		return "android"
	case strings.Contains(n, "chrome os"), strings.Contains(n, "chromium os"):
		return "chromeos"
	case strings.Contains(n, "tizen"):
		return "tizen"
	case strings.Contains(n, "playstation"):
		return "playstation"
	case isLinuxFamily(n):
		return "linux"
	default:
		return "other"
	}
}

var linuxFamilies = []string{
	"linux", "ubuntu", "fedora", "debian", "arch", "mint", "suse",
	"centos", "red hat", "gentoo", "slackware", "kali", "manjaro", "freebsd",
}

func isLinuxFamily(nameLower string) bool {
	for _, f := range linuxFamilies {
		if strings.Contains(nameLower, f) {
			return true
		}
	}
	return false
}

// formFactorFromType derives device.form_factor when Sec-CH-UA-Form-Factors
// was absent; wearable maps to the CH vocabulary's "watch".
func formFactorFromType(deviceType string) string {
	if deviceType == "wearable" {
		return "watch"
	}
	return deviceType
}

// detectFrozenUA reports UA strings that are frozen/reduced templates: the
// Chromium reduced UA (version frozen to N.0.0.0, Android model to "K"), and
// the Mac OS X token capped at 10_15_7 (real Catalina tops out at Safari 15).
func detectFrozenUA(uaLower string) bool {
	if strings.Contains(uaLower, "android 10; k)") {
		return true
	}
	for _, prefix := range []string{"chrome/", "chromium/"} {
		if v := extractVersionAfter(uaLower, prefix); strings.HasSuffix(v, ".0.0.0") {
			return true
		}
	}
	if strings.Contains(uaLower, "intel mac os x 10_15_7") {
		if strings.Contains(uaLower, "chrome/") || strings.Contains(uaLower, "chromium/") {
			return true
		}
		if v := extractVersionAfter(uaLower, "version/"); v != "" {
			if major, err := strconv.Atoi(majorOf(v)); err == nil && major >= 16 {
				return true
			}
		}
	}
	return false
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

// matchBotIdentity returns the first identity whose token appears in the UA.
func matchBotIdentity(uaLower string, table []botIdentity) *botIdentity {
	for i := range table {
		if strings.Contains(uaLower, table[i].token) {
			return &table[i]
		}
	}
	return nil
}

// isJunkBotName reports that uap-core failed to isolate a real agent family:
// its generic fallback patterns yield "Other", a bare "crawler"/"spider"/
// "bot", or a URL fragment containing a slash ("com/bot").
func isJunkBotName(name string) bool {
	switch strings.ToLower(name) {
	case "other", "crawler", "spider", "bot":
		return true
	}
	return strings.Contains(name, "/")
}

// extractAgentIdentity recovers the canonical agent name (original casing)
// and its version from the UA, given the lowercase token that matched:
// "...; ChatGPT-User/1.0; ..." → ("ChatGPT-User", "1.0").
//
// The matched token may be only a PREFIX of the real agent (the table lists
// "googleagent-" to catch GoogleAgent-Mariner / -URLContext), so the name is
// grown from the match start over agent-name characters to the natural
// boundary — otherwise "GoogleAgent-Mariner/1.0" would truncate to
// "googleagent" and lose its version.
func extractAgentIdentity(ua, uaLower, token string) (name, version string) {
	idx := strings.Index(uaLower, token)
	if idx == -1 {
		return "", ""
	}
	end := idx + len(token)
	for end < len(ua) && isAgentNameChar(ua[end]) {
		end++
	}
	name = strings.Trim(ua[idx:end], "-_ ")

	rest := ua[end:]
	if len(rest) > 1 && (rest[0] == '/' || rest[0] == ' ') {
		v := rest[1:]
		vend := 0
		for vend < len(v) && (v[vend] >= '0' && v[vend] <= '9' || v[vend] == '.') {
			vend++
		}
		version = strings.Trim(v[:vend], ".")
	}
	return name, version
}

// isAgentNameChar reports whether b can appear inside an agent token name
// (letters, digits, '-', '_'); '.' and '/' are excluded so a version suffix
// is never swallowed into the name.
func isAgentNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '-' || b == '_'
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
	if bitness != "" {
		res.CPU.Bitness = bitness
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
			res.Device.FormFactor = "watch"
		case strings.Contains(ff, "xr"):
			res.Device.Type = "xr"
			res.Device.FormFactor = "xr"
		case strings.Contains(ff, "automotive"):
			res.Device.Type = "automotive"
			res.Device.FormFactor = "automotive"
		case strings.Contains(ff, "tablet"):
			res.Device.Type = "tablet"
			res.Device.FormFactor = "tablet"
		case strings.Contains(ff, "eink"):
			// EInk describes the display, not the device class — keep the type.
		case strings.Contains(ff, "mobile"):
			res.Device.Type = "mobile"
			res.Device.FormFactor = "mobile"
		case strings.Contains(ff, "desktop"):
			res.Device.Type = "desktop"
			res.Device.FormFactor = "desktop"
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
