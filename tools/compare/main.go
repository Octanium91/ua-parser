// Command compare runs the shared UA corpus through this project's core parser.
// It is one half of the ua-parser-js comparison harness (see node/parse.mjs for
// the other half and README.md for the methodology).
//
// Modes:
//
//	go run ./tools/compare -corpus tools/compare/corpus.json -out out/go.json
//	go run ./tools/compare -corpus tools/compare/corpus.json -bench 200000
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	core "github.com/Octanium91/ua-parser/pkg/core"
)

type corpusEntry struct {
	ID      string            `json:"id"`
	UA      string            `json:"ua"`
	Headers map[string]string `json:"headers,omitempty"`
}

type reportEntry struct {
	ID     string       `json:"id"`
	Result *core.Result `json:"result"`
}

func main() {
	corpusPath := flag.String("corpus", "corpus.json", "path to corpus.json")
	outPath := flag.String("out", "", "write parse results to this file (default: stdout)")
	benchN := flag.Int("bench", 0, "run benchmark with N iterations instead of dumping results")
	cacheSize := flag.Int("cache", 0, "LRU cache size for the benchmark cached pass")
	flag.Parse()

	raw, err := os.ReadFile(*corpusPath)
	if err != nil {
		fatal(err)
	}
	var corpus []corpusEntry
	if err := json.Unmarshal(raw, &corpus); err != nil {
		fatal(err)
	}

	if *benchN > 0 {
		bench(corpus, *benchN, *cacheSize)
		return
	}

	parser, err := core.New(core.Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	if err != nil {
		fatal(err)
	}
	defer parser.Close()

	report := make([]reportEntry, 0, len(corpus))
	for _, e := range corpus {
		report = append(report, reportEntry{ID: e.ID, Result: parser.Parse(e.UA, e.Headers)})
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	if *outPath == "" {
		fmt.Println(string(out))
		return
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*outPath, out, 0o644); err != nil {
		fatal(err)
	}
}

// bench measures full-pipeline throughput (regex parse + inference + Client
// Hints + bot/AI detection) in two passes: cache disabled (every parse is a
// miss) and, when -cache > 0, cache enabled (steady-state hits, the realistic
// web-traffic profile where the same UAs repeat).
func bench(corpus []corpusEntry, n, cacheSize int) {
	runPass := func(label string, size int) {
		parser, err := core.New(core.Config{DisableAutoUpdate: true, LRUCacheSize: size})
		if err != nil {
			fatal(err)
		}
		defer parser.Close()

		// Warmup: exercises all code paths and, for the cached pass, fills the LRU.
		for _, e := range corpus {
			parser.Parse(e.UA, e.Headers)
		}

		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		start := time.Now()
		for i := 0; i < n; i++ {
			e := corpus[i%len(corpus)]
			parser.Parse(e.UA, e.Headers)
		}
		elapsed := time.Since(start)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		fmt.Printf("%s: %d parses in %v — %.0f ops/sec, %.2f µs/op, %.0f B/op alloc\n",
			label, n, elapsed.Round(time.Millisecond),
			float64(n)/elapsed.Seconds(),
			float64(elapsed.Microseconds())/float64(n),
			float64(after.TotalAlloc-before.TotalAlloc)/float64(n))
	}

	runPass("uncached", 0)
	if cacheSize > 0 {
		runPass(fmt.Sprintf("cached(size=%d)", cacheSize), cacheSize)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("process heap in use: %.1f MB\n", float64(ms.HeapInuse)/1024/1024)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
