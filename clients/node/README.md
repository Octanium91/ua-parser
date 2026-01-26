# Universal User-Agent Parser - Node.js Client

This is the Node.js wrapper for the high-performance Universal User-Agent Parser. It uses `koffi` to interface with the core Go-based shared library.

## Installation

The package is hosted on **GitHub Packages**. You need to configure your environment to use this registry.

### 1. Configure Registry

Create or update a `.npmrc` file in your project root:

```text
@octanium91:registry=https://npm.pkg.github.com
```

### 2. Install Package

```bash
npm install @octanium91/ua-parser
```

> **Note**: The package automatically includes the required native binaries for Windows, Linux, and macOS (amd64 and arm64).

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
        // The parser automatically looks for 'sec-ch-ua-*' keys.
        const result = parser.parse(req.headers['user-agent'], req.headers);

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(result));
    }).listen(3000);
}

start();
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
| `disable_auto_update` | `boolean` | `false` | If `true`, background regex updates are disabled. |
| `lru_cache_size` | `number` | `1000` | Number of entries to keep in the LRU cache. Set to `0` to disable. |
| `update_url` | `string` | *(official uap-core)* | Custom URL to download `regexes.yaml` from. |
| `update_interval` | `string` | `"24h"` | Interval for background updates (e.g., `"12h"`, `"1h"`). |

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
    "version": "11"
  },
  "device": {
    "model": "Pixel 5",
    "vendor": "Google",
    "type": "mobile"
  },
  "cpu": {
    "architecture": "arm64"
  },
  "engine": {
    "name": "Blink",
    "version": "119.0.6045.105"
  },
  "category": "mobile",
  "is_bot": false,
  "is_ai_crawler": false
}
```

## Usage (Browser / Bundlers)

The package supports WebAssembly and is compatible with modern bundlers like Webpack and Vite.

### Modern Bundlers (React, Vue, Vite, Webpack)

When using a bundler, the parser automatically attempts to resolve `wasm_exec.js` and `ua-parser.wasm` assets. You can use it directly without manual setup:

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

If you are not using a bundler or the automatic resolution fails:

1. Copy `ua-parser.wasm` and `wasm_exec.js` from `node_modules/@octanium91/ua-parser/lib/` to your public assets directory (e.g., `public/`).
2. Include `wasm_exec.js` in your HTML:
   ```html
   <script src="/wasm_exec.js"></script>
   ```
3. Initialize the parser providing the URL to the WASM file:
   ```javascript
   const parser = new UaParser('/ua-parser.wasm');
   await parser.init();
   ```

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
