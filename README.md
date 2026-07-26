# Universal User-Agent Parser

A high-performance, cross-platform User-Agent parser: one Go core exposed to **Go, Java, Node.js, and Python** (plus a REST microservice and WebAssembly), with Client Hints support, bot/AI-crawler classification, and a **runtime-updated regex database and correction layer** — so detection stays fresh without redeploying.

## Quickstart — pick your mode

One Go core gives byte-identical results everywhere; choose how you run it:

| I want to… | Use | Jump to |
|---|---|---|
| Parse UAs inside a **Go** service | the Go package | [Go Library Usage](#go-library-usage) |
| Parse from **Java / Node.js / Python** | that language's client | [Client Libraries](#client-libraries) |
| A **standalone HTTP service** (any language, or a sidecar) | the REST server / Docker image | [REST API Server](#rest-api-server) |
| Parse **in the browser** (SPA / vanilla JS) | the JavaScript client (WASM build) | [Usage (Browser / Bundlers)](./clients/node#usage-browser--bundlers) |

Fastest taste — run the service and parse a request, no install beyond Docker:

```bash
docker run -p 8080:8080 ghcr.io/octanium91/ua-parser:latest
curl -X POST http://localhost:8080/ -H 'Content-Type: application/json' \
  -d '{"ua":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"}'
```

New terms used below — **Client Hints** (`Sec-CH-UA-*` headers the browser sends), **frozen UA**, **correction layer**, **signals**, **`class_hash`** — each has its own section further down.

## Features

- **Ways to run it**:
  - **Native Go library**: importable package.
  - **HTTP microservice**: ready-to-use REST API server (Docker image provided).
  - **Language clients**: official wrappers for **Python**, **Node.js**, and **Java** (in `/clients`).
- **Multi-platform**: native builds for **linux/amd64**, **linux/arm64** (glibc and musl artifacts), **windows/amd64**, **macOS (amd64/arm64)**, plus **WebAssembly** (WASI reactor and browser js/wasm).
- **Graceful Degradation (Java & Node.js)**: Smart client architecture that attempts to load the ultra-fast native driver (JNA / koffi), but transparently falls back to a bundled WebAssembly engine if the native library cannot be loaded (e.g., on Alpine Linux).
- **Client Hints Priority**: Automatically uses `Sec-CH-UA` headers with **highest priority** for precise OS and device detection (e.g., distinguishing Windows 11 from Windows 10 where the UA string might be ambiguous).
- **Correction Layer**: A declarative override config ([corrections.yaml](./pkg/core/resources/corrections.yaml)) patches known detection gaps (in-app browsers, vehicles, consoles, device vendors) on top of uap-core — embedded at build time and **hot-updated at runtime in every mode**, including the browser WASM build. Design: [docs/correction-layer.md](./docs/correction-layer.md).
- **Browser Signals**: An optional `signals` block (touch points, `navigator.platform`, WebGL renderer, screen) unmasks what UA and Client Hints cannot — e.g. iPads masquerading as Macs in Safari, which sends no Client Hints at all. The browser WASM client collects them automatically.
- **Rich Result**: Beyond browser/OS/device/engine — canonical `os.platform`, CPU bitness, `device.form_factor`, `is_frozen_ua`, and a classified `bot` object (`{name, category, vendor}` — AI: training / search / user-fetch / agent / other; classic: search-crawler / seo / monitoring / social-preview) with canonical names synthesized even where uap-core yields junk.
- **Traffic-quality signals** (Result v1.2, no external DB): `automation` (headless / Electron / webdriver — *undeclared* automation), `integrity` (UA vs Client Hints vs signals consistency → spoofed clients), `security` (attack payloads in the UA), `detection` provenance, plus convenience flags and a coarse `class_hash` bucket key.
- **Hot-Swap**: Background `regexes.yaml` and `corrections.yaml` updates without service interruption, with detailed logging for observability.
- **High Performance**: Optimized for low-latency processing using an LRU cache and efficient logic.
- **Embedded**: Core regex patterns are bundled into the binary using `go:embed`.
- **CI/CD**: Fully automated builds and multi-platform distribution (GitHub Packages, GHCR) via **GitHub Actions**.

## Comparison with popular alternatives

Measured against the most popular parser in each ecosystem: [ua-parser-js](https://uaparser.dev/) v2 (JavaScript), [Yauaa](https://yauaa.basjes.nl/) and [uap-java](https://github.com/ua-parser/uap-java) (Java), [uap-python](https://github.com/ua-parser/uap-python) (Python). Every row runs the same 52-UA corpus (desktop/mobile, Client Hints cases, bots, AI crawlers) single-threaded, full pipeline where the library supports it; our client rows go through the real published drivers (JNA / koffi / ctypes → Go shared library). All numbers are reproducible with the harness in [`tools/compare`](./tools/compare).

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

The point of this comparison is not to win every detection edge case — ua-parser-js resolves some exotics (vehicle browsers, some device vendor mapping) more precisely, and Yauaa derives the most fields per agent. The point is that this is **one engine, everywhere**: byte-identical results in Go, Java, Node.js, and Python, or as a drop-in Docker/REST microservice next to any stack — with a regex database ([uap-core](https://github.com/ua-parser/uap-core)) that **hot-swaps in the background without a redeploy** (plus the [correction layer](#correction-layer) that closes gaps uap-core hasn't), while every alternative ships detection updates as package releases you have to roll out. On mainstream traffic the detection results agree with ua-parser-js anyway (browser/OS/engine/versions, Windows 11 via Client Hints, Brave/Opera GX via `Sec-CH-UA` brands, all major search bots and AI crawlers); a full side-by-side diff on the shared corpus is one `report.mjs` run away in [`tools/compare`](./tools/compare).

### When to pick which

- **ua-parser-js**: frontend/browser detection, JS-only stacks, minimal bundle size.
- **Yauaa**: JVM-only stacks where maximum detection detail matters more than RAM/startup, and traffic repeats enough for its cache.
- **uap-python / uap-java**: minimal-dependency parsing of the UA string alone, when Client Hints, bot flags, and throughput don't matter.
- **This project**: polyglot backends that need identical results across services, high-throughput parsing on diverse traffic, low RAM/startup, regex updates without redeploys, a self-hosted parsing microservice, or permissive licensing for Client Hints + bot/AI detection.

## Client Libraries

We provide official wrappers for major languages that use the core shared library:

- **[Go](./clients/go)**: `go get github.com/Octanium91/ua-parser/clients/go` (the importable client package; the bare module path also resolves)
- **[Python](./clients/python)**: Download .whl from [GitHub Releases](https://github.com/octanium91/ua-parser/releases)
- **[Node.js / Browser](./clients/node)**: `@octanium91/ua-parser` (GitHub Packages) — one package for server-side Node.js (native) and browser/SPA (WASM)
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
| **Go**      | `go get github.com/Octanium91/ua-parser/clients/go` | — | [Go Setup](./clients/go) |
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
import (
    "fmt"

    uaparser "github.com/Octanium91/ua-parser/clients/go"
)

cfg := uaparser.Config{
    DisableAutoUpdate: false,   // set true (+ DisableCorrectionsUpdate: true) to run fully offline
    LRUCacheSize:      1000,
    // CorrectionsURL / DisableCorrectionsUpdate also available (correction layer).
}

parser, _ := uaparser.New(cfg)

// Headers for Client Hints (optional). Pass values raw — quotes included,
// exactly as the browser sends them (the parser strips the quotes itself).
headers := map[string]string{
    "Sec-CH-UA-Platform":         "\"Windows\"",
    "Sec-CH-UA-Platform-Version": "\"15.0.0\"",
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
//   result = parser.ParseFull(ua, headers, &uaparser.Signals{MaxTouchPoints: 5})
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

These are the only headers the engine reads (plus `X-Requested-With` for in-app detection — see the [correction layer](#correction-layer)). Values arrive **quoted**, exactly as the browser sends them; pass them through unchanged (the parser strips the quotes).

**Example — what Chrome 126 on Windows 11 actually sends** (once the server opts in with `Accept-CH`):

```http
Sec-CH-UA: "Not/A)Brand";v="8", "Chromium";v="126", "Google Chrome";v="126"
Sec-CH-UA-Mobile: ?0
Sec-CH-UA-Platform: "Windows"
Sec-CH-UA-Platform-Version: "15.0.0"
Sec-CH-UA-Arch: "x86"
Sec-CH-UA-Bitness: "64"
Sec-CH-UA-Full-Version-List: "Not/A)Brand";v="8.0.0.0", "Chromium";v="126.0.6478.127", "Google Chrome";v="126.0.6478.127"
```

`Sec-CH-UA-Platform-Version: "15.0.0"` is how the parser reports **Windows 11** even though the UA still says `Windows NT 10.0`. The low-entropy `Sec-CH-UA`, `-Mobile`, `-Platform` are sent by default; the rest are **high-entropy** and require `Accept-CH` (below). On mobile the equivalent set carries `Sec-CH-UA-Mobile: ?1`, `Sec-CH-UA-Platform: "Android"`, and `Sec-CH-UA-Model: "Pixel 8 Pro"` (the UA model is frozen to `K`).

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
- Also forward **`X-Requested-With`** when present: Android WebView sends the embedding app's package id (`com.tencent.mm` → WeChat, `com.instagram.android` → Instagram) — a precise in-app browser signal the [correction layer](#correction-layer) consumes to identify the host app.
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

**Browser signals** — beyond headers, a page can hand the parser touch/GPU/screen evidence that the UA and Client Hints don't carry (the biggest win for Safari/Firefox, which send no Client Hints). See the [Browser Signals](#browser-signals) section below.

## Browser Signals

Client Hints exist **only in Chromium** — **Safari and Firefox send no `Sec-CH-UA-*` headers at all**, and Safari ships the worst UA lies (an iPad in desktop mode is byte-identical to a Mac). The optional `signals` object lets JavaScript on the page hand the parser evidence the User-Agent and Client Hints can't provide. Priority inside the engine is **Client Hints > signals > UA string** — a signal never overrides real CH data.

Signals reach the engine three ways: the `signals` field of the REST POST body, the third argument of `ParseFull` / `parse(ua, headers, signals)` in the native clients, or — in the **browser WASM client** — collected automatically at `init()` (you pass nothing).

| Field (JSON) | JS source | What it does today |
|---|---|---|
| `max_touch_points` | `navigator.maxTouchPoints` | **iPad unmask** — a `Macintosh` UA with >1 touch point → iPadOS / tablet; also flips a desktop-mode Android tablet to `tablet` |
| `webgl_renderer` | `WEBGL_debug_renderer_info` → `UNMASKED_RENDERER_WEBGL` | **Apple Silicon** — `Apple M…` on a frozen Mac UA → `arm64` (only when `Sec-CH-UA-Arch` is absent); also populates `gpu.renderer` (Android SoC tier) |
| `webgl_vendor` | `WEBGL_debug_renderer_info` → `UNMASKED_VENDOR_WEBGL` | populates `gpu.vendor` |
| `webdriver` | `navigator.webdriver` | sets `automation.webdriver` (Selenium / Puppeteer / Playwright) |
| `platform` | `navigator.platform` | accepted; reserved for future rules |
| `screen` `{w,h,dpr}` | `screen.width` / `screen.height` / `devicePixelRatio` | accepted; reserved for future rules |
| `device_memory` | `navigator.deviceMemory` (Chromium-only, `0.25`–`8`) | accepted; coarse device-tier hint, reserved |
| `hardware_concurrency` | `navigator.hardwareConcurrency` | accepted; coarse device-tier hint, reserved |

> **Honest scope:** only `max_touch_points`, `webgl_*`, and `webdriver` change detection today (`webgl_*` is echoed back as the `gpu` object, `webdriver` feeds `automation.webdriver`); the other fields are accepted and reserved for future inference rules, so sending them is harmless but not yet impactful. All fields are optional. `webgl_*` is fingerprinting-adjacent — collect it only if that is acceptable for your users.

**Collect on the page:**

```js
const signals = {
    max_touch_points: navigator.maxTouchPoints,
    platform: navigator.platform,
    screen: { w: screen.width, h: screen.height, dpr: devicePixelRatio },
    hardware_concurrency: navigator.hardwareConcurrency,
    device_memory: navigator.deviceMemory,           // Chromium only (undefined elsewhere)
    webdriver: navigator.webdriver === true,         // automation flag
};

// Optional GPU probe — fingerprinting-adjacent, include only if acceptable:
const gl = document.createElement("canvas").getContext("webgl");
const dbg = gl && gl.getExtension("WEBGL_debug_renderer_info");
if (dbg) {
    signals.webgl_vendor   = gl.getParameter(dbg.UNMASKED_VENDOR_WEBGL);
    signals.webgl_renderer = gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL);
}
```

**Forward it** (send `signals` to your backend, then on to the parser). REST example — the flagship iPad-as-Mac unmask:

```bash
curl -X POST http://localhost:8080/ -H "Content-Type: application/json" -d '{
  "ua": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
  "signals": { "max_touch_points": 5, "platform": "MacIntel" }
}'
# Without signals → os "Mac OS X", device "desktop".
# With    signals → os "iPadOS", device "tablet" (Apple iPad), category "mobile".
```

Native clients pass the same object as the last argument — e.g. Go `parser.ParseFull(ua, headers, &uaparser.Signals{MaxTouchPoints: 5})`, Node `parser.parse(ua, headers, { max_touch_points: 5 })`, Python `parser.parse(ua, headers, signals={"max_touch_points": 5})`, Java `parser.parse(ua, headers, signals)`.

## REST API Server

### Running Locally

To run the server locally without Docker (**requires Go 1.26+**; the regex database is embedded, no generation step needed):

```bash
go run ./cmd/server
```

> **Windows Git Bash:** MSYS rewrites slash-valued env vars, so `UA_BASE_PATH=/api` becomes a Windows path and the server rejects it. Prefix such runs with `MSYS_NO_PATHCONV=1`, or use PowerShell/Docker. (Not an issue for the parse itself, only slash-prefixed env values.)

### Running with Docker

Pre-built multi-arch images (amd64/arm64, Alpine-based, statically linked server):

```bash
docker run -p 8080:8080 ghcr.io/octanium91/ua-parser:latest
```

> The registry/image name is lowercase (`octanium91`) as container registries require; the Go module path keeps the canonical `Octanium91` casing. Both are intentional — not a typo.

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

The parse endpoint is the configured `UA_ROUTE_PATH` (default `/`) and accepts **POST** only (a GET returns `405 Method Not Allowed`). The minimal body is `{"ua":"<string>"}`; `headers` is optional but recommended for Client Hints, and `signals` is an optional block of browser-collected evidence (see [Browser Signals](#browser-signals)).

This is the **exact request that produces the "Maximal" response below** — a real Chrome 150 on Windows 11 with the full high-entropy Client Hints set and browser signals, so you can copy it and reproduce the result verbatim:

```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{
    "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
    "headers": {
      "Sec-CH-UA": "\"Not)A;Brand\";v=\"8\", \"Chromium\";v=\"150\", \"Google Chrome\";v=\"150\"",
      "Sec-CH-UA-Mobile": "?0",
      "Sec-CH-UA-Platform": "\"Windows\"",
      "Sec-CH-UA-Platform-Version": "\"19.0.0\"",
      "Sec-CH-UA-Arch": "\"x86\"",
      "Sec-CH-UA-Bitness": "\"64\"",
      "Sec-CH-UA-Model": "\"\"",
      "Sec-CH-UA-Full-Version-List": "\"Not)A;Brand\";v=\"8.0.0.0\", \"Chromium\";v=\"150.0.7871.182\", \"Google Chrome\";v=\"150.0.7871.182\"",
      "Sec-CH-UA-Form-Factors": "\"Desktop\""
    },
    "signals": {
      "max_touch_points": 10,
      "platform": "Win32",
      "hardware_concurrency": 32,
      "device_memory": 32,
      "screen": { "w": 1463, "h": 915, "dpr": 1.75 },
      "webgl_vendor": "Google Inc. (AMD)",
      "webgl_renderer": "ANGLE (AMD, AMD Radeon(TM) 8060S Graphics (0x00001586) Direct3D11 vs_5_0 ps_5_0, D3D11)"
    }
  }'
```

For the **"Incomplete input"** response further down, send the same body with only the `ua` field (no `headers`, no `signals`).

### Example Response

Every result carries `result_version` — the version of the JSON shape (see [`ResultSchemaVersion`](./pkg/core/types.go)). It's bumped only when fields change, so a stored result stays traceable to the format (and thus the library range) that produced it even after you upgrade.

**Maximal** — the [request above](#example-request) returns:

```json
{
  "result_version": "1.2",
  "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
  "browser": { "name": "Chrome", "version": "150.0.7871.182", "major": "150", "type": "browser" },
  "os": { "name": "Windows", "version": "11", "platform": "windows", "version_name": "Windows 11", "version_raw": "19.0.0" },
  "device": { "model": "", "vendor": "", "type": "desktop", "form_factor": "desktop" },
  "cpu": { "architecture": "amd64", "bitness": "64" },
  "engine": { "name": "Blink", "version": "150.0.7871.182" },
  "category": "desktop",
  "is_bot": false, "is_ai_crawler": false, "is_frozen_ua": true,
  "is_mobile": false, "is_desktop": true, "is_touch_capable": true,
  "is_chrome_family": true, "is_apple_silicon": false,
  "automation": { "headless": false, "electron": false, "webdriver": false },
  "integrity": { "spoofed": false, "reasons": [] },
  "security": { "suspicious": false },
  "detection": { "client_hints_used": true, "high_entropy": true, "signals_used": true },
  "class_hash": "f1eae05fe8edff24",
  "gpu": { "vendor": "Google Inc. (AMD)", "renderer": "ANGLE (AMD, AMD Radeon(TM) 8060S Graphics (0x00001586) Direct3D11 vs_5_0 ps_5_0, D3D11)" }
}
```

The frozen UA alone says `Chrome/150.0.0.0` on `Windows NT 10.0`; the Client Hints promote it to the **exact build `150.0.7871.182`** and, crucially, to **Windows 11** (`Sec-CH-UA-Platform-Version: "19.0.0"`, preserved in `version_raw`). The `gpu` block and `is_touch_capable` come from the signals.

**Incomplete input — the SAME device/browser, but no headers and no signals** (e.g. a raw log line). The response degrades honestly and says so via `detection`:

```json
{
  "result_version": "1.2",
  "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
  "browser": { "name": "Chrome", "version": "150.0.0", "major": "150", "type": "browser" },
  "os": { "name": "Windows", "version": "10", "platform": "windows", "version_name": "Windows 10", "version_raw": "10" },
  "device": { "model": "", "vendor": "", "type": "desktop", "form_factor": "desktop" },
  "cpu": { "architecture": "amd64", "bitness": "64" },
  "engine": { "name": "Blink", "version": "150.0.0.0" },
  "category": "desktop",
  "is_bot": false, "is_ai_crawler": false, "is_frozen_ua": true,
  "is_mobile": false, "is_desktop": true, "is_touch_capable": false,
  "is_chrome_family": true, "is_apple_silicon": false,
  "automation": { "headless": false, "electron": false, "webdriver": false },
  "integrity": { "spoofed": false, "reasons": [] },
  "security": { "suspicious": false },
  "detection": { "client_hints_used": false, "high_entropy": false, "signals_used": false },
  "class_hash": "5b21517b9f08b503"
}
```

Same machine, different answer: **Windows 10 not 11** (the frozen UA can't tell them apart without Client Hints), a generic `150.0.0` version, no touch, and the `bot`/`gpu` keys are omitted entirely (nothing to populate them — they are the only `omitempty` fields; everything else is always present). **`detection` makes the input quality explicit** — `client_hints_used`/`signals_used`/`high_entropy` are all `false`, so a consumer immediately knows this result is a best-effort UA-only guess, not a CH-backed reading.

> **What `is_frozen_ua` means.** Since ~Chrome 110, Chromium *reduces* the User-Agent to a fixed template that deliberately hides detail: the browser version is frozen to `MAJOR.0.0.0`, Windows is always `Windows NT 10.0`, macOS always `Intel Mac OS X 10_15_7`, Android always `Android 10; K`, and the arch always claims x64. The real build, Win10-vs-11, true macOS/Android version, device model, and CPU architecture then live **only in Client Hints**. `is_frozen_ua: true` is the heads-up that the UA is a template — trust Client Hints (or signals) over it. It's exactly why the two examples above disagree: the frozen UA alone yields Windows 10 / `150.0.0`, while the Client Hints promote it to Windows 11 / `150.0.7871.182`. Pair it with `detection`: `is_frozen_ua: true` **and** `client_hints_used: false` means the specifics are unreliable — request `Accept-CH` from that client.

Enrichment &amp; derived fields beyond the base browser/OS/device/engine (all computed locally — **no external DB**):

| Group | Fields | Purpose |
|---|---|---|
| `is_frozen_ua` | — | UA is a reduced/frozen template (see the note above) — trust Client Hints over it |
| Convenience | `is_mobile`, `is_desktop`, `is_touch_capable`, `is_chrome_family`, `is_apple_silicon` | ready booleans for common branching |
| `automation` | `headless`, `electron`, `webdriver` | **undeclared** automation (unlike `is_bot`) — headless browsers, Electron shells, Selenium/Puppeteer |
| `integrity` | `spoofed`, `reasons[]` | cross-checks UA vs Client Hints vs signals for contradictions (spoofed clients) |
| `security` | `suspicious`, `category` | attack payloads in the UA (scanners, SQL-injection, XSS) |
| `detection` | `client_hints_used`, `high_entropy`, `signals_used` | **input provenance** — which richer inputs were actually present (all `false` on a UA-only parse) |
| `bot` | `name`, `category`, `vendor` | classified identity for bots (`null` for humans); see [Bot & AI Crawler Detection](#bot--ai-crawler-detection) |
| `gpu` | `vendor`, `renderer` | present **only when a WebGL signal was supplied** (`webgl_vendor`/`webgl_renderer`); source of Apple-Silicon / Android-SoC inference |
| `os.version_name` / `os.version_raw` | — | human label (`macOS Sonoma`) and exact CH version (`19.0.0` behind Windows `11`) |
| `class_hash` | — | stable hash of the client-**class** tuple (same for every client of the same class); an analytics bucket key — deliberately coarse, **not** a per-user/device tracking fingerprint |

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
- **Classified identity**: every bot result carries `bot: {name, category, vendor}`. AI agents are tagged `training` / `search` / `user-fetch` / `agent` / `other` per the vendor's own documentation (robots-policy and billing decisions need more than a boolean); classic automation as `search-crawler` / `seo` / `monitoring` / `social-preview`.

## Correction Layer

A declarative override config, [pkg/core/resources/corrections.yaml](./pkg/core/resources/corrections.yaml), fixes known detection gaps that the uap-core database cannot express or has not yet fixed upstream: in-app browsers (WeChat, VK, `X-Requested-With` package ids), vehicles (Tesla, Android Automotive), consoles (PS5 OS, Xbox model), Fire TV / Tizen TV, and an Android vendor-from-model prefix table (`SM-*` → Samsung, Xiaomi date codes, `CPH*` → OPPO, …).

- Rules are **matched behind cheap substring gates** (near-zero cost on mainstream traffic, hard cap 64 rules) and applied **after Client Hints** — corrections are terminal, but fill-gap guards ensure genuine CH data is never overwritten.
- The file is **embedded at build time and hot-swapped at runtime** with full validation: schema check, RE2 compilation, per-rule inline tests executed through the real pipeline before every swap; any failure keeps the last good rules.
- **Every mode gets live rules**: native builds fetch it once at startup and again on every update tick; the browser WASM build fetches it once at `initUA` (Fetch-backed `net/http`); the WASI fallback engines receive it from their Java/Node hosts via the `updateCorrections` export.
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
