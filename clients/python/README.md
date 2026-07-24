# Universal User-Agent Parser - Python Client

This is the Python wrapper for the high-performance Universal User-Agent Parser. It uses `ctypes` to interface with the core Go-based shared library.

## Installation

This package is distributed via GitHub Releases.

1.  Go to the [Releases Page](https://github.com/Octanium91/ua-parser/releases).
2.  Download the `.whl` file from the latest release (e.g., `ua_parser_core-1.1.7-py3-none-any.whl`).
3.  Install it using pip:
    ```bash
    pip install ./ua_parser_core-1.1.7-py3-none-any.whl
    ```

> **Note**: The package automatically includes the required native binaries for Windows and Linux (amd64 and arm64).

## Alpine Linux / musl

The Python client does **not** currently work on Alpine Linux or other musl-based systems. Go shared libraries (`-buildmode=c-shared`) cannot be loaded by musl's dynamic linker due to a Go toolchain limitation ([golang/go#54805](https://github.com/golang/go/issues/54805)); loading fails with an error like `initial-exec TLS resolves to dynamic definition`. On musl systems the client detects this and raises a `RuntimeError` with guidance.

Working alternatives:

- Run the ua-parser REST server container (`ghcr.io/octanium91/ua-parser`) next to your application and call it over HTTP.
- Use a glibc-based image instead (e.g. `python:3.12-slim`).
- Use the Node.js or Java clients, which fall back to WebAssembly automatically on musl.

Once the Go toolchain fix lands and the libraries are rebuilt, the bundled `libua-parser-linux-{arch}-musl.so` binaries will be picked up automatically on musl systems.

## Usage

```python
from uaparser import UaParser

# Initialize the parser
parser = UaParser()

# Initialize the core (starts updater if not disabled)
parser.init({"disable_auto_update": False, "lru_cache_size": 1000})

# Parse a User-Agent
ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
headers = {
    "Sec-CH-UA-Platform": '"Windows"',
    "Sec-CH-UA-Platform-Version": '"13.0.0"'
}

result = parser.parse(ua, headers)

print(f"OS: {result['os']['name']} {result['os']['version']}")
print(f"Browser: {result['browser']['name']} {result['browser']['version']}")
```

## Configuration

The `init()` method accepts a dictionary:
- `disable_auto_update` (bool): If true, background regex updates are disabled.
- `update_url` (string): Custom URL for regex updates.
- `update_interval` (string): Update interval (e.g., "24h").
- `lru_cache_size` (int): Number of entries to keep in the LRU cache.
