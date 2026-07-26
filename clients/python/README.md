# Universal User-Agent Parser - Python Client

This is the Python wrapper for the high-performance Universal User-Agent Parser. It uses `ctypes` to interface with the core Go-based shared library.

## Installation

> ⚠️ **Not on PyPI.** Do **not** run `pip install ua-parser` or `pip install ua-parser-core` — `ua-parser` on PyPI is an **unrelated** project and will install the wrong package (after which `from uaparser import UaParser` fails). This parser ships only as a wheel attached to GitHub Releases; install it as shown below.

This package is distributed via GitHub Releases.

1.  Go to the [Releases Page](https://github.com/Octanium91/ua-parser/releases) and note the latest version.
2.  Download the wheel — it is named `ua_parser_core-<VERSION>-py3-none-any.whl`.
3.  Install it using pip with the **exact filename** you downloaded (don't rely on a `*` wildcard — it isn't expanded on Windows cmd/PowerShell):
    ```bash
    pip install ./ua_parser_core-0.0.51-py3-none-any.whl
    ```

> **Note**: Install the **released wheel**, not a source checkout — only release wheels bundle the native driver. `pip install .` on a clone produces a driver-less package and `UaParser()` then raises `FileNotFoundError`. The release wheel automatically includes the native binaries for Windows, Linux, and macOS (amd64 and arm64). The distribution name is `ua-parser-core`, but the **import** name is `uaparser` (i.e. `from uaparser import UaParser`).

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

print(f"OS: {result['os']['name']} {result['os']['version']} ({result['os']['platform']})")
print(f"Browser: {result['browser']['name']} {result['browser']['version']}")
print(f"Device: {result['device']['vendor']} / {result['device']['form_factor']}")
print(f"CPU: {result['cpu']['architecture']} {result['cpu']['bitness']}, frozen UA: {result['is_frozen_ua']}")
if result.get("bot"):
    print(f"Bot: {result['bot']['name']} ({result['bot']['category']}, {result['bot']['vendor']})")
```

### Browser signals (optional)

`parse(ua, headers, signals)` accepts a dict of browser-side evidence that UA and Client Hints can't provide (Safari/Firefox send no Client Hints):

```python
result = parser.parse(ua, headers, signals={
    "max_touch_points": 5,        # unmasks iPads reporting a desktop (Mac) UA
    "webgl_renderer": "Apple M2", # Apple Silicon / Android SoC
    "screen": {"w": 1180, "h": 820, "dpr": 2},
})
```

Priority inside the engine: **Client Hints > signals > UA string**.

The parse result is a plain `dict`, so **every** field is available directly —
including `result_version`, `os.platform` / `os.version_name` / `os.version_raw`,
`device.form_factor`, `cpu.bitness`, `is_frozen_ua`, the convenience flags
(`is_mobile` / `is_desktop` / `is_touch_capable` / `is_chrome_family` /
`is_apple_silicon`), `automation`, `integrity`, `security`, `detection`,
`class_hash`, `bot` (`{name, category, vendor}`, absent/`None` for humans) and
`gpu`. Full field semantics: [root README](../../README.md#example-response).

## Forwarding headers from a real request

For maximum accuracy don't enumerate headers — copy the `User-Agent` plus **every** request header starting with `Sec-CH-` (and `X-Requested-With` if present). Flask example (same idea for Django/FastAPI):

```python
headers = {
    k.lower(): v
    for k, v in request.headers
    if k.lower().startswith("sec-ch-") or k.lower() == "x-requested-with"
}
result = parser.parse(request.headers.get("User-Agent", ""), headers)
```

Pass values raw, quotes included (`'"Windows"'`). See the [backend forwarding guide](../../README.md#forwarding-headers-from-your-backend) and [Requesting Client Hints](../../README.md#requesting-client-hints) (`Accept-CH`) in the root README.

## Configuration

The `init()` method accepts a dictionary:
- `disable_auto_update` (bool): Master switch — if true, no background fetching (regexes or corrections).
- `update_url` (string): Custom URL for regex updates.
- `update_interval` (string): Update interval (e.g., "24h").
- `lru_cache_size` (int): Number of entries to keep in the LRU cache.
- `corrections_url` (string): Custom URL for the hot-updated [correction layer](../../README.md#correction-layer) (`corrections.yaml`).
- `disable_corrections_update` (bool): Suppress only correction updates while regex updates continue (the embedded snapshot still applies).
