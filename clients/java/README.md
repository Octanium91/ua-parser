# Universal User-Agent Parser - Java Client

This is the Java wrapper for the high-performance Universal User-Agent Parser. It uses JNA (Java Native Access) to interface with the core Go-based shared library.

## Installation

### JitPack

Alternatively, you can use **JitPack** to include the library directly from GitHub.

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

> **Note**: If you are using **JitPack**, make sure you are using a version that includes the driver for your platform. The GitHub Packages version is recommended for the most complete set of pre-built drivers.

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
        
        parser.init(config);

        // 3. Prepare data
        String ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ...";
        
        // Headers are optional (can be null), but recommended for Client Hints support
        Map<String, String> headers = new HashMap<>();
        headers.put("Sec-CH-UA-Platform", "\"Windows\"");
        headers.put("Sec-CH-UA-Platform-Version", "\"13.0.0\"");

        // 4. Parse (Returns a typed Result object)
        UaParser.Result result = parser.parse(ua, headers);

        // 5. Use data
        System.out.println("OS: " + result.os.name + " " + result.os.version);
        System.out.println("Browser: " + result.browser.name + " " + result.browser.version);
        System.out.println("Is Bot: " + result.isBot);
    }
}
```

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
