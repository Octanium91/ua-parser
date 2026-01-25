# Universal User-Agent Parser - Node.js Client

This is the Node.js wrapper for the high-performance Universal User-Agent Parser. It uses `koffi` to interface with the core Go-based shared library.

## Installation

Install the package from GitHub Packages:
```bash
npm install @octanium91/ua-parser
```

> **Note**: The package automatically includes the required native binaries for Windows and Linux (amd64 and arm64).

## Usage

```javascript
const UaParser = require('@octanium91/ua-parser');

// Initialize the parser
const parser = new UaParser();

// Initialize the core
parser.init({ disable_auto_update: false, lru_cache_size: 1000 });

// Parse a User-Agent
const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36";
const headers = {
    "Sec-CH-UA-Platform": '"Windows"',
    "Sec-CH-UA-Platform-Version": '"13.0.0"'
};

const result = parser.parse(ua, headers);

console.log(`OS: ${result.os.name} ${result.os.version}`);
console.log(`Browser: ${result.browser.name} ${result.browser.version}`);
```

## Why Koffi?

We use [Koffi](https://koffi.dev/) instead of `ffi-napi` because it is:
- Faster.
- Better supported on modern Node.js versions.
- Easier to use with simple API.
