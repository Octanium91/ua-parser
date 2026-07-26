package compare;

import com.github.octanium91.UaParser;
import com.google.gson.Gson;
import com.google.gson.reflect.TypeToken;
import nl.basjes.parse.useragent.UserAgent;
import nl.basjes.parse.useragent.UserAgentAnalyzer;
import ua_parser.Client;
import ua_parser.Parser;

import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Java side of the comparison harness (see tools/compare/README.md).
 *
 * Each invocation measures exactly ONE library in a fresh JVM, so heap and RSS
 * numbers are attributable:
 *
 *   java -Xmx2g -cp "target/classes;target/libs/*" compare.Main \
 *       <corpus.json> <ours|yauaa|uap> <iterations> [cacheSize]
 *
 * Reported per run: init time (construction + first parse), heap after init,
 * uncached/cached throughput, heap after the bench. Peak process RSS is
 * sampled externally by the runner script.
 */
public final class Main {

    static final class Entry {
        String id;
        String ua;
        Map<String, String> headers;
    }

    /** Common interface over the three libraries: one full parse of an entry. */
    interface ParseFn {
        Object parse(Entry e);
    }

    public static void main(String[] args) throws Exception {
        String corpusPath = args[0];
        String impl = args[1];
        int n = Integer.parseInt(args[2]);
        int cacheSize = args.length > 3 ? Integer.parseInt(args[3]) : 0;

        List<Entry> corpus = new Gson().fromJson(
                Files.newBufferedReader(Paths.get(corpusPath)),
                new TypeToken<List<Entry>>() {}.getType());

        long t0 = System.nanoTime();
        ParseFn fn = build(impl, cacheSize);
        fn.parse(corpus.get(0)); // include lazy-init paths in the init measurement
        long initMs = (System.nanoTime() - t0) / 1_000_000;

        double heapInit = heapUsedMb();

        for (Entry e : corpus) { // warmup (also fills caches when enabled)
            fn.parse(e);
        }

        long t1 = System.nanoTime();
        for (int i = 0; i < n; i++) {
            fn.parse(corpus.get(i % corpus.size()));
        }
        double elapsedMs = (System.nanoTime() - t1) / 1e6;

        double heapAfter = heapUsedMb();

        System.out.printf(
                "impl=%s cache=%d init=%dms heapAfterInit=%.1fMB | %d parses in %.0fms — %.0f ops/sec, %.2f µs/op | heapAfterBench=%.1fMB%n",
                impl, cacheSize, initMs, heapInit,
                n, elapsedMs, n / elapsedMs * 1000, elapsedMs * 1000 / n,
                heapAfter);
        System.out.flush();

        // Settle window: heapUsedMb() above already ran a double GC; sleeping
        // here lets the external runner sample the post-GC steady-state RSS
        // before the process exits (its last sample ≈ settled footprint).
        Thread.sleep(2000);
    }

    private static ParseFn build(String impl, int cacheSize) {
        switch (impl) {
            case "ours": {
                UaParser parser = new UaParser();
                UaParser.Config cfg = new UaParser.Config();
                cfg.disableAutoUpdate = true;
                cfg.lruCacheSize = cacheSize;
                parser.init(cfg);
                System.out.println("backend: " + parser.getBackendName());
                return e -> parser.parse(e.ua, e.headers);
            }
            case "yauaa": {
                UserAgentAnalyzer.UserAgentAnalyzerBuilder builder = UserAgentAnalyzer
                        .newBuilder()
                        .hideMatcherLoadStats()
                        .immediateInitialization();
                if (cacheSize > 0) {
                    builder.withCache(cacheSize);
                } else {
                    builder.withoutCache();
                }
                UserAgentAnalyzer analyzer = builder.build();
                return e -> {
                    Map<String, String> headers = new HashMap<>();
                    headers.put("User-Agent", e.ua);
                    if (e.headers != null) {
                        headers.putAll(e.headers);
                    }
                    UserAgent result = analyzer.parse(headers);
                    return result.getValue(UserAgent.AGENT_NAME_VERSION);
                };
            }
            case "uap": {
                // uap-java: UA string only (no Client Hints, no bot flags, no cache).
                Parser parser = new Parser();
                return e -> {
                    Client c = parser.parse(e.ua);
                    return c.userAgent.family;
                };
            }
            default:
                throw new IllegalArgumentException("unknown impl: " + impl);
        }
    }

    private static double heapUsedMb() throws InterruptedException {
        for (int i = 0; i < 2; i++) {
            System.gc();
            Thread.sleep(200);
        }
        Runtime rt = Runtime.getRuntime();
        return (rt.totalMemory() - rt.freeMemory()) / 1024.0 / 1024.0;
    }

    private Main() {
    }
}
