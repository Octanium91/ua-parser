package com.github.octanium91;

import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;

/**
 * Intelligent loader for the native library.
 */
public class NativeLoader {

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