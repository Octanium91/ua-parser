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
        if (Platform.isLinux()) {
            try {
                String arch = Platform.is64Bit() && "x86-64".equals(Platform.ARCH) ? "linux-x86-64" : 
                             (Platform.is64Bit() && "aarch64".equals(Platform.ARCH) ? "linux-aarch64" : null);

                if (arch != null) {
                    String variant = isMusl() ? "musl" : "glibc";
                    String resourcePath = "/" + arch + "/libua_parser_" + variant + ".so";
                    
                    File libFile = extractLibrary(resourcePath);
                    if (libFile == null) {
                        // Fallback to generic name
                        resourcePath = "/" + arch + "/libua_parser.so";
                        libFile = extractLibrary(resourcePath);
                    }
                    
                    if (libFile != null) {
                        System.setProperty("jna.library.path", libFile.getParent());
                        Native.register(interfaceClass, libFile.getAbsolutePath());
                        System.out.println("Loaded native library [" + arch + "/" + variant + "]: " + libFile.getAbsolutePath());
                        return;
                    }
                }
            } catch (Exception e) {
                System.err.println("Failed to load native library automatically: " + e.getMessage());
            }
        }
        
        Native.register(interfaceClass, LIB_NAME);
    }

    private static boolean isMusl() {
        // Method 1: Check for known musl dynamic loader files
        if (new File("/lib/ld-musl-x86_64.so.1").exists()) return true;
        if (new File("/lib/ld-musl-aarch64.so.1").exists()) return true;

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

    private static File extractLibrary(String resourcePath) {
        try {
            InputStream in = NativeLoader.class.getResourceAsStream(resourcePath);
            if (in == null) {
                // Try without leading slash as fallback
                String altPath = resourcePath.startsWith("/") ? resourcePath.substring(1) : resourcePath;
                in = NativeLoader.class.getClassLoader().getResourceAsStream(altPath);
            }
            
            if (in == null) {
                return null;
            }
            
            String suffix = ".so";
            if (resourcePath.endsWith(".dll")) suffix = ".dll";
            else if (resourcePath.endsWith(".dylib")) suffix = ".dylib";
            
            File tempFile = Files.createTempFile("libua_parser", suffix).toFile();
            tempFile.deleteOnExit();
            
            Files.copy(in, tempFile.toPath(), StandardCopyOption.REPLACE_EXISTING);
            return tempFile;
        } catch (IOException e) {
            return null;
        }
    }
}
