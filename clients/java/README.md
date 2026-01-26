# Universal User-Agent Parser - Java Client

This is the Java wrapper for the high-performance Universal User-Agent Parser. It uses JNA (Java Native Access) to interface with the core Go-based shared library.

## Installation

### GitHub Packages

The package is hosted on **GitHub Packages**.

#### Maven (`pom.xml`)

```xml
<repositories>
    <repository>
        <id>github</id>
        <url>https://maven.pkg.github.com/octanium91/ua-parser</url>
    </repository>
</repositories>

<dependencies>
    <dependency>
        <groupId>com.github.octanium91.ua-parser</groupId>
        <artifactId>ua-parser</artifactId>
        <version>LATEST_VERSION</version>
    </dependency>
</dependencies>
```

#### Gradle (`build.gradle`)

```gradle
repositories {
    maven {
        url = uri("https://maven.pkg.github.com/octanium91/ua-parser")
    }
}

dependencies {
    implementation("com.github.octanium91.ua-parser:ua-parser:LATEST_VERSION")
}
```

### Driver

Ensure you have the shared library (`ua-parser-linux-amd64.so`, `ua-parser-linux-arm64.so`, `ua-parser-windows-amd64.dll`, `ua-parser-darwin-amd64.dylib` or `ua-parser-darwin-arm64.dylib`) from the [GitHub Releases](https://github.com/octanium91/ua-parser/releases).

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
