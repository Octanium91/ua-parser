# Universal User-Agent Parser - Node.js Client

This is the Node.js wrapper for the high-performance Universal User-Agent Parser. It uses `koffi` to interface with the core Go-based shared library.

## Installation

Install the package from GitHub Packages:
```bash
npm install @octanium91/ua-parser
```

> **Note**: The package automatically includes the required native binaries for Windows and Linux (amd64 and arm64).

## Usage (Node.js)

```javascript
const UaParser = require('@octanium91/ua-parser');

async function run() {
    // Initialize the parser
    const parser = new UaParser();

    // Initialize the core
    await parser.init({ disable_auto_update: false, lru_cache_size: 1000 });

    // Parse a User-Agent
    const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36";
    const headers = {
        "Sec-CH-UA-Platform": '"Windows"',
        "Sec-CH-UA-Platform-Version": '"13.0.0"'
    };

    const result = parser.parse(ua, headers);

    console.log(`OS: ${result.os.name} ${result.os.version}`);
    console.log(`Browser: ${result.browser.name} ${result.browser.version}`);
}

run();
```

## Usage (Browser)

The package supports WebAssembly for use in browser environments.

### 1. Setup
Copy `ua-parser.wasm` and `wasm_exec.js` from `node_modules/@octanium91/ua-parser/lib/` to your public assets directory (e.g., `public/`).

### 2. Implementation
Include `wasm_exec.js` in your HTML and use the parser:

```html
<script src="/wasm_exec.js"></script>
<script type="module">
    import { UaParser } from 'https://cdn.jsdelivr.net/npm/@octanium91/ua-parser/lib/index.js'; // Or use your bundler

    async function init() {
        const parser = new UaParser('/ua-parser.wasm');
        await parser.init();

        const result = parser.parse(navigator.userAgent);
        console.log(result);
    }

    init();
</script>
```

### React Example
```javascript
import { useEffect, useState } from 'react';
import UaParser from '@octanium91/ua-parser';

function App() {
  const [result, setResult] = useState(null);

  useEffect(() => {
    async function parse() {
      const parser = new UaParser('/ua-parser.wasm');
      await parser.init();
      setResult(parser.parse(navigator.userAgent));
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
