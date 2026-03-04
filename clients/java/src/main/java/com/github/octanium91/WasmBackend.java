package com.github.octanium91;

import com.dylibso.chicory.runtime.Instance;
import com.dylibso.chicory.runtime.ImportValues;
import com.dylibso.chicory.runtime.Memory;
import com.dylibso.chicory.runtime.ExportFunction;
import com.dylibso.chicory.wasm.Parser;
import com.dylibso.chicory.wasm.WasmModule;
import com.dylibso.chicory.wasm.types.Value;
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
    private final WasiPreview1 wasi; // Заменили Wasi на WasiPreview1

    public WasmBackend() {
        try {
            InputStream wasmInput = getClass().getResourceAsStream("/ua-parser.wasm");
            if (wasmInput == null) {
                throw new RuntimeException("ua-parser.wasm not found in resources");
            }

            this.module = Parser.parse(wasmInput);

            // 1. По-новому инициализируем WASI (прокидываем потоки для логов)
            WasiOptions options = WasiOptions.builder()
                    .withStdout(System.out)
                    .withStderr(System.err)
                    .build();
            this.wasi = WasiPreview1.builder().withOptions(options).build();

            // 2. Оборачиваем WASI-функции в ImportValues
            ImportValues imports = ImportValues.builder()
                    .withFunctions(Arrays.asList(wasi.toHostFunctions()))
                    .build();

            // 3. Передаем импорты в билдер инстанса
            this.instance = Instance.builder(module)
                    .withImportValues(imports)
                    .build();

            this.memory = instance.memory();
            this.malloc = instance.export("malloc");
            this.free = instance.export("free");
            this.initUA = instance.export("initUA");
            this.parseUA = instance.export("parseUA");

            // Инициализация рантайма Go (через _initialize для WASI reactor или _start для command)
            ExportFunction initialize = instance.export("_initialize");
            if (initialize != null) {
                initialize.apply();
            } else {
                ExportFunction start = instance.export("_start");
                if (start != null) {
                    // Go WASM command initialization
                    try {
                        start.apply();
                    } catch (Exception e) {
                        // Ignore _start exit errors if it's not a true reactor
                    }
                }
            }

            // Инициализация парсера
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

        // Выделяем память внутри WASM
        long ptr = malloc.apply(Value.i32(len))[0].asLong();
        try {
            memory.write((int)ptr, inputBytes);

            long resultPacked = parseUA.apply(Value.i32((int)ptr), Value.i32(len))[0].asLong();

            int resLen = (int)(resultPacked >> 32);
            int resPtr = (int)(resultPacked & 0xFFFFFFFFL);

            if (resPtr == 0) return null;

            // Здесь джун сделал всё верно — метод readBytes читает массив нужной длины
            byte[] resBytes = memory.readBytes(resPtr, resLen);
            String result = new String(resBytes, StandardCharsets.UTF_8);

            free.apply(Value.i32(resPtr));

            return result;
        } finally {
            free.apply(Value.i32((int)ptr));
        }
    }
}