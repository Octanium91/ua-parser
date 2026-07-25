package core

// Regression tests for the 2026-07 core-quality review. Each test pins a bug
// that was found by the live functional battery and the code audit.

import (
	"testing"
)

func newTestParser(t *testing.T, cacheSize int) *Parser {
	t.Helper()
	p, err := New(Config{DisableAutoUpdate: true, LRUCacheSize: cacheSize})
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// The LRU key must include every consumed CH header; previously a cached
// desktop result was served for a mobile hint.
func TestCacheKeyIncludesMobileHint(t *testing.T) {
	p := newTestParser(t, 100)

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

	res0 := p.Parse(ua, map[string]string{"Sec-CH-UA-Mobile": "?0"})
	if res0.Device.Type != "desktop" {
		t.Fatalf("?0: expected desktop, got %s", res0.Device.Type)
	}
	res1 := p.Parse(ua, map[string]string{"Sec-CH-UA-Mobile": "?1"})
	if res1.Device.Type != "mobile" {
		t.Errorf("?1 after cached ?0: expected mobile, got %s (cache-key collision)", res1.Device.Type)
	}
}

// Android Chromium families ("Chrome Mobile", "Edge Mobile"...) must match the
// CH brands; previously the full-version-list was dead code for all mobile
// Chromium traffic — exactly where the UA version is frozen.
func TestFullVersionListMobileChrome(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36"
	headers := map[string]string{
		"Sec-CH-UA-Full-Version-List": `"Not)A;Brand";v="8.0.0.0", "Chromium";v="138.0.7204.63", "Google Chrome";v="138.0.7204.63"`,
	}

	res := p.Parse(ua, headers)
	if res.Browser.Version != "138.0.7204.63" {
		t.Errorf("Expected CH full version 138.0.7204.63, got %q (browser %q)", res.Browser.Version, res.Browser.Name)
	}
	if res.Browser.Major != "138" {
		t.Errorf("Expected Major 138, got %q", res.Browser.Major)
	}
	if res.Engine.Name == "Blink" && res.Engine.Version != "138.0.7204.63" {
		t.Errorf("Expected Blink version from Chromium brand, got %q", res.Engine.Version)
	}
}

// Cubot is a phone brand, not a bot; the left-boundary gap flagged every
// Cubot owner as a bot.
func TestCubotPhoneIsNotBot(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Linux; Android 13; CUBOT KINGKONG 9 Build/TP1A.220624.014) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Mobile Safari/537.36"
	res := p.Parse(ua, nil)
	if res.IsBot {
		t.Errorf("Cubot phone flagged as bot")
	}
	if res.Category != "mobile" {
		t.Errorf("Expected category mobile for Cubot phone, got %q", res.Category)
	}
}

// AI fetchers without a generic bot token must still be bots.
func TestAIUserFetchersAreBots(t *testing.T) {
	p := newTestParser(t, 0)

	for _, ua := range []string{
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Claude-User/1.0; +Claude-User@anthropic.com)",
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)",
	} {
		res := p.Parse(ua, nil)
		if !res.IsAICrawler || !res.IsBot || res.Category != "bot" {
			t.Errorf("UA %q: IsAICrawler=%v IsBot=%v Category=%q; want true/true/bot",
				ua, res.IsAICrawler, res.IsBot, res.Category)
		}
	}
}

// CH platform names must normalize to the uap-core vocabulary so the same OS
// reports one name with and without hints.
func TestPlatformNameNormalization(t *testing.T) {
	p := newTestParser(t, 0)

	macUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

	noHints := p.Parse(macUA, nil)
	withHints := p.Parse(macUA, map[string]string{
		"Sec-CH-UA-Platform":         `"macOS"`,
		"Sec-CH-UA-Platform-Version": `"14.5.0"`,
	})
	if noHints.OS.Name != withHints.OS.Name {
		t.Errorf("OS name differs with/without hints: %q vs %q", noHints.OS.Name, withHints.OS.Name)
	}
	if withHints.OS.Name != "Mac OS X" {
		t.Errorf("Expected canonical 'Mac OS X', got %q", withHints.OS.Name)
	}
	if withHints.OS.Version != "14.5.0" {
		t.Errorf("Expected real macOS version from CH (frozen UA says 10.15.7), got %q", withHints.OS.Version)
	}

	// "Unknown" must never clobber a good UA-derived OS name.
	winUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	unk := p.Parse(winUA, map[string]string{"Sec-CH-UA-Platform": `"Unknown"`})
	if unk.OS.Name != "Windows" {
		t.Errorf("Platform 'Unknown' clobbered OS name: got %q", unk.OS.Name)
	}
}

