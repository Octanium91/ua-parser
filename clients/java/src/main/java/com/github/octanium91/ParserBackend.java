package com.github.octanium91;

import java.util.Map;

/**
 * Common interface for User-Agent parsing backends.
 */
public interface ParserBackend {
    /**
     * Initializes the parser backend with a configuration.
     * @param configJson JSON string representing the configuration.
     */
    void init(String configJson);

    /**
     * Parses the payload JSON and returns a JSON result string.
     * @param payloadJson JSON string with "ua" and "headers".
     * @return JSON string representing the result.
     */
    String parse(String payloadJson);
}
