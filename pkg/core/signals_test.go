package core

import "testing"

// iPad unmask: the single highest-value signal rule — iPadOS Safari in
// desktop mode is UA-indistinguishable from an Intel Mac, and Safari sends
// no Client Hints whatsoever.
func TestSignalsIPadUnmask(t *testing.T) {
	p := newTestParser(t, 0)
	macUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"

	res := p.ParseFull(macUA, nil, &Signals{MaxTouchPoints: 5, Platform: "MacIntel"})
	if res.OS.Name != "iPadOS" || res.OS.Platform != "ios" {
		t.Errorf("os = %q/%q, want iPadOS/ios", res.OS.Name, res.OS.Platform)
	}
	if res.Device.Type != "tablet" || res.Device.Model != "iPad" || res.Device.Vendor != "Apple" {
		t.Errorf("device = %+v, want Apple iPad tablet", res.Device)
	}
	if res.Category != "mobile" {
		t.Errorf("category = %q, want mobile", res.Category)
	}

	// A real Mac (0 touch points) must stay a Mac.
	res = p.ParseFull(macUA, nil, &Signals{MaxTouchPoints: 0, Platform: "MacIntel"})
	if res.OS.Name != "Mac OS X" || res.Device.Type != "desktop" {
		t.Errorf("real Mac misclassified: os %q device %q", res.OS.Name, res.Device.Type)
	}
}

// Apple Silicon fallback: frozen Mac UA + Chromium WebGL renderer naming an
// M-series GPU → arm64; Sec-CH-UA-Arch, when present, wins.
func TestSignalsAppleSilicon(t *testing.T) {
	p := newTestParser(t, 0)
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	sig := &Signals{WebGLVendor: "Apple", WebGLRenderer: "ANGLE (Apple, ANGLE Metal Renderer: Apple M2 Pro, Unspecified Version)"}

	res := p.ParseFull(ua, nil, sig)
	if res.CPU.Architecture != "arm64" || res.CPU.Bitness != "64" {
		t.Errorf("cpu = %+v, want arm64/64", res.CPU)
	}
	if res.GPU == nil || res.GPU.Renderer == "" {
		t.Error("gpu object not populated from the WebGL signal")
	}

	// CH arch answered — the signal must not override it.
	headers := map[string]string{"Sec-CH-UA-Arch": `"x86"`, "Sec-CH-UA-Bitness": `"64"`}
	res = p.ParseFull(ua, headers, sig)
	if res.CPU.Architecture != "amd64" {
		t.Errorf("cpu.arch = %q, want amd64 (CH must win over the signal)", res.CPU.Architecture)
	}
}

// Android SoC GPU passthrough and the Linux-desktop tablet assist.
func TestSignalsGPUAndTabletAssist(t *testing.T) {
	p := newTestParser(t, 0)

	res := p.ParseFull(
		"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36",
		nil,
		&Signals{WebGLVendor: "Qualcomm", WebGLRenderer: "Adreno (TM) 740"},
	)
	if res.GPU == nil || res.GPU.Renderer != "Adreno (TM) 740" {
		t.Errorf("gpu = %+v, want Adreno renderer", res.GPU)
	}

	// Desktop-mode Android tablet: Linux desktop UA + multi-touch.
	res = p.ParseFull(
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		nil,
		&Signals{MaxTouchPoints: 5},
	)
	if res.Device.Type != "tablet" {
		t.Errorf("device.type = %q, want tablet (desktop-mode Android)", res.Device.Type)
	}

	// A Windows touch laptop must remain a desktop.
	res = p.ParseFull(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		nil,
		&Signals{MaxTouchPoints: 10},
	)
	if res.Device.Type != "desktop" {
		t.Errorf("device.type = %q, want desktop (Windows touch laptop)", res.Device.Type)
	}
}

// Two requests differing only in signals must not collide in the cache.
func TestCacheKeyIncludesSignals(t *testing.T) {
	p := newTestParser(t, 10)
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"

	mac := p.ParseFull(ua, nil, nil)
	ipad := p.ParseFull(ua, nil, &Signals{MaxTouchPoints: 5})
	if mac.Device.Type == ipad.Device.Type {
		t.Errorf("cache collision: %q == %q despite differing signals", mac.Device.Type, ipad.Device.Type)
	}
}
