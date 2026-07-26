# Universal User-Agent Parser - Go Client

This is the official Go client wrapper for the high-performance Universal User-Agent Parser.

## Installation

```bash
go get github.com/Octanium91/ua-parser/clients/go
```

Note: Since this is a submodule of the main repository, you can also just import it if you are already using the main module.

## Usage

```go
package main

import (
	"fmt"
	"github.com/Octanium91/ua-parser/clients/go"
)

func main() {
	cfg := uaparser.Config{
		DisableAutoUpdate: false,
		LRUCacheSize:      1000,
		// Correction layer (hot-updated); optional overrides:
		// CorrectionsURL:           "https://example.com/corrections.yaml",
		// DisableCorrectionsUpdate: false,
	}

	parser, err := uaparser.New(cfg)
	if err != nil {
		panic(err)
	}

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	headers := map[string]string{
		"Sec-CH-UA-Platform":         "\"Windows\"",
		"Sec-CH-UA-Platform-Version": "\"13.0.0\"",
	}

	result := parser.Parse(ua, headers)
	fmt.Printf("OS: %s %s (%s)\n", result.OS.Name, result.OS.Version, result.OS.Platform)
	fmt.Printf("Browser: %s %s\n", result.Browser.Name, result.Browser.Version)
	fmt.Printf("Device: %s / %s (%s)\n", result.Device.Vendor, result.Device.Model, result.Device.FormFactor)
	fmt.Printf("CPU: %s %s | frozen UA: %v\n", result.CPU.Architecture, result.CPU.Bitness, result.IsFrozenUA)
	if result.Bot != nil {
		fmt.Printf("Bot: %s (%s, %s)\n", result.Bot.Name, result.Bot.Category, result.Bot.Vendor)
	}
}
```

The Go client re-exports the core types (`uaparser.Result`, `uaparser.Signals`, …), so it always exposes the full result and you never import the internal `core` package directly — see [Result fields](#result-fields) below.

## Browser signals (optional)

`ParseFull(ua, headers, *uaparser.Signals)` accepts browser-side evidence that UA and Client Hints can't provide (Safari/Firefox send no Client Hints). `Parse` is `ParseFull` with `nil` signals.

```go
result := parser.ParseFull(ua, headers, &uaparser.Signals{
    MaxTouchPoints: 5,            // unmasks iPads reporting a desktop (Mac) UA
    WebGLRenderer:  "Apple M2",   // Apple Silicon / Android SoC
})
```

Priority inside the engine: **Client Hints > signals > UA string**.

## Result fields

`Result` carries `Browser{Name,Version,Major,Type}`, `OS{Name,Version,Platform}`, `Device{Model,Vendor,Type,FormFactor}`, `CPU{Architecture,Bitness}`, `Engine{Name,Version}`, `Category`, `IsBot`, `IsAICrawler`, `IsFrozenUA`, `*Bot{Name,Category,Vendor}` (nil for humans), and `*GPU{Vendor,Renderer}` (nil unless a WebGL signal was supplied). See the [root README](../../README.md#example-response) for a full JSON example and field semantics.

## Forwarding headers from a real request

For maximum accuracy don't enumerate headers — copy the `User-Agent` plus **every** request header starting with `Sec-CH-` (and `X-Requested-With` if present) into the map:

```go
headers := make(map[string]string)
for name, vals := range r.Header {
    if lower := strings.ToLower(name); strings.HasPrefix(lower, "sec-ch-") || lower == "x-requested-with" {
        headers[lower] = vals[0]
    }
}
result := parser.Parse(r.Header.Get("User-Agent"), headers)
```

Pass values raw, quotes included. See the [backend forwarding guide](../../README.md#forwarding-headers-from-your-backend) and [Requesting Client Hints](../../README.md#requesting-client-hints) (`Accept-CH`) in the root README.

## Features

- **High Performance**: LRU caching and optimized regex matching.
- **Client Hints Support**: Accurate detection of Windows 11 and full browser versions.
- **Automatic Updates**: Background updates of regex patterns **and** the correction layer (can be disabled).
- **Correction Layer**: Declarative overrides for in-app browsers, vehicles, consoles and device vendors, hot-updated at runtime ([details](../../README.md#correction-layer)).
- **Rich Result**: canonical `OS.Platform`, `CPU.Bitness`, `Device.FormFactor`, `IsFrozenUA`, and a classified `Bot` object.
