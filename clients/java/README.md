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

public class Main {
    public static void main(String[] args) {
        // Initialize the parser (automatically detects OS and loads bundled lib)
        UaParser parser = new UaParser();

        // Or specify path explicitly if not using bundled libs
        // UaParser parser = new UaParser("/path/to/ua-parser-linux-amd64.so");

        // Initialize the core
        parser.init("{\"disable_auto_update\": false, \"lru_cache_size\": 1000}");

        // Parse a User-Agent
        String payload = "{\"ua\": \"Mozilla/5.0...\", \"headers\": {}}";
        String resultJson = parser.parse(payload);

        System.out.println(resultJson);
    }
}
```

## Compilation

To build the JAR yourself:
```bash
mvn clean package
```
