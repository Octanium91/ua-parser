package com.github.octanium91;

import com.dylibso.chicory.runtime.Instance;
import com.dylibso.chicory.runtime.ImportValues;
import com.dylibso.chicory.runtime.Memory;
import com.dylibso.chicory.runtime.ExportFunction;
import com.dylibso.chicory.wasm.Parser;
import com.dylibso.chicory.wasm.WasmModule;
import com.dylibso.chicory.wasi.WasiContext;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;

public class WasmBackend implements ParserBackend {
    private final WasmModule module;
    private final Instance instance;
    private final Memory memory;
    private final ExportFunction malloc;
    private final ExportFunction free;
    private final ExportFunction initUA;
    private final ExportFunction parseUA;
    private final WasiContext wasi;

    public WasmBackend() {
        try {
            InputStream wasmInput = getClass().getResourceAsStream("/ua-parser.wasm");
            if (wasmInput == null) {
                throw new RuntimeException("ua-parser.wasm not found in resources");
            }

            this.module = Parser.parse(wasmInput);
            this.wasi = WasiContext.builder().build();
            this.instance = Instance.builder(module)
                    .withImportValues(wasi.toImportValues())
                    .build();
            this.memory = instance.memory();
            this.malloc = instance.export("malloc");
            this.free = instance.export("free");
            this.initUA = instance.export("initUA");
            this.parseUA = instance.export("parseUA");

            // Initialize parser in WASM with default config
            if (initUA != null) {
                initUA.apply(0, 0);
            }
        } catch (Exception e) {
            throw new RuntimeException("Failed to initialize WASM backend", e);
        }
    }

    @Override
    public void init(String configJson) {
        if (initUA == null) return;
        byte[] configBytes = configJson.getBytes(StandardCharsets.UTF_8);
        long ptr = malloc.apply((long) configBytes.length)[0];
        try {
            memory.write((int)ptr, configBytes);
            initUA.apply(ptr, (long) configBytes.length);
        } finally {
            free.apply(ptr);
        }
    }

    @Override
    public String parse(String payloadJson) {
        byte[] inputBytes = payloadJson.getBytes(StandardCharsets.UTF_8);
        int len = inputBytes.length;
        
        // Allocate memory in WASM
        long ptr = malloc.apply((long) len)[0];
        try {
            // Write to WASM memory
            memory.write((int)ptr, inputBytes);
            
            // Call parseUA(ptr, len)
            // It returns a uint64: (len << 32) | ptr
            long resultPacked = parseUA.apply(ptr, (long) len)[0];
            
            int resLen = (int)(resultPacked >> 32);
            int resPtr = (int)(resultPacked & 0xFFFFFFFFL);
            
            if (resPtr == 0) return null;
            
            byte[] resBytes = memory.read(resPtr, resLen);
            String result = new String(resBytes, StandardCharsets.UTF_8);
            
            // Free the result buffer in WASM
            free.apply((long) resPtr);
            
            return result;
        } finally {
            // Free the input buffer in WASM
            free.apply(ptr);
        }
    }
}
