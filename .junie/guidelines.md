# Documentation & Coding Guidelines

## Language
- All code documentation, including comments, docstrings, and README files, must be written exclusively in **English**.
- Variable names, functions, and other identifiers should be descriptive and in English.

## Architecture overview
- `pkg/core` — the parser core (regex + Client Hints logic, LRU cache, background updater). Everything else wraps it.
- `cmd/server` — standalone HTTP REST server (`CGO_ENABLED=0`, static, embeds the core).
- `cmd/cshared` — C-shared library build (`.so`/`.dll`/`.dylib`) for the FFI clients (JNA/koffi/ctypes).
- `cmd/wasm` — WebAssembly **WASI reactor** (`GOOS=wasip1`), the automatic fallback engine for the Java (Chicory) and Node.js (`node:wasi`) clients. Artifact: `ua-parser.wasm`.
- `cmd/wasmjs` — WebAssembly **browser** build (`GOOS=js`). Artifacts: `ua-parser-js.wasm` + Go's `wasm_exec.js`.
- `clients/{go,java,node,python}` — official wrappers. The Go client is a thin alias of `pkg/core` in the same module.
- There is **no** code-generation command; the core is self-contained (see "Regex database").

## Header Priority
- When parsing User-Agent data, **Client Hints (Sec-CH-UA headers) must take priority** over the raw User-Agent string.
- The logic should first check for available Client Hints to determine the Operating System (e.g., distinguishing Windows 11 from Windows 10) and Device details before falling back to Regex-based UA parsing.

## Regex database (embedding) — do NOT reintroduce code generation
- `pkg/core/parser.go` embeds the regex database directly as YAML: `//go:embed resources/regexes.yaml`, and `core.New` parses it with `yaml.Unmarshal` at init. The module is therefore **self-contained on the Go proxy** — a plain `go get` + `go build` works with no extra steps.
- **Never** switch the embed back to a generated `resources/regexes.json` and **never** reintroduce `go generate` / a `cmd/gen-json` step. That previously shipped a broken module: the generated JSON was git-ignored and absent from the module zip on the proxy, so `go get` failed for every external consumer (`pattern resources/regexes.json: no matching files found`). `regexes.json` stays git-ignored; only `regexes.yaml` is committed and embedded.
- The background updater (`pkg/core/updater_default.go`) downloads `regexes.yaml` and hot-swaps it via `yaml.Unmarshal` (JSON accepted only as a fallback). Keep the embedded default and the updater on the same YAML path.

## Go Version
- The project uses **Go 1.26** as the minimum version.

