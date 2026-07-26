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
- **Correction Layer**: A declarative override config ([corrections.yaml](./pkg/core/resources/corrections.yaml)) patches known detection gaps (in-app browsers, vehicles, consoles, device vendors) on top of uap-core — embedded at build time and **hot-updated at runtime in every mode**, including the browser WASM build. Design: [docs/correction-layer.md](./docs/correction-layer.md).
- **Browser Signals**: An optional `signals` block (touch points, `navigator.platform`, WebGL renderer, screen) unmasks what UA and Client Hints cannot — e.g. iPads masquerading as Macs in Safari, which sends no Client Hints at all. The browser WASM client collects them automatically.
- **Rich Result**: Beyond browser/OS/device/engine — canonical `os.platform`, CPU bitness, `device.form_factor`, `is_frozen_ua`, and a classified `bot` object (`{name, category, vendor}` — training / search / user-fetch / agent / search-crawler / seo / social-preview) with canonical names synthesized even where uap-core yields junk.
- **Hot-Swap**: Background `regexes.yaml` and `corrections.yaml` updates without service interruption, with detailed logging for observability.
- **High Performance**: Optimized for low-latency processing using an LRU cache and efficient logic.
- **Embedded**: Core regex patterns are bundled into the binary using `go:embed`.
- **CI/CD**: Fully automated builds and multi-platform distribution (GitHub Packages, GHCR) via **GitHub Actions**.

## Comparison with popular alternatives

