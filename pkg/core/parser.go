package core

//go:generate go run ../../cmd/gen-json/main.go

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ua-parser/uap-go/uaparser"
)

//go:embed resources/regexes.json
var defaultRegexes []byte

var aiBots = []string{
	"gptbot", "chatgpt-user", "google-extended", "claudebot", "anthropic-ai",
	"perplexitybot", "ccbot", "yandexforai", "omgilibot", "facebookbot",
}

type Parser struct {
	mu     sync.RWMutex
	uap    *uaparser.Parser
	cache  *lru.Cache[string, *Result]
	config Config
	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg Config) (*Parser, error) {
	def := uaparser.RegexDefinitions{}
	if err := json.Unmarshal(defaultRegexes, &def); err != nil {
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
		var b strings.Builder
		b.Grow(len(ua) + 100)
		b.WriteString(ua)
		b.WriteByte('|')
		b.WriteString(normalizedHeaders["sec-ch-ua-platform"])
		b.WriteByte('|')
		b.WriteString(normalizedHeaders["sec-ch-ua-platform-version"])
		b.WriteByte('|')
		b.WriteString(normalizedHeaders["sec-ch-ua-model"])
		b.WriteByte('|')
		b.WriteString(normalizedHeaders["sec-ch-ua-arch"])
		b.WriteByte('|')
		b.WriteString(normalizedHeaders["sec-ch-ua-full-version-list"])
		cacheKey = b.String()
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

	// Apply Client Hints (overrides) using already-normalized headers
	p.applyClientHints(res, normalizedHeaders)

	// Post-process category
	if res.IsBot {
		res.Category = "bot"
	} else if res.Device.Type == "mobile" || res.Device.Type == "tablet" {
		res.Category = "mobile"
	} else if res.Device.Type == "tv" || res.Device.Type == "console" || res.Device.Type == "wearable" {
		res.Category = res.Device.Type
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
	uaLower := strings.ToLower(res.UA)
	nameLower := strings.ToLower(res.Browser.Name)

	// Bot detection
	res.IsBot = isBot(nameLower, uaLower)
	res.IsAICrawler = isAICrawler(uaLower)

	// Browser Type
	if res.IsBot {
		res.Browser.Type = "bot"
	} else if strings.Contains(uaLower, "email") || strings.Contains(uaLower, "thunderbird") || nameLower == "airmail" {
		res.Browser.Type = "email"
	} else if strings.Contains(uaLower, "library") || strings.Contains(uaLower, "curl") || strings.Contains(uaLower, "wget") || strings.Contains(uaLower, "http-client") {
		res.Browser.Type = "library"
	}

	// Engine detection (order matters: check specific engines before generic ones)
	if strings.Contains(uaLower, "edge/") && !strings.Contains(uaLower, "edg/") {
		res.Engine.Name = "EdgeHTML"
	} else if strings.Contains(uaLower, "trident") {
		res.Engine.Name = "Trident"
	} else if strings.Contains(uaLower, "webkit") {
		res.Engine.Name = "WebKit"
		if strings.Contains(uaLower, "chrome") || strings.Contains(uaLower, "edg") {
			res.Engine.Name = "Blink"
		}
	} else if strings.Contains(uaLower, "gecko") {
		res.Engine.Name = "Gecko"
	} else if strings.Contains(uaLower, "presto") {
		res.Engine.Name = "Presto"
	}

	// Engine Version
	if res.Engine.Name == "Blink" {
		res.Engine.Version = res.Browser.Version
	} else if res.Engine.Name != "" {
		res.Engine.Version = extractEngineVersion(res.Engine.Name, uaLower)
	}

	// Device Type
	if strings.Contains(uaLower, "smart-tv") || strings.Contains(uaLower, "smarttv") ||
		strings.Contains(uaLower, "appletv") || strings.Contains(uaLower, "roku") ||
		strings.Contains(uaLower, "crkey") || strings.Contains(uaLower, "firetv") ||
		strings.Contains(uaLower, "googletv") || strings.Contains(uaLower, "hbbtv") ||
		strings.Contains(uaLower, "tizen") || strings.Contains(uaLower, "webos") {
		res.Device.Type = "tv"
	} else if strings.Contains(uaLower, "playstation") || strings.Contains(uaLower, "xbox") ||
		strings.Contains(uaLower, "nintendo") {
		res.Device.Type = "console"
	} else if strings.Contains(uaLower, "watch") {
		res.Device.Type = "wearable"
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

func (p *Parser) applyClientHints(res *Result, headers map[string]string) {
	if headers == nil {
		return
	}

	// Headers are already normalized (lowercased keys) by Parse()
	platform := cleanHeader(headers["sec-ch-ua-platform"])
	platformVer := cleanHeader(headers["sec-ch-ua-platform-version"])
	model := cleanHeader(headers["sec-ch-ua-model"])
	arch := cleanHeader(headers["sec-ch-ua-arch"])
	mobile := cleanHeader(headers["sec-ch-ua-mobile"])
	fullVersionList := headers["sec-ch-ua-full-version-list"]

	if platform != "" {
		if platform == "Windows" {
			res.OS.Name = "Windows"
			if platformVer != "" {
				res.OS.Version = mapWindowsVersion(platformVer)
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
		res.Device.Type = "desktop"
	}
}

func cleanHeader(h string) string {
	return strings.Trim(h, `" `)
}

func mapWindowsVersion(ver string) string {
	parts := strings.Split(ver, ".")
	if len(parts) == 0 {
		return ver
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return ver
	}
	// https://learn.microsoft.com/en-us/microsoft-edge/web-platform/how-to-detect-win11
	if major >= 13 {
		return "11"
	}
	if major > 0 {
		return "10"
	}
	return ver
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

func extractEngineVersion(engineName, uaLower string) string {
	switch engineName {
	case "WebKit":
		if idx := strings.Index(uaLower, "applewebkit/"); idx != -1 {
			version := uaLower[idx+len("applewebkit/"):]
			if end := strings.IndexAny(version, " "); end != -1 {
				return version[:end]
			}
			return version
		}
	case "Gecko":
		if idx := strings.Index(uaLower, "rv:"); idx != -1 {
			version := uaLower[idx+3:]
			if end := strings.IndexAny(version, ") "); end != -1 {
				return version[:end]
			}
			return version
		}
	case "EdgeHTML":
		if idx := strings.Index(uaLower, "edge/"); idx != -1 {
			version := uaLower[idx+5:]
			if end := strings.IndexAny(version, " "); end != -1 {
				return version[:end]
			}
			return version
		}
	}
	return ""
}
