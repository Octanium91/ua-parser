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
                    // On musl systems (Alpine), try musl build
                    try {
                        String muslPath = "/" + arch + "-musl/libua_parser.so";
                        File muslLib = NativeLoader.extractLibrary(muslPath);
                        if (muslLib != null) {
                            return Native.load(muslLib.getAbsolutePath(), UaParserLib.class);
                        }
                    } catch (UnsatisfiedLinkError e) {
                        // musl build didn't work, try glibc build (via gcompat)
                    }
                }

                // Load standard library (glibc / gcompat)
                String resourcePath = "/" + arch + "/libua_parser.so";
                File libFile = NativeLoader.extractLibrary(resourcePath);

                if (libFile != null) {
                    return Native.load(libFile.getAbsolutePath(), UaParserLib.class);
                }
            }
        }

        // Standard JNA fallback for Windows, macOS, or if file was not extracted
        return Native.load("ua-parser", UaParserLib.class);
    }
}
