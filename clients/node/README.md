# Universal User-Agent Parser - Node.js Client

This is the Node.js wrapper for the high-performance Universal User-Agent Parser. It uses `koffi` to interface with the core Go-based shared library.

## Installation

The package is hosted on **GitHub Packages**. GitHub Packages requires authentication for **every** install — including public packages — so you need a GitHub Personal Access Token even though this repository is public. (See [Alternative: install without a token](#alternative-install-without-a-token) below if you cannot use one.)

### 1. Create a token

Create a GitHub **Personal Access Token (classic)** with the `read:packages` scope: <https://github.com/settings/tokens>. Export it, e.g. `export GITHUB_TOKEN=ghp_xxx`.

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

> **Note**: The package automatically includes the required native binaries for Windows, Linux, and macOS (amd64 and arm64), plus WebAssembly builds for the Alpine/musl fallback and the browser.

### Alternative: install without a token

If you do not want to configure a GitHub token, download the tarball asset attached to the [latest release](https://github.com/Octanium91/ua-parser/releases/latest) (no auth required) and install it directly:

```bash
npm install ./octanium91-ua-parser-<version>.tgz
```

## Usage (Node.js)

### Basic Example

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

    // Parse a User-Agent with Client Hints for maximum accuracy
    const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36";
    const headers = {
        "Sec-CH-UA-Platform": '"Windows"',
        "Sec-CH-UA-Platform-Version": '"13.0.0"',
        "Sec-CH-UA-Full-Version-List": '"Chromium";v="119.0.6045.105", "Google Chrome";v="119.0.6045.105"'
    };

    const result = parser.parse(ua, headers);

    console.log(`OS: ${result.os.name} ${result.os.version}`); // OS: Windows 11
    console.log(`Browser: ${result.browser.name} ${result.browser.version}`); // Browser: Chrome 119.0.6045.105
    console.log(`Category: ${result.category}`); // Category: desktop
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
        // Simply pass the request headers object.
        // The parser automatically looks for 'sec-ch-ua-*' keys, and future
        // signals (e.g. x-requested-with for in-app detection) flow through
        // automatically — see the backend forwarding guide in the root README.
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

> **⚠️ STRICTLY NOT RECOMMENDED**: This method is discouraged for production use. Relying on the `navigator.userAgentData` API is less reliable than server-side headers, may be blocked by privacy settings, and adds asynchronous complexity. Use the **Server-Side (Nginx)** approach whenever possible.

If you must collect high-entropy values via JS:

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

The `init(config)` method accepts an optional configuration object:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `disable_auto_update` | `boolean` | `false` (native), `true` (WASM) | If `true`, background regex updates are disabled. In WASM mode (browser, or the automatic Alpine/musl fallback) the default is `true`. |
| `lru_cache_size` | `number` | `1000` | Number of entries to keep in the LRU cache. Set to `0` to disable. |
| `update_url` | `string` | *(official uap-core)* | Custom URL to download `regexes.yaml` from. |
| `update_interval` | `string` | `"24h"` | Interval for background updates (e.g., `"12h"`, `"1h"`). |
| `corrections_url` | `string` | *(this repo's `main`)* | Custom URL for the hot-updated correction layer (`corrections.yaml`). |
| `disable_corrections_update` | `boolean` | `false` | If `true`, runtime correction updates are disabled (the embedded snapshot still applies). See note below. |

> **Correction updates in the browser/WASM client:** even though `disable_auto_update` defaults to `true` in WASM mode (the multi-MB regex DB is release-bound), the small [correction layer](../../README.md#correction-layer) is still fetched — in the browser the module fetches `corrections.yaml` itself at init; in the Node WASI fallback the client fetches and pushes it. Set `disable_corrections_update: true` to opt out.

## Browser signals (optional)

`parse(ua, headers, signals)` accepts a third argument with browser-side evidence that UA and Client Hints can't provide (Safari/Firefox send no Client Hints at all). The **browser build collects these automatically** at init; for the Node client you pass them explicitly:

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

The `parse()` method returns a detailed object:

```json
{
  "ua": "Mozilla/5.0 ...",
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
    "model": "Pixel 5",
    "vendor": "Google",
    "type": "mobile",
    "form_factor": "mobile"
  },
  "cpu": {
    "architecture": "arm64",
    "bitness": "64"
  },
  "engine": {
    "name": "Blink",
    "version": "119.0.6045.105"
  },
  "category": "mobile",
  "is_bot": false,
  "is_ai_crawler": false,
  "is_frozen_ua": false,
  "bot": null,
  "gpu": null
}
```

Field notes (added in Result v1.1):

| Field | Description |
|-------|-------------|
| `os.platform` | Canonical machine-readable OS key: `windows`, `macos`, `ios`, `android`, `chromeos`, `linux`, `tizen`, `playstation`, `other`. `os.name` keeps the marketing spelling. |
| `device.form_factor` | `desktop` / `mobile` / `tablet` / `watch` / `xr` / `automotive` / `tv` (from `Sec-CH-UA-Form-Factors` when present, else derived from `type`). |
| `cpu.bitness` | `"64"`, `"32"`, or `""`. |
| `is_frozen_ua` | `true` when the UA is a frozen/reduced template (Chromium reduced UA, `Android 10; K`, capped `Mac OS X 10_15_7`) — a hint to trust Client Hints over the UA. |
| `bot` | `null` for humans; for bots `{ "name", "category", "vendor" }` where category is `training` / `search` / `user-fetch` / `agent` (AI) or `search-crawler` / `seo` / `monitoring` / `social-preview` (classic). |
| `gpu` | `null` unless a WebGL signal was supplied; then `{ "vendor", "renderer" }`. |

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

## Usage (Browser / Bundlers)

The package supports WebAssembly and is compatible with modern bundlers like Webpack and Vite.

### Modern Bundlers (React, Vue, Vite, Webpack)

When using a bundler, the parser automatically attempts to resolve `wasm_exec.js` and `ua-parser-js.wasm` assets. You can use it directly without manual setup:

```javascript
import { UaParser } from '@octanium91/ua-parser';

async function init() {
    const parser = new UaParser();
    await parser.init();

    // Recommended: Use User-Agent provided by your server
    const result = parser.parse(window.__UA__); 
    console.log(result);
}
```

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
      const parser = new UaParser();
      await parser.init();
      
      // Use hints and UA injected by server
      const hints = window.__CH_HEADERS__;
      const ua = window.__UA__;

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

## Why Koffi?

We use [Koffi](https://koffi.dev/) instead of `ffi-napi` because it is:
- Faster.
- Better supported on modern Node.js versions.
- Easier to use with simple API.
