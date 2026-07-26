//go:build js && wasm

// This is the browser (GOOS=js GOARCH=wasm) build of the UA parser, executed
// via Go's wasm_exec.js runtime.
//
// JS ABI (consumed by clients/node/lib/index.js in its browser path):
//   - globalThis.initUA(configJson string) -> null on success, or an error
//     message string on failure (the caller treats any truthy value as an
//     error).
//   - globalThis.parseUA(payloadJson string) -> JSON string with the parse
//     result; failures are reported as a JSON string {"error": "..."}.
//
// The payload is {"ua": "...", "headers": {...}}; a plain (non-JSON) string
// is accepted as a raw User-Agent for convenience, mirroring the wasip1
// build. main blocks forever after registering the functions so the Go
// runtime stays alive for subsequent calls from JS.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"syscall/js"
	"time"

	"github.com/Octanium91/ua-parser/pkg/core"
)

var parser *core.Parser

// defaultCorrectionsURL mirrors pkg/core/updater_default.go (which is not
// compiled for wasm). raw.githubusercontent.com serves ACAO:* with a 5-minute
// TTL, so a browser page may fetch it cross-origin; high-traffic deployments
// should point corrections_url at their own origin/CDN.
const defaultCorrectionsURL = "https://raw.githubusercontent.com/Octanium91/ua-parser/main/pkg/core/resources/corrections.yaml"

// initParser creates the parser, applying the optional config JSON on top of
// browser-friendly defaults (no regex auto-update, LRU cache of 1000 entries).
// Correction rules ARE live in the browser: Go's net/http on js/wasm is
// backed by the Fetch API, so a non-blocking goroutine pulls corrections.yaml
// once at init (parse serves the embedded rules until the swap lands).
// browserConfig extends core.Config with wasmjs-only collection switches:
// automatic signal collection is on by default (navigator basics), the WebGL
// GPU probe is opt-in (fingerprinting-adjacent).
type browserConfig struct {
	core.Config
	CollectGPU              bool `json:"collect_gpu"`
	DisableSignalCollection bool `json:"disable_signal_collection"`
}

func initParser(configJSON string) error {
	cfg := browserConfig{Config: core.Config{
		LRUCacheSize:      1000,
		DisableAutoUpdate: true, // regex DB updates stay release-bound in the browser
	}}

	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			// If invalid JSON, we'll just use the default config instead of failing hard
			cfg = browserConfig{Config: core.Config{
				LRUCacheSize:      1000,
				DisableAutoUpdate: true,
			}}
		}
	}

	p, err := core.New(cfg.Config)
	if err != nil {
		return err
	}
	parser = p

	if !cfg.DisableCorrectionsUpdate {
		go fetchCorrections(p, cfg.CorrectionsURL)
	}
	if !cfg.DisableSignalCollection {
		collectBrowserSignals(cfg.CollectGPU)
	}
	return nil
}

// fetchCorrections pulls corrections.yaml once and hot-swaps the rule set.
// Failures are non-fatal: the embedded snapshot keeps serving.
func fetchCorrections(p *core.Parser, url string) {
	if url == "" {
		url = defaultCorrectionsURL
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("ua-parser: corrections fetch failed (embedded rules stay active): %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("ua-parser: corrections fetch status %d (embedded rules stay active)", resp.StatusCode)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		log.Printf("ua-parser: corrections read failed: %v", err)
		return
	}
	if err := p.ApplyCorrectionsYAML(data); err != nil {
		log.Printf("ua-parser: downloaded corrections rejected (keeping last good): %v", err)
	}
}

// errorJSON marshals an error message into the {"error": "..."} shape the JS
// client expects, with proper string escaping.
func errorJSON(msg string) string {
	b, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		return `{"error":"internal error"}`
	}
	return string(b)
}

// jsInitUA implements globalThis.initUA(configJson). Returns null on success
// or an error message string on failure.
func jsInitUA(this js.Value, args []js.Value) any {
	configJSON := ""
	if len(args) > 0 && args[0].Type() == js.TypeString {
		configJSON = args[0].String()
	}

	if err := initParser(configJSON); err != nil {
		return "Failed to initialize parser: " + err.Error()
	}
	return js.Null()
}

// jsParseUA implements globalThis.parseUA(payloadJson). Always returns a JSON
// string; errors are encoded as {"error": "..."}.
func jsParseUA(this js.Value, args []js.Value) any {
	if parser == nil {
		// Lazy-init with defaults, mirroring the wasip1 build.
		if err := initParser(""); err != nil {
			return errorJSON("Parser not initialized: " + err.Error())
		}
	}

	if len(args) == 0 || args[0].Type() != js.TypeString {
		return errorJSON("parseUA expects a JSON string payload")
	}
	input := args[0].String()

	var payload struct {
		UA      string            `json:"ua"`
		Headers map[string]string `json:"headers"`
		Signals *core.Signals     `json:"signals"`
	}

	// Try to parse as JSON payload (which allows passing headers).
	// Fallback to treating the entire input as a raw User-Agent string.
	if err := json.Unmarshal([]byte(input), &payload); err != nil || payload.UA == "" {
		payload.UA = input
		payload.Headers = nil
		payload.Signals = nil
	}

	// Fill in what the page did not supply from the auto-collected browser
	// evidence (userAgentData high-entropy hints + navigator signals).
	headers, signals := mergeCollected(payload.Headers, payload.Signals)

	result := parser.ParseFull(payload.UA, headers, signals)
	resBytes, err := json.Marshal(result)
	if err != nil {
		return errorJSON("Failed to marshal result: " + err.Error())
	}
	return string(resBytes)
}

// jsUpdateCorrections implements globalThis.updateCorrectionsUA(yaml string):
// manual rule push for long-lived SPAs (the automatic path is the one-shot
// fetch at init). Returns null on success or an error message string.
func jsUpdateCorrections(this js.Value, args []js.Value) any {
	if parser == nil {
		return "Parser not initialized"
	}
	if len(args) == 0 || args[0].Type() != js.TypeString {
		return "updateCorrectionsUA expects a YAML string"
	}
	if err := parser.ApplyCorrectionsYAML([]byte(args[0].String())); err != nil {
		return "Failed to apply corrections: " + err.Error()
	}
	return js.Null()
}

func main() {
	js.Global().Set("initUA", js.FuncOf(jsInitUA))
	js.Global().Set("parseUA", js.FuncOf(jsParseUA))
	js.Global().Set("updateCorrectionsUA", js.FuncOf(jsUpdateCorrections))

	// Block forever so the Go runtime (and the registered callbacks) stay
	// alive after main would otherwise return.
	select {}
}
