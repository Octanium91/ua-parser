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
 * Universal User-Agent Parser Java Wrapper with Native (JNA) and WASM fallback.
 */
public class UaParser {
    private ParserBackend backend;
    private final Gson gson;

    public UaParser() {
        this.gson = new Gson();
        try {
            // Try to run at maximum speed (native)
            this.backend = new JnaBackend();
        } catch (UnsatisfiedLinkError e) {
            // Native library failed to load -- musl build didn't work or other error
            System.err.println("WARN: Native UA-Parser library failed to load.");
            System.err.println("REASON: " + e.getMessage());

            if (new File("/etc/alpine-release").exists()) {
                System.err.println("WARN: Detected Alpine Linux. Ensure you are using the latest ua-parser version with native musl support.");
            }

            System.err.println("WARN: Falling back to WebAssembly (WASM) mode for compatibility.");
            this.backend = new WasmBackend();
        }
    }

    public UaParser(String libPath) {
        this.gson = new Gson();
        this.backend = new JnaBackend(libPath);
    }

    public static class Config {
        @SerializedName("disable_auto_update")
        public boolean disableAutoUpdate;

        @SerializedName("lru_cache_size")
        public int lruCacheSize;

        @SerializedName("update_url")
        public String updateUrl;

        @SerializedName("update_interval")
        public String updateInterval;
    }

    public static class OSInfo {
        public String name;
        public String version;
    }

    public static class BrowserInfo {
        public String name;
        public String version;
        public String major;
        public String type;
    }

    public static class DeviceInfo {
        public String model;
        public String vendor;
        public String type;
    }

    public static class CPUInfo {
        public String architecture;
    }

    public static class EngineInfo {
        public String name;
        public String version;
    }

    public static class Result {
        public String ua;
        public OSInfo os;
        public BrowserInfo browser;
        public DeviceInfo device;
        public CPUInfo cpu;
        public EngineInfo engine;
        public String category;

        @SerializedName("is_bot")
        public boolean isBot;

        @SerializedName("is_ai_crawler")
        public boolean isAiCrawler;
    }

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
        backend.init(configJson);
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
        return backend.parse(payloadJson);
    }
}