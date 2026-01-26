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

Ensure you have the shared library (`ua-parser-linux-amd64.so`, `ua-parser-linux-arm64.so`, `ua-parser-windows-amd64.dll`, `ua-parser-darwin-amd64.dylib` or `ua-parser-darwin-arm64.dylib`) from the [GitHub Releases](https://github.com/Octanium91/ua-parser/releases).

> **Note**: Native libraries are bundled inside the JAR, but you can also manually place the shared library in your working directory if needed.

## Usage

```java
import com.github.octanium91.UaParser;

public class Main {
    public static void main(String[] args) {
        // Initialize the parser (automatically detects OS for lib name)
        UaParser parser = new UaParser();

        // Or specify path explicitly
        // UaParser parser = new UaParser("./ua-parser-linux.so");

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