Measured against the most popular parser in each ecosystem: [ua-parser-js](https://uaparser.dev/) v2 (JavaScript), [Yauaa](https://yauaa.basjes.nl/) and [uap-java](https://github.com/ua-parser/uap-java) (Java), [uap-python](https://github.com/ua-parser/uap-python) (Python). Every row runs the same 50-UA corpus (desktop/mobile, Client Hints cases, bots, AI crawlers) single-threaded, full pipeline where the library supports it; our client rows go through the real published drivers (JNA / koffi / ctypes → Go shared library). All numbers are reproducible with the harness in [`tools/compare`](./tools/compare).

| Solution | Runtime | Init | Throughput, uncached | Cache hits² | Settled RSS | Result detail | Client Hints | Bot/AI flags | Data updates | License |
|---|---|---|---|---|---|---|---|---|---|---|
| **This solution — Go core** | Go 1.26 | ~0.1 s | **78,500 ops/s** (12.7 µs) | 2,010,000 ops/s | ~88 MB | browser/engine/OS/device/CPU + category + bot/AI | ✅ | ✅ | **hot-swap at runtime** | Apache 2.0 |
| **This solution — Node.js client** | Node 20 | 0.07 s | **62,100 ops/s** (16.1 µs) | ¹ | ~79 MB | same (identical core) | ✅ | ✅ | **hot-swap at runtime** | Apache 2.0 |
| **This solution — Java client** | JVM 17 | 0.4 s | **58,900 ops/s** (17.0 µs) | ¹ | ~106 MB | same (identical core) | ✅ | ✅ | **hot-swap at runtime** | Apache 2.0 |
| **This solution — Python client** | CPython 3.14 | 0.1 s | **53,100 ops/s** (18.8 µs) | ¹ | ~57 MB | same (identical core) | ✅ | ✅ | **hot-swap at runtime** | Apache 2.0 |
| ua-parser-js 2.0.10 | Node 20 | ~0 | 5,300 ops/s (187 µs) | — (no cache) | ~78 MB | browser/engine/OS/device/CPU + agent-type taxonomy | ✅ | ✅ | package release | AGPLv3 or paid PRO³ |
| Yauaa 8.2.0 | JVM 17+ | 3.3 s | 2,200 ops/s (453 µs) | 505,000 ops/s | ~245–390 MB⁴ | ~60 fields (20–40 populated per UA) | ✅ | partial (`Robot` class, no AI flag) | package release | Apache 2.0 |
| uap-python 1.0.2 | CPython 3.14 | 0.2 s | 1,530 ops/s⁵ (655 µs) | 462,000 ops/s | ~21 MB | UA/OS/device families | — | — | package release (monthly data snapshots) | Apache 2.0 |
| uap-java 1.6.1 | JVM 17 | 0.3 s | 1,055 ops/s (948 µs) | opt. `CachingParser`⁶ | ~63 MB | UA/OS/device families | — | — | bundled snapshot (uap-core of May 2023)⁶ | Apache 2.0 |

¹ Our clients' LRU lives in the Go core, behind the FFI/JSON boundary — even a cache hit pays the ~16–19 µs round-trip, so cached ≈ uncached. If your traffic is dominated by a few repeating UAs, put a small in-process memo in front (or use the REST server); on cold/diverse traffic the FFI clients are 25–50× faster than the in-language alternatives.
² In-process cache hits on repeating UAs: our Go core LRU, Yauaa's Caffeine cache (default 10,000), uap-python's default S3-FIFO cache (2,000). A cache hit is a map lookup — these numbers say nothing about parsing speed on diverse traffic.
³ ua-parser-js v1 is MIT, but Client Hints and bot/AI detection are v2-only — and v2 is AGPLv3 or a paid PRO license: $14 Personal (non-commercial use only), $29 Business (1 product), $599 Enterprise. PRO editions also advertise an enhanced device database, so detection quality differs between the AGPL and PRO builds.
⁴ Yauaa additionally retains ~138 MB of JVM heap before the first parse; its own docs state initialization "takes 2–5 seconds and uses a few hundred MiB", and our uncached throughput matches its self-published ~0.5 ms/parse. Peak RSS for every JVM/CPython library spikes hundreds of MB above settled under sustained max load (transient allocation garbage) — settled post-GC RSS is the honest steady-state number.
⁵ Measured on the pure-Python backend a plain `pip install ua-parser` gets. The optional native backends (`ua-parser[regex]` / `[re2]`) are ~10–20× faster uncached per the project's own docs — still UA-string-only, no Client Hints or bot flags.
⁶ uap-java's optional `CachingParser` (LRUMap, default 1,000) is not thread-safe. The project has had no releases since Nov 2023, and its bundled regex snapshot (uap-core 0.18.0, May 2023) predates every AI crawler — GPTBot, ClaudeBot, PerplexityBot are invisible to it. A newer `regexes.yaml` can be supplied manually via `Parser(InputStream)`.

Environment: AMD Ryzen AI Max+ 395, Windows 11, single thread. The Go core additionally scales across cores under concurrent load; Node and CPython parse on a single thread per process, and a Yauaa analyzer instance is synchronized (parallel throughput requires multiple instances).

The point of this comparison is not to win every detection edge case — ua-parser-js resolves some exotics (in-app browsers, vehicle browsers, device vendor mapping) more precisely, and Yauaa derives the most fields per agent. The point is that this is **one engine, everywhere**: byte-identical results in Go, Java, Node.js, and Python, or as a drop-in Docker/REST microservice next to any stack — with a regex database ([uap-core](https://github.com/ua-parser/uap-core)) that **hot-swaps in the background without a redeploy**, while every alternative ships detection updates as package releases you have to roll out. On mainstream traffic the detection results agree with ua-parser-js anyway (browser/OS/engine/versions, Windows 11 via Client Hints, Brave/Opera GX via `Sec-CH-UA` brands, all major search bots and AI crawlers); a full side-by-side diff on the shared corpus is one `report.mjs` run away in [`tools/compare`](./tools/compare).

### When to pick which

- **ua-parser-js**: frontend/browser detection, JS-only stacks, minimal bundle size.
- **Yauaa**: JVM-only stacks where maximum detection detail matters more than RAM/startup, and traffic repeats enough for its cache.
- **uap-python / uap-java**: minimal-dependency parsing of the UA string alone, when Client Hints, bot flags, and throughput don't matter.
- **This project**: polyglot backends that need identical results across services, high-throughput parsing on diverse traffic, low RAM/startup, regex updates without redeploys, a self-hosted parsing microservice, or permissive licensing for Client Hints + bot/AI detection.

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
    // CorrectionsURL / DisableCorrectionsUpdate also available (correction layer).
}

parser, _ := uaparser.New(cfg)

// Headers for Client Hints (optional)
headers := map[string]string{
    "Sec-CH-UA-Platform":         "Windows",
    "Sec-CH-UA-Platform-Version": "15.0.0",
}

result := parser.Parse("Mozilla/5.0...", headers)
fmt.Printf("OS: %s %s (%s)\n", result.OS.Name, result.OS.Version, result.OS.Platform)
fmt.Printf("Browser: %s (%s)\n", result.Browser.Name, result.Browser.Version)
fmt.Printf("Engine: %s %s\n", result.Engine.Name, result.Engine.Version)
fmt.Printf("Device: %s / %s (%s)\n", result.Device.Vendor, result.Device.Model, result.Device.FormFactor)
fmt.Printf("CPU: %s %s | frozen UA: %v\n", result.CPU.Architecture, result.CPU.Bitness, result.IsFrozenUA)
fmt.Printf("Category: %s\n", result.Category)
fmt.Printf("Is Bot: %v (AI: %v)\n", result.IsBot, result.IsAICrawler)
if result.Bot != nil {
    fmt.Printf("Bot: %s (%s, %s)\n", result.Bot.Name, result.Bot.Category, result.Bot.Vendor)
}

// Optional browser signals (Safari/Firefox send no Client Hints):
//   result = parser.ParseFull(ua, headers, &core.Signals{MaxTouchPoints: 5})
```

## Supported Client Hints Headers

To achieve high accuracy (especially for Windows 11 and full browser versions), it is recommended to pass the following headers:

| Header | Description | Impact |
|--------|-------------|--------|
| `Sec-CH-UA` | Browser brands + major version (sent by default) | Corrects spoofed majors; identifies Brave / Opera GX, which are UA-identical to Chrome |
| `Sec-CH-UA-Platform` | Operating system name | Accurate OS detection (normalized to canonical names) |
| `Sec-CH-UA-Platform-Version` | Operating system version | Distinguishes Windows 11 from 10; the **only** source of real macOS/Android versions (UA is frozen) |
| `Sec-CH-UA-Model` | Device model name | Precise device identification (Android UA model is frozen to "K") |
| `Sec-CH-UA-Arch` + `Sec-CH-UA-Bitness` | CPU architecture + bitness | Real architecture (`"arm"`+`"64"` → `arm64`); the frozen UA always claims x64 |
| `Sec-CH-UA-Mobile` | Mobile device flag | Improves category detection |
| `Sec-CH-UA-Full-Version-List` | Full browser version list | Exact version (e.g., 120.0.6099.129) incl. mobile Chromium, plus the true Blink engine version from the Chromium entry |
| `Sec-CH-UA-Form-Factors` | Device form factor (Chrome 124+) | Distinguishes tablets, watches, XR and automotive devices — Android tablets are UA-indistinguishable from phones |

### Missing Headers
If specific Client Hints are unavailable (e.g., browser policy or HTTP connection), the parser automatically falls back to standard regex-based parsing of the `User-Agent` string.

### Requesting Client Hints
To receive high-entropy Client Hints (like `Sec-CH-UA-Platform-Version` or `Sec-CH-UA-Model`), your server must explicitly request them using the `Accept-CH` response header:

```http
Accept-CH: Sec-CH-UA-Platform-Version, Sec-CH-UA-Model, Sec-CH-UA-Full-Version-List, Sec-CH-UA-Arch, Sec-CH-UA-Bitness, Sec-CH-UA-Form-Factors
```

**Note:** Browsers will only send these headers on subsequent requests after receiving the `Accept-CH` header, and only over **HTTPS**. To ensure they are sent on the first request, you can use the `Critical-CH` header:

```http
Accept-CH: Sec-CH-UA-Platform-Version, Sec-CH-UA-Model
Critical-CH: Sec-CH-UA-Platform-Version, Sec-CH-UA-Model
```

> **Nginx Users:** Standard Nginx configurations typically forward these headers **out-of-the-box**. Explicit configuration is usually not required unless your proxy is configured to strip unknown headers.

### Forwarding headers from your backend

The parser is only as accurate as the headers you hand it. The golden rule for every backend client: **don't enumerate individual headers — copy the `User-Agent` plus every request header whose name starts with `Sec-CH-` into the headers map.** New hints the engine learns in future versions then flow through automatically, with no integrator changes.

- Header names are case-insensitive (the engine normalizes them); pass values **raw, quotes included** (`Sec-CH-UA-Platform: "Windows"` — don't strip the quotes).
- Also forward **`X-Requested-With`** when present: Android WebView sends the embedding app's package id (`com.tencent.mm` → WeChat) — a precise in-app browser signal consumed by the upcoming correction layer ([design](./docs/correction-layer.md)); harmless to forward today.
- If you cache parse results on your side, your cache key must include every forwarded header (the built-in LRU cache already handles this internally).

**Go**

```go
headers := make(map[string]string)
for name, vals := range r.Header {
    if lower := strings.ToLower(name); strings.HasPrefix(lower, "sec-ch-") || lower == "x-requested-with" {
        headers[lower] = vals[0]
    }
}
result := parser.Parse(r.Header.Get("User-Agent"), headers)
```

**Java (servlet API — same idea for Spring's `@RequestHeader MultiValueMap`)**

```java
Map<String, String> headers = new HashMap<>();
for (Enumeration<String> e = request.getHeaderNames(); e.hasMoreElements(); ) {
    String name = e.nextElement().toLowerCase();
    if (name.startsWith("sec-ch-") || name.equals("x-requested-with")) {
        headers.put(name, request.getHeader(name));
    }
}
UaParser.Result result = parser.parse(request.getHeader("User-Agent"), headers);
```

**Node.js (Express — `req.headers` keys are already lowercase)**

```js
const headers = Object.fromEntries(
    Object.entries(req.headers)
        .filter(([k]) => k.startsWith('sec-ch-') || k === 'x-requested-with')
);
const result = parser.parse(req.headers['user-agent'], headers);
```

**Python (Flask — same idea for Django's `request.headers` / FastAPI's `request.headers`)**

```python
headers = {
    k.lower(): v
    for k, v in request.headers
    if k.lower().startswith("sec-ch-") or k.lower() == "x-requested-with"
}
result = parser.parse(request.headers.get("User-Agent", ""), headers)
```

**REST server** — same rule, headers go into the `headers` object of the POST body (see [Example Request](#example-request)).

**Browser signals (optional, biggest win for Safari/Firefox traffic)** — Safari and Firefox send no `Sec-CH-UA` headers at all, but the page can still collect evidence the UA string hides. Gather this object on the page and forward it as `signals` in the parse payload (the browser WASM client collects it automatically):

```js
const signals = {
    max_touch_points: navigator.maxTouchPoints,        // unmasks iPads posing as Macs
    platform: navigator.platform,
    screen: { w: screen.width, h: screen.height, dpr: devicePixelRatio },
    // Optional, fingerprinting-adjacent — include only if acceptable for your users:
    // webgl_renderer from WEBGL_debug_renderer_info (Apple Silicon / Android SoC detection)
};
```

Priority inside the engine: Client Hints > signals > UA string — signals never override real CH data.

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
| `UA_CORRECTIONS_URL` | Remote URL for `corrections.yaml` (correction layer) | this repo's `main` branch |
| `UA_DISABLE_CORRECTIONS_UPDATE` | Disable correction hot-updates (embedded snapshot stays) | `false` |

### Health Check

The server exposes a `GET` health-check endpoint for liveness/readiness probes, at `/health` by default. It also reports the live correction-layer version and rule count for observability:

```bash
curl http://localhost:8080/health
# -> {"status":"ok","corrections":{"version":"2026-07-26.1","rules":15}}
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

The parse endpoint is the configured `UA_ROUTE_PATH` (default `/`) and accepts **POST** only (a GET returns `405 Method Not Allowed`). The minimal body is `{"ua":"<string>"}`; `headers` is optional but recommended for Client Hints, and `signals` is an optional block of browser-collected evidence (see [Browser signals](#forwarding-headers-from-your-backend)):

```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{
    "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
    "headers": {
      "Sec-CH-UA-Platform": "\"Windows\"",
      "Sec-CH-UA-Platform-Version": "\"13.0.0\"",
      "Sec-CH-UA-Full-Version-List": "\"Chromium\";v=\"119.0.6045.105\", \"Google Chrome\";v=\"119.0.6045.105\""
    },
    "signals": { "max_touch_points": 0, "webgl_renderer": "" }
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
    "version": "11",
    "platform": "windows"
  },
  "device": {
    "model": "",
    "vendor": "",
    "type": "desktop",
    "form_factor": "desktop"
  },
  "cpu": {
    "architecture": "amd64",
    "bitness": "64"
  },
  "engine": {
    "name": "Blink",
    "version": "119.0.6045.105"
  },
  "category": "desktop",
  "is_bot": false,
  "is_ai_crawler": false,
  "is_frozen_ua": true
}
```

For bots the response additionally carries a classified identity object — canonical name synthesized even where uap-core's generic patterns produce junk (`ChatGPT-User` used to parse as `"com/bot"`):

```json
{
  "browser": { "name": "ChatGPT-User", "version": "1.0", "major": "1", "type": "bot" },
  "category": "bot",
  "is_bot": true,
  "is_ai_crawler": true,
  "bot": { "name": "ChatGPT-User", "category": "user-fetch", "vendor": "OpenAI" }
}
```

## Bot & AI Crawler Detection

The parser includes a dedicated logic to detect common bots and AI-related crawlers:
- **General Bots**: Googlebot, Bingbot, YandexBot, etc.
- **AI Crawlers**: GPTBot, ClaudeBot, PerplexityBot, Google-Extended, and more.
- **Categorization**: Automatically sets `Category: "bot"` and `Browser.Type: "bot"` for identified automated agents.
- **Classified identity**: every bot result carries `bot: {name, category, vendor}` — AI agents are tagged `training` / `search` / `user-fetch` / `agent` per the vendor's own documentation (robots-policy and billing decisions need more than a boolean), classic automation as `search-crawler` / `seo` / `monitoring` / `social-preview`.

## Correction Layer

A declarative override config, [pkg/core/resources/corrections.yaml](./pkg/core/resources/corrections.yaml), fixes known detection gaps that the uap-core database cannot express or has not yet fixed upstream: in-app browsers (WeChat, VK, `X-Requested-With` package ids), vehicles (Tesla, Android Automotive), consoles (PS5 OS, Xbox model), Fire TV / Tizen TV, and an Android vendor-from-model prefix table (`SM-*` → Samsung, Xiaomi date codes, `CPH*` → OPPO, …).

- Rules are **matched behind cheap substring gates** (near-zero cost on mainstream traffic, hard cap 64 rules) and applied **after Client Hints** — corrections are terminal, but fill-gap guards ensure genuine CH data is never overwritten.
- The file is **embedded at build time and hot-swapped at runtime** with full validation: schema check, RE2 compilation, per-rule inline tests executed through the real pipeline before every swap; any failure keeps the last good rules.
- **Every mode gets live rules**: native builds fetch it on the update tick; the browser WASM build fetches it once at `initUA` (Fetch-backed `net/http`); the WASI fallback engines receive it from their Java/Node hosts via the `updateCorrections` export.
- Every rule carries its own test corpus and an upstream link; a dead-rule lint in CI forces deleting rules once upstream uap-core catches up. Design details: [docs/correction-layer.md](./docs/correction-layer.md).

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
- `Parse(payloadJSON)` — Parses data (returns JSON string). The payload accepts `{"ua", "headers", "signals"}`.
- `UpdateCorrections(yaml)` — Pushes a corrections.yaml payload into the engine (validated; whole-file reject keeps last good). For hosts that manage delivery themselves.
- `FreeString(ptr)` — Frees memory allocated for strings.

The WASI build additionally exports `updateCorrections(ptr, len)` (host-push — WASI has no sockets), and the browser js/wasm build exposes `globalThis.updateCorrectionsUA(yaml)` plus automatic fetch-at-init of the corrections file.

## Project Structure

- `/pkg/core` — Parser core (logic, cache, updater).
- `/pkg/core/resources` — Bundled regex patterns.
- `/cmd/server` — Entry point for the HTTP server.
- `/cmd/cshared` — Wrapper for compiling into a native shared library (C-FFI).
- `/cmd/wasm` — WebAssembly WASI reactor build (`ua-parser.wasm`, fallback engine for Java/Node.js).
- `/cmd/wasmjs` — WebAssembly js/wasm build for browsers (`ua-parser-js.wasm`).
- `/clients` — Official Go, Python, Node.js, and Java wrappers.
