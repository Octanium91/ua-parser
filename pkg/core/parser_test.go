package core

import (
	"sync"
	"testing"
)

func TestParser(t *testing.T) {
	cfg := Config{
		DisableAutoUpdate: true,
		LRUCacheSize:      10,
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	// Test standard UA (Windows 10)
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	res := p.Parse(ua, nil)
	// Base parser usually identifies this as Windows 10 or Windows
	if res.OS.Name != "Windows" && res.OS.Name != "Windows 10" {
		t.Errorf("Expected OS Windows or Windows 10, got %s", res.OS.Name)
	}
	if res.Category != "desktop" {
		t.Errorf("Expected Category desktop, got %s", res.Category)
	}
	if res.Engine.Name != "Blink" {
		t.Errorf("Expected Engine Blink, got %s", res.Engine.Name)
	}

	// Test Client Hints for Windows 11
	headers := map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"15.0.0"`,
	}
	res = p.Parse(ua, headers)
	if res.OS.Name != "Windows" {
		t.Errorf("Expected OS Windows from Client Hints, got %s", res.OS.Name)
	}
	if res.OS.Version != "11" {
		t.Errorf("Expected OS Version 11 from Client Hints, got %s", res.OS.Version)
	}

	// Test Client Hints for Model and Arch
	headers["Sec-CH-UA-Model"] = `"Pixel 5"`
	headers["Sec-CH-UA-Arch"] = `"arm64"`
	headers["Sec-CH-UA-Mobile"] = `?1`
	res = p.Parse(ua, headers)
	if res.Device.Model != "Pixel 5" {
		t.Errorf("Expected Model Pixel 5 from Client Hints, got %s", res.Device.Model)
	}
	if res.CPU.Architecture != "arm64" {
		t.Errorf("Expected CPU arm64, got %s", res.CPU.Architecture)
	}
	if res.Device.Type != "mobile" {
		t.Errorf("Expected Device Type mobile, got %s", res.Device.Type)
	}
	if res.Category != "mobile" {
		t.Errorf("Expected Category mobile, got %s", res.Category)
	}

	// Test Client Hints overriding UA for Architecture
	uaAMD64 := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	headersArch := map[string]string{
		"Sec-CH-UA-Arch": `"arm64"`,
	}
	res = p.Parse(uaAMD64, headersArch)
	if res.CPU.Architecture != "arm64" {
		t.Errorf("Expected CPU arm64 from Client Hints overriding UA, got %s", res.CPU.Architecture)
	}

	// Test Client Hints case-insensitivity
	headersCase := map[string]string{
		"sec-ch-ua-platform":         `"Windows"`,
		"sec-ch-ua-platform-version": `"13.0.0"`,
	}
	res = p.Parse(ua, headersCase)
	if res.OS.Name != "Windows" || res.OS.Version != "11" {
		t.Errorf("Expected Windows 11 from lowercase Client Hints, got %s %s", res.OS.Name, res.OS.Version)
	}

	// Test Bot Detection
	botUA := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	res = p.Parse(botUA, nil)
	if !res.IsBot {
		t.Errorf("Expected IsBot to be true for Googlebot")
	}
	if res.Category != "bot" {
		t.Errorf("Expected Category bot, got %s", res.Category)
	}

	// Test AI Crawler
	aiUA := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.0; +https://openai.com/gptbot)"
	res = p.Parse(aiUA, nil)
	if !res.IsAICrawler {
		t.Errorf("Expected IsAICrawler to be true for GPTBot")
	}
	if !res.IsBot {
		t.Errorf("Expected IsBot to be true for GPTBot")
	}
}

func TestClientHintsAccuracy(t *testing.T) {
	cfg := Config{
		DisableAutoUpdate: true,
		LRUCacheSize:      0, // Disable cache for this test to ensure fresh results
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	headers := map[string]string{
		"Sec-CH-UA-Platform":          "\"Windows\"",
		"Sec-CH-UA-Platform-Version":  "\"13.0.0\"",
		"Sec-CH-UA-Full-Version-List": "\"Chromium\";v=\"144.0.7559.97\", \"Google Chrome\";v=\"144.0.7559.97\"",
	}

	res := p.Parse(ua, headers)

	// OS Verification
	if res.OS.Name != "Windows" {
		t.Errorf("Expected OS Name 'Windows', got '%s'", res.OS.Name)
	}
	if res.OS.Version != "11" {
		t.Errorf("Expected OS Version '11', got '%s'", res.OS.Version)
	}

	// Browser Verification
	if res.Browser.Name != "Chrome" {
		t.Errorf("Expected Browser Name 'Chrome', got '%s'", res.Browser.Name)
	}
	if res.Browser.Version != "144.0.7559.97" {
		t.Errorf("Expected Browser Version '144.0.7559.97', got '%s'", res.Browser.Version)
	}

	// Engine Verification
	if res.Engine.Name != "Blink" {
		t.Errorf("Expected Engine Name 'Blink', got '%s'", res.Engine.Name)
	}
	if res.Engine.Version != "144.0.7559.97" {
		t.Errorf("Expected Engine Version '144.0.7559.97', got '%s'", res.Engine.Version)
	}
}

func TestBotDetectionFalsePositives(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	notBots := []string{
		"Mozilla/5.0 (Linux; Android 12; SM-G991B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.120 Mobile Safari/537.36",
		"Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.1; WOW64; Trident/6.0) about:blank",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.0 Mobile/15E148 Safari/604.1",
	}

	for _, ua := range notBots {
		res := p.Parse(ua, nil)
		if res.IsBot {
			t.Errorf("False positive: UA=%q was detected as bot", ua)
		}
	}
}

func TestBotDetectionTruePositives(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	bots := []struct {
		ua   string
		name string
	}{
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "Googlebot"},
		{"Mozilla/5.0 (compatible; Bingbot/2.0; +http://www.bing.com/bingbot.htm)", "Bingbot"},
		{"Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)", "YandexBot"},
		{"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.0; +https://openai.com/gptbot)", "GPTBot"},
	}

	for _, tc := range bots {
		res := p.Parse(tc.ua, nil)
		if !res.IsBot {
			t.Errorf("Expected IsBot=true for %s, UA=%q", tc.name, tc.ua)
		}
	}
}

func TestAICrawlerDetection(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	aiCrawlers := []string{
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.0; +https://openai.com/gptbot)",
		"ChatGPT-User/1.0",
		"Mozilla/5.0 (compatible; Google-Extended; +http://google.com)",
		"ClaudeBot/1.0",
		"anthropic-ai/1.0",
		"Mozilla/5.0 (compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)",
		"CCBot/2.0 (https://commoncrawl.org/faq/)",
	}

	for _, ua := range aiCrawlers {
		res := p.Parse(ua, nil)
		if !res.IsAICrawler {
			t.Errorf("Expected IsAICrawler=true for UA=%q", ua)
		}
	}
}

func TestDeviceTypeDetection(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	tests := []struct {
		name       string
		ua         string
		deviceType string
	}{
		{"iPhone", "Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)", "mobile"},
		{"iPad", "Mozilla/5.0 (iPad; CPU OS 15_0 like Mac OS X)", "tablet"},
		{"Android phone", "Mozilla/5.0 (Linux; Android 12; Pixel 6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0 Mobile Safari/537.36", "mobile"},
		{"Android tablet", "Mozilla/5.0 (Linux; Android 12; SM-T870) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0 Safari/537.36", "tablet"},
		{"Desktop", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0 Safari/537.36", "desktop"},
		{"Smart TV", "Mozilla/5.0 (SMART-TV; Linux; Tizen 5.0) AppleWebKit/537.36", "tv"},
		{"PlayStation", "Mozilla/5.0 (PlayStation 5) AppleWebKit/537.36", "console"},
		{"Xbox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; Xbox; Xbox One) AppleWebKit/537.36", "console"},
		{"Nintendo", "Mozilla/5.0 (Nintendo Switch; WebApplet) AppleWebKit/606.4", "console"},
		{"Apple Watch", "Mozilla/5.0 (Watch; Apple Watch) AppleWebKit/605.1.15", "wearable"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := p.Parse(tc.ua, nil)
			if res.Device.Type != tc.deviceType {
				t.Errorf("Expected device type %q, got %q for UA=%q", tc.deviceType, res.Device.Type, tc.ua)
			}
		})
	}
}

func TestEngineDetection(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	tests := []struct {
		name   string
		ua     string
		engine string
	}{
		{"Blink (Chrome)", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0 Safari/537.36", "Blink"},
		{"Blink (Edge)", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0 Safari/537.36 Edg/91.0", "Blink"},
		{"WebKit (Safari)", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.0 Safari/605.1.15", "WebKit"},
		{"Gecko (Firefox)", "Mozilla/5.0 (Windows NT 10.0; rv:91.0) Gecko/20100101 Firefox/91.0", "Gecko"},
		{"Trident (IE)", "Mozilla/5.0 (Windows NT 10.0; Trident/7.0; rv:11.0) like Gecko", "Trident"},
		{"EdgeHTML (Legacy Edge)", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.140 Safari/537.36 Edge/18.17763", "EdgeHTML"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := p.Parse(tc.ua, nil)
			if res.Engine.Name != tc.engine {
				t.Errorf("Expected engine %q, got %q for UA=%q", tc.engine, res.Engine.Name, tc.ua)
			}
		})
	}
}

func TestCacheHit(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true, LRUCacheSize: 100})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0 Safari/537.36"
	res1 := p.Parse(ua, nil)
	res2 := p.Parse(ua, nil)

	// Both should return the same pointer (cache hit)
	if res1 != res2 {
		t.Errorf("Expected cache hit to return same pointer, got different results")
	}
}

func TestConcurrentParsing(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true, LRUCacheSize: 100})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	uas := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/91.0 Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1",
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Mozilla/5.0 (Linux; Android 12; Pixel 6) AppleWebKit/537.36 Chrome/91.0 Mobile Safari/537.36",
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ua := uas[idx%len(uas)]
			res := p.Parse(ua, nil)
			if res == nil {
				t.Errorf("Got nil result for UA=%q", ua)
			}
		}(i)
	}
	wg.Wait()
}

func TestParserClose(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	// Close should not panic
	p.Close()

	// Parse should still work after Close (Close only stops the updater)
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
	res := p.Parse(ua, nil)
	if res == nil {
		t.Errorf("Expected non-nil result after Close()")
	}
}

func TestWindowsVersionMapping(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	tests := []struct {
		platformVer string
		expected    string
	}{
		{"15.0.0", "11"},
		{"13.0.0", "11"},
		{"10.0.0", "10"},
		{"1.0.0", "10"},
		{"0.0.0", "0.0.0"},
	}

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/91.0 Safari/537.36"
	for _, tc := range tests {
		t.Run("ver_"+tc.platformVer, func(t *testing.T) {
			headers := map[string]string{
				"Sec-CH-UA-Platform":         `"Windows"`,
				"Sec-CH-UA-Platform-Version": `"` + tc.platformVer + `"`,
			}
			res := p.Parse(ua, headers)
			if res.OS.Version != tc.expected {
				t.Errorf("Platform version %q: expected OS version %q, got %q", tc.platformVer, tc.expected, res.OS.Version)
			}
		})
	}
}

func TestEmptyAndNilInputs(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true, LRUCacheSize: 10})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	// Empty UA with nil headers
	res := p.Parse("", nil)
	if res == nil {
		t.Fatal("Expected non-nil result for empty UA")
	}

	// Empty UA with empty headers
	res = p.Parse("", map[string]string{})
	if res == nil {
		t.Fatal("Expected non-nil result for empty UA with empty headers")
	}
}

func TestEdgeHTMLEngine(t *testing.T) {
	p, err := New(Config{DisableAutoUpdate: true})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer p.Close()

	// Legacy Edge (EdgeHTML) uses "Edge/" while Chromium Edge uses "Edg/"
	legacyEdgeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.140 Safari/537.36 Edge/18.17763"
	res := p.Parse(legacyEdgeUA, nil)
	if res.Engine.Name != "EdgeHTML" {
		t.Errorf("Expected engine EdgeHTML for legacy Edge, got %q", res.Engine.Name)
	}
	if res.Engine.Version == "" {
		t.Error("Expected non-empty engine version for EdgeHTML")
	}

	// Chromium Edge should still be Blink
	chromiumEdgeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 Edg/91.0.864.59"
	res = p.Parse(chromiumEdgeUA, nil)
	if res.Engine.Name != "Blink" {
		t.Errorf("Expected engine Blink for Chromium Edge, got %q", res.Engine.Name)
	}
}

func TestContainsBotPattern(t *testing.T) {
	tests := []struct {
		s      string
		word   string
		expect bool
	}{
		{"googlebot/2.1", "bot", true},
		{"my-bot/1.0", "bot", true},
		{"bot", "bot", true},
		{"bot;compatible", "bot", true},
		{"the spider crawls", "spider", true}, // "spider" followed by space (non-letter) → match
		{"bottle of water", "bot", false},     // "bot" followed by "t" (letter) → rejected
		{"bottom line", "bot", false},         // "bot" followed by "t" (letter) → rejected
		{"bots are here", "bot", false},       // "bot" followed by "s" (letter) → rejected
		{"", "bot", false},
		{"webcrawler/1.0", "crawler", true},
	}

	for _, tc := range tests {
		t.Run(tc.s+"_"+tc.word, func(t *testing.T) {
			got := containsBotPattern(tc.s, tc.word)
			if got != tc.expect {
				t.Errorf("containsBotPattern(%q, %q) = %v, want %v", tc.s, tc.word, got, tc.expect)
			}
		})
	}
}

// Benchmarks

func BenchmarkParse(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(ua, nil)
	}
}

func BenchmarkParseWithCache(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 1000})
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(ua, nil)
	}
}

func BenchmarkParseWithHeaders(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	headers := map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"15.0.0"`,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(ua, headers)
	}
}

func BenchmarkParseParallel(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 1000})
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Parse(ua, nil)
		}
	})
}
