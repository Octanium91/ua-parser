package com.github.octanium91;

import com.dylibso.chicory.runtime.Instance;
import com.dylibso.chicory.runtime.ImportValues;
import com.dylibso.chicory.runtime.Memory;
import com.dylibso.chicory.runtime.ExportFunction;
import com.dylibso.chicory.wasm.Parser;
import com.dylibso.chicory.wasm.WasmModule;
import com.dylibso.chicory.wasi.WasiOptions;
import com.dylibso.chicory.wasi.WasiPreview1;

import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;

public class WasmBackend implements ParserBackend {
    private final WasmModule module;
    private final Instance instance;
    private final Memory memory;
    private final ExportFunction malloc;
    private final ExportFunction free;
    private final ExportFunction initUA;
    private final ExportFunction parseUA;
    private final WasiPreview1 wasi;

    public WasmBackend() {
        try {
            InputStream wasmInput = getClass().getResourceAsStream("/ua-parser.wasm");
            if (wasmInput == null) {
                throw new RuntimeException("ua-parser.wasm not found in resources");
            }

            this.module = Parser.parse(wasmInput);

            WasiOptions options = WasiOptions.builder()
                    .withStdout(System.out)
                    .withStderr(System.err)
                    .build();
            this.wasi = WasiPreview1.builder().withOptions(options).build();

            ImportValues imports = ImportValues.builder()
                    .withFunctions(Arrays.asList(wasi.toHostFunctions()))
                    .build();

            this.instance = Instance.builder(module)
                    .withImportValues(imports)
                    .build();

            this.memory = instance.memory();
            this.malloc = instance.export("malloc");
            this.free = instance.export("free");
            this.initUA = instance.export("initUA");
            this.parseUA = instance.export("parseUA");

            // Initialize Go runtime
            ExportFunction initialize = instance.export("_initialize");
            if (initialize != null) {
                initialize.apply();
            }

            // Initialize parser
            if (initUA != null) {
                // Pass primitive 0L instead of Value objects
                initUA.apply(0L, 0L);
            }
        } catch (Exception e) {
            throw new RuntimeException("Failed to initialize WASM backend", e);
        }
    }

    @Override
    public synchronized void init(String configJson) {
        if (initUA == null || malloc == null) return;
        byte[] configBytes = configJson.getBytes(StandardCharsets.UTF_8);

        // Результат apply - это long[], берем [0]
        long ptr = malloc.apply((long) configBytes.length)[0];
        try {
            memory.write((int)ptr, configBytes);
            initUA.apply(ptr, (long) configBytes.length);
        } finally {
            free.apply(ptr);
        }
    }

    @Override
    public synchronized String parse(String payloadJson) {
        if (parseUA == null || malloc == null) return null;
        byte[] inputBytes = payloadJson.getBytes(StandardCharsets.UTF_8);
        int len = inputBytes.length;

        long ptr = malloc.apply((long) len)[0];
        try {
            memory.write((int)ptr, inputBytes);

            long resultPacked = parseUA.apply(ptr, (long) len)[0];

            int resLen = (int)(resultPacked >> 32);
            int resPtr = (int)(resultPacked & 0xFFFFFFFFL);

            if (resPtr == 0) return null;

            byte[] resBytes = memory.readBytes(resPtr, resLen);
            String result = new String(resBytes, StandardCharsets.UTF_8);

            free.apply((long) resPtr);
            return result;
        } finally {
            free.apply(ptr);
        }
    }
}