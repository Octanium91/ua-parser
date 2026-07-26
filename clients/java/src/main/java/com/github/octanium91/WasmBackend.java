package com.github.octanium91;

import com.dylibso.chicory.compiler.MachineFactoryCompiler;
import com.dylibso.chicory.runtime.ByteArrayMemory;
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
    // Parsing the 5+ MB module is expensive; do it once per JVM.
    private static volatile WasmModule cachedModule;

    private final Instance instance;
    private final Memory memory;
    private final ExportFunction malloc;
    private final ExportFunction free;
    private final ExportFunction initUA;
    private final ExportFunction parseUA;
    private final ExportFunction updateCorrections; // null on wasm modules predating the export
    private final WasiPreview1 wasi;

    public WasmBackend() {
        try {
            WasiOptions options = WasiOptions.builder()
                    .withStdout(System.out)
                    .withStderr(System.err)
                    .build();
            this.wasi = WasiPreview1.builder().withOptions(options).build();

            ImportValues imports = ImportValues.builder()
                    .withFunctions(Arrays.asList(wasi.toHostFunctions()))
                    .build();

            this.instance = Instance.builder(loadModule())
                    .withImportValues(imports)
                    // Translate WASM to JVM bytecode instead of interpreting:
                    // cuts init and parse latency by orders of magnitude.
                    .withMachineFactory(MachineFactoryCompiler::compile)
                    .withMemoryFactory(ByteArrayMemory::new)
                    .build();

            this.memory = instance.memory();
            this.malloc = instance.export("malloc");
            this.free = instance.export("free");
            this.initUA = instance.export("initUA");
            this.parseUA = instance.export("parseUA");
            this.updateCorrections = tryExport(instance, "updateCorrections");

            // Go wasip1 reactors require _initialize before any other export.
            instance.export("_initialize").apply();
        } catch (Exception e) {
            throw new RuntimeException("Failed to initialize WASM backend", e);
        }
    }

    // tryExport resolves an optional export: older bundled wasm modules
    // predate updateCorrections and must keep working.
    private static ExportFunction tryExport(Instance instance, String name) {
        try {
            return instance.export(name);
        } catch (RuntimeException missing) {
            return null;
        }
    }

    /**
     * Pushes a corrections.yaml payload into the engine (validated inside;
     * whole-file reject keeps the last good rules).
     *
     * @return true when the engine accepted the payload.
     */
    public synchronized boolean pushCorrections(byte[] yaml) {
        if (updateCorrections == null || yaml == null || yaml.length == 0) {
            return false;
        }
        long ptr = malloc.apply((long) yaml.length)[0];
        try {
            memory.write((int) ptr, yaml);
            long rc = updateCorrections.apply(ptr, (long) yaml.length)[0];
            return (int) rc == 0;
        } finally {
            free.apply(ptr);
        }
    }

    private static WasmModule loadModule() {
        WasmModule module = cachedModule;
        if (module == null) {
            synchronized (WasmBackend.class) {
                module = cachedModule;
                if (module == null) {
                    InputStream wasmInput = WasmBackend.class.getResourceAsStream("/ua-parser.wasm");
                    if (wasmInput == null) {
                        throw new RuntimeException("ua-parser.wasm not found in resources");
                    }
                    module = Parser.parse(wasmInput);
                    cachedModule = module;
                }
            }
        }
        return module;
    }

    @Override
    public synchronized void init(String configJson) {
        byte[] configBytes = configJson.getBytes(StandardCharsets.UTF_8);

        long ptr = malloc.apply((long) configBytes.length)[0];
        try {
            memory.write((int) ptr, configBytes);
            long rc = initUA.apply(ptr, (long) configBytes.length)[0];
            if ((int) rc != 0) {
                throw new RuntimeException("WASM parser initialization failed (rc=" + (int) rc + ")");
            }
        } finally {
            free.apply(ptr);
        }
    }

    @Override
    public synchronized String parse(String payloadJson) {
        byte[] inputBytes = payloadJson.getBytes(StandardCharsets.UTF_8);
        int len = inputBytes.length;

        long ptr = malloc.apply((long) len)[0];
        try {
            memory.write((int) ptr, inputBytes);

            long resultPacked = parseUA.apply(ptr, (long) len)[0];

            int resLen = (int) (resultPacked >> 32);
            int resPtr = (int) (resultPacked & 0xFFFFFFFFL);

            if (resPtr == 0) return null;

            try {
                byte[] resBytes = memory.readBytes(resPtr, resLen);
                return new String(resBytes, StandardCharsets.UTF_8);
            } finally {
                free.apply((long) resPtr);
            }
        } finally {
            free.apply(ptr);
        }
    }
}
