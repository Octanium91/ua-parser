# Universal User-Agent Parser - JavaScript Client (Node.js & Browser)

One npm package — `@octanium91/ua-parser` — running the same high-performance Go parsing core everywhere JavaScript runs. This is **not a server-only library**: the package ships both the native drivers for Node.js and a WebAssembly build for the browser, behind one API.

| Environment | Engine used | Jump to |
|---|---|---|
| **Node.js** — glibc Linux, Windows, macOS | native Go shared library via [koffi](https://koffi.dev/) (fastest) | [Usage (Node.js)](#usage-nodejs) |
| **Node.js** — Alpine / musl | automatic WebAssembly (WASI) fallback | [Alpine Linux / musl](#alpine-linux--musl) |
| **Browser / SPA** — React, Vue, Vite, Webpack | WebAssembly (js/wasm) build | [Usage (Browser / Bundlers)](#usage-browser--bundlers) |
| **Vanilla JS / CDN** — no bundler | same WASM build via the raw `initUA`/`parseUA` ABI | [Manual Setup](#manual-setup-vanilla-js--cdn) |

The API (`new UaParser()` → `init()` → `parse(ua, headers, signals)`) and the [result object](#result-object-structure) are identical in every environment. In the browser the client even auto-collects Client Hints and [browser signals](#browser-signals-optional) for you at `init()`.

## Installation

The same package covers every environment above — it bundles the native binaries for Windows, Linux, and macOS (amd64 and arm64), plus the WebAssembly builds for the Alpine/musl fallback and the browser.

> **No GitHub token? Use the auth-free tarball → jump to [Install without a token](#alternative-install-without-a-token).** The GitHub Packages path below needs a token because GitHub authenticates **every** package install, even for public repos.
>
> Either way you must install the **published package or release tarball**, not a git clone — the native drivers and WASM modules ship inside those, not in the git tree (`lib/` in git holds only `index.js`), so `npm install` in a checkout gives a driver-less package and `init()` throws `Shared library not found`.

The package is hosted on **GitHub Packages**, which requires authentication for every install — including public packages.

### 1. Create a token

Create a GitHub **Personal Access Token (classic)** with the `read:packages` scope: <https://github.com/settings/tokens>. Set it as `GITHUB_TOKEN` in your shell:

```bash
export GITHUB_TOKEN=ghp_xxx        # bash / zsh
# PowerShell:  $env:GITHUB_TOKEN = "ghp_xxx"
# cmd.exe:     set GITHUB_TOKEN=ghp_xxx
```

### 2. Configure the registry and auth

Create or update a `.npmrc` file in your project root with **both** lines:

```text
@octanium91:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```

### 3. Install the package

```bash
npm install @octanium91/ua-parser
```

### Alternative: install without a token

If you do not want to configure a GitHub token, download the tarball asset attached to the [latest release](https://github.com/Octanium91/ua-parser/releases/latest) (no auth required) and install it directly:

```bash
npm install ./octanium91-ua-parser-<version>.tgz
```

## Usage (Node.js)

### Basic Example

A real Chrome 150 on Windows 11 with the full input set — the same request/response pair as the root README's [Example Request](../../README.md#example-request), so you can reproduce the result verbatim. The frozen UA alone only says "Chrome 150.0.0.0 on Windows NT 10.0"; the Client Hints unlock the exact build and Windows 11, and the optional signals add the GPU and touch capability.

```javascript
const UaParser = require('@octanium91/ua-parser');

async function run() {
    // Initialize the parser
    const parser = new UaParser();

    // Initialize the core
    await parser.init({ 
        disable_auto_update: false, 
        lru_cache_size: 1000 
    });

    const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36";

    // Client Hints headers (in a real server, just pass req.headers — see the HTTP example)
    const headers = {
        "Sec-CH-UA": '"Not)A;Brand";v="8", "Chromium";v="150", "Google Chrome";v="150"',
        "Sec-CH-UA-Mobile": "?0",
        "Sec-CH-UA-Platform": '"Windows"',
        "Sec-CH-UA-Platform-Version": '"19.0.0"',
        "Sec-CH-UA-Arch": '"x86"',
        "Sec-CH-UA-Bitness": '"64"',
        "Sec-CH-UA-Model": '""',
        "Sec-CH-UA-Full-Version-List": '"Not)A;Brand";v="8.0.0.0", "Chromium";v="150.0.7871.182", "Google Chrome";v="150.0.7871.182"',
        "Sec-CH-UA-Form-Factors": '"Desktop"'
    };

    // Optional third argument: browser-collected evidence (see "Browser signals" below)
    const signals = {
        max_touch_points: 10,
        platform: "Win32",
        hardware_concurrency: 32,
        device_memory: 32,
        screen: { w: 1463, h: 915, dpr: 1.75 },
        webgl_vendor: "Google Inc. (AMD)",
        webgl_renderer: "ANGLE (AMD, AMD Radeon(TM) 8060S Graphics (0x00001586) Direct3D11 vs_5_0 ps_5_0, D3D11)"
    };

    const result = parser.parse(ua, headers, signals);

    console.log(`OS: ${result.os.name} ${result.os.version}`); // OS: Windows 11   (UA alone would say Windows 10)
    console.log(`Browser: ${result.browser.name} ${result.browser.version}`); // Browser: Chrome 150.0.7871.182   (UA alone: 150.0.0)
    console.log(`Category: ${result.category}`); // Category: desktop
    console.log(`Touch: ${result.is_touch_capable}`); // Touch: true   (from signals)
    console.log(`GPU: ${result.gpu.renderer}`); // GPU: ANGLE (AMD, AMD Radeon(TM) 8060S Graphics ...)   (from signals)
}

run();
```

### HTTP Server Example

```javascript
const http = require('http');
const UaParser = require('@octanium91/ua-parser');

const parser = new UaParser();

async function start() {
    // Remember to initialize the parser!
    await parser.init();

    http.createServer((req, res) => {
        // Simply pass the request headers object. The parser reads the
        // 'sec-ch-ua-*' keys and 'x-requested-with' (Android WebView in-app
        // detection) — passing the whole headers object means new signals
        // flow through with no code change. See the backend forwarding guide
        // in the root README.
        const result = parser.parse(req.headers['user-agent'], req.headers);

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(result));
    }).listen(3000);
}

start();
```

### Alpine Linux / musl

The native `koffi` path is **not available on Alpine Linux** (or any musl-based distribution): Go `c-shared` libraries currently cannot be loaded by the musl dynamic linker due to an upstream Go toolchain limitation ([golang/go#54805](https://github.com/golang/go/issues/54805)).

On such systems the client detects the failure and **automatically falls back to the bundled WebAssembly module**, executed via `node:wasi` (requires Node.js >= 18.17). No code changes are needed — `init()` and `parse()` work exactly the same.

The overhead is small because V8 JIT-compiles the WASM module: measured ~0.5s for `init()` and ~36ms for the first `parse()` (node:22-alpine); subsequent parses are faster and results are LRU-cached.

> **Note**: In WASM mode `disable_auto_update` defaults to `true` (the WASM core assumes no network access), unlike native mode where it defaults to `false`. Pass `disable_auto_update: false` explicitly to `init()` if you want background regex updates in WASM mode.

## Usage (Browser / Bundlers)

The same package runs fully client-side — in a SPA (React, Vue, …) or plain `<script>` page — via the bundled WebAssembly build. In the browser the engine **auto-collects all available evidence at `init()`** — the full high-entropy Client Hints set and the navigator/screen signals (see [Auto-collected Client Hints & signals](#auto-collected-client-hints--signals)) — so a bare `parse(navigator.userAgent)` already delivers Client-Hints-grade accuracy with no server round-trip.

### Modern Bundlers (React, Vue, Vite, Webpack)

In the browser the parser runs the `ua-parser-js.wasm` module via Go's `wasm_exec.js`. Bundlers **do not** turn a bare `.wasm` import into a servable URL automatically, so you must (a) make sure both `ua-parser-js.wasm` and `wasm_exec.js` are emitted/served, and (b) hand the wasm URL to the parser. Pass the URL to the constructor — `new UaParser(wasmUrl)` — or, if you omit it, the parser fetches `/ua-parser-js.wasm` from the site root (so serving both files at the web root also works).

**Vite** — import the wasm as a URL and load `wasm_exec.js` once:

```javascript
import { UaParser } from '@octanium91/ua-parser';
import wasmUrl from '@octanium91/ua-parser/lib/ua-parser-js.wasm?url';
import '@octanium91/ua-parser/lib/wasm_exec.js'; // defines globalThis.Go

async function detect() {
    const parser = new UaParser(wasmUrl);       // pass the resolved wasm URL
    await parser.init({ collect_gpu: true });   // GPU probe is opt-in; hints & signals are collected by default

    // No headers/signals arguments needed in the browser — init() already
    // collected the high-entropy Client Hints and the navigator signals.
    const result = parser.parse(navigator.userAgent);

    console.log(result.os.version_name);        // "Windows 11" — real version from auto-collected CH, not the frozen UA
    console.log(`${result.browser.name} ${result.browser.version}`); // exact build, e.g. "Chrome 150.0.7871.182"
    console.log(result.detection);              // { client_hints_used: true, high_entropy: true, signals_used: true }
    console.log(result.is_touch_capable);       // from the auto-collected signals
    console.log(result.gpu?.renderer);          // "ANGLE (AMD, AMD Radeon(TM) 8060S ..." — present because of collect_gpu
}
```

**Webpack 5** — emit the wasm as an asset (`type: 'asset/resource'` for `.wasm`, or `new URL('.../ua-parser-js.wasm', import.meta.url)`) and pass that URL to `new UaParser(url)`; load `wasm_exec.js` the same way.

### Auto-collected Client Hints & signals

At `init()` the browser build gathers everything the page can offer, so a plain `parse(navigator.userAgent)` is already maximal:

- **Client Hints** — reconstructed from `navigator.userAgentData.getHighEntropyValues()` (brands, mobile, platform, platformVersion, model, architecture, bitness, fullVersionList, formFactors) into the same `sec-ch-ua-*` headers a server would receive — no `Accept-CH` round-trip needed. Chromium-only: Safari and Firefox don't implement UA-CH.
- **Signals** — `maxTouchPoints`, `navigator.platform`, `deviceMemory`, `hardwareConcurrency`, screen size + `devicePixelRatio`. Collected in every browser; on Safari/Firefox this is all the evidence there is — it's what unmasks e.g. iPads reporting a desktop Mac UA.
- **GPU (opt-in)** — pass `{ collect_gpu: true }` to `init()` to probe the WebGL renderer (adds the `gpu` block to results). Off by default because it's fingerprinting-adjacent.

How it merges: **explicit arguments always win** — `parse(ua, headers, signals)` uses the collected Client Hints only when you pass no `headers` at all, and the collected signals only when you omit `signals`. Opt out of collection entirely with `{ disable_signal_collection: true }`.

> The high-entropy hints resolve asynchronously right after `init()` (a `getHighEntropyValues` promise); a parse in the very same tick may not have them yet. `result.detection.client_hints_used` always tells you what was actually used.

> The `.wasm` must be served with `Content-Type: application/wasm` (required by `WebAssembly.instantiateStreaming`); most static hosts and dev servers do this by default. If yours doesn't, either fix the MIME type or the loader falls back to `arrayBuffer` instantiation.

### Manual Setup (Vanilla JS / CDN)

If you are not using a bundler, the `UaParser` class is not available as a plain global, so use the underlying WASM ABI directly. It exposes two global functions after the module starts: `initUA(configJson)` and `parseUA(payloadJson)`, both taking/returning JSON **strings**.

1. Get the two assets. No npm token is needed — download them straight from the release:
   - `https://github.com/Octanium91/ua-parser/releases/latest/download/ua-parser-js.wasm`
   - `https://github.com/Octanium91/ua-parser/releases/latest/download/wasm_exec.js`

   (or copy them from `node_modules/@octanium91/ua-parser/lib/` if you installed via npm). Place them in your public assets directory.

2. Load `wasm_exec.js` (Go's official js/wasm loader) and instantiate the module:

   ```html
   <script src="/wasm_exec.js"></script>
   <script>
     (async () => {
       const go = new Go();
       const result = await WebAssembly.instantiateStreaming(fetch('/ua-parser-js.wasm'), go.importObject);
       go.run(result.instance); // registers globalThis.initUA / globalThis.parseUA

       // Config is optional; disable_auto_update defaults to true in WASM mode.
       initUA(JSON.stringify({ disable_auto_update: true, lru_cache_size: 1000 }));

       const payload = JSON.stringify({ ua: navigator.userAgent, headers: {} });
       const info = JSON.parse(parseUA(payload));
       console.log(info.browser.name, info.os.name); // e.g. "Chrome" "Windows"
     })();
   </script>
   ```

   `parseUA` accepts a JSON payload `{ "ua": "<string>", "headers": { "Sec-CH-UA-Platform": "\"Windows\"", ... } }` and returns the same [result object](#result-object-structure) as a JSON string. `initUA` returns `null` on success or an error string.

> **Note**: The browser uses the `ua-parser-js.wasm` build (js/wasm ABI, loaded via `wasm_exec.js`). The `ua-parser.wasm` file also shipped in `lib/` is a WASI (wasip1) build used only by the Node.js fallback on Alpine/musl — do not use it in the browser.

### React Example (WASM)
For maximum accuracy, especially to detect **Windows 11**, always pass Client Hints collected from your server. See [Collecting Client Hints](#collecting-client-hints) for details.

```javascript
import { useEffect, useState } from 'react';
import { UaParser } from '@octanium91/ua-parser';

function App() {
  const [result, setResult] = useState(null);

  useEffect(() => {
    async function parse() {
      const parser = new UaParser(wasmUrl);  // wasmUrl imported as above
      await parser.init();

      // Server-injected UA + Client Hints when present; fall back to the
      // browser's own UA so this still works without server injection —
      // on Chromium the auto-collected Client Hints from init() then fill
      // the gap (empty headers → collected ones are used).
      // (parse(undefined) does NOT throw — it silently parses an empty UA,
      //  so always provide a real UA string.)
      const ua = window.__UA__ ?? navigator.userAgent;
      const hints = window.__CH_HEADERS__ ?? {};

      setResult(parser.parse(ua, hints));
    }
    parse();
  }, []);

  return (
    <div>
      {result && <pre>{JSON.stringify(result, null, 2)}</pre>}
    </div>
  );
}
```

## Collecting Client Hints

Modern browsers "freeze" the User-Agent string. To get accurate data (like Windows 11 or full browser versions), you must use **Client Hints**.

### Recommended: Nginx Configuration (Server-Side)

Getting data via HTTP headers is the most reliable method. Browsers automatically send `Sec-CH-UA` headers via HTTPS. Ensure your Nginx configuration passes these headers to your Node.js application.

> **Note**: The `/api/ua-hints` location below is used as an example for **Option B: Fetch from API**. If you are running a standard SSR server, you should apply these `proxy_set_header` directives to your main `location /` block.

**Nginx Config:**

```nginx
location /api/ua-hints {
    proxy_pass http://your_node_app:3000;
    
    # Standard headers
    proxy_set_header User-Agent $http_user_agent;

    # Client Hints - Explicitly pass these to the backend
    proxy_set_header Sec-CH-UA $http_sec_ch_ua;
    proxy_set_header Sec-CH-UA-Mobile $http_sec_ch_ua_mobile;
    proxy_set_header Sec-CH-UA-Platform $http_sec_ch_ua_platform;
    proxy_set_header Sec-CH-UA-Platform-Version $http_sec_ch_ua_platform_version;
    proxy_set_header Sec-CH-UA-Model $http_sec_ch_ua_model;
    proxy_set_header Sec-CH-UA-Full-Version-List $http_sec_ch_ua_full_version_list;
    proxy_set_header Sec-CH-UA-Arch $http_sec_ch_ua_arch;
    proxy_set_header Sec-CH-UA-Bitness $http_sec_ch_ua_bitness;
}
```

**React Example (SPA):**

When using the parser in a React app (WASM mode), you have two recommended ways to get Client Hints from your server to ensure maximum accuracy.

#### Option A: HTML Injection (Fastest)
Your server (e.g., Nginx or Node.js) injects the collected headers and the raw User-Agent directly into your HTML template. This is the fastest method as it avoids extra network requests.

```javascript
import { useEffect, useState } from 'react';
import { UaParser } from '@octanium91/ua-parser';

function App() {
  const [result, setResult] = useState(null);

  useEffect(() => {
    async function init() {
      const parser = new UaParser();
      await parser.init();

      // These should be populated by your server during page render
      const serverHints = window.__CH_HEADERS__; 
      const userAgent = window.__UA__;

      const res = parser.parse(userAgent, serverHints);
      setResult(res);
    }
    init();
  }, []);

  return (
    <div>
      <h1>Device Info</h1>
      {result && <pre>{JSON.stringify(result, null, 2)}</pre>}
    </div>
  );
}
```

#### Option B: Fetch from API
If you cannot modify the HTML (e.g., when serving from a static CDN), fetch the collected headers from a dedicated endpoint on your server.

```javascript
  useEffect(() => {
    async function init() {
      const parser = new UaParser();
      await parser.init();

      // Fetch headers from your backend
      const response = await fetch('/api/ua-hints');
      const { ua, headers } = await response.json(); 
      
      // Use server-provided UA and headers
      setResult(parser.parse(ua, headers));
    }
    init();
  }, []);
```

### Alternative: Client-side (SPA)

> **Note**: In the browser (WASM) build the engine **already collects** the high-entropy Client Hints and browser signals for you at `init()` (see [Browser signals](#browser-signals-optional)) — you usually don't need the manual code below. It remains valid if you want explicit control or must override what was collected.

> **⚠️ STRICTLY NOT RECOMMENDED**: Manually relying on the `navigator.userAgentData` API is less reliable than server-side headers, may be blocked by privacy settings, and adds asynchronous complexity. Use the **Server-Side (Nginx)** approach whenever possible.

If you must collect high-entropy values via JS yourself:

```javascript
const getClientHints = async () => {
  const headers = {};
  
  // Check if the API is supported
  if (window.navigator.userAgentData) {
    const highEntropyValues = await window.navigator.userAgentData.getHighEntropyValues([
      "platform",
      "platformVersion",
      "architecture",
      "model",
      "bitness",
      "fullVersionList"
    ]);

    // Construct headers manually to match the parser expectations
    headers["Sec-CH-UA"] = window.navigator.userAgentData.brands
        .map(b => `"${b.brand}"; v="${b.version}"`)
        .join(", ");
    headers["Sec-CH-UA-Mobile"] = window.navigator.userAgentData.mobile ? "?1" : "?0";
    headers["Sec-CH-UA-Platform"] = `"${highEntropyValues.platform}"`;
    headers["Sec-CH-UA-Platform-Version"] = `"${highEntropyValues.platformVersion}"`;
    headers["Sec-CH-UA-Arch"] = `"${highEntropyValues.architecture}"`;
    headers["Sec-CH-UA-Model"] = `"${highEntropyValues.model}"`;
    headers["Sec-CH-UA-Bitness"] = `"${highEntropyValues.bitness}"`;
  }
  
  return headers;
};

// Usage
const headers = await getClientHints();
const result = parser.parse(navigator.userAgent, headers);
```

## Configuration

The `init(config)` method accepts an optional configuration object (all environments):

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `disable_auto_update` | `boolean` | `false` (native), `true` (WASM) | If `true`, background regex updates are disabled. In WASM mode (browser, or the automatic Alpine/musl fallback) the default is `true`. |
| `lru_cache_size` | `number` | `1000` | Number of entries to keep in the LRU cache. Set to `0` to disable. |
| `update_url` | `string` | *(official uap-core)* | Custom URL to download `regexes.yaml` from. |
| `update_interval` | `string` | `"24h"` | Interval for background updates (e.g., `"12h"`, `"1h"`). |
| `corrections_url` | `string` | *(this repo's `main`)* | Custom URL for the hot-updated correction layer (`corrections.yaml`). |
| `disable_corrections_update` | `boolean` | `false` | If `true`, runtime correction updates are disabled (the embedded snapshot still applies). See note below. |
| `collect_gpu` | `boolean` | `false` | **Browser build only.** Probe the WebGL renderer at init and add the `gpu` block to results. Off by default (fingerprinting-adjacent). |
| `disable_signal_collection` | `boolean` | `false` | **Browser build only.** If `true`, skips the [automatic Client Hints & signals collection](#auto-collected-client-hints--signals) at init. |

> **Correction updates in the browser/WASM client:** even though `disable_auto_update` defaults to `true` in WASM mode (the multi-MB regex DB is release-bound), the small [correction layer](../../README.md#correction-layer) is still fetched — in the browser the module fetches `corrections.yaml` itself at init; in the Node WASI fallback the client fetches and pushes it. Set `disable_corrections_update: true` to opt out.

## Browser signals (optional)

`parse(ua, headers, signals)` accepts a third argument with browser-side evidence that UA and Client Hints can't provide (Safari/Firefox send no Client Hints at all). The **browser build collects these automatically** at init ([details](#auto-collected-client-hints--signals)); for the Node client you pass them explicitly:

```js
const result = parser.parse(req.headers['user-agent'], req.headers, {
    max_touch_points: 5,          // unmasks iPads reporting a desktop (Mac) UA
    platform: 'MacIntel',
    webgl_renderer: 'Apple M2',   // Apple Silicon / Android SoC (fingerprinting-adjacent)
    screen: { w: 1180, h: 820, dpr: 2 }
});
```

Priority inside the engine: **Client Hints > signals > UA string**.

## Result Object Structure

The `parse()` method returns a detailed object (identical shape in Node.js and the browser):

```json
{
  "result_version": "1.2",
  "ua": "Mozilla/5.0 ...",
  "browser": { "name": "Chrome", "version": "126.0.6478.61", "major": "126", "type": "browser" },
  "os": { "name": "Android", "version": "14", "platform": "android", "version_name": "Android 14", "version_raw": "14" },
  "device": { "model": "Pixel 8", "vendor": "Google", "type": "mobile", "form_factor": "mobile" },
  "cpu": { "architecture": "arm64", "bitness": "64" },
  "engine": { "name": "Blink", "version": "126.0.6478.61" },
  "category": "mobile",
  "is_bot": false, "is_ai_crawler": false, "is_frozen_ua": true,
  "is_mobile": true, "is_desktop": false, "is_touch_capable": true,
  "is_chrome_family": true, "is_apple_silicon": false,
  "automation": { "headless": false, "electron": false, "webdriver": false },
  "integrity": { "spoofed": false, "reasons": [] },
  "security": { "suspicious": false },
  "detection": { "client_hints_used": true, "high_entropy": true, "signals_used": false },
  "class_hash": "9a1c0f5e2b7d4e83"
}
```

> `bot` and `gpu` are the only **optional** keys — they are present only for bots and when a WebGL signal was supplied, and **omitted entirely** otherwise (check with `if (result.bot)`, not `result.bot === null`). Every other field is always present.

Field notes (the full [root-README reference](../../README.md#example-response) has the complete table):

| Field | Description |
|-------|-------------|
| `result_version` | Version of the result JSON shape (e.g. `"1.2"`) — traceability for stored results. |
| `os.platform` | Canonical machine-readable OS key: `windows`, `macos`, `ios`, `android`, `chromeos`, `linux`, `tizen`, `playstation`, `other`. `os.name` keeps the marketing spelling. |
| `os.version_name` / `os.version_raw` | Human label (`macOS Sonoma`, `Windows 11`) and the exact CH version (`19.0.0` behind Windows `11`). |
| `device.form_factor` | `desktop` / `mobile` / `tablet` / `watch` / `xr` / `automotive` / `tv` (from `Sec-CH-UA-Form-Factors` when present, else derived from `type`). |
| `cpu.bitness` | `"64"`, `"32"`, or `""`. |
| `is_frozen_ua` | `true` when the UA is a frozen/reduced template (Chromium reduced UA, `Android 10; K`, capped `Mac OS X 10_15_7`) — a hint to trust Client Hints over the UA. |
| `is_mobile` / `is_desktop` / `is_touch_capable` / `is_chrome_family` / `is_apple_silicon` | Convenience booleans for common branching. |
| `automation` | `{ headless, electron, webdriver }` — **undeclared** automation (unlike `is_bot`). |
| `integrity` | `{ spoofed, reasons[] }` — UA vs Client Hints vs signals consistency (spoofed clients). |
| `security` | `{ suspicious, category }` — attack payloads in the UA (scanners, SQL-injection, XSS). |
| `detection` | `{ client_hints_used, high_entropy, signals_used }` — which richer inputs were present (all `false` on a UA-only parse). |
| `class_hash` | Stable hash of the client **class** — an analytics bucket key, identical for every client of the same class. Deliberately coarse, **not** a tracking fingerprint. |
| `bot` | **omitted** for humans; for bots `{ "name", "category", "vendor" }` where category is `training` / `search` / `user-fetch` / `agent` / `other` (AI) or `search-crawler` / `seo` / `monitoring` / `social-preview` (classic). |
| `gpu` | **omitted** unless a WebGL signal was supplied; then `{ "vendor", "renderer" }`. |

Example bot result:

```json
{
  "browser": { "name": "ChatGPT-User", "version": "1.0", "major": "1", "type": "bot" },
  "category": "bot",
  "is_bot": true,
  "is_ai_crawler": true,
  "bot": { "name": "ChatGPT-User", "category": "user-fetch", "vendor": "OpenAI" }
}
```

## TypeScript

Bundled type declarations aren't shipped yet, so TypeScript sees the client as `any`. The surface is small — `new UaParser(libPath?)`, `init(config?)`, `parse(ua, headers?, signals?)` — and the returned object matches the [Result Object Structure](#result-object-structure) above; declare a local `interface` from that shape if you want typing today.

## Why Koffi?

For the Node.js native path we use [Koffi](https://koffi.dev/) instead of `ffi-napi` because it is:
- Faster.
- Better supported on modern Node.js versions.
- Easier to use with simple API.

(The browser build doesn't use FFI at all — it runs the WebAssembly module directly.)
