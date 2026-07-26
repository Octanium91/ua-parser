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

type Result struct {
	UA          string      `json:"ua"`
	Browser     BrowserInfo `json:"browser"`
	OS          OSInfo      `json:"os"`
	Device      DeviceInfo  `json:"device"`
	CPU         CPUInfo     `json:"cpu"`
	Engine      EngineInfo  `json:"engine"`
	Category    string      `json:"category"`
	IsBot       bool        `json:"is_bot"`
	IsAICrawler bool        `json:"is_ai_crawler"`

	// IsFrozenUA reports that the UA string is a frozen/reduced template
	// (Chromium reduced UA, the capped Mac OS X 10_15_7 token, "Android 10;
	// K") — a signal that Client Hints, not the UA, carry the truth.
	IsFrozenUA bool `json:"is_frozen_ua"`

	// Bot is set for every detected bot: canonical agent name plus a curated
	// category/vendor (training | search | user-fetch | agent | search-crawler
	// | seo | monitoring | social-preview | other). The is_bot/is_ai_crawler
	// booleans above stay for compatibility.
	Bot *BotInfo `json:"bot,omitempty"`

	// GPU is populated only when the client supplied a WebGL signal.
	GPU *GPUInfo `json:"gpu,omitempty"`
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
