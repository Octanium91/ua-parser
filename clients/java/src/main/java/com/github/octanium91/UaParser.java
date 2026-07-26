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
    // Guards the corrections-push daemon so repeated init() calls never spawn
    // more than one pusher thread (init is otherwise not idempotent).
    private final java.util.concurrent.atomic.AtomicBoolean correctionsPushStarted =
            new java.util.concurrent.atomic.AtomicBoolean(false);

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

        /** Overrides the corrections.yaml hot-update source. */
        @SerializedName("corrections_url")
        public String correctionsUrl;

        /** Disables runtime correction updates (embedded snapshot stays). */
        @SerializedName("disable_corrections_update")
        public boolean disableCorrectionsUpdate;
    }

    public static class OSInfo {
        public String name;
        public String version;
        /** Canonical machine-readable OS key (windows, macos, ios, android, ...). */
        public String platform;
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

        @SerializedName("form_factor")
        public String formFactor;
    }

    public static class CPUInfo {
        public String architecture;
        /** "64", "32", or "". */
        public String bitness;
    }

    public static class EngineInfo {
        public String name;
        public String version;
    }

    /** Classified bot identity; null for non-bot traffic. */
    public static class BotInfo {
        public String name;
        public String category;
        public String vendor;
    }

    /** GPU info; populated only when a WebGL signal was supplied, else null. */
    public static class GPUInfo {
        public String vendor;
        public String renderer;
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

        @SerializedName("is_frozen_ua")
        public boolean isFrozenUa;

        /** Non-null for bots: {name, category, vendor}. */
        public BotInfo bot;

        /** Non-null only when a WebGL signal was provided. */
        public GPUInfo gpu;
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
        startCorrectionsPush(configJson);
    }

    /**
     * In native (JNA) mode the Go core fetches correction updates itself; the
     * WASM fallback has no network (WASI preview1), so the HOST fetches
     * corrections.yaml and pushes it into the engine — one fetch at init,
     * then daily, on a daemon thread. Failures are non-fatal: the embedded
     * snapshot keeps serving.
     */
    private void startCorrectionsPush(String configJson) {
        if (!(backend instanceof WasmBackend)) {
            return;
        }
        // A repeated init() must not spawn a second daily pusher thread.
        if (!correctionsPushStarted.compareAndSet(false, true)) {
            return;
        }
        Config cfg;
        try {
            cfg = gson.fromJson(configJson, Config.class);
        } catch (RuntimeException invalid) {
            cfg = null;
        }
        if (cfg != null && cfg.disableCorrectionsUpdate) {
            return;
        }
        String url = (cfg != null && cfg.correctionsUrl != null && !cfg.correctionsUrl.isEmpty())
                ? cfg.correctionsUrl
                : "https://raw.githubusercontent.com/Octanium91/ua-parser/main/pkg/core/resources/corrections.yaml";

        WasmBackend wasm = (WasmBackend) backend;
        Thread pusher = new Thread(() -> {
            while (true) {
                try {
                    java.net.HttpURLConnection conn = (java.net.HttpURLConnection)
                            java.net.URI.create(url).toURL().openConnection();
                    conn.setConnectTimeout(15_000);
                    conn.setReadTimeout(30_000);
                    // Don't silently chase a redirect to another host (mild SSRF shape).
                    conn.setInstanceFollowRedirects(false);
                    try (java.io.InputStream in = conn.getInputStream();
                         java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream()) {
                        byte[] buf = new byte[8192];
                        int n;
                        int total = 0;
                        while ((n = in.read(buf)) != -1) {
                            total += n;
                            if (total > (1 << 20)) { // 1 MB cap, mirrors the engine
                                throw new java.io.IOException("corrections payload exceeds 1 MB");
                            }
                            out.write(buf, 0, n);
                        }
                        int code = conn.getResponseCode();
                        if (code != 200) {
                            System.err.println("WARN: ua-parser corrections fetch status " + code + " (embedded rules stay active)");
                        } else if (!wasm.pushCorrections(out.toByteArray())) {
                            System.err.println("WARN: ua-parser corrections rejected by engine (keeping last good)");
                        }
                    }
                } catch (Exception e) {
                    System.err.println("WARN: ua-parser corrections fetch failed (embedded rules stay active): " + e);
                }
                try {
                    Thread.sleep(24L * 60 * 60 * 1000);
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    return;
                }
            }
        }, "ua-parser-corrections");
        pusher.setDaemon(true);
        pusher.start();
    }

    /** Optional browser-side evidence beyond headers (see the README). */
    public static class Signals {
        @SerializedName("max_touch_points")
        public int maxTouchPoints;
        public String platform;
        @SerializedName("webgl_vendor")
        public String webglVendor;
        @SerializedName("webgl_renderer")
        public String webglRenderer;
        @SerializedName("device_memory")
        public double deviceMemory;
        @SerializedName("hardware_concurrency")
        public int hardwareConcurrency;
    }

    /**
     * Parses a User-Agent string with optional headers.
     */
    public Result parse(String userAgent, Map<String, String> headers) {
        return parse(userAgent, headers, null);
    }

    /**
     * Parses a User-Agent string with optional headers and browser signals.
     * Signals (touch points, WebGL renderer, ...) unmask what UA and Client
     * Hints cannot — e.g. iPads posing as Macs in Safari.
     */
    public Result parse(String userAgent, Map<String, String> headers, Signals signals) {
        if (headers == null) {
            headers = new HashMap<>();
        }
        Map<String, Object> payload = new HashMap<>();
        payload.put("ua", userAgent);
        payload.put("headers", headers);
        if (signals != null) {
            payload.put("signals", signals);
        }

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