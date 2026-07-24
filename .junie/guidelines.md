# Documentation & Coding Guidelines

## Language
- All code documentation, including comments, docstrings, and README files, must be written exclusively in **English**.
- Variable names, functions, and other identifiers should be descriptive and in English.

## Header Priority
- When parsing User-Agent data, **Client Hints (Sec-CH-UA headers) must take priority** over the raw User-Agent string.
- The logic should first check for available Client Hints to determine the Operating System (e.g., distinguishing Windows 11 from Windows 10) and Device details before falling back to Regex-based UA parsing.

## CI/CD
- The project uses **GitHub Actions** for automated builds and testing.
- Any changes to the core logic or infrastructure should be verified against existing CI workflows.

## Go Version
- The project uses **Go 1.26** as the minimum version.

## musl/Alpine: native loading is impossible — do NOT try build-level workarounds
- Go's `c-shared` (and `c-archive`) buildmode emits **initial-exec TLS** for the runtime; musl's dynamic loader rejects `dlopen()` of such libraries with "initial-exec TLS resolves to dynamic definition" ([golang/go#54805](https://github.com/golang/go/issues/54805), [#13492](https://github.com/golang/go/issues/13492)). This affects **every FFI host** (JNA, koffi, ctypes) on Alpine, even for `.so` files built natively with the musl toolchain.
- **No build flags fix this** (verified empirically on Go 1.26.5, 2026-07-24): `CGO_CFLAGS="-ftls-model=global-dynamic"` only affects cgo C code, not the Go runtime's own TLS access; `-Wl,-Bsymbolic` is already applied by Go and does not help; a two-step `c-archive` + `gcc -shared` build has the same IE TLS relocations. `LD_PRELOAD` and gcompat also do not work (JVM segfault / loader-level rejection).
- The upstream fix is the `-tls` linker flag (general-dynamic/TLSDESC) in [golang/go PR #75048](https://github.com/golang/go/pull/75048) — unmerged; expected in Go 1.27 at the earliest. When a Go release ships it, rebuild the musl `.so` artifacts and native loading on Alpine will start working; the client loaders already attempt the musl `.so` first by design.
- **Supported strategy on Alpine until then**: the clients fall back to WebAssembly automatically — Java uses Chicory (with the `compiler` module — never remove it, the interpreter takes 60+ s to start), Node.js uses `node:wasi` (Node >= 18.17), the browser uses a separate `GOOS=js` build (`cmd/wasmjs`, `ua-parser-js.wasm` + `wasm_exec.js`). Python has no WASM fallback and raises a descriptive error on musl. Keep shipping the musl `.so` artifacts for forward compatibility.
- The `ua-parser.wasm` (wasip1 reactor, `cmd/wasm`) and `ua-parser-js.wasm` (browser, `cmd/wasmjs`) are **different ABIs** — never interchange them.
- ARM64 builds in CI run on **native `ubuntu-24.04-arm` runners** — do not reintroduce QEMU-emulated Go builds (flaky: [golang/go#68976](https://github.com/golang/go/issues/68976)).

## Code Quality
- Maintain high test coverage for both Regex and Client Hints logic.
- Ensure thread safety when handling shared resources (like the parser instance and cache).

## Package Distribution
- **Official Repository**: [https://github.com/Octanium91/ua-parser](https://github.com/Octanium91/ua-parser)
- **Multi-Platform Clients**: The project provides official clients for multiple platforms (Go, Python, Node.js, Java) located in the `/clients` directory.
- **Package Distribution**:
  - All artifacts are primarily published to **GitHub Packages** (Maven for Java, npm for Node.js).
  - Docker images are published to **GitHub Container Registry (ghcr.io)**.
  - Shared libraries and Python wheels are distributed via **GitHub Releases**.
- **Version Management**: Manual changes to package versions (e.g., in `package.json`, `pom.xml`, `setup.py`) are strictly prohibited. Versions are automatically managed and synchronized by the CI/CD pipeline during the release process based on the Git tag.
- **Installation Guides**: All installation documentation must explicitly state that for Node.js and Java, the user must configure their local package manager (npm, Maven) to use the GitHub Packages registry. Since the repository is public, authentication is generally not required for downloading packages, but registry configuration is still necessary.

## Performance & Logging
- The application is designed for **high performance**; use LRU caching and avoid unnecessary allocations in the hot path.
- The system must provide **clear logs for resource updates** (e.g., downloading and swapping `regexes.yaml`) to ensure observability of the background updater.

## Git Operations
- **Restrictions**: The AI assistant is strictly prohibited from performing any Git operations, including `git commit`, `git push`, `git tag`, or creating GitHub releases.
- **Responsibility**: All version control operations and deployment triggers must be performed manually by the user.
