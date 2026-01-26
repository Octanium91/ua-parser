package com.github.uaparser;

import com.sun.jna.Library;
import com.sun.jna.Native;
import com.sun.jna.Pointer;

/**
 * Universal User-Agent Parser Java Wrapper using JNA.
 */
public class UaParser {
    public interface UaParserLib extends Library {
        // Go: func Init(configJSON *C.char) *C.char
        Pointer Init(String configJSON);
        
        // Go: func Parse(payloadJSON *C.char) *C.char
        Pointer Parse(String payloadJSON);
        
        // Go: func FreeString(ptr *C.char)
        void FreeString(Pointer ptr);
    }

    private final UaParserLib lib;

    public UaParser() {
        this(getDefaultLibPath());
    }

    public UaParser(String libPath) {
        this.lib = Native.load(libPath, UaParserLib.class);
    }

    private static String getDefaultLibPath() {
        String os = System.getProperty("os.name").toLowerCase();
        String arch = System.getProperty("os.arch").toLowerCase();
        String archSuffix = (arch.contains("arm") || arch.contains("aarch64")) ? "arm64" : "amd64";

        if (os.contains("win")) {
            return "ua-parser-windows-" + archSuffix + ".dll";
        } else if (os.contains("mac") || os.contains("darwin")) {
            return "./ua-parser-darwin-" + archSuffix + ".dylib";
        } else {
            return "./ua-parser-linux-" + archSuffix + ".so";
        }
    }

    /**
     * Initializes the parser with a JSON configuration string.
     * @param configJson e.g. "{\"disable_auto_update\": false}"
     */
    public void init(String configJson) {
        Pointer errPtr = lib.Init(configJson);
        if (errPtr != null) {
            String err = errPtr.getString(0);
            lib.FreeString(errPtr);
            throw new RuntimeException("Failed to initialize parser: " + err);
        }
    }

    /**
     * Parses data and returns a JSON result string.
     * @param payloadJson e.g. "{\"ua\": \"...\", \"headers\": {}}"
     * @return JSON string containing the result
     */
    public String parse(String payloadJson) {
        Pointer resPtr = lib.Parse(payloadJson);
        if (resPtr != null) {
            String res = resPtr.getString(0);
            lib.FreeString(resPtr);
            return res;
        }
        return null;
    }
}
