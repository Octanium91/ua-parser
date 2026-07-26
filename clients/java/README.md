# Universal User-Agent Parser - Java Client

This is the Java wrapper for the high-performance Universal User-Agent Parser. It uses JNA (Java Native Access) to interface with the core Go-based shared library.

**Requires Java 11 or higher.**

## Installation

### JitPack

**JitPack** is the recommended way to include the library directly from GitHub — it requires **no authentication**.

> **Replace `TAG` with the exact release tag, including the leading `v`** — e.g. `v0.0.48` (not `0.0.48`). Using a version without the `v` will fail to resolve on JitPack. **Check the [Releases page](https://github.com/Octanium91/ua-parser/releases) for the current tag** (the example here may be behind).

#### Maven (`pom.xml`)

```xml
<repositories>
    <repository>
        <id>jitpack.io</id>
        <url>https://jitpack.io</url>
    </repository>
</repositories>

<dependencies>
    <dependency>
        <groupId>com.github.Octanium91</groupId>
        <artifactId>ua-parser</artifactId>
        <version>TAG</version>
    </dependency>
</dependencies>
```

#### Gradle (`build.gradle`)

```gradle
repositories {
    mavenCentral()
    maven { url 'https://jitpack.io' }
}

dependencies {
    implementation 'com.github.Octanium91:ua-parser:TAG'
}
```

### GitHub Packages

The package is also hosted on **GitHub Packages**. Note that you may need to configure your `settings.xml` or `build.gradle` to authenticate with GitHub Packages.

#### Maven (`pom.xml`)

```xml
<repositories>
    <repository>
        <id>github</id>
        <url>https://maven.pkg.github.com/Octanium91/ua-parser</url>
    </repository>
</repositories>

<dependencies>
    <dependency>
        <groupId>com.github.Octanium91</groupId>
        <artifactId>ua-parser</artifactId>
        <version>LATEST_VERSION</version>
    </dependency>
</dependencies>
```

#### Gradle (`build.gradle`)

```gradle
repositories {
    mavenCentral()
    maven {
        url = uri("https://maven.pkg.github.com/Octanium91/ua-parser")
    }
}

dependencies {
    implementation("com.github.Octanium91:ua-parser:LATEST_VERSION")
}
```

### Driver

Native libraries for supported platforms are bundled inside the JAR:
- **Linux (glibc)**: x86-64, arm64
- **Linux (musl)**: x86-64, arm64 — bundled, but see the Alpine section below
- **Windows**: x86-64 (win32-x86-64)
- **macOS**: x86-64, arm64

The library automatically detects the operating system, architecture, and libc to load the correct driver using JNA. If no native driver can be loaded, it automatically falls back to a bundled WebAssembly build of the same engine (Chicory runtime, pure JVM) — same results, slower startup.

You can check which backend is active via `parser.getBackendName()` (`"JnaBackend"` or `"WasmBackend"`).

> **Note**: JitPack and GitHub Packages serve the **same** pre-built release JAR — every native driver plus the WASM fallback are bundled (CI gates the release on all of them being present). Prefer **JitPack** for the auth-free path; use a real released `v`-tag (see the [Releases page](https://github.com/Octanium91/ua-parser/releases) for the latest).

#### Alpine Linux / musl

On Alpine (and any musl-based distro) the native driver currently **cannot** be loaded: musl's dynamic loader rejects `dlopen` of Go c-shared libraries ([golang/go#54805](https://github.com/golang/go/issues/54805); the fix is expected no earlier than Go 1.27). The client detects musl and switches to the WebAssembly backend automatically — no configuration needed. Expected overhead: several seconds of one-time initialization (WASM is compiled to JVM bytecode at startup) and slower parsing than native; results are identical and LRU-cached. The bundled musl `.so` will start loading automatically once a fixed Go toolchain ships and libraries are rebuilt.

If you need native-level throughput on Alpine, run the standalone REST server (`ghcr.io/octanium91/ua-parser`) next to your application, or use a glibc-based base image (e.g. `eclipse-temurin:17-jre`).

#### Troubleshooting `UnsatisfiedLinkError`
If you encounter an `UnsatisfiedLinkError`, it usually means the native library for your specific OS/Architecture is missing from the JAR or cannot be loaded due to missing system dependencies.
- On Windows, ensure you have the Visual C++ Redistributable installed (though Go libs are usually self-contained).
- You can enable JNA debug logging by setting `-Djna.debug_load=true` to see where it searches for the library.
- If your `/tmp` (or default temp dir) is mounted `noexec`, set `-Djna.tmpdir=/path/to/exec/dir` — the loader honors it for extraction.

> **Manual Path**: You can also manually provide a path to a custom shared library when creating the `UaParser` instance:
> `UaParser parser = new UaParser("/path/to/libua-parser.so");`

## Usage

> Works out of the box only from a **released artifact** (JitPack / GitHub Packages / the Releases JAR) — those bundle the native drivers and the WASM fallback. A bare `mvn package` of a source checkout produces a driver-less JAR and `new UaParser()` will fail; see [Compilation](#compilation).

```java
import com.github.octanium91.UaParser;
import java.util.HashMap;
import java.util.Map;

public class Main {
    public static void main(String[] args) {
        // 1. Initialize the parser
        UaParser parser = new UaParser();

        // 2. Configure (Typed Config object)
        UaParser.Config config = new UaParser.Config();
        config.lruCacheSize = 2000;
        config.disableAutoUpdate = false;
        // Correction layer (hot-updated) — optional overrides:
        // config.correctionsUrl = "https://example.com/corrections.yaml";
        // config.disableCorrectionsUpdate = false;

        parser.init(config);

        // 3. Prepare data
        String ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ...";
        
        // Headers are optional (can be null), but recommended for Client Hints support
        Map<String, String> headers = new HashMap<>();
        headers.put("Sec-CH-UA-Platform", "\"Windows\"");
        headers.put("Sec-CH-UA-Platform-Version", "\"13.0.0\"");

        // 4. Parse (Returns a typed Result object)
        UaParser.Result result = parser.parse(ua, headers);

        // 5. Use data (full Result v1.2 shape)
        System.out.println("OS: " + result.os.name + " " + result.os.version + " (" + result.os.platform + ")");
        System.out.println("Browser: " + result.browser.name + " " + result.browser.version);
        System.out.println("Device: " + result.device.vendor + " / " + result.device.formFactor);
        System.out.println("CPU: " + result.cpu.architecture + " " + result.cpu.bitness + ", frozen UA: " + result.isFrozenUa);
        if (result.bot != null) {
            System.out.println("Bot: " + result.bot.name + " (" + result.bot.category + ", " + result.bot.vendor + ")");
        }
    }
}
```

### Browser signals (optional)

`parse(ua, headers, signals)` accepts browser-side evidence that UA and Client Hints can't provide (Safari/Firefox send no Client Hints):

```java
UaParser.Signals signals = new UaParser.Signals();
signals.maxTouchPoints = 5;         // unmasks iPads reporting a desktop (Mac) UA
signals.webglRenderer = "Apple M2"; // Apple Silicon / Android SoC
UaParser.Result result = parser.parse(ua, headers, signals);
```

Priority inside the engine: **Client Hints > signals > UA string**.

### Typed Result fields

The `Result` class mirrors the full engine output (schema v1.2). Fields:

- `resultVersion` — result schema version (`"1.2"`).
- `browser{name,version,major,type}`, `engine{name,version}`, `category`.
- `os{name,version,platform,versionName,versionRaw}`.
- `device{model,vendor,type,formFactor}`, `cpu{architecture,bitness}`.
- `isBot`, `isAiCrawler`, `isFrozenUa`.
- Convenience flags: `isMobile`, `isDesktop`, `isTouchCapable`, `isChromeFamily`, `isAppleSilicon`.
- `automation` (`AutomationInfo{headless,electron,webdriver}`) — undeclared automation.
- `integrity` (`IntegrityInfo{spoofed,reasons}`) — UA/CH/signals consistency.
- `security` (`SecurityInfo{suspicious,category}`) — attack payloads in the UA.
- `detection` (`DetectionInfo{clientHintsUsed,highEntropy,signalsUsed}`) — input provenance.
- `classHash` — coarse client-class bucket key (not a tracking fingerprint).
- `bot` (`BotInfo{name,category,vendor}`, null for humans), `gpu` (`GPUInfo{vendor,renderer}`, null unless a WebGL signal was supplied).

Full JSON example and field semantics: [root README](../../README.md#example-response).

## Forwarding headers from a real request

For maximum accuracy don't enumerate headers — copy the `User-Agent` plus **every** request header starting with `Sec-CH-` (and `X-Requested-With` if present). Servlet example (same idea for Spring):

```java
Map<String, String> headers = new HashMap<>();
for (Enumeration<String> e = request.getHeaderNames(); e.hasMoreElements(); ) {
    String name = e.nextElement().toLowerCase();
    if (name.startsWith("sec-ch-") || name.equals("x-requested-with")) {
        headers.put(name, request.getHeader(name));
    }
}
UaParser.Result result = parser.parse(request.getHeader("User-Agent"), headers);
```

Pass values raw, quotes included (`"\"Windows\""`). See the [backend forwarding guide](../../README.md#forwarding-headers-from-your-backend) and [Requesting Client Hints](../../README.md#requesting-client-hints) (`Accept-CH`) in the root README.

## Compilation

To build the JAR yourself:
```bash
mvn clean package
```

> **Important**: a bare source build produces a JAR **without** native drivers or the WASM module — `new UaParser()` will fail at runtime. Before packaging, place the release artifacts into `src/main/resources` using this layout (as CI does):
> ```
> src/main/resources/linux-x86-64/libua_parser.so        (glibc amd64)
> src/main/resources/linux-aarch64/libua_parser.so       (glibc arm64)
> src/main/resources/linux-x86-64-musl/libua_parser.so   (musl amd64)
> src/main/resources/linux-aarch64-musl/libua_parser.so  (musl arm64)
> src/main/resources/win32-x86-64/ua-parser.dll
> src/main/resources/darwin-x86-64/libua-parser.dylib
> src/main/resources/darwin-aarch64/libua-parser.dylib
> src/main/resources/ua-parser.wasm                      (WASI reactor)
> ```
> At minimum, `ua-parser.wasm` alone gives a working (WASM-only) build. The bundled smoke tests run automatically when resources are present and are skipped otherwise.
