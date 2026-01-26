package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/Octanium91/ua-parser/pkg/core"
)

var parser *core.Parser

func initUA(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "Missing configuration"
	}

	configJSON := args[0].String()
	var cfg core.Config
	if configJSON != "" && configJSON != "undefined" && configJSON != "null" {
		err := json.Unmarshal([]byte(configJSON), &cfg)
		if err != nil {
			return "Failed to unmarshal config: " + err.Error()
		}
	}

	if cfg.LRUCacheSize == 0 {
		cfg.LRUCacheSize = 1000
	}

	// For Wasm, we might want to disable auto-update by default if it's running in a restricted environment,
	// but we'll follow the provided config.
	p, err := core.New(cfg)
	if err != nil {
		return "Failed to initialize parser: " + err.Error()
	}
	parser = p
	return nil
}

func parseUA(this js.Value, args []js.Value) interface{} {
	if parser == nil {
		return `{"error": "Parser not initialized"}`
	}

	if len(args) < 1 {
		return `{"error": "Missing payload"}`
	}

	payloadJSON := args[0].String()
	var payload struct {
		UA      string            `json:"ua"`
		Headers map[string]string `json:"headers"`
	}

	err := json.Unmarshal([]byte(payloadJSON), &payload)
	if err != nil {
		return `{"error": "Invalid payload: ` + err.Error() + `"}`
	}

	result := parser.Parse(payload.UA, payload.Headers)
	resBytes, err := json.Marshal(result)
	if err != nil {
		return `{"error": "Failed to marshal result"}`
	}

	return string(resBytes)
}

func main() {
	c := make(chan struct{}, 0)

	js.Global().Set("initUA", js.FuncOf(initUA))
	js.Global().Set("parseUA", js.FuncOf(parseUA))

	<-c
}