// Android platform-version carries the real OS version (UA is frozen at 10).
func TestAndroidVersionFromClientHints(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36"
	res := p.Parse(ua, map[string]string{
		"Sec-CH-UA-Platform":         `"Android"`,
		"Sec-CH-UA-Platform-Version": `"16.0.0"`,
	})
	if res.OS.Name != "Android" {
		t.Errorf("Expected Android, got %q", res.OS.Name)
	}
	if res.OS.Version != "16.0.0" {
		t.Errorf("Expected Android 16.0.0 from CH, got %q", res.OS.Version)
	}
}

// The frozen Android model placeholder "K" is never a real device.
func TestFrozenModelKDiscarded(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36"

	plain := p.Parse(ua, nil)
	if plain.Device.Model == "K" {
		t.Errorf("Frozen placeholder model 'K' leaked into the result")
	}

	withModel := p.Parse(ua, map[string]string{"Sec-CH-UA-Model": `"Pixel 9 Pro"`})
	if withModel.Device.Model != "Pixel 9 Pro" {
		t.Errorf("Expected CH model, got %q", withModel.Device.Model)
	}
}

// Brave is UA-identical to Chrome; only the sec-ch-ua brand reveals it.
func TestBrandCorrectionBrave(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	res := p.Parse(ua, map[string]string{
		"Sec-CH-UA": `"Brave";v="126", "Chromium";v="126", "Not_A Brand";v="24"`,
	})
	if res.Browser.Name != "Brave" {
		t.Errorf("Expected Brave from sec-ch-ua brand, got %q", res.Browser.Name)
	}
	if res.Browser.Major != "126" {
		t.Errorf("Expected Major 126, got %q", res.Browser.Major)
	}
}

// Opera GX declares its own brand; the version must be taken and the name
// upgraded to the more specific product.
func TestBrandOperaGX(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 OPR/111.0.0.0"
	res := p.Parse(ua, map[string]string{
		"Sec-CH-UA-Full-Version-List": `"Opera GX";v="111.0.5168.55", "Chromium";v="125.0.6422.60", "Not.A/Brand";v="8.0.0.0"`,
	})
	if res.Browser.Name != "Opera GX" {
		t.Errorf("Expected Opera GX, got %q", res.Browser.Name)
	}
	if res.Browser.Version != "111.0.5168.55" {
		t.Errorf("Expected Opera GX version from CH, got %q", res.Browser.Version)
	}
	if res.Engine.Name == "Blink" && res.Engine.Version != "125.0.6422.60" {
		t.Errorf("Expected Blink version from Chromium entry, got %q", res.Engine.Version)
	}
}

// Blink engine version comes from the Chrome/ token, not the product version.
func TestBlinkVersionFromChromeToken(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Linux; Android 13; SM-S921B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36"
	res := p.Parse(ua, nil)
	if res.Engine.Name != "Blink" {
		t.Fatalf("Expected Blink for Samsung Internet, got %q", res.Engine.Name)
	}
	if res.Engine.Version != "115.0.0.0" {
		t.Errorf("Expected Blink 115.0.0.0 (Chrome token), got %q", res.Engine.Version)
	}
}

// On iOS every browser is WebKit; EdgiOS/CriOS must never report Blink.
func TestIOSBrowsersAreWebKit(t *testing.T) {
	p := newTestParser(t, 0)

	for _, ua := range []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) EdgiOS/125.2535.60 Version/17.0 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.6478.54 Mobile/15E148 Safari/604.1",
	} {
		res := p.Parse(ua, nil)
		if res.Engine.Name != "WebKit" {
			t.Errorf("UA %q: expected WebKit, got %q", ua, res.Engine.Name)
		}
	}
}

