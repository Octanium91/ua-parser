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
- **Linux**: x86-64, arm64
- **Windows**: x86-64 (win32-x86-64)
- **macOS**: x86-64, arm64 (Universal)

The library automatically detects the operating system and architecture to load the correct driver using JNA.

> **Note**: If you are using **JitPack**, make sure you are using a version that includes the driver for your platform. The GitHub Packages version is recommended for the most complete set of pre-built drivers.

#### Troubleshooting `UnsatisfiedLinkError`
If you encounter an `UnsatisfiedLinkError`, it usually means the native library for your specific OS/Architecture is missing from the JAR or cannot be loaded due to missing system dependencies.
- On Windows, ensure you have the Visual C++ Redistributable installed (though Go libs are usually self-contained).
- You can enable JNA debug logging by setting `-Djna.debug_load=true` to see where it searches for the library.

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
