package com.github.octanium91;

import com.sun.jna.Library;
import com.sun.jna.Native;
import com.sun.jna.Platform;
import com.sun.jna.Pointer;

import java.io.File;

public class JnaBackend implements ParserBackend {
    public interface UaParserLib extends Library {
        Pointer Init(String configJSON);
        Pointer Parse(String payloadJSON);
        void FreeString(Pointer ptr);
    }

    private final UaParserLib lib;

    public JnaBackend() {
        this.lib = loadLibrary();
    }

    public JnaBackend(String libPath) {
        this.lib = Native.load(libPath, UaParserLib.class);
    }

    @Override
    public void init(String configJson) {
        Pointer errPtr = lib.Init(configJson);
        if (errPtr != null) {
            String err = errPtr.getString(0);
            lib.FreeString(errPtr);
            throw new RuntimeException("Failed to initialize JNA parser: " + err);
        }
    }

    @Override
    public String parse(String payloadJson) {
        Pointer resPtr = lib.Parse(payloadJson);
        if (resPtr != null) {
            String res = resPtr.getString(0);
            lib.FreeString(resPtr);
            return res;
        }
        return null;
    }

    static boolean isMusl() {
        return new File("/lib/ld-musl-x86_64.so.1").exists() ||
               new File("/lib/ld-musl-aarch64.so.1").exists();
    }

    private static UaParserLib loadLibrary() {
        if (Platform.isLinux()) {
            String arch = Platform.is64Bit() && "x86-64".equals(Platform.ARCH) ? "linux-x86-64" :
                    (Platform.is64Bit() && "aarch64".equals(Platform.ARCH) ? "linux-aarch64" : null);

            if (arch != null) {
                if (isMusl()) {
                    // musl's dynamic loader rejects dlopen of Go c-shared libraries
                    // (initial-exec TLS, golang/go#54805) on every Go release up to and
                    // including 1.26.x. We still attempt the musl build so deployments
                    // pick up native mode automatically once a fixed Go toolchain ships,
                    // but we never try the glibc build here: it cannot load on musl and
                    // its error would mask the real reason.
                    String muslPath = "/" + arch + "-musl/libua_parser.so";
                    File muslLib = NativeLoader.extractLibrary(muslPath);
                    if (muslLib == null) {
                        throw new UnsatisfiedLinkError(
                                "ua-parser: musl native library not found in JAR resources: " + muslPath);
                    }
                    return Native.load(muslLib.getAbsolutePath(), UaParserLib.class);
                }

                String resourcePath = "/" + arch + "/libua_parser.so";
                File libFile = NativeLoader.extractLibrary(resourcePath);
                if (libFile != null) {
                    return Native.load(libFile.getAbsolutePath(), UaParserLib.class);
                }
            }
        }

        // Windows and macOS resolve via JNA's classpath convention
        // ({os-arch}/mapped-name inside the JAR); also the last resort elsewhere.
        return Native.load("ua-parser", UaParserLib.class);
    }
}
