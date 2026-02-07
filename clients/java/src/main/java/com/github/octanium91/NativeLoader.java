package com.github.octanium91;

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

    static boolean isMusl() {
        if (new File("/etc/alpine-release").exists()) return true;
        if (new File("/lib/ld-musl-x86_64.so.1").exists()) return true;
        if (new File("/lib/ld-musl-aarch64.so.1").exists()) return true;

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

    static File extractLibrary(String resourcePath) {
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