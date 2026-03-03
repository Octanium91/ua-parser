package com.github.octanium91;

import com.dylibso.chicory.runtime.Instance;
import com.dylibso.chicory.runtime.Module;
import com.dylibso.chicory.runtime.Memory;
import com.dylibso.chicory.runtime.ExportFunction;
import com.dylibso.chicory.wasm.types.Value;
import com.dylibso.chicory.wasi.Wasi;
import com.dylibso.chicory.wasi.WasiOptions;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;

public class WasmBackend implements ParserBackend {
    private final Module module;
    private final Instance instance;
    private final Memory memory;
    private final ExportFunction malloc;
    private final ExportFunction free;
    private final ExportFunction initUA;
    private final ExportFunction parseUA;
    private final Wasi wasi;

    public WasmBackend() {
        try {
            InputStream wasmInput = getClass().getResourceAsStream("/ua-parser.wasm");
            if (wasmInput == null) {
                // Try alternate location if any, but standard is root of resources
                throw new RuntimeException("ua-parser.wasm not found in resources");
            }

            this.module = Module.builder(wasmInput).build();
            this.wasi = new Wasi(WasiOptions.builder().build());
            this.instance = module.instantiate(wasi.toImportValues());
            this.memory = instance.memory();
            this.malloc = instance.export("malloc");
            this.free = instance.export("free");
            this.initUA = instance.export("initUA");
            this.parseUA = instance.export("parseUA");

            // Initialize parser in WASM with default config
            if (initUA != null) {
                initUA.apply(Value.i32(0), Value.i32(0));
            }
        } catch (Exception e) {
            throw new RuntimeException("Failed to initialize WASM backend", e);
        }
    }

    @Override
    public void init(String configJson) {
        if (initUA == null) return;
        byte[] configBytes = configJson.getBytes(StandardCharsets.UTF_8);
        long ptr = malloc.apply(Value.i32(configBytes.length))[0].asLong();
        try {
            memory.write((int)ptr, configBytes);
            initUA.apply(Value.i32((int)ptr), Value.i32(configBytes.length));
        } finally {
            free.apply(Value.i32((int)ptr));
        }
    }

    @Override
    public String parse(String payloadJson) {
        byte[] inputBytes = payloadJson.getBytes(StandardCharsets.UTF_8);
        int len = inputBytes.length;
        
        // Allocate memory in WASM
        long ptr = malloc.apply(Value.i32(len))[0].asLong();
        try {
            // Write to WASM memory
            memory.write((int)ptr, inputBytes);
            
            // Call parseUA(ptr, len)
            // It returns a uint64: (len << 32) | ptr
            long resultPacked = parseUA.apply(Value.i32((int)ptr), Value.i32(len))[0].asLong();
            
            int resLen = (int)(resultPacked >> 32);
            int resPtr = (int)(resultPacked & 0xFFFFFFFFL);
            
            if (resPtr == 0) return null;
            
            byte[] resBytes = memory.read(resPtr, resLen);
            String result = new String(resBytes, StandardCharsets.UTF_8);
            
            // Free the result buffer in WASM
            free.apply(Value.i32(resPtr));
            
            return result;
        } finally {
            // Free the input buffer in WASM
            free.apply(Value.i32((int)ptr));
        }
    }
}
