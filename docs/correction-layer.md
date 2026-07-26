# Design: Declarative Correction Layer (`corrections.yaml`)

Status: **implemented** (2026-07-26; synthesized from a three-agent design
review, then built — see `pkg/core/corrections.go`, `resources/corrections.yaml`,
`signals` in `types.go`/`parser.go`, exports in `cmd/*`, host-push in the
Java/Node clients, `.github/workflows/ci.yml`).

Deviations from the proposal, decided during implementation:

- **AI-agent name synthesis is engine-native**, not a YAML rule: it needs the
  matched aiBots token and junk-name heuristics that the closed match
  vocabulary cannot express, and it must work even with an empty corrections
  file. Lives in `inferInfo` (see `extractAgentIdentity`).
- **The vendor prefix table is a dedicated top-level `vendor_prefixes`
  section**, not 18 separate rules — one lookup concept, one cap (24 rows),
  engine-guarded to fill-gap-only semantics.
- **`x_requested_with` shipped in schema v1** as a match condition (the
  X-Requested-With removal is suspended upstream, so the signal is
  first-class), with the package-id rules included in the embedded file. The
  header joined `cacheKeyHeaders`.
- **Result v1.1 and the signals block shipped together with the layer**
  (sections 11–12): `os.platform`, `cpu.bitness`, `device.form_factor`,
  `is_frozen_ua`, the `bot` object, `gpu` from WebGL signals, `ParseFull`,
  and automatic signal collection in the browser client.

Hardening added after a multi-agent QA pass (2026-07-26):

- **Self-test DoS bound**: inline-test UA length is capped (`maxTestUALen`
  4 KB), tests-per-rule capped (16), and cumulative test-UA bytes per file
  capped (`maxTotalTestBytes` 64 KB) — a crafted-but-valid ~1 MB file could
  otherwise stall the pre-swap self-test ~4–5 s (and block the Node event
  loop / the browser thread, which call the swap synchronously).
- **Strict decode**: `decodeRuleStrict` now distinguishes an unknown-field
  error (forward-compat → skip the rule) from a structural error — a
  type mismatch or duplicate key is an author bug and rejects the whole file,
  instead of silently vanishing a rule under the "newer engine" banner.
- **SSRF**: `fetchResource` refuses HTTP redirects (both resources are single
  static files; a 3xx to a metadata endpoint was otherwise followed).
- **Cache key** is now length-prefixed (injective for any field bytes,
  including NUL) rather than NUL-separated.
- **Bot name synthesis** grows prefix tokens to the full agent name
  (`GoogleAgent-Mariner`, not `googleagent`); the Apple-Silicon signal rule is
  gated on the parsed macOS family, not the raw-UA "mac os x" substring (which
  iPhone/iPad UAs also contain); the `$N` capture-ref out-of-range case is
  rejected at compile; the vendor-from-Build model extraction no longer blanks
  a generic model on a whitespace-only capture.
- **Clients**: Java typed `Result` gained the six v1.1 fields (`platform`,
  `form_factor`, `bitness`, `is_frozen_ua`, `bot`, `gpu`); Java + Python
  `parse()` gained a `signals` argument; Java corrections-push is idempotent
  (one daemon thread) and refuses cross-host redirects; the Node initial fetch
  socket is unref'd; the c-shared single-init contract is documented.
- **CI** runs `go test -race ./pkg/core`, exercised by
  `TestCorrectionsConcurrentParseAndSwap`.

Native vs WASM update gating: `DisableAutoUpdate` is the master network switch
for native builds (no background fetch when set); `DisableCorrectionsUpdate`
suppresses only corrections. The browser build defaults `DisableAutoUpdate`
true (no multi-MB regex re-download) but still fetches the tiny corrections
file, gated by `DisableCorrectionsUpdate` alone.
Goal: raise detection accuracy without waiting for upstream uap-core, by adding a
third pipeline layer — a declarative override config that lives in this repo,
is embedded at build time like `regexes.yaml`, and **hot-updates at runtime**
through the existing updater machinery. Accuracy fixes ship without releases.

