package core

import "context"

type Config struct {
	Ctx               context.Context `json:"-"`
	DisableAutoUpdate bool            `json:"disable_auto_update"`
	UpdateURL         string          `json:"update_url"`
	UpdateInterval    string          `json:"update_interval"` // e.g., "24h"
	LRUCacheSize      int             `json:"lru_cache_size"`

	// Correction layer (docs/correction-layer.md). CorrectionsURL overrides
	// the default hot-update source for corrections.yaml.
	//
	// DisableAutoUpdate is the master network switch (set it and nothing is
	// fetched in the background). DisableCorrectionsUpdate is a sub-switch:
	// with auto-update on, it suppresses only corrections while regex updates
	// continue. When auto-update is on and corrections are not suppressed,
	// corrections are fetched once at startup (regexes wait for the first
	// tick, since the regex download is large and corrections are a few KB).
	//
	// WASM nuance: the browser client defaults DisableAutoUpdate=true (it
	// cannot re-download the multi-MB regex DB) yet still fetches the tiny
	// corrections file — there, corrections are gated by
	// DisableCorrectionsUpdate alone. The master-switch semantics apply to
	// native builds.
	CorrectionsURL           string `json:"corrections_url"`
	DisableCorrectionsUpdate bool   `json:"disable_corrections_update"`
}

// ResultSchemaVersion is the version of the Result JSON shape. Bump it
// whenever fields are added or their meaning changes, so a stored result
// stays traceable to the format (and thus the library range) that produced
// it. Emitted on every result as `result_version`.
//
//	1.0 — browser/os/device/cpu/engine/category + is_bot/is_ai_crawler
//	1.1 — os.platform, cpu.bitness, device.form_factor, is_frozen_ua, bot{}, gpu{}
//	1.2 — automation, integrity, security, detection, convenience flags,
//	      os.version_name/version_raw, class_hash
const ResultSchemaVersion = "1.2"

type Result struct {
	// ResultVersion is ResultSchemaVersion at parse time (see above).
	ResultVersion string      `json:"result_version"`
	UA            string      `json:"ua"`
	Browser       BrowserInfo `json:"browser"`
	OS            OSInfo      `json:"os"`
	Device        DeviceInfo  `json:"device"`
	CPU           CPUInfo     `json:"cpu"`
	Engine        EngineInfo  `json:"engine"`
	Category      string      `json:"category"`
	IsBot         bool        `json:"is_bot"`
	IsAICrawler   bool        `json:"is_ai_crawler"`

	// IsFrozenUA reports that the UA string is a frozen/reduced template
	// (Chromium reduced UA, the capped Mac OS X 10_15_7 token, "Android 10;
	// K") — a signal that Client Hints, not the UA, carry the truth.
	IsFrozenUA bool `json:"is_frozen_ua"`

	// --- Result v1.2: enrichment derived from UA + CH + signals (no DB) ---

	// Convenience classifications (derived from device/engine/cpu).
	IsMobile       bool `json:"is_mobile"`
	IsDesktop      bool `json:"is_desktop"`
	IsTouchCapable bool `json:"is_touch_capable"`
	IsChromeFamily bool `json:"is_chrome_family"`
	IsAppleSilicon bool `json:"is_apple_silicon"`

	// Automation flags UNDECLARED automation (unlike is_bot, which is declared
	// bots): headless browsers, Electron shells, and navigator.webdriver.
	Automation AutomationInfo `json:"automation"`

	// Integrity cross-checks UA vs Client Hints vs signals for contradictions
	// — a spoofed/inconsistent client. Reasons is empty when consistent.
	Integrity IntegrityInfo `json:"integrity"`

	// Security flags attack payloads embedded in the UA string (scanners,
	// SQL-injection, XSS) — the WAF-adjacent "Hacker" signal.
	Security SecurityInfo `json:"security"`

	// Detection reports whether Client Hints drove the result (trust/quality).
	Detection DetectionInfo `json:"detection"`

	// ClassHash is a stable hash of the client CLASS tuple (browser/os/device/
	// engine/cpu/category) — a ready analytics bucket key, identical for every
	// client of the same class. It is deliberately coarse: NOT a per-user/
	// per-device tracking fingerprint (no version, screen, IP, or fonts).
	ClassHash string `json:"class_hash"`

	// Bot is set for every detected bot: canonical agent name plus a curated
	// category/vendor (training | search | user-fetch | agent | search-crawler
	// | seo | monitoring | social-preview | other). The is_bot/is_ai_crawler
	// booleans above stay for compatibility.
	Bot *BotInfo `json:"bot,omitempty"`

	// GPU is populated only when the client supplied a WebGL signal.
	GPU *GPUInfo `json:"gpu,omitempty"`
}