// Low-entropy sec-ch-ua (sent by default) corrects a spoofed/stale UA major.
func TestLowEntropyMajorCorrection(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/110.0.0.0 Safari/537.36"
	res := p.Parse(ua, map[string]string{
		"Sec-CH-UA": `"Google Chrome";v="138", "Chromium";v="138", "Not;A=Brand";v="99"`,
	})
	if res.Browser.Major != "138" {
		t.Errorf("Expected CH-corrected major 138, got %q", res.Browser.Major)
	}
}

// Form-Factors is the most specific device signal (Chrome 124+).
func TestFormFactors(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	cases := []struct {
		ff       string
		devType  string
		category string
	}{
		{`"Tablet"`, "tablet", "mobile"},
		{`"XR"`, "xr", "xr"},
		{`"Watch"`, "wearable", "wearable"},
		{`"Automotive"`, "automotive", "automotive"},
	}
	for _, tc := range cases {
		res := p.Parse(ua, map[string]string{"Sec-CH-UA-Form-Factors": tc.ff})
		if res.Device.Type != tc.devType {
			t.Errorf("Form-Factors %s: expected device %q, got %q", tc.ff, tc.devType, res.Device.Type)
		}
		if res.Category != tc.category {
			t.Errorf("Form-Factors %s: expected category %q, got %q", tc.ff, tc.category, res.Category)
		}
	}
}

// Tizen powers both watches and TVs; a Galaxy Watch must not be a TV.
func TestTizenWatchIsWearable(t *testing.T) {
	p := newTestParser(t, 0)

	ua := "Mozilla/5.0 (Linux; Tizen 4.0; SAMSUNG Galaxy Watch) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/2.2 Mobile Safari/537.36"
	res := p.Parse(ua, nil)
	if res.Device.Type != "wearable" {
		t.Errorf("Expected wearable for Galaxy Watch, got %q", res.Device.Type)
	}

	// A real Tizen TV keeps working.
	tv := p.Parse("Mozilla/5.0 (SMART-TV; Linux; Tizen 5.0) AppleWebKit/537.36", nil)
	if tv.Device.Type != "tv" {
		t.Errorf("Expected tv for Tizen SMART-TV, got %q", tv.Device.Type)
	}
}

// Common HTTP clients must be typed as libraries.
func TestLibraryDetection(t *testing.T) {
	p := newTestParser(t, 0)

	for _, ua := range []string{
		"python-requests/2.32.3",
		"PostmanRuntime/7.39.0",
		"okhttp/4.12.0",
		"axios/1.7.2",
		"curl/8.9.1",
	} {
		res := p.Parse(ua, nil)
		if res.Browser.Type != "library" {
			t.Errorf("UA %q: expected type library, got %q", ua, res.Browser.Type)
		}
	}
}

// Grease brands in every registered punctuation variant must be skipped.
func TestGreaseBrandFiltering(t *testing.T) {
	for _, grease := range []string{
		"Not;A=Brand", "Not_A Brand", "Not/A)Brand", "Not.A/Brand",
		"Not(A:Brand", "Not-A.Brand", "Not?A_Brand", " Not A;Brand",
	} {
		if !isGreaseBrand(grease) {
			t.Errorf("Grease brand %q not filtered", grease)
		}
	}
	for _, real := range []string{"Google Chrome", "Brave", "Opera GX", "Microsoft Edge"} {
		if isGreaseBrand(real) {
			t.Errorf("Real brand %q misdetected as grease", real)
		}
	}

	// Quoted separators inside grease brands must not break the list parse.
	brands := parseBrandList(`"Not;A=Brand";v="8", "Chromium";v="138.0.7204.63", "Google Chrome";v="138.0.7204.63"`)
	if len(brands) != 2 {
		t.Fatalf("Expected 2 brands after grease filtering, got %d (%v)", len(brands), brands)
	}
	if brands[0].name != "Chromium" || brands[0].version != "138.0.7204.63" {
		t.Errorf("Unexpected first brand: %+v", brands[0])
	}
}
