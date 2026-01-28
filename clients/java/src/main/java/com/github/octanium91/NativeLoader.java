package com.github.octanium91;

import com.sun.jna.Native;
import com.sun.jna.Platform;
import java.io.BufferedReader;
import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;

/**
 * Intelligent loader for the native library that handles different Linux C libraries (glibc vs musl).
 */
public class NativeLoader {

    private static final String LIB_NAME = "ua-parser";

    public static void load(Class<?> interfaceClass) {
        if (Platform.isLinux() && Platform.is64Bit() && "x86-64".equals(Platform.ARCH)) {
            try {
                String variant = isMusl() ? "musl" : "glibc";
                String resourcePath = "/linux-x86-64/libua_parser_" + variant + ".so";
                
                File libFile = extractLibrary(resourcePath);
                
                // For Linux, we might need to set jna.library.path or load via absolute path
                System.setProperty("jna.library.path", libFile.getParent());
                Native.register(interfaceClass, libFile.getAbsolutePath());
                
                return;
            } catch (Exception e) {
                // Fallback to standard JNA loading if something goes wrong
                System.err.println("Failed to load native library automatically: " + e.getMessage());
            }
        }
        
        // Standard loading for other OS or if Linux detection failed
        Native.register(interfaceClass, LIB_NAME);
    }

    private static boolean isMusl() {
        // Method 1: Check for known musl dynamic loader files
        File muslLoader = new File("/lib/ld-musl-x86_64.so.1");
        if (muslLoader.exists()) {
            return true;
        }

        // Method 2: Check ldd version
        try {
            Process p = new ProcessBuilder("ldd", "--version").start();
            try (BufferedReader reader = new BufferedReader(new InputStreamReader(p.getInputStream()))) {
                String line = reader.readLine();
                if (line != null && line.toLowerCase().contains("musl")) {
                    return true;
                }
            }
        } catch (Exception ignored) {
        }
        
        return false;
    }

    private static File extractLibrary(String resourcePath) throws IOException {
        InputStream in = NativeLoader.class.getResourceAsStream(resourcePath);
        if (in == null) {
            throw new IOException("Resource not found: " + resourcePath);
        }
        
        String suffix = ".so";
        File tempFile = Files.createTempFile("libua_parser", suffix).toFile();
        tempFile.deleteOnExit();
        
        Files.copy(in, tempFile.toPath(), StandardCopyOption.REPLACE_EXISTING);
        return tempFile;
    }
}
