package core

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ua-parser/uap-go/uaparser"
	"gopkg.in/yaml.v3"
)

//go:embed resources/regexes.yaml
var defaultRegexes []byte

type Parser struct {
	mu     sync.RWMutex
	uap    *uaparser.Parser
	cache  *lru.Cache[string, *Result]
	config Config
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
		cache, _ = lru.New[string, *Result](cfg.LRUCacheSize)
	}

	p := &Parser{
		uap:    uap,
		cache:  cache,
		config: cfg,
	}

	if !cfg.DisableAutoUpdate {
		go p.startUpdater()
	}

	return p, nil
}

func (p *Parser) Parse(ua string, headers map[string]string) *Result {
	cacheKey := ""
	if p.cache != nil {
		// Create a stable cache key from UA and relevant headers
		cacheKey = ua + "|" + headers["Sec-CH-UA-Platform"] + "|" + headers["Sec-CH-UA-Platform-Version"] + "|" + headers["Sec-CH-UA-Model"] + "|" + headers["Sec-CH-UA-Arch"] + "|" + headers["Sec-CH-UA-Full-Version-List"]
		if res, ok := p.cache.Get(cacheKey); ok {
			return res
		}
	}

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

	// Apply Client Hints (overrides)
	p.applyClientHints(res, headers)

	// Post-process category
	if res.IsBot {
		res.Category = "bot"
	} else if res.Device.Type == "mobile" || res.Device.Type == "tablet" {
		res.Category = "mobile"
	} else {
		res.Category = "desktop"
	}

	if p.cache != nil {
		p.cache.Add(cacheKey, res)
	}

	return res
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

func (p *Parser) inferInfo(res *Result) {
	ua := strings.ToLower(res.UA)

	// Bot detection
	res.IsBot = isBot(res.Browser.Name, ua)
	res.IsAICrawler = isAICrawler(ua)

	// Browser Type
	if res.IsBot {
		res.Browser.Type = "bot"
	} else if strings.Contains(ua, "email") || strings.Contains(ua, "thunderbird") || res.Browser.Name == "Airmail" {
		res.Browser.Type = "email"
	} else if strings.Contains(ua, "library") || strings.Contains(ua, "curl") || strings.Contains(ua, "wget") || strings.Contains(ua, "http-client") {
		res.Browser.Type = "library"
	}

	// Engine detection
	if strings.Contains(ua, "webkit") {
		res.Engine.Name = "WebKit"
		if strings.Contains(ua, "chrome") || strings.Contains(ua, "edg") {
			res.Engine.Name = "Blink"
		}
	} else if strings.Contains(ua, "gecko") && !strings.Contains(ua, "webkit") {
		res.Engine.Name = "Gecko"
	} else if strings.Contains(ua, "trident") {
		res.Engine.Name = "Trident"
	} else if strings.Contains(ua, "presto") {
		res.Engine.Name = "Presto"
	}

	// Engine Version
	if res.Engine.Name == "Blink" {
		res.Engine.Version = res.Browser.Version
	} else if res.Engine.Name != "" {
		res.Engine.Version = extractEngineVersion(res.Engine.Name, ua)
	}

	// Device Type
	if strings.Contains(ua, "mobi") || strings.Contains(ua, "iphone") || strings.Contains(ua, "android") {
		res.Device.Type = "mobile"
	}
	if strings.Contains(ua, "tablet") || strings.Contains(ua, "ipad") {
		res.Device.Type = "tablet"
	}

	// CPU architecture (best effort from UA)
	if strings.Contains(ua, "x86_64") || strings.Contains(ua, "amd64") || strings.Contains(ua, "win64") || strings.Contains(ua, "x64") {
		res.CPU.Architecture = "amd64"
	} else if strings.Contains(ua, "arm64") || strings.Contains(ua, "aarch64") {
		res.CPU.Architecture = "arm64"
	} else if strings.Contains(ua, "i686") || strings.Contains(ua, "i386") {
		res.CPU.Architecture = "x86"
	}
}

func isBot(name, ua string) bool {
	name = strings.ToLower(name)
	if strings.Contains(name, "bot") || strings.Contains(name, "crawler") || strings.Contains(name, "spider") || strings.Contains(name, "scrap") {
		return true
	}
	if strings.Contains(ua, "bot") || strings.Contains(ua, "crawler") || strings.Contains(ua, "spider") || strings.Contains(ua, "google-extended") {
		return true
	}
	return false
}

func isAICrawler(ua string) bool {
	aiBots := []string{
		"gptbot", "chatgpt-user", "google-extended", "claudebot", "anthropic-ai",
		"perplexitybot", "ccbot", "yandexforai", "omgilibot", "facebookbot",
	}
	for _, bot := range aiBots {
		if strings.Contains(ua, bot) {
			return true
		}
	}
	return false
}

func (p *Parser) applyClientHints(res *Result, headers map[string]string) {
	if headers == nil {
		return
	}

	getHeader := func(key string) string {
		if v, ok := headers[key]; ok {
			return v
		}
		// Fallback to lowercase
		lowKey := strings.ToLower(key)
		for k, v := range headers {
			if strings.ToLower(k) == lowKey {
				return v
			}
		}
		return ""
	}

	platform := cleanHeader(getHeader("Sec-CH-UA-Platform"))
	platformVer := cleanHeader(getHeader("Sec-CH-UA-Platform-Version"))
	model := cleanHeader(getHeader("Sec-CH-UA-Model"))
	arch := cleanHeader(getHeader("Sec-CH-UA-Arch"))
	mobile := cleanHeader(getHeader("Sec-CH-UA-Mobile"))
	fullVersionList := getHeader("Sec-CH-UA-Full-Version-List")

	if platform != "" {
		if platform == "Windows" {
			res.OS.Name = "Windows"
			if isWindows11(platformVer) {
				res.OS.Version = "11"
			} else if platformVer != "" {
				res.OS.Version = platformVer
			}
		} else {
			res.OS.Name = platform
			if platformVer != "" {
				res.OS.Version = platformVer
			}
		}
	}

	if fullVersionList != "" {
		if fullVer := parseFullVersionList(fullVersionList, res.Browser.Name); fullVer != "" {
			res.Browser.Version = fullVer
			// Update engine version if it's Blink
			if res.Engine.Name == "Blink" {
				res.Engine.Version = fullVer
			}
		}
	}

	if model != "" {
		res.Device.Model = model
	}

	if arch != "" {
		res.CPU.Architecture = arch
	}

	if mobile == "?1" {
		res.Device.Type = "mobile"
	} else if mobile == "?0" && res.Device.Type == "desktop" {
		// Already desktop by default, but let's be explicit
		res.Device.Type = "desktop"
	}
}

func cleanHeader(h string) string {
	return strings.Trim(h, `" `)
}

func isWindows11(ver string) bool {
	parts := strings.Split(ver, ".")
	if len(parts) > 0 {
		major, err := strconv.Atoi(parts[0])
		if err != nil {
			return false
		}
		// Sec-CH-UA-Platform-Version for Windows 11 is 13.0.0+
		if major >= 13 {
			return true
		}
	}
	return false
}

func parseFullVersionList(header string, browserName string) string {
	// Format: "Chromium";v="144.0.7559.97", "Google Chrome";v="144.0.7559.97"
	brands := strings.Split(header, ",")
	for _, b := range brands {
		parts := strings.Split(b, ";")
		if len(parts) < 2 {
			continue
		}
		brand := strings.Trim(parts[0], `" `)
		versionPart := strings.Trim(parts[1], " ")

		match := false
		if strings.EqualFold(brand, browserName) {
			match = true
		} else if browserName == "Chrome" && (brand == "Google Chrome" || brand == "Chromium") {
			match = true
		} else if browserName == "Edge" && (brand == "Microsoft Edge" || brand == "Edge") {
			match = true
		}

		if match {
			if strings.HasPrefix(versionPart, "v=") {
				return strings.Trim(versionPart[2:], `"`)
			}
		}
	}
	return ""
}

func extractEngineVersion(engineName, ua string) string {
	ua = strings.ToLower(ua)
	switch engineName {
	case "WebKit":
		if idx := strings.Index(ua, "applewebkit/"); idx != -1 {
			version := ua[idx+len("applewebkit/"):]
			if end := strings.IndexAny(version, " "); end != -1 {
				return version[:end]
			}
			return version
		}
	case "Gecko":
		if idx := strings.Index(ua, "rv:"); idx != -1 {
			version := ua[idx+3:]
			if end := strings.IndexAny(version, ") "); end != -1 {
				return version[:end]
			}
			return version
		}
	}
	return ""
}
