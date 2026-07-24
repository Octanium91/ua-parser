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
	"syscall/js"

	"github.com/Octanium91/ua-parser/pkg/core"
)

var parser *core.Parser

// initParser creates the parser, applying the optional config JSON on top of
// browser-friendly defaults (no auto-update, LRU cache of 1000 entries).
func initParser(configJSON string) error {
	cfg := core.Config{
		LRUCacheSize:      1000,
		DisableAutoUpdate: true, // browsers cannot fetch regex updates cross-origin by default
	}

	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			// If invalid JSON, we'll just use the default config instead of failing hard
			cfg = core.Config{
				LRUCacheSize:      1000,
				DisableAutoUpdate: true,
			}
		}
	}

	p, err := core.New(cfg)
	if err != nil {
		return err
	}
	parser = p
	return nil
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
	}

	// Try to parse as JSON payload (which allows passing headers).
	// Fallback to treating the entire input as a raw User-Agent string.
	if err := json.Unmarshal([]byte(input), &payload); err != nil || payload.UA == "" {
		payload.UA = input
		payload.Headers = nil
	}

	result := parser.Parse(payload.UA, payload.Headers)
	resBytes, err := json.Marshal(result)
	if err != nil {
		return errorJSON("Failed to marshal result: " + err.Error())
	}
	return string(resBytes)
}

func main() {
	js.Global().Set("initUA", js.FuncOf(jsInitUA))
	js.Global().Set("parseUA", js.FuncOf(jsParseUA))

	// Block forever so the Go runtime (and the registered callbacks) stay
	// alive after main would otherwise return.
	select {}
}
