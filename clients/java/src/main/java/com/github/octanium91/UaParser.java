package com.github.octanium91;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;
import com.sun.jna.Library;
import com.sun.jna.Native;
import com.sun.jna.Pointer;
import com.sun.jna.Platform;
import java.io.File;

import java.util.HashMap;
import java.util.Map;

/**
 * Universal User-Agent Parser Java Wrapper using JNA.
 */
public class UaParser {
    public interface UaParserLib extends Library {
        Pointer Init(String configJSON);
        Pointer Parse(String payloadJSON);
        void FreeString(Pointer ptr);
    }

    private final UaParserLib lib;
    private final Gson gson;

    public UaParser() {
        this.gson = new Gson();
        this.lib = loadLibrary();
    }

    public UaParser(String libPath) {
        this.gson = new Gson();
        this.lib = Native.load(libPath, UaParserLib.class);
    }

    private static UaParserLib loadLibrary() {
        if (Platform.isLinux()) {
            String arch = Platform.is64Bit() && "x86-64".equals(Platform.ARCH) ? "linux-x86-64" :
                    (Platform.is64Bit() && "aarch64".equals(Platform.ARCH) ? "linux-aarch64" : null);

            if (arch != null) {
                String variant = NativeLoader.isMusl() ? "musl" : "glibc";
                String resourcePath = "/" + arch + "/libua_parser_" + variant + ".so";

                File libFile = NativeLoader.extractLibrary(resourcePath);
                if (libFile == null) {
                    resourcePath = "/" + arch + "/libua_parser.so";
                    libFile = NativeLoader.extractLibrary(resourcePath);
                }

                if (libFile != null) {
                    try {
                        UaParserLib loaded = Native.load(libFile.getAbsolutePath(), UaParserLib.class);
                        System.out.println("Loaded native library [" + arch + "/" + variant + "]: " + libFile.getAbsolutePath());
                        return loaded;
                    } catch (UnsatisfiedLinkError e) {
                        if (!"musl".equals(variant)) {
                            System.err.println("Failed to load " + variant + " library: " + e.getMessage());
                            System.err.println("Falling back to musl (statically linked) variant...");
                            String muslPath = "/" + arch + "/libua_parser_musl.so";
                            File muslFile = NativeLoader.extractLibrary(muslPath);
                            if (muslFile != null) {
                                UaParserLib loaded = Native.load(muslFile.getAbsolutePath(), UaParserLib.class);
                                System.out.println("Loaded native library [" + arch + "/musl] (fallback): " + muslFile.getAbsolutePath());
                                return loaded;
                            }
                        }
                        throw e;
                    }
                }
            }
        }

        return Native.load("ua-parser", UaParserLib.class);
    }

    // ... existing code ...

    /**
     * Initializes the parser with a configuration object.
     */
    public void init(Config config) {
        init(gson.toJson(config));
    }

    /**
     * Initializes the parser with a JSON configuration string.
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
     * Parses a User-Agent string with optional headers.
     */
    public Result parse(String userAgent, Map<String, String> headers) {
        if (headers == null) {
            headers = new HashMap<>();
        }
        Map<String, Object> payload = new HashMap<>();
        payload.put("ua", userAgent);
        payload.put("headers", headers);

        String resJson = parse(gson.toJson(payload));
        return gson.fromJson(resJson, Result.class);
    }

    /**
     * Parses data and returns a JSON result string.
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