Motivating gaps (measured vs ua-parser-js on `tools/compare`, all still present
at the current upstream tip — the embedded snapshot `f87f3a9` IS upstream
master, so the weekly sync cannot close any of them):

- in-app browsers: WeChat parses as generic `Chrome Mobile WebView` (upstream
  placement bug, issue #479, PRs #515/#613 open)
- vehicles: Tesla parses as desktop Chromium (absent upstream entirely)
- consoles: PS5 os `Other`, Xbox vendor/model empty
- AI-agent junk names: `ChatGPT-User` → `"com/bot"`, `Claude-User` → `"Other"`,
  `meta-externalagent` → `"crawler"` (uap-core generic fallbacks grab URL
  fragments; ~17 of our aiBots tokens are affected)
- Android vendor unresolved from model codes (`SM-X910`, Xiaomi date codes),
  especially on the Client-Hints path (`sec-ch-ua-model` is a bare string)

## 1. Config format

One YAML file, `pkg/core/resources/corrections.yaml`, committed and embedded
(`go:embed`) — never generated (see the regexes.json incident in guidelines).

Rule = `match` (AND-ed conditions) + `set` (field overrides) + inline `tests`.

```yaml
schema_version: 1
version: "2026-07-26.1"   # informational, logged on swap
rules:
  - id: wechat-inapp-browser
    upstream: "https://github.com/ua-parser/uap-core/issues/479"
    match:
      ua_contains: "micromessenger/"          # MANDATORY string-or-list prefilter
      ua_regex: '(?i)micromessenger/(\d+(?:\.\d+){0,3})'   # optional, RE2
      browser_name: ["Chrome Mobile WebView", "Mobile Safari", "Safari"]
    set:
      browser_name: "WeChat"
      browser_version: "$1"                   # regexp.Expand from ua_regex
      browser_type: "inapp"
    tests:
      - ua: "Mozilla/5.0 (Linux; Android 13; 22081212C ... MicroMessenger/8.0.47.2560(0x28002F35) WeChat/arm64"
        expect: { browser.name: "WeChat", browser.type: "inapp", device.type: "mobile" }

  - id: samsung-sm-model-vendor
    permanent: true                           # encodes our schema; never graduates upstream
    match:
      ua_contains: ["android", "sm-"]         # list = AND
      device_vendor: ["", "Generic_Android", "Generic_Android_Tablet"]  # fill gaps only
      ua_regex: '(?i)\b(SM-[A-Z]\d{3,4}[A-Z0-9]*)\b'
    set:
      device_vendor: "Samsung"
      device_model: "$1"
    tests:
      - ua: "Mozilla/5.0 (Linux; Android 14; SM-X910) ... Chrome/126.0.0.0 Safari/537.36"
        expect: { device.vendor: "Samsung", device.model: "SM-X910" }
```

Semantics:

- `match.ua_contains` — **mandatory** (string or AND-list), lowercased, checked
  against the `uaLower` already computed for `inferInfo` (hoist it into `Parse`
  so it is computed once). This is the perf gate; no rule exists without one.
- `match.ua_regex` — optional, compiled once at load (Go RE2 → linear time, no
  ReDoS class), runs on the original-case UA so captures preserve case.
- Parsed-result conditions — closed vocabulary of equals-any matches:
  `browser_name`, `browser_type`, `os_name`, `device_type`, `device_vendor`
  (string-or-list, `""` matches empty) + booleans `is_bot`, `is_ai_crawler`.
- `set` — closed vocabulary: `browser_name/_version/_type`, `os_name/_version`,
  `device_vendor/_model/_type`, `category`, `is_bot`, `is_ai_crawler`. Values
  support `$1`/`${name}` expansion from `ua_regex`. Setting `browser_version`
  refreshes `Major`. Go-side `set` struct uses pointers so an explicit `""`
  clears a field while an absent key leaves it untouched.
- All matching rules apply in file order; later sets overwrite earlier ones
  (composability: vendor rule + in-app rule on the same UA).

## 2. Pipeline position — corrections are terminal

`Parse()` order: uap regex parse → `inferInfo` → `applyClientHints` →
**`applyCorrections`** → category switch → cache add.

The two designs on the table were pre-CH ("CH is browser ground truth, must
win") and post-CH ("corrections are the last word"). **Post-CH wins** for a
concrete reason: Chromium WebViews send `Sec-CH-UA` with brand
`"Android WebView"`, so a pre-CH WeChat correction would be immediately
clobbered by the CH brand override — defeating the layer's flagship use case.
Protecting genuine CH data is instead achieved by rule discipline: rules that
touch fields CH can supply (vendor, model, os) MUST carry fill-gap guards
(`device_vendor: [""]`), enforced at review. Matching on the final post-CH
state is also strictly more precise. The category switch runs after
corrections, so `device_type: automotive` yields `category: automotive` for
free; an explicit `set.category` is applied after the switch.

## 3. Performance

Budget: ≤5% of the 12.7 µs uncached parse; zero cost on cache hits (layer runs
before `cache.Add`, hit path returns earlier — unchanged).

- Compile once at load into an immutable `compiledCorrections`; store in
  `atomic.Pointer[compiledCorrections]` on `Parser` — one atomic load per
  parse, no widening of the `p.mu` critical section, writers never block
  readers.
- Linear `strings.Contains` prefilter per rule (~10–25 ns each), hard cap
  **64 rules** (launch set ≈ 13 → ~150–400 ns ≈ 1–3%). Regex + field checks run
  only for rules whose anchor hit — i.e. only on currently-misparsed traffic.
- Aho-Corasick over anchors only if the 64 cap ever pressures (avoid the
  dependency today). CI gate: fail if `BenchmarkParse` regresses >5%.

## 4. Hot-swap

- Same updater goroutine and tick as regexes (`updater_default.go`): tick →
  `updateRegexes()` → `updateCorrections()`. Preserves the "ETag fields touched
  only from the updater goroutine" invariant; no second goroutine (cshared
  never calls Close — one goroutine is the leak-safe shape).
- Own ETag (`lastCorrectionsETag`), If-None-Match/304, `io.LimitReader` cap
  (1 MB — corrections are legitimately tiny).
- Default URL `https://raw.githubusercontent.com/Octanium91/ua-parser/main/pkg/core/resources/corrections.yaml`;
  new `Config` keys `corrections_url`, `disable_corrections_update` + server
  env `UA_CORRECTIONS_URL`, `UA_DISABLE_CORRECTIONS_UPDATE`. JSON config decode
  ignores unknown keys, so **no client changes required** (Python/Node pass raw
  dicts/objects through today; Java gets two optional Config fields for
  ergonomics only).
- **Cache invalidation: reuse the single `gen` counter.** Swap = build new set
  → `p.corrections.Store(...)` → `p.gen.Add(1)` → `p.cache.Purge()` — the
  bump-BEFORE-purge ordering replicates `updateRegexes` exactly and reuses the
  existing race guard (`Parse` skips `cache.Add` when gen moved). No cache-key
  changes; a rules-version in the key would only let dead entries squat.

## 5. Safety of the remote config

Same trust model as the regexes URL (HTTPS + repo-controlled), still treated
as hostile input:

- Validate fully before swap; **reject the whole file on any error,
  keep-last-good** (embedded snapshot is the boot-time last-good). Errors:
  YAML parse, unsupported `schema_version`, >64 rules, missing `ua_contains`,
  regex compile failure, regex source >512 B, set value >128 chars, `$N` with
  no/out-of-range group, duplicate ids, rule without tests.
- The updater additionally runs the file's own inline `tests` through a real
  parse before swapping (refuse on failure) — CI and runtime can never
  disagree because they share `validateCorrections()`.
- RE2 → no catastrophic-backtracking DoS; caps bound memory instead. `set`
  writes a fixed field vocabulary — no injection surface.
- Observability: log swap (`version=… rules=N skipped=M etag=…`) and rejection
  reason; expose `Parser.CorrectionsInfo()` for the health endpoint.
- Note the trust-boundary shift: a push to the default branch propagates to
  fleets within one update interval. Optionally point the default URL at a
  release tag instead of the branch if stricter gating is wanted.

## 6. Versioning

- File-level `schema_version` (int) — hard gate: binaries reject files above
  their supported version (keep-last-good). Bumped only for structural changes.
- Within a version, `match`/`set` vocabularies are closed sets validated per
  rule: a rule with an unknown key is **skipped and logged, not rejected** —
  old binaries safely ignore rules using newer fields while applying the rest.
  (Silent-drop unmarshal is explicitly avoided: dropping an unknown `set` key
  would invisibly change a rule's meaning.)
- Optional rule-level `requires: [capability, ...]` for semantics not visible
  as new keys. No file-level `min_core_version` — it would stall all fixes for
  all old binaries the moment one rule needs a new feature.

## 7. WASM builds — live rules everywhere (revised)

Original proposal was embedded-only for WASM; **revised: every environment,
including the browser, gets live rules**. Requirement: frontend deployments
must receive accuracy fixes without a library update.

The engine gains one transport-agnostic entry point, compiled into all builds:

```go
// ApplyCorrectionsYAML validates (schema + inline tests) and hot-swaps the
// rule set; identical semantics to the updater path: whole-file reject,
// keep-last-good, gen bump before cache purge.
func (p *Parser) ApplyCorrectionsYAML(data []byte) error
```

Per-build wiring:

- **Native (c-shared, server)** — unchanged: the background updater goroutine
  fetches and calls it. Additionally export `UpdateCorrections(yaml) *char`
  from cmd/cshared so host apps that disable the updater can push rules on
  their own schedule.
- **Browser (js/wasm, cmd/wasmjs)** — Go's net/http on js/wasm is backed by
  the browser Fetch API, so the engine CAN fetch for itself. On `initUA`:
  parse starts immediately on embedded rules; a non-blocking goroutine fetches
  `corrections_url` once and swaps when it lands (typical page gets fresh
  rules within ~100–300 ms of init). No ticker by default — page lifetimes are
  short; an optional `corrections_refresh_interval` covers long-lived SPAs.
  Config keys ride in the existing `initUA(configJSON)`.
  Serving (facts verified live 2026-07-26): raw.githubusercontent.com sends
  `Access-Control-Allow-Origin: *` and `Cache-Control: max-age=300` — CORS
  works and freshness is excellent, but GitHub tightened unauthenticated rate
  limits on raw in May 2025 and it is explicitly not a production CDN.
  jsDelivr (`cdn.jsdelivr.net/gh/...@master/...`) is also ACAO `*` but serves
  `max-age=604800` — returning browsers may hold a stale file up to 7 days.
  Recommendation: default to raw.githubusercontent (fine for dev and moderate
  traffic); high-traffic frontends should point `corrections_url` at their own
  origin/CDN (it is one small static file) — never hard-depend on either
  third-party host.
- **WASI (wasip1, cmd/wasm — the Java/Node fallback engine)** — WASI preview1
  has no sockets, the module cannot fetch. Export
  `updateCorrections(ptr, len) i32` instead; the **host** fetches the YAML
  (Node: https.get, Java: java.net.http) at init + on the same 24 h cadence
  and pushes it in. Clients wire this automatically; failures are non-fatal
  (embedded rules remain).

Result: hot rules in every mode — native (self-updating), browser
(self-updating via fetch), WASI fallback (host-push). The embedded snapshot is
always the boot state and the fallback when fetch/push fails or is disabled.
Keep the fetch-at-init code in a `js && wasm` build file so wasip1 stays free
of it.

## 8. Testing & CI

- Inline `tests:` per rule (≥1 mandatory, enforced by
  `TestEveryRuleHasATestUA`). Tests run through the FULL `Parse` pipeline, so
  interactions with inferInfo/CH/category are covered.
- **No-op lint (dead-rule detector)**: parse every test UA with corrections on
  vs off; if a rule changes nothing, fail naming the rule. This fires inside
  the weekly uap-core sync (`sync-uap-core.yml` already runs `go test` before
  opening its PR), so when upstream catches up the sync PR forces deleting the
  obsolete rule. Rules marked `permanent: true` (our-schema rules: `inapp`
  type, AI name synthesis, vendor table) are exempt.
- Regression entries in `regression_test.go` for what YAML can't express:
  CH-vs-corrections ordering, swap-purges-cache (mirror
  `TestUpdaterSwapAndPurge`), gen ordering.
- **Gap found during review: the repo has no PR/push-triggered test workflow**
  (only weekly sync / tag release / post-release integration). Add a minimal
  `ci.yml` (`pull_request` + push to master, paths `pkg/**`, `cmd/**`, `go.*`)
  running `go build ./... && go test -count=1 ./...` — it protects the
  hot-updated corrections file (and everything else).
- Corrections PRs rerun `tools/compare` and report the accuracy delta vs
  ua-parser-js in the PR body.

## 9. Initial rule set (v1)

P0 — ship immediately (each with corpus-proven test UA):

| id | Fix | Upstream status |
|---|---|---|
| `wechat-inapp-browser` | WeChat name/version/type=inapp | open PRs #515/#613 → retires via no-op lint when merged |
| `ai-agent-name-synthesis` | one generic rule: aiBots token + junk name (`Other`/`crawler`/URL fragment) → canonical name + `Token/(ver)` version; covers ~17 tokens incl. ChatGPT-User, Claude-User, meta-externalagent, perplexity-user, mistralai-*, kimi-user | permanent (upstream can't fix agents it doesn't list) |
| `tesla-vehicle` | vendor=Tesla, device.type=automotive → category automotive | absent upstream — also file an uap-core PR |
| `playstation-os` | `PlayStation (\d)/` → os PlayStation N | upstream issue #276 stale |
| `xbox-device` | vendor=Microsoft, model=`Xbox $1` (UA token frozen at "One" — approximate) | ancient patterns only |
| `inapp-type-flags` | browser.type=inapp for names uap-core already resolves (Facebook, Instagram, Line, Snapchat, TikTok, Twitter, Telegram, VK, Weibo, GSA…) | permanent (`browser.type` is our schema) |
| `vk-inapp` | `VKAndroidApp/(ver)` → VK/inapp | absent upstream — verify against real traffic first |

P1 — needs care: Android **vendor-from-model prefix table** (~18 rows: SM-→
Samsung, Pixel→Google, Redmi/POCO/date-codes→Xiaomi, CPH→Oppo/OnePlus…,
explicit Huawei code list — never a generic `[A-Z]{3}-`; guard: vendor empty or
`Generic*` only), model extraction fallback from `; X Build/`, Fire TV
(`AFT*`), Samsung Tizen TV browser identity, `Android Automotive` device type.

P2 — rejected: full device model DB (unmaintainable, license-contaminated
territory; Yauaa's own docs call brand detection "the most brittle part"),
MediaPlayers/Emails/CLIs categories (features, not corrections), Edge WebView2
(no stable token), non-Tesla vehicles (no traffic, and the known model-code
tables edge toward AGPL copying).

Budget: ~13 of the 64-rule cap. New rules require a compare-harness divergence
or real-traffic evidence; per-model granularity is auto-rejected.

## 10. Maintenance policy

1. Every rule ships with: id, rationale, upstream link (or "absent"),
   `permanent` flag if it encodes our schema, ≥1 inline test.
2. Graduation: a rule expressible in uap-core's schema that survived a month
   unchanged → open the upstream PR; the weekly sync delivers the fix and the
   no-op lint then forces the rule's deletion. Corrections are temporary by
   design; the lint is the forcing function against dead weight.
3. Vendor table capped at ~20 rows; file at 64 rules (engine-enforced).

## 11. Result schema v1.1 — richer output (additive)

All additions are new JSON fields: Python/Node clients (dynamic) get them for
free; Java's Gson and Go consumers ignore unknown fields until their structs
are updated — no breaking change, no client lockstep.

| Field | Type | Source | Notes |
|---|---|---|---|
| `cpu.bitness` | `"64"`/`"32"`/`""` | `Sec-CH-UA-Bitness`, UA tokens (Win64/WOW64/x86_64) | already consumed for arch; expose it |
| `os.platform` | canonical enum: `windows`, `macos`, `linux`, `android`, `ios`, `chromeos`, `tizen`, `playstation`, `other` | normalized from os.name | stable key for analytics; os.name stays the marketing name |
| `device.form_factor` | `desktop`/`mobile`/`tablet`/`xr`/`watch`/`automotive`/`tv`/`""` | `Sec-CH-UA-Form-Factors`, else derived from device.type | exposes the CH value we already parse but discard after using |
| `is_frozen_ua` | bool | UA matches known frozen/reduced patterns (Chrome ≥110 reduced UA, Mac 10_15_7, Android 10; K) | tells consumers "trust CH, the UA lies" |
| `bot` | `{name, category, vendor}` or `null` | materializes the tags already curated in the aiBots list (`training`, `search`, `user-fetch`, `agent`) + classic classes (`search-crawler`, `seo`, `monitoring`, `http-library`) | the differentiator: `is_bot`/`is_ai_crawler` booleans stay for compat, `bot.category` enables robots-policy/billing decisions |
| `gpu` | `{vendor, renderer}` or `null` | only when the client supplies a WebGL signal (section 12) | pass-through + used internally for Apple-Silicon/SoC inference |

Corrections `set` vocabulary grows the matching keys (`bot_category`,
`form_factor`, …). Explicitly rejected: `confidence` scores (unfalsifiable),
marketing OS version names, SoC model guessing without a client signal.

## 12. Extended client signals (beyond Sec-CH-UA headers)

Two realities: Chromium sends UA-CH headers; **Safari and Firefox send none**
(no `navigator.userAgentData` either), and it is exactly Safari that ships the
worst UA lies (iPad masquerading as macOS desktop). Extra signals close that
gap. The parse payload gains an optional block:

```json
{ "ua": "...", "headers": { ... },
  "signals": {
    "max_touch_points": 5,
    "platform": "MacIntel",
    "webgl_vendor": "Apple", "webgl_renderer": "Apple M2",
    "screen": { "w": 1180, "h": 820, "dpr": 2 },
    "device_memory": 8, "hardware_concurrency": 8
  } }
```

Priority: **CH headers > signals > UA regex** (signals are weaker than CH but
stronger than a frozen UA). Cache key extends with the consumed signal fields,
canonically serialized — same rule as cacheKeyHeaders.

v1 inference rules (each individually verified, no ML guessing):

1. **iPad unmask** — UA says `Macintosh` + `max_touch_points > 1` →
   os `iOS (iPadOS)`, device Apple iPad, type tablet, category mobile. The
   single highest-value signal rule; Safari-only traffic, CH can never help.
   Use `> 1`, never an exact value (models report 5 or 10; real Macs 0–1).
   Known edge: visionOS Safari reportedly also presents a Mac-like UA with
   touch — acceptable misclassification for v1, revisit with real samples.
2. **Apple Silicon fallback** — UA says Mac (frozen `Intel Mac OS X 10_15_7`)
   + `webgl_renderer` contains `Apple M` → cpu.architecture arm64. Scope
   honestly: works in **Chromium only** (full ANGLE strings, M-generation
   visible); Safari has served a uniform masked `"Apple GPU"` since 2020, and
   Chromium users usually surface arch via `Sec-CH-UA-Arch` anyway — so this
   is a fallback for Chromium traffic without high-entropy CH, not a Safari
   solution. There is no WebGL path to M-series detection in Safari.
3. **Android SoC tier** — `webgl_renderer` Adreno/Mali/Xclipse string →
   `gpu` populated (fully readable in Chromium on Android; no device-model
   guessing from GPU in v1).
4. **Form factor assist** — no CH, `screen` + touch + platform → refine
   phone-vs-tablet for Android tablets browsing with desktop-mode UA.

**Backend forwarding contract** (documented in the root README section
"Forwarding headers from your backend" + per-client READMEs): backends must
copy `User-Agent` plus **every** `Sec-CH-*` request header (prefix copy, not
an enumerated list — future hints flow through automatically), values raw
with quotes intact, plus `X-Requested-With` when present; the `signals`
object below is forwarded the same way once collected on the page. Backends
that cache parse results must key on everything they forward.

Collection: `cmd/wasmjs` (browser client) collects automatically at init —
`navigator.userAgentData.getHighEntropyValues()` mapped onto the same
`sec-ch-ua-*` header keys (removes the Accept-CH round-trip requirement
entirely for the WASM client), plus the signals block; WebGL probe behind a
`collect_gpu: true` config flag (fingerprinting-adjacent — opt-in, documented).
For backend deployments README ships a ~15-line snippet that gathers the same
object on the page and forwards it with the request.

Server-side bonus header (no JS needed): **`X-Requested-With`** — Android
WebView carries the host app package id (`com.tencent.mm` → WeChat,
`com.instagram.android` → Instagram). **Verified 2026-07: Google's removal
was suspended** (chromestatus: "On hold … unable to provide a sufficient
alternative"), the header is still sent by default, so this is a first-class
in-app detector for Android WebView traffic. Caveats: Android-only (iOS
WKWebView never sent it), spoofable, individual apps can suppress it — strong
hint, never ground truth. Package-id → app-name mapping lives in
corrections.yaml as a lookup rule, hot-updatable like everything else.

Verified platform facts this section rests on (agent-checked 2026-07-26):
UA-CH is still Chromium-only — Safari and Firefox send **no** `Sec-CH-UA*`
headers and lack `navigator.userAgentData` (both vendors hold negative
positions), so the legacy UA string + the signals above are everything those
browsers offer; WebGL renderer strings are full in Chromium, bucketed in
Firefox (since 91), masked in Safari; `deviceMemory` is Chromium-only and
quantized (0.25–8), `hardwareConcurrency` is universal but Safari caps it —
both are coarse tier signals at best.

## 13. Rollout (independently shippable)

| # | PR | Size |
|---|---|---|
| 1 | Schema + loader + validation + empty `corrections.yaml` (zero behavior change) | M |
| 2 | Engine wiring: Parser field, `Parse()` insertion, uaLower hoist, cache/gen tests | S/M |
| 3 | Rules v1 (P0 set) + inline tests + no-op lint + regression entries | M |
| 4 | Runtime updater: `fetchResource` refactor, `updateCorrections`, Config/env keys, README, Java Config fields | M |
| 5 | `ci.yml` (PR/push test workflow) — can also ship first | S |
| 6 | `ApplyCorrectionsYAML` + cshared `UpdateCorrections` export + wasip1 `updateCorrections` export + host-push wiring in Java/Node clients + js/wasm fetch-at-init | M/L |
| 7 | Result v1.1 fields (`cpu.bitness`, `os.platform`, `form_factor`, `is_frozen_ua`, `bot` object) + client struct updates | M |
| 8 | Signals block: payload/cache-key extension, iPad/Apple-Silicon/GPU rules, wasmjs auto-collection, README snippet, X-Requested-With lookup | L |

Known risks carried from code review: cshared re-`Init` silently ignores new
config (pre-existing — document); `lastCorrectionsETag` must stay
updater-goroutine-only (no public `RefreshCorrections()`); gen bump-before-
purge ordering must be pinned by a test; `corrections.yaml` must always be
committed (embed of a missing file breaks `go get`).
