//go:build js && wasm

package main

// Automatic browser-signal collection for the js/wasm client. Gathered once
// at init and merged into every parse whose payload did not supply its own
// headers/signals (an explicit payload always wins).
//
//   - navigator.userAgentData.getHighEntropyValues() → reconstructed
//     sec-ch-ua-* headers, so the WASM client needs no Accept-CH round-trip.
//   - maxTouchPoints / platform / screen / deviceMemory / hardwareConcurrency
//     → the Signals block (Safari/Firefox send no UA-CH; these are all the
//     evidence they offer).
//   - WebGL renderer probe → opt-in via {"collect_gpu": true} in initUA's
//     config (fingerprinting-adjacent, so off by default).

import (
	"fmt"
	"strings"
	"sync"
	"syscall/js"

	"github.com/Octanium91/ua-parser/pkg/core"
)

var collectMu sync.Mutex

// collectedHeaders holds sec-ch-ua-* headers reconstructed from
// navigator.userAgentData; nil until the async high-entropy promise resolves.
var collectedHeaders map[string]string

// collectedSignals holds the synchronously readable navigator/screen facts.
var collectedSignals *core.Signals

// collectBrowserSignals reads the synchronous signals immediately and kicks
// off the async high-entropy Client Hints request. Never fails: every probe
// is individually recovered.
func collectBrowserSignals(collectGPU bool) {
	defer func() { recover() }() // navigator/screen may be absent in workers

	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return
	}

	sig := &core.Signals{}
	if v := nav.Get("maxTouchPoints"); v.Type() == js.TypeNumber {
		sig.MaxTouchPoints = v.Int()
	}
	if v := nav.Get("platform"); v.Type() == js.TypeString {
		sig.Platform = v.String()
	}
	if v := nav.Get("deviceMemory"); v.Type() == js.TypeNumber {
		sig.DeviceMemory = v.Float()
	}
	if v := nav.Get("hardwareConcurrency"); v.Type() == js.TypeNumber {
		sig.HardwareConcurrency = v.Int()
	}
	if screen := js.Global().Get("screen"); screen.Truthy() {
		s := &core.ScreenInfo{DPR: 1}
		if v := screen.Get("width"); v.Type() == js.TypeNumber {
			s.W = v.Int()
		}
		if v := screen.Get("height"); v.Type() == js.TypeNumber {
			s.H = v.Int()
		}
		if v := js.Global().Get("devicePixelRatio"); v.Type() == js.TypeNumber {
			s.DPR = v.Float()
		}
		sig.Screen = s
	}
	if collectGPU {
		probeWebGL(sig)
	}

	collectMu.Lock()
	collectedSignals = sig
	collectMu.Unlock()

	collectHighEntropyHints(nav)
}

// probeWebGL reads the unmasked GPU renderer where the browser exposes it
// (full strings in Chromium; Safari serves a masked "Apple GPU").
func probeWebGL(sig *core.Signals) {
	defer func() { recover() }()

	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	canvas := doc.Call("createElement", "canvas")
	gl := canvas.Call("getContext", "webgl")
	if !gl.Truthy() {
		gl = canvas.Call("getContext", "experimental-webgl")
	}
	if !gl.Truthy() {
		return
	}
	ext := gl.Call("getExtension", "WEBGL_debug_renderer_info")
	if ext.Truthy() {
		sig.WebGLVendor = gl.Call("getParameter", ext.Get("UNMASKED_VENDOR_WEBGL")).String()
		sig.WebGLRenderer = gl.Call("getParameter", ext.Get("UNMASKED_RENDERER_WEBGL")).String()
	}
}

// collectHighEntropyHints asks userAgentData for the high-entropy values and
// reconstructs the equivalent sec-ch-ua-* headers when the promise resolves.
func collectHighEntropyHints(nav js.Value) {
	uad := nav.Get("userAgentData")
	if !uad.Truthy() {
		return // Safari/Firefox: no UA-CH at all
	}

	hints := []any{
		"platform", "platformVersion", "model", "architecture",
		"bitness", "fullVersionList", "formFactors",
	}
	promise := uad.Call("getHighEntropyValues", hints)

	var then js.Func
	then = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer then.Release()
		if len(args) == 0 {
			return nil
		}
		v := args[0]
		headers := make(map[string]string, 9)

		if brands := uad.Get("brands"); brands.Truthy() {
			headers["sec-ch-ua"] = brandListHeader(brands)
		}
		if mobile := uad.Get("mobile"); mobile.Type() == js.TypeBoolean {
			if mobile.Bool() {
				headers["sec-ch-ua-mobile"] = "?1"
			} else {
				headers["sec-ch-ua-mobile"] = "?0"
			}
		}
		setQuoted := func(header, field string) {
			if s := v.Get(field); s.Type() == js.TypeString && s.String() != "" {
				headers[header] = `"` + s.String() + `"`
			}
		}
		setQuoted("sec-ch-ua-platform", "platform")
		setQuoted("sec-ch-ua-platform-version", "platformVersion")
		setQuoted("sec-ch-ua-model", "model")
		setQuoted("sec-ch-ua-arch", "architecture")
		setQuoted("sec-ch-ua-bitness", "bitness")
		if fvl := v.Get("fullVersionList"); fvl.Truthy() {
			headers["sec-ch-ua-full-version-list"] = brandListHeader(fvl)
		}
		if ff := v.Get("formFactors"); ff.Truthy() && ff.Length() > 0 {
			parts := make([]string, 0, ff.Length())
			for i := 0; i < ff.Length(); i++ {
				parts = append(parts, `"`+ff.Index(i).String()+`"`)
			}
			headers["sec-ch-ua-form-factors"] = strings.Join(parts, ", ")
		}

		collectMu.Lock()
		collectedHeaders = headers
		collectMu.Unlock()
		return nil
	})
	promise.Call("then", then)
}

// brandListHeader serializes a userAgentData brands array into the on-wire
// Sec-CH-UA format: "Brand";v="ver", "Brand2";v="ver2".
func brandListHeader(brands js.Value) string {
	parts := make([]string, 0, brands.Length())
	for i := 0; i < brands.Length(); i++ {
		b := brands.Index(i)
		parts = append(parts, fmt.Sprintf(`"%s";v="%s"`, b.Get("brand").String(), b.Get("version").String()))
	}
	return strings.Join(parts, ", ")
}

// mergeCollected fills headers/signals the caller did not supply. Explicit
// payload values always win; collected sec-ch headers are only used when the
// payload carried none at all (mixing two sources could disagree).
func mergeCollected(headers map[string]string, signals *core.Signals) (map[string]string, *core.Signals) {
	collectMu.Lock()
	defer collectMu.Unlock()

	if len(headers) == 0 && collectedHeaders != nil {
		headers = collectedHeaders
	}
	if signals == nil && collectedSignals != nil {
		signals = collectedSignals
	}
	return headers, signals
}
