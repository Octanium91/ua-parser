# Universal User-Agent Parser

A high-performance User-Agent parser written in Go, featuring Sec-CH-UA (Client Hints) support and automatic regex updates.

## Features

- **Three Modes of Operation**:
  - **Native Library**: Importable Go package.
  - **Microservice**: Ready-to-use HTTP REST API server.
  - **Multi-Language Support**: Official wrappers for **Python**, **Node.js**, and **Java** (located in `/clients`).
  - **Multi-Platform**: Native support for **linux/amd64**, **linux/arm64** (glibc and musl artifacts), **windows/amd64**, **macOS (amd64/arm64)**, plus **WebAssembly** builds (WASI reactor and browser js/wasm).
- **Graceful Degradation (Java & Node.js)**: Smart client architecture that attempts to load the ultra-fast native driver (JNA / koffi), but transparently falls back to a bundled WebAssembly engine if the native library cannot be loaded (e.g., on Alpine Linux).
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

### Support Matrix

| Environment | Go | Java | Node.js | Python | REST Server |
|---|---|---|---|---|---|
| **Linux glibc** (amd64/arm64) | ✅ native | ✅ native (JNA) | ✅ native (koffi) | ✅ native (ctypes) | ✅ |
| **Alpine / musl** (amd64/arm64) | ✅ native | ✅ WASM fallback¹ | ✅ WASM fallback¹ | ⚠️ clear error² | ✅ |
| **Windows** (amd64) | ✅ native | ✅ native | ✅ native | ✅ native | ✅ |
| **macOS** (amd64/arm64) | ✅ native | ✅ native | ✅ native | ✅ native | — |
| **Browser** | — | — | ✅ js/wasm | — | — |

