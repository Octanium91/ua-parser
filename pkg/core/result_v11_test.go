package core

// Tests for the Result v1.1 fields: bot identity object + name synthesis,
// os.platform, cpu.bitness, device.form_factor, is_frozen_ua.

import "testing"

func TestBotIdentitySynthesis(t *testing.T) {
	p := newTestParser(t, 0)

	cases := []struct {
		name     string
		ua       string
		botName  string
		version  string
		category string
		vendor   string
	}{
		{
			name:     "ChatGPT-User junk name recovered",
			ua:       "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot",
			botName:  "ChatGPT-User",
			version:  "1.0",
			category: "user-fetch",
			vendor:   "OpenAI",
		},
		{
			name:     "Claude-User Other name recovered",
			ua:       "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; Claude-User/1.0; +Claude-User@anthropic.com",
			botName:  "Claude-User",
			version:  "1.0",
			category: "user-fetch",
			vendor:   "Anthropic",
		},
		{
			name:     "meta-externalagent crawler name recovered",
			ua:       "meta-externalagent/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler)",
			botName:  "meta-externalagent",
			version:  "1.1",
			category: "training",
			vendor:   "Meta",
		},
		{
			name:     "GPTBot keeps its uap-core family",
			ua:       "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.2; +https://openai.com/gptbot",
			botName:  "GPTBot",
			version:  "1.2",
			category: "training",
			vendor:   "OpenAI",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := p.Parse(tc.ua, nil)
			if !res.IsBot || !res.IsAICrawler {
				t.Fatalf("is_bot=%t is_ai_crawler=%t, want true/true", res.IsBot, res.IsAICrawler)
			}
			if res.Browser.Name != tc.botName {
				t.Errorf("browser.name = %q, want %q", res.Browser.Name, tc.botName)
			}
			if tc.version != "" && res.Browser.Version != tc.version {
				t.Errorf("browser.version = %q, want %q", res.Browser.Version, tc.version)
			}
			if res.Bot == nil {
				t.Fatal("bot object missing")
			}
			if res.Bot.Name != tc.botName || res.Bot.Category != tc.category || res.Bot.Vendor != tc.vendor {
				t.Errorf("bot = %+v, want {%s %s %s}", res.Bot, tc.botName, tc.category, tc.vendor)
			}
		})
	}
}

// Regression: a prefix token ("googleagent-") must grow to the full agent
// name and still recover the version — not truncate to "GoogleAgent".
func TestBotPrefixTokenNameGrowth(t *testing.T) {
	p := newTestParser(t, 0)
	res := p.Parse("Mozilla/5.0 (compatible; GoogleAgent-Mariner/1.0; +https://www.google.com)", nil)
	if res.Browser.Name != "GoogleAgent-Mariner" {
		t.Errorf("browser.name = %q, want GoogleAgent-Mariner", res.Browser.Name)
	}
	if res.Browser.Version != "1.0" {
		t.Errorf("browser.version = %q, want 1.0", res.Browser.Version)
	}
	if res.Bot == nil || res.Bot.Vendor != "Google" || res.Bot.Category != "agent" {
		t.Errorf("bot = %+v, want Google/agent", res.Bot)
	}
}

// Regression: the Apple-Silicon signal rule must NOT fire on an iPhone, whose
// UA contains "like Mac OS X". iPhones are arm64, but the rule must be gated
// on the parsed macOS family, not the raw-UA substring.
func TestAppleSiliconRuleIgnoresIOS(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	res := p.ParseFull(ua, nil, &Signals{WebGLRenderer: "Apple M2"})
	if res.OS.Name != "iOS" {
		t.Errorf("os.name = %q, want iOS", res.OS.Name)
	}
	// The rule is scoped to desktop Macs; an iPhone must not acquire arch via it.
	if res.CPU.Architecture == "arm64" {
		t.Errorf("apple-silicon rule wrongly fired on iPhone (arch=%q)", res.CPU.Architecture)
	}

	// A genuine desktop Mac on Apple Silicon still resolves.
	mac := p.ParseFull(
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		nil, &Signals{WebGLRenderer: "ANGLE (Apple, ANGLE Metal Renderer: Apple M2 Pro)"})
	if mac.CPU.Architecture != "arm64" {
		t.Errorf("desktop Mac arch = %q, want arm64", mac.CPU.Architecture)
	}
}

