# Universal User-Agent Parser

A high-performance User-Agent parser written in Go, featuring Sec-CH-UA (Client Hints) support and automatic regex updates.

## Features

- **Three Modes of Operation**:
  - **Native Library**: Importable Go package.
  - **Microservice**: Ready-to-use HTTP REST API server.
  - **Multi-Language Support**: Official wrappers for **Python**, **Node.js**, and **Java** (located in `/clients`).
  - **Multi-Platform**: Native support for **linux/amd64**, **linux/arm64**, and **windows/amd64**.
- **Client Hints Priority**: Automatically uses `Sec-CH-UA` headers with **highest priority** for precise OS and device detection (e.g., distinguishing Windows 11 from Windows 10 where the UA string might be ambiguous).
- **Hot-Swap**: Background `regexes.yaml` updates without service interruption, with detailed logging for observability.
- **High Performance**: Optimized for low-latency processing using an LRU cache and efficient logic.
- **Embedded**: Core regex patterns are bundled into the binary using `go:embed`.
- **CI/CD**: Fully automated builds and multi-platform distribution (GitHub Packages, GHCR) via **GitHub Actions**.

## Client Libraries

We provide official wrappers for major languages that use the core shared library:

- **[Go](./clients/go)**: `go get github.com/Octanium91/ua-parser`
- **[Python](./clients/python)**: Download .whl from [GitHub Releases](https://github.com/octanium91/ua-parser/releases)
- **[Node.js](./clients/node)**: `@octanium91/ua-parser` (GitHub Packages)
- **[Java](./clients/java)**: `com.github.Octanium91:ua-parser` (JitPack, GitHub Packages)

### Package Registry Setup

For Node.js and Java, you must configure your package manager to find the packages. 

| Platform | Setup Requirement | Link |
|----------|-------------------|------|
| **Node.js** | Create `.npmrc` with GitHub registry | [Node.js Setup](./clients/node#installation) |
| **Java** | Configure GitHub repository | [Java Setup](./clients/java#installation) |
| **Python** | Manual download of `.whl` from Releases | [Python Setup](./clients/python#installation) |

---

## Go Library Usage

```go
import "github.com/Octanium91/ua-parser/clients/go"

cfg := uaparser.Config{
    DisableAutoUpdate: false,
    LRUCacheSize:      1000,
}

parser, _ := uaparser.New(cfg)

// Headers for Client Hints (optional)
headers := map[string]string{
    "Sec-CH-UA-Platform":         "Windows",
    "Sec-CH-UA-Platform-Version": "15.0.0",
}

result := parser.Parse("Mozilla/5.0...", headers)
fmt.Printf("OS: %s %s\n", result.OS.Name, result.OS.Version)
fmt.Printf("Browser: %s (%s)\n", result.Browser.Name, result.Browser.Version)
fmt.Printf("Engine: %s %s\n", result.Engine.Name, result.Engine.Version)
fmt.Printf("Category: %s\n", result.Category)
fmt.Printf("Is Bot: %v (AI: %v)\n", result.IsBot, result.IsAICrawler)
```

## Supported Client Hints Headers

To achieve high accuracy (especially for Windows 11 and full browser versions), it is recommended to pass the following headers:

| Header | Description | Impact |
|--------|-------------|--------|
| `Sec-CH-UA-Platform` | Operating system name | Accurate OS detection |
| `Sec-CH-UA-Platform-Version` | Operating system version | Distinguishes Windows 11 from 10 |
| `Sec-CH-UA-Model` | Device model name | Precise device identification |
| `Sec-CH-UA-Arch` | CPU architecture | Architecture detection (e.g., arm64) |
| `Sec-CH-UA-Mobile` | Mobile device flag | Improves category detection |
| `Sec-CH-UA-Full-Version-List` | Full browser version list | Provides exact version (e.g., 120.0.6099.129) |

## REST API Server

### Running Locally

To run the server locally without Docker:

```bash
# Generate required JSON resources
go generate ./pkg/core/...

# Start the server
go run ./cmd/server/main.go
```

### Running with Docker

```bash
docker build -t ua-parser .
docker run -p 8080:8080 ua-parser
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `UA_PORT` | Server port | `8080` |
| `UA_ROUTE_PATH` | API route path | `/` |
| `UA_DISABLE_UPDATE` | Disable auto-updates | `false` |
| `UA_CACHE_SIZE` | LRU cache size | `1000` |
| `UA_UPDATE_URL` | Remote URL for `regexes.yaml` | `https://raw.githubusercontent.com/ua-parser/uap-core/master/regexes.yaml` |
| `UA_UPDATE_INTERVAL` | Background update check interval | `24h` |

### Example Request

```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{
    "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
    "headers": {
      "Sec-CH-UA-Platform": "\"Windows\"",
      "Sec-CH-UA-Platform-Version": "\"13.0.0\"",
      "Sec-CH-UA-Full-Version-List": "\"Chromium\";v=\"119.0.6045.105\", \"Google Chrome\";v=\"119.0.6045.105\""
    }
  }'
```

### Example Response

```json
{
  "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ... Chrome/119.0.0.0 ...",
  "browser": {
    "name": "Chrome",
    "version": "119.0.6045.105",
    "major": "119",
    "type": "browser"
  },
  "os": {
    "name": "Windows",
    "version": "11"
  },
  "device": {
    "model": "",
    "vendor": "",
    "type": "desktop"
  },
  "cpu": {
    "architecture": "amd64"
  },
  "engine": {
    "name": "Blink",
    "version": "119.0.6045.105"
  },
  "category": "desktop",
  "is_bot": false,
  "is_ai_crawler": false
}
```

## Bot & AI Crawler Detection

The parser includes a dedicated logic to detect common bots and AI-related crawlers:
- **General Bots**: Googlebot, Bingbot, YandexBot, etc.
- **AI Crawlers**: GPTBot, ClaudeBot, PerplexityBot, Google-Extended, and more.
- **Categorization**: Automatically sets `Category: "bot"` and `Browser.Type: "bot"` for identified automated agents.

## Shared Library (C-FFI)

The library can be compiled into a shared library for use with other languages via FFI. Pre-compiled binaries are available in GitHub Releases.

- **Linux**: `libua-parser-linux-amd64.so`, `libua-parser-linux-arm64.so`
- **Windows**: `ua-parser-windows-amd64.dll`
- **macOS**: `libua-parser-darwin-amd64.dylib`, `libua-parser-darwin-arm64.dylib`

These files are the **required drivers** for integrations. Note that Python and Node.js packages already bundle these drivers automatically for all supported architectures.

### Exported Functions:
- `Init(configJSON)` — Initializes the parser.
- `Parse(payloadJSON)` — Parses data (returns JSON string).
- `FreeString(ptr)` — Frees memory allocated for strings.

## Project Structure

- `/pkg/core` — Parser core (logic, cache, updater).
- `/cmd/server` — Entry point for the HTTP server.
- `/cmd/cshared` — Wrapper for compiling into a shared library.
- `/pkg/core/resources` — Bundled regex patterns.
