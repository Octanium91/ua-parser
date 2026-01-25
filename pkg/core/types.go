package core

type Config struct {
	DisableAutoUpdate bool   `json:"disable_auto_update"`
	UpdateURL         string `json:"update_url"`
	UpdateInterval    string `json:"update_interval"` // e.g., "24h"
	LRUCacheSize      int    `json:"lru_cache_size"`
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
}

type DeviceInfo struct {
	Model  string `json:"model"`
	Vendor string `json:"vendor"`
	Type   string `json:"type"`
}

type CPUInfo struct {
	Architecture string `json:"architecture"`
}

type EngineInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