type AutomationInfo struct {
	Headless  bool `json:"headless"`
	Electron  bool `json:"electron"`
	Webdriver bool `json:"webdriver"`
}

type IntegrityInfo struct {
	Spoofed bool     `json:"spoofed"`
	Reasons []string `json:"reasons"`
}

type SecurityInfo struct {
	Suspicious bool   `json:"suspicious"`
	Category   string `json:"category,omitempty"` // scanner | sql-injection | xss | path-traversal | jndi
}

// DetectionInfo tells the consumer WHICH inputs drove the result, so data
// quality is visible at a glance: client_hints_used / signals_used report
// whether those richer inputs were present at all (UA-only parses set both
// false), and high_entropy whether high-entropy Client Hints were supplied.
type DetectionInfo struct {
	ClientHintsUsed bool `json:"client_hints_used"`
	HighEntropy     bool `json:"high_entropy"`
	SignalsUsed     bool `json:"signals_used"`
}

type BrowserInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Major   string `json:"major"`
	Type    string `json:"type"`
}

type OSInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Platform is the canonical machine-readable OS key (windows, macos,
	// linux, android, ios, chromeos, tizen, playstation, other) — Name keeps
	// the marketing spelling.
	Platform string `json:"platform"`
	// VersionName is a human display label ("Windows 11", "macOS Sonoma",
	// "Android 14"); VersionRaw is the exact CH platform-version ("15.0.0" for
	// Windows 11) before normalization, or the UA-derived version otherwise.
	VersionName string `json:"version_name"`
	VersionRaw  string `json:"version_raw"`
}

type DeviceInfo struct {
	Model  string `json:"model"`
	Vendor string `json:"vendor"`
	Type   string `json:"type"`
	// FormFactor surfaces Sec-CH-UA-Form-Factors when sent, otherwise it is
	// derived from Type (wearable → watch).
	FormFactor string `json:"form_factor"`
}

type CPUInfo struct {
	Architecture string `json:"architecture"`
	Bitness      string `json:"bitness"` // "64", "32", or ""
}

type EngineInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type BotInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Vendor   string `json:"vendor"`
}

// Signals carries browser-side evidence beyond HTTP headers, collected by
// frontend JS (the wasmjs client gathers them automatically). Priority in the
// pipeline: Client Hints > signals > UA string. Safari and Firefox send no
// UA-CH headers at all, which is exactly where these matter most (e.g. the
// iPad-as-Mac unmask via max_touch_points).
type Signals struct {
	MaxTouchPoints      int         `json:"max_touch_points,omitempty"`
	Platform            string      `json:"platform,omitempty"` // navigator.platform
	WebGLVendor         string      `json:"webgl_vendor,omitempty"`
	WebGLRenderer       string      `json:"webgl_renderer,omitempty"`
	Screen              *ScreenInfo `json:"screen,omitempty"`
	DeviceMemory        float64     `json:"device_memory,omitempty"`
	HardwareConcurrency int         `json:"hardware_concurrency,omitempty"`
	// Webdriver mirrors navigator.webdriver — true under Selenium/Puppeteer/
	// Playwright automation; feeds automation.webdriver.
	Webdriver bool `json:"webdriver,omitempty"`
}

type ScreenInfo struct {
	W   int     `json:"w"`
	H   int     `json:"h"`
	DPR float64 `json:"dpr"`
}

type GPUInfo struct {
	Vendor   string `json:"vendor"`
	Renderer string `json:"renderer"`
}
