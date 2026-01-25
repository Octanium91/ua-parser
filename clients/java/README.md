# Universal User-Agent Parser - Java Client

This is the Java wrapper for the high-performance Universal User-Agent Parser. It uses JNA (Java Native Access) to interface with the core Go-based shared library.

## Installation

### Maven

Add the dependency to your `pom.xml` (Ensure you have configured GitHub Packages repository):

```xml
<dependency>
    <groupId>com.github.octanium91</groupId>
    <artifactId>ua-parser</artifactId>
    <version>1.1.0</version>
</dependency>
```

### Driver

Ensure you have the shared library (`ua-parser-linux.so` or `ua-parser-windows.dll`) from the [GitHub Releases](https://github.com/octanium91/ua-parser/releases).

## Usage

```java
import com.github.uaparser.UaParser;

public class Main {
    public static void main(String[] args) {
        // Initialize with path to the shared library
        UaParser parser = new UaParser("./ua-parser-linux.so");

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
