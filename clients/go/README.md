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
	fmt.Printf("OS: %s %s\n", result.OS.Name, result.OS.Version)
	fmt.Printf("Browser: %s %s\n", result.Browser.Name, result.Browser.Version)
}
```

## Features

- **High Performance**: LRU caching and optimized regex matching.
- **Client Hints Support**: Accurate detection of Windows 11 and full browser versions.
- **Automatic Updates**: Background updates of regex patterns (can be disabled).