¹ Automatic — native `dlopen` of Go libraries is impossible on musl until the Go toolchain fix ([golang/go#54805](https://github.com/golang/go/issues/54805), expected Go 1.27+); after that, rebuilt releases load natively with no client changes.
² Python has no WASM fallback; on Alpine it raises a descriptive error suggesting the REST server, a glibc image, or the Java/Node clients.

### Package Registry Setup

Each client has an **auth-free** installation path; the GitHub Packages registry (npm/Maven) additionally requires a Personal Access Token, since GitHub Packages authenticates every install even for public packages.

| Platform    | Auth-free path | Also on (needs token) | Link |
|-------------|----------------|-----------------------|------|
| **Go**      | `go get github.com/Octanium91/ua-parser` | — | [Go Setup](./clients/go) |
| **Node.js** | npm tarball attached to Releases | GitHub Packages (npm, `read:packages` token) | [Node.js Setup](./clients/node#installation) |
| **Java**    | JitPack (`v`-prefixed tag) | GitHub Packages (Maven, token) | [Java Setup](./clients/java#installation) |
| **Python**  | `.whl` from Releases | — | [Python Setup](./clients/python#installation) |

### Hybrid Execution (Java)

Our Java client requires **Java 11 or higher**.

#### Performance via Go Core
Our Java client is designed to provide a significantly lower memory footprint and better performance compared to pure Java alternatives by leveraging a high-performance core written in Go.

#### Graceful Degradation (Native + WASM)
1. **Primary Route (Native)**: By default, the client uses **JNA** to load a native shared library (`.so`, `.dll`, or `.dylib`) for glibc-based Linux, Windows, or macOS. This provides maximum throughput and minimal overhead.
   - **Linux Compatibility**: Native libraries are compiled against **GLIBC 2.31** (Debian 11) to ensure compatibility with a wide range of distributions, including Amazon Linux 2023, Debian 11+, RHEL 8+, and Ubuntu 20.04+.
2. **Fallback Route (WASM)**: If the native library fails to load (e.g., on **Alpine Linux** using `musl libc`, or older systems with outdated GLIBC), the client will not crash with `UnsatisfiedLinkError`. Instead, it will log a **WARN** and transparently switch to an embedded **WebAssembly** engine (Chicory, pure JVM — the WASM module is compiled to JVM bytecode at startup). This ensures compatibility across all environments where Java can run.

> [!NOTE]
> **Performance Note on WASM Mode:** The first initialization of the WASM engine takes a few seconds (~9s measured on Alpine) while the module is compiled to JVM bytecode; parsing is then fast and LRU-cached. Check the active mode via `parser.getBackendName()`.

> [!IMPORTANT]
> **⚠️ Alpine Linux Users:** Native loading of Go shared libraries on musl is currently **impossible at the toolchain level** ([golang/go#54805](https://github.com/golang/go/issues/54805)) — this is not fixable with `gcompat` (do **not** install it for this purpose), `LD_PRELOAD`, or build flags. The WASM fallback is the supported mode on Alpine and engages automatically. For native-level throughput on Alpine, run the REST server container (`ghcr.io/octanium91/ua-parser`) next to your app or use a glibc-based image. Once the upstream Go fix ships (expected Go 1.27+), rebuilt releases will load natively on Alpine with no client changes.

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
| `Sec-CH-UA-Bitness` | CPU architecture bitness | Architecture bitness (e.g., 64) |

### Missing Headers
If specific Client Hints are unavailable (e.g., browser policy or HTTP connection), the parser automatically falls back to standard regex-based parsing of the `User-Agent` string.

### Requesting Client Hints
To receive high-entropy Client Hints (like `Sec-CH-UA-Platform-Version` or `Sec-CH-UA-Model`), your server must explicitly request them using the `Accept-CH` response header:

```http
Accept-CH: Sec-CH-UA-Platform-Version, Sec-CH-UA-Model, Sec-CH-UA-Full-Version-List, Sec-CH-UA-Arch, Sec-CH-UA-Bitness
```

**Note:** Browsers will only send these headers on subsequent requests after receiving the `Accept-CH` header, and only over **HTTPS**. To ensure they are sent on the first request, you can use the `Critical-CH` header:

```http
Accept-CH: Sec-CH-UA-Platform-Version, Sec-CH-UA-Model
Critical-CH: Sec-CH-UA-Platform-Version, Sec-CH-UA-Model
```

> **Nginx Users:** Standard Nginx configurations typically forward these headers **out-of-the-box**. Explicit configuration is usually not required unless your proxy is configured to strip unknown headers.

## REST API Server

### Running Locally

To run the server locally without Docker (the regex database is embedded, no generation step needed):

```bash
go run ./cmd/server/main.go
```

### Running with Docker

Pre-built multi-arch images (amd64/arm64, Alpine-based, statically linked server):

```bash
docker run -p 8080:8080 ghcr.io/octanium91/ua-parser:latest
```

Or build locally:

```bash
docker build -t ua-parser .
docker run -p 8080:8080 ua-parser
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `UA_PORT` | Server port | `8080` |
| `UA_BASE_PATH` | Base prefix all endpoints are mounted under (e.g. `/api`) | *(root)* |
| `UA_ROUTE_PATH` | Parse endpoint sub-path (POST), relative to the base | `/` |
| `UA_HEALTH_PATH` | Health-check sub-path (GET), relative to the base | `/health` |
| `UA_DISABLE_UPDATE` | Disable auto-updates | `false` |
| `UA_CACHE_SIZE` | LRU cache size | `1000` |
| `UA_UPDATE_URL` | Remote URL for `regexes.yaml` | `https://raw.githubusercontent.com/ua-parser/uap-core/master/regexes.yaml` |
| `UA_UPDATE_INTERVAL` | Background update check interval | `24h` |

### Health Check

The server exposes a `GET` health-check endpoint for liveness/readiness probes, at `/health` by default:

```bash
curl http://localhost:8080/health   # -> {"status":"ok"}
```

### Relocating the endpoints

The whole API can be mounted under a single **base prefix** — handy behind a reverse proxy — and any future endpoints inherit it automatically:

```bash
# Everything under /api  ->  parse at POST /api, health at GET /api/health
docker run -p 8080:8080 \
  -e UA_BASE_PATH=/api \
  ghcr.io/octanium91/ua-parser:latest
```

For finer control, `UA_ROUTE_PATH` and `UA_HEALTH_PATH` set each endpoint's sub-path relative to the base (defaults `/` and `/health`):

```bash
# parse at POST /svc/parse, health at GET /svc/healthz
docker run -p 8080:8080 \
  -e UA_BASE_PATH=/svc -e UA_ROUTE_PATH=/parse -e UA_HEALTH_PATH=/healthz \
  ghcr.io/octanium91/ua-parser:latest
```

Notes: a leading slash is optional (`api` == `/api`); leaving `UA_BASE_PATH` unset keeps the legacy root behavior (`/` and `/health`); and if two endpoints resolve to the **same** path the server still starts, dispatching by method on that path (`GET` = health, `POST` = parse).

### Example Request

The parse endpoint is the configured `UA_ROUTE_PATH` (default `/`) and accepts **POST** only (a GET returns `405 Method Not Allowed`). The minimal body is `{"ua":"<string>"}`; `headers` is optional but recommended for Client Hints:

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

- **Linux (glibc)**: `libua-parser-linux-amd64.so`, `libua-parser-linux-arm64.so`
- **Linux (musl)**: `libua-parser-linux-amd64-musl.so`, `libua-parser-linux-arm64-musl.so` — shipped for forward compatibility; current Go toolchains cannot be `dlopen`'d on musl ([golang/go#54805](https://github.com/golang/go/issues/54805)), clients fall back to WASM on Alpine automatically
- **Windows**: `ua-parser-windows-amd64.dll`
- **macOS**: `libua-parser-darwin-amd64.dylib`, `libua-parser-darwin-arm64.dylib`
- **WebAssembly (WASI reactor)**: `ua-parser.wasm` — the automatic fallback engine for the Java (Chicory) and Node.js (`node:wasi`) clients
- **WebAssembly (browser)**: `ua-parser-js.wasm` + `wasm_exec.js` — js/wasm build for browser usage (different ABI from the WASI build; not interchangeable)

These files are the **required drivers** for integrations. Note that Python, Node.js, and Java packages already bundle these drivers automatically for all supported architectures.

### Exported Functions:
- `Init(configJSON)` — Initializes the parser.
- `Parse(payloadJSON)` — Parses data (returns JSON string).
- `FreeString(ptr)` — Frees memory allocated for strings.

## Project Structure

- `/pkg/core` — Parser core (logic, cache, updater).
- `/pkg/core/resources` — Bundled regex patterns.
- `/cmd/server` — Entry point for the HTTP server.
- `/cmd/cshared` — Wrapper for compiling into a native shared library (C-FFI).
- `/cmd/wasm` — WebAssembly WASI reactor build (`ua-parser.wasm`, fallback engine for Java/Node.js).
- `/cmd/wasmjs` — WebAssembly js/wasm build for browsers (`ua-parser-js.wasm`).
- `/clients` — Official Go, Python, Node.js, and Java wrappers.
