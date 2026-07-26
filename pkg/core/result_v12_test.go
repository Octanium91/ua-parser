package core

import "testing"

func TestConvenienceFlags(t *testing.T) {
	p := newTestParser(t, 0)

	desktop := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
	if !desktop.IsDesktop || desktop.IsMobile || !desktop.IsChromeFamily {
		t.Errorf("desktop Chrome flags: desktop=%v mobile=%v chromeFamily=%v", desktop.IsDesktop, desktop.IsMobile, desktop.IsChromeFamily)
	}
	if desktop.IsTouchCapable {
		t.Error("desktop must not be touch-capable without a touch signal")
	}

	mobile := p.Parse("Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36", nil)
	if !mobile.IsMobile || !mobile.IsTouchCapable || mobile.IsDesktop {
		t.Errorf("mobile flags: mobile=%v touch=%v desktop=%v", mobile.IsMobile, mobile.IsTouchCapable, mobile.IsDesktop)
	}

	firefox := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0", nil)
	if firefox.IsChromeFamily {
		t.Error("Firefox must not be flagged chrome-family")
	}
}

func TestIsAppleSilicon(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	if p.Parse(ua, nil).IsAppleSilicon {
		t.Error("without arm64 evidence, is_apple_silicon must be false")
	}
	res := p.ParseFull(ua, nil, &Signals{WebGLRenderer: "ANGLE (Apple, ANGLE Metal Renderer: Apple M2 Pro)"})
	if !res.IsAppleSilicon {
		t.Errorf("Mac + Apple M renderer must be apple-silicon (arch=%q)", res.CPU.Architecture)
	}
}

func TestAutomationDetection(t *testing.T) {
	p := newTestParser(t, 0)

	hl := p.Parse("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/125.0.0.0 Safari/537.36", nil)
	if !hl.Automation.Headless {
		t.Error("HeadlessChrome must set automation.headless")
	}

	el := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) MyApp/1.0 Chrome/120.0.0.0 Electron/28.0.0 Safari/537.36", nil)
	if !el.Automation.Electron {
		t.Error("Electron token must set automation.electron")
	}

	normal := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
	if normal.Automation.Headless || normal.Automation.Electron {
		t.Error("normal Chrome must not be flagged as automation")
	}

	wd := p.ParseFull("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil, &Signals{Webdriver: true})
	if !wd.Automation.Webdriver {
		t.Error("navigator.webdriver signal must set automation.webdriver")
	}
}

func TestIntegritySpoofDetection(t *testing.T) {
	p := newTestParser(t, 0)

	// Consistent Chrome on Windows — no spoof.
	ok := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		map[string]string{"Sec-CH-UA-Platform": `"Windows"`, "Sec-CH-UA": `"Chromium";v="126", "Google Chrome";v="126"`})
	if ok.Integrity.Spoofed {
		t.Errorf("consistent client flagged spoofed: %v", ok.Integrity.Reasons)
	}
	if ok.Integrity.Reasons == nil {
		t.Error("Reasons must serialize as [] (non-nil), not null")
	}

	// UA says Windows, CH says Android → platform mismatch.
	mm := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		map[string]string{"Sec-CH-UA-Platform": `"Android"`})
	if !mm.Integrity.Spoofed || !containsStr(mm.Integrity.Reasons, "ua-platform≠ch-platform") {
		t.Errorf("platform mismatch not flagged: %+v", mm.Integrity)
	}

	// Sec-CH-UA present on a Gecko (Firefox) engine — only Chromium sends it.
	forged := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0",
		map[string]string{"Sec-CH-UA": `"Chromium";v="126", "Google Chrome";v="126"`})
	if !containsStr(forged.Integrity.Reasons, "sec-ch-ua-on-non-chromium") {
		t.Errorf("Sec-CH-UA on non-Chromium not flagged: %+v", forged.Integrity)
	}
}

func TestSecurityPayloadInUA(t *testing.T) {
	p := newTestParser(t, 0)

	scan := p.Parse("sqlmap/1.7#stable (https://sqlmap.org)", nil)
	if !scan.Security.Suspicious || scan.Security.Category != "scanner" {
		t.Errorf("sqlmap not flagged: %+v", scan.Security)
	}

	clean := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
	if clean.Security.Suspicious {
		t.Errorf("clean UA flagged suspicious: %+v", clean.Security)
	}
}

func TestOSVersionLabelsAndDetection(t *testing.T) {
	p := newTestParser(t, 0)

	// Windows 11 via CH: normalized "11", raw "15.0.0", label "Windows 11".
	win := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		map[string]string{"Sec-CH-UA-Platform": `"Windows"`, "Sec-CH-UA-Platform-Version": `"15.0.0"`})
	if win.OS.Version != "11" || win.OS.VersionRaw != "15.0.0" || win.OS.VersionName != "Windows 11" {
		t.Errorf("windows os = %+v, want version 11 / raw 15.0.0 / name 'Windows 11'", win.OS)
	}
	if !win.Detection.ClientHintsUsed || !win.Detection.HighEntropy {
		t.Errorf("detection = %+v, want CH used + high entropy", win.Detection)
	}

	// macOS codename.
	mac := p.Parse("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		map[string]string{"Sec-CH-UA-Platform": `"macOS"`, "Sec-CH-UA-Platform-Version": `"14.5.0"`})
	if mac.OS.VersionName != "macOS Sonoma" {
		t.Errorf("macOS version_name = %q, want 'macOS Sonoma'", mac.OS.VersionName)
	}

	// No CH → detection false, raw falls back to UA version.
	noch := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0", nil)
	if noch.Detection.ClientHintsUsed {
		t.Error("no-CH parse must report client_hints_used=false")
	}
}

func TestClassHashStableAndDistinct(t *testing.T) {
	p := newTestParser(t, 0)
	a := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	b := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"

	h1 := p.Parse(a, nil).ClassHash
	h2 := p.Parse(a, nil).ClassHash
	h3 := p.Parse(b, nil).ClassHash
	if h1 == "" || h1 != h2 {
		t.Errorf("class_hash not stable: %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Error("different client classes must not share a class_hash")
	}
}

func TestDetectionSignalsUsed(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

	bare := p.Parse(ua, nil)
	if bare.Detection.SignalsUsed || bare.Detection.ClientHintsUsed {
		t.Errorf("UA-only parse must report no signals/CH: %+v", bare.Detection)
	}

	withSig := p.ParseFull(ua, nil, &Signals{MaxTouchPoints: 10})
	if !withSig.Detection.SignalsUsed {
		t.Error("signals block must set detection.signals_used")
	}

	// An empty signals block counts as "no signals".
	empty := p.ParseFull(ua, nil, &Signals{})
	if empty.Detection.SignalsUsed {
		t.Error("empty signals block must not set signals_used")
	}
}

func TestResultVersionStamped(t *testing.T) {
	p := newTestParser(t, 0)
	res := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", nil)
	if res.ResultVersion != ResultSchemaVersion {
		t.Errorf("result_version = %q, want %q", res.ResultVersion, ResultSchemaVersion)
	}
	if ResultSchemaVersion == "" {
		t.Error("ResultSchemaVersion must not be empty")
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