func TestClassicBotClassification(t *testing.T) {
	p := newTestParser(t, 0)

	res := p.Parse("Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/126.0.6478.126 Safari/537.36", nil)
	if res.Bot == nil || res.Bot.Category != "search-crawler" || res.Bot.Vendor != "Google" {
		t.Errorf("googlebot bot = %+v, want search-crawler/Google", res.Bot)
	}
	if res.IsAICrawler {
		t.Error("plain Googlebot must not be flagged as AI")
	}

	res = p.Parse("Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", nil)
	if res.Bot == nil || res.Bot.Category != "seo" || res.Bot.Vendor != "Ahrefs" {
		t.Errorf("ahrefsbot bot = %+v, want seo/Ahrefs", res.Bot)
	}

	// Non-bot traffic must carry no bot object.
	res = p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
	if res.Bot != nil {
		t.Errorf("desktop Chrome got a bot object: %+v", res.Bot)
	}
}

func TestOSPlatformNormalization(t *testing.T) {
	p := newTestParser(t, 0)

	cases := []struct {
		ua       string
		headers  map[string]string
		platform string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil, "windows"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", nil, "macos"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", nil, "ios"},
		{"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:126.0) Gecko/20100101 Firefox/126.0", nil, "linux"},
		{"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36", nil, "android"},
		{"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36", nil, "chromeos"},
		{"Mozilla/5.0 (PlayStation; PlayStation 5/8.40) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", nil, "playstation"},
	}
	for _, tc := range cases {
		res := p.Parse(tc.ua, tc.headers)
		if res.OS.Platform != tc.platform {
			t.Errorf("os.platform for %q = %q (os.name %q), want %q", tc.ua[:40], res.OS.Platform, res.OS.Name, tc.platform)
		}
	}
}

func TestBitnessAndFormFactor(t *testing.T) {
	p := newTestParser(t, 0)

	// From the UA.
	res := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
	if res.CPU.Bitness != "64" {
		t.Errorf("bitness = %q, want 64", res.CPU.Bitness)
	}
	if res.Device.FormFactor != "desktop" {
		t.Errorf("form_factor = %q, want desktop", res.Device.FormFactor)
	}

	// From Client Hints (frozen UA claims x64; CH says arm64).
	headers := map[string]string{
		"Sec-CH-UA-Arch":         `"arm"`,
		"Sec-CH-UA-Bitness":      `"64"`,
		"Sec-CH-UA-Form-Factors": `"Tablet"`,
	}
	res = p.Parse("Mozilla/5.0 (Linux; Android 14; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", headers)
	if res.CPU.Architecture != "arm64" || res.CPU.Bitness != "64" {
		t.Errorf("cpu = %+v, want arm64/64", res.CPU)
	}
	if res.Device.FormFactor != "tablet" {
		t.Errorf("form_factor = %q, want tablet", res.Device.FormFactor)
	}

	// Wearable maps to the CH "watch" vocabulary.
	res = p.Parse("Mozilla/5.0 (Linux; Android 10; SM-R860 Watch) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.0.0 Mobile Safari/537.36", nil)
	if res.Device.FormFactor != "watch" {
		t.Errorf("form_factor = %q, want watch", res.Device.FormFactor)
	}
}

func TestFrozenUADetection(t *testing.T) {
	p := newTestParser(t, 0)

	frozen := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
	}
	for _, ua := range frozen {
		if !p.Parse(ua, nil).IsFrozenUA {
			t.Errorf("is_frozen_ua = false for %q", ua)
		}
	}

	notFrozen := []string{
		// Real pre-freeze Chrome version with non-zero build/patch.
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.102 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0",
		// Safari 15 on genuine Catalina 10.15.7.
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.6 Safari/605.1.15",
	}
	for _, ua := range notFrozen {
		if p.Parse(ua, nil).IsFrozenUA {
			t.Errorf("is_frozen_ua = true for %q", ua)
		}
	}
}

// The cached copy and returned copies must not share Bot/GPU pointers.
func TestResultCopyIsDeep(t *testing.T) {
	p := newTestParser(t, 10)
	ua := "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)"

	first := p.Parse(ua, nil)
	first.Bot.Name = "MUTATED"

	second := p.Parse(ua, nil)
	if second.Bot.Name == "MUTATED" {
		t.Error("caller mutation leaked into the cache: Bot pointer is shared")
	}
}