## musl/Alpine: native loading is impossible — do NOT try build-level workarounds
- Go's `c-shared` (and `c-archive`) buildmode emits **initial-exec TLS** for the runtime; musl's dynamic loader rejects `dlopen()` of such libraries with "initial-exec TLS resolves to dynamic definition" ([golang/go#54805](https://github.com/golang/go/issues/54805), [#13492](https://github.com/golang/go/issues/13492)). This affects **every FFI host** (JNA, koffi, ctypes) on Alpine, even for `.so` files built natively with the musl toolchain.
- **No build flags fix this** (verified empirically on Go 1.26.5): `CGO_CFLAGS="-ftls-model=global-dynamic"` only affects cgo C code, not the Go runtime's own TLS access; `-Wl,-Bsymbolic` is already applied by Go and does not help; a two-step `c-archive` + `gcc -shared` build has the same IE TLS relocations. `LD_PRELOAD` and gcompat also do not work (JVM segfault / loader-level rejection).
- The upstream fix is the `-tls` linker flag (general-dynamic/TLSDESC) in [golang/go PR #75048](https://github.com/golang/go/pull/75048) — unmerged; expected in Go 1.27 at the earliest. When a Go release ships it, rebuild the musl `.so` artifacts and native loading on Alpine will start working; the client loaders already attempt the musl `.so` first by design.
- Note the **pure-Go client and the REST server are unaffected** — they have no cgo/dlopen, so they build and run statically on Alpine/musl normally. The limitation is only for the FFI clients loading the `c-shared` library.
- **Supported strategy on Alpine for the FFI clients**: fall back to WebAssembly automatically — Java uses Chicory, Node.js uses `node:wasi` (Node >= 18.17). Python has no WASM fallback and raises a descriptive `RuntimeError` on musl (naming golang/go#54805 and the alternatives). Keep shipping the musl `.so` artifacts for forward compatibility.

## WebAssembly backends — two distinct ABIs
- `ua-parser.wasm` (from `cmd/wasm`, `GOOS=wasip1` reactor) and `ua-parser-js.wasm` (from `cmd/wasmjs`, `GOOS=js`) are **different ABIs and are not interchangeable**.
- Java loads `ua-parser.wasm` via **Chicory**, and the `com.dylibso.chicory:compiler` module is **required** (it compiles the module to JVM bytecode; without it the interpreter takes 60+ seconds to start). Never drop the `compiler` dependency.
- Node.js loads `ua-parser.wasm` via `node:wasi`.
- The browser build exposes a global API after `go.run(instance)`: `globalThis.initUA(configJson)` and `globalThis.parseUA(payloadJson)`, where the parse payload is a JSON string `{"ua": "...", "headers": {...}}` and the result is a JSON string. Keep the vanilla/CDN docs pointed at this API (there is no `UaParser` global without a bundler).

## REST server endpoints
- Endpoints are relocatable via env vars, computed through `joinPath(basePath, sub)` in `cmd/server/main.go`:
  - `UA_BASE_PATH` — single prefix all endpoints mount under (default: root). This is the primary knob.
  - `UA_ROUTE_PATH` — parse endpoint sub-path (POST), relative to the base (default `/`).
  - `UA_HEALTH_PATH` — health-check sub-path (GET), relative to the base (default `/health`).
- **Any new endpoint must be derived through `joinPath(basePath, ...)`** so it inherits `UA_BASE_PATH` automatically.
- A leading slash is optional. If two endpoints resolve to the same path the server must **not** panic — it dispatches by method on that path (`GET` = health, `POST` = parse). Preserve this collision-safe behavior when editing the router (Go 1.22+ `ServeMux` panics on duplicate patterns).
- The parse endpoint is POST-only (`405` otherwise); `/health` is GET-only.

## CI/CD
- The project uses **GitHub Actions**, with **two** workflows:
  - `release.yml` (trigger: push of a `v*` tag): `test` → `build-server` → `build-shared-libs` (matrix) → `docker` → `publish-java` / `publish-node` / `publish-python` → `release`. ARM64 builds run on **native `ubuntu-24.04-arm` runners** — do not reintroduce QEMU-emulated Go builds (flaky: [golang/go#68976](https://github.com/golang/go/issues/68976)). Any job that attaches files to the GitHub Release (e.g. `publish-node` attaching the npm tarball, `publish-java` attaching the JAR) needs `permissions: contents: write`.
  - `integration-test.yml` (trigger: `workflow_run` after "Build and Release" succeeds, plus manual `workflow_dispatch`): see "Integration testing".
- Any change to the core logic or infrastructure should be verified against these workflows, and any change to a client or to the set of published artifacts must be reflected in `integration-test.yml`.
- A release created by `GITHUB_TOKEN` does **not** emit a `release: published` event (Actions anti-recursion), which is why the integration test uses `workflow_run` and resolves the tag from the latest release via `gh`.

## Integration testing (real published-flow verification)
- `integration-test.yml` matrix-tests the **already-published** release exactly as an external user would — it does **not** check out the repo; every artifact comes from its public channel (Go module proxy, GitHub Release assets, JitPack, ghcr image) on both **glibc and Alpine/musl**:
  - **Go**: `go get @tag` → static build → parse.
  - **Node**: install the **release npm tarball without a token** → native (glibc) / `node:wasi` WASM (Alpine).
  - **Python**: install the release wheel → native (glibc) / descriptive error (Alpine).
  - **Browser**: run `ua-parser-js.wasm` + `wasm_exec.js` through the documented global API.
  - **Docker REST**: pull the ghcr image, verify `/health` + parse, and the `UA_BASE_PATH` shift.
  - **Java**: JitPack → shaded jar → native `JnaBackend` (glibc) / `WasmBackend` (Alpine).
- Each functional check asserts the standard UA parses to browser "Chrome". The workflow can be run manually against any tag via `workflow_dispatch`.

## Code Quality
- Maintain high test coverage for both Regex and Client Hints logic.
- Ensure thread safety when handling shared resources (like the parser instance and cache).
- The Java client ships smoke tests (`clients/java/src/test`) that run during `publish-java`, including on an Alpine container; keep them green.

## Package Distribution
- **Official Repository**: [https://github.com/Octanium91/ua-parser](https://github.com/Octanium91/ua-parser)
- **Multi-Platform Clients**: official clients for Go, Python, Node.js, and Java live in `/clients`.
- **Channels**:
  - Java (Maven) and Node.js (npm) → **GitHub Packages**; Docker images → **GitHub Container Registry (ghcr.io)** (the `:latest` tag is published on every `v*` tag); shared libraries, the Python wheel, and the Node npm tarball → **GitHub Releases**.
- **Version Management**: Manual changes to package versions (`package.json`, `pom.xml`, `setup.py`) are strictly prohibited. Versions are managed and synchronized by CI during the release based on the Git tag. Documentation examples must use version-agnostic placeholders, not a pinned version.
- **Installation Guides**: for Node.js and Java the user must configure their package manager for the GitHub Packages registry. **GitHub Packages requires authentication for every install, including public packages** — document the required Personal Access Token (`read:packages` scope). Prefer and document the auth-free alternative for each client: **JitPack** for Java (use the `v`-prefixed tag), the **`.whl` from Releases** for Python, the attached **npm tarball from Releases** for Node.js, and **`go get`** for Go.

## Performance & Logging
- The application is designed for **high performance**; use LRU caching and avoid unnecessary allocations in the hot path.
- The system must provide **clear logs for resource updates** (e.g., downloading and swapping `regexes.yaml`) to ensure observability of the background updater.

## Git Operations
- **Restrictions**: The AI assistant is strictly prohibited from performing any Git operations, including `git commit`, `git push`, `git tag`, or creating GitHub releases.
- **Responsibility**: All version control operations and deployment triggers must be performed manually by the user.
