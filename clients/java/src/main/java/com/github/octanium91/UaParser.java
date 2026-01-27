package com.github.octanium91;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;
import com.sun.jna.Library;
import com.sun.jna.Native;
import com.sun.jna.Pointer;

import java.util.HashMap;
import java.util.Map;

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
    private final Gson gson;

    public UaParser() {
        this("ua-parser");
    }

    public UaParser(String libPath) {
        this.lib = Native.load(libPath, UaParserLib.class);
        this.gson = new Gson();
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
     * @param config configuration object
     */
    public void init(Config config) {
        init(gson.toJson(config));
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
     * Parses a User-Agent string with optional headers.
     * @param userAgent User-Agent string
     * @param headers HTTP headers (optional, can be null)
     * @return typed result object
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
