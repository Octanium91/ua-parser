package com.github.octanium91;

import org.junit.Test;

import java.util.Collections;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assume.assumeTrue;

/**
 * Smoke tests that run when native/WASM resources are staged
 * (CI does this before packaging); they are skipped in bare source builds.
 */
public class UaParserSmokeTest {

    private static final String CHROME_UA =
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
                    + " (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";

    private static boolean hasResource(String path) {
        return UaParserSmokeTest.class.getResource(path) != null;
    }

    @Test
    public void wasmBackendParsesChromeUa() {
        assumeTrue("ua-parser.wasm not staged; skipping", hasResource("/ua-parser.wasm"));

        WasmBackend backend = new WasmBackend();
        backend.init("{\"disable_auto_update\":true,\"lru_cache_size\":100}");
        String json = backend.parse("{\"ua\":\"" + CHROME_UA + "\",\"headers\":{}}");
        assertNotNull("WASM backend returned null", json);
        assumeTrue(json.contains("\"Chrome\""));
    }

    @Test
    public void endToEndSelectsSomeBackendAndParses() {
        assumeTrue("no resources staged; skipping", hasResource("/ua-parser.wasm"));

        UaParser parser = new UaParser();
        UaParser.Config cfg = new UaParser.Config();
        cfg.disableAutoUpdate = true;
        cfg.lruCacheSize = 100;
        parser.init(cfg);

        UaParser.Result result = parser.parse(CHROME_UA, Collections.emptyMap());
        assertNotNull(result);
        assertNotNull("browser missing in result", result.browser);
        assertEquals("Chrome", result.browser.name);
    }
}
