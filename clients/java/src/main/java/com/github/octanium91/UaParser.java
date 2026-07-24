package com.github.octanium91;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;

import java.util.HashMap;
import java.util.Map;

/**
 * Universal User-Agent Parser Java Wrapper with Native (JNA) and WASM fallback.
 */
public class UaParser {
    private final ParserBackend backend;
    private final Gson gson;

    public UaParser() {
        this.gson = new Gson();
        ParserBackend selected;
        try {
            // Try to run at maximum speed (native)
            selected = new JnaBackend();
        } catch (LinkageError | RuntimeException nativeFailure) {
            // LinkageError covers UnsatisfiedLinkError on first JNA touch and
            // NoClassDefFoundError on any subsequent one.
            System.err.println("WARN: Native UA-Parser library failed to load.");
            System.err.println("REASON: " + nativeFailure);

            if (JnaBackend.isMusl()) {
                System.err.println("NOTE: Alpine/musl cannot dlopen Go native libraries until"
                        + " golang/go#54805 is fixed in the Go toolchain (expected Go 1.27+)."
                        + " The WebAssembly backend is the supported mode on Alpine.");
            }

            System.err.println("WARN: Falling back to WebAssembly (WASM) mode.");
            try {
                selected = new WasmBackend();
            } catch (RuntimeException wasmFailure) {
                wasmFailure.addSuppressed(nativeFailure);
                throw wasmFailure;
            }
        }
        this.backend = selected;
    }

    /**
     * @return the active backend implementation name ("JnaBackend" or "WasmBackend").
     */
    public String getBackendName() {
        return backend.getClass().getSimpleName();
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