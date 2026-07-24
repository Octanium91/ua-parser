package com.github.octanium91;

import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardCopyOption;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Intelligent loader for the native library.
 */
public class NativeLoader {

    private static final Map<String, File> EXTRACTED = new ConcurrentHashMap<>();

    static File extractLibrary(String resourcePath) {
        return EXTRACTED.computeIfAbsent(resourcePath, NativeLoader::doExtract);
    }

    private static File doExtract(String resourcePath) {
        try (InputStream in = openResource(resourcePath)) {
            if (in == null) {
                return null;
            }

            String suffix = ".so";
            if (resourcePath.endsWith(".dll")) suffix = ".dll";
            else if (resourcePath.endsWith(".dylib")) suffix = ".dylib";

            // Honor jna.tmpdir so deployments with a noexec default temp dir
            // can redirect extraction the same way they already do for JNA.
            String tmpDir = System.getProperty("jna.tmpdir");
            Path tempFile;
            if (tmpDir != null && !tmpDir.isEmpty()) {
                Path dir = Paths.get(tmpDir);
                Files.createDirectories(dir);
                tempFile = Files.createTempFile(dir, "libua_parser", suffix);
            } else {
                tempFile = Files.createTempFile("libua_parser", suffix);
            }
            File file = tempFile.toFile();
            file.deleteOnExit();

            Files.copy(in, tempFile, StandardCopyOption.REPLACE_EXISTING);
            return file;
        } catch (IOException e) {
            System.err.println("WARN: ua-parser failed to extract native library "
                    + resourcePath + ": " + e);
            return null;
        }
    }

    private static InputStream openResource(String resourcePath) {
        InputStream in = NativeLoader.class.getResourceAsStream(resourcePath);
        if (in == null) {
            // Try without leading slash as fallback
            String altPath = resourcePath.startsWith("/") ? resourcePath.substring(1) : resourcePath;
            in = NativeLoader.class.getClassLoader().getResourceAsStream(altPath);
        }
        return in;
    }
}
