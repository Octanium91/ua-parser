# Comparison harness: ua-parser vs ua-parser-js / Yauaa / uap-java / uap-python

Reproducible harness behind the "Comparison with popular alternatives" section
of the root README. All parsers run the same corpus ([corpus.json](./corpus.json),
52 entries: desktop/mobile browsers, Client Hints cases, bots, AI crawlers,
HTTP tools) in their strongest server-side configuration:

- **ua-parser (this project)**: the Go core (`core.Parser.Parse(ua, headers)` —
  full pipeline: regex parse + inference + Client Hints + bot/AI detection),
  plus the real published clients: Java (JNA), Node.js (koffi), Python (ctypes),
  each driving the platform shared library from GitHub Releases.
- **ua-parser-js v2**: `UAParser(Bots, headers).withClientHints()` plus the
  `isBot()` / `isAIBot()` helpers — the equivalent full pipeline.
- **Java** (see [java/](./java)): [Yauaa](https://yauaa.basjes.nl/)
  (`parse(headers)`, Client Hints included) and
  [uap-java](https://github.com/ua-parser/uap-java) (UA string only — it
  supports neither Client Hints nor bot flags).
- **Python** (see [python/](./python)): [uap-python](https://github.com/ua-parser/uap-python)
  (PyPI `ua-parser`, pure-Python backend that a plain pip install gets; UA
  string only). Its default global `parse()` wraps the resolver in a cache, so
  the harness measures `uap` (uncached `basic.Resolver`) and `uap-cached`
  (the default) separately.

## Reproduce

```bash
# one-time setup
cd tools/compare/node && npm install && cd ../../..

# side-by-side results (writes tools/compare/out/{go,js}.json)
go run ./tools/compare -corpus tools/compare/corpus.json -out tools/compare/out/go.json
node tools/compare/node/parse.mjs tools/compare/corpus.json tools/compare/out/js.json
node tools/compare/node/report.mjs tools/compare/out/go.json tools/compare/out/js.json

# benchmark (200k parses each, single-threaded)
go run ./tools/compare -corpus tools/compare/corpus.json -bench 200000 -cache 1000
node tools/compare/node/parse.mjs tools/compare/corpus.json --bench 200000
```

## Java (resource-focused)

Each library is measured in a **fresh JVM** (Java 17, `-Xmx2g`, single thread)
so heap and RSS numbers are attributable: init time (construction + first
parse), retained heap after init (post double-GC), uncached/cached throughput,
retained heap after the bench, plus peak and settled process RSS sampled
externally by the runner.

```bash
# one-time setup: put this project's Java client into the local Maven repo
# (the release jar bundles the native drivers for all platforms)
gh release download --repo octanium91/ua-parser --pattern "ua-parser-<ver>.jar"
mvn install:install-file -Dfile=ua-parser-<ver>.jar -DpomFile=clients/java/pom.xml -Dversion=<ver>

# build the harness (downloads yauaa + uap-java from Maven Central)
mvn -f tools/compare/java/pom.xml package
```

```powershell
# run (Windows; each invocation = one library in one fresh JVM)
tools\compare\java\run.ps1 -Impl ours  -N 100000 -Cache 0
tools\compare\java\run.ps1 -Impl ours  -N 100000 -Cache 1000
tools\compare\java\run.ps1 -Impl yauaa -N 20000  -Cache 0
tools\compare\java\run.ps1 -Impl yauaa -N 100000 -Cache 10000
tools\compare\java\run.ps1 -Impl uap   -N 20000  -Cache 0
```

Peak RSS is dominated by transient allocation garbage in every JVM library
(hundreds of MB under sustained max load before GC kicks in) — the settled
post-GC RSS is the number that reflects the real steady-state footprint.

## Node.js / Python clients (this project)

Both clients accept an explicit driver path, so no wheel/npm install is needed
when running from the repo — download the platform driver from GitHub Releases
(e.g. `ua-parser-windows-amd64.dll`) and pass it as the last argument:

```bash
# this project's Node.js client (koffi FFI)
node tools/compare/node/ours.cjs tools/compare/corpus.json 100000 0 <path-to-driver>

# this project's Python client (ctypes) — uncached and cached
python tools/compare/python/parse.py tools/compare/corpus.json ours 100000 0 <path-to-driver>
python tools/compare/python/parse.py tools/compare/corpus.json ours 100000 1000 <path-to-driver>

# uap-python (pip install ua-parser), uncached resolver and default cached parse()
python tools/compare/python/parse.py tools/compare/corpus.json uap 20000
python tools/compare/python/parse.py tools/compare/corpus.json uap-cached 100000
```

The Python harness reports settled/peak RSS in-process (psapi / /proc), the
Node harnesses report `process.memoryUsage().rss`.

Benchmark methodology notes:

- Both sides parse the same 52-entry corpus round-robin, full pipeline per
  iteration, after a warmup pass; both are single-threaded (the Go core
  additionally scales across cores under concurrent load, the Node library is
  single-threaded per process).
- The Go `uncached` pass runs with `LRUCacheSize: 0` so every parse is a full
  regex run — this is the apples-to-apples number (ua-parser-js has no cache).
  The `cached` pass reflects this project's production default, where repeating
  UAs hit the LRU.
- Memory is peak process RSS during the benchmark (Go binary measured
  externally; Node via `process.memoryUsage().rss`).
