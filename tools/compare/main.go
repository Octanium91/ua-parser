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
	"strconv"
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

// uniqueUA makes the i-th parse of a corpus entry a string that has never been
// parsed before, while keeping its shape: the last run of digits (normally
// the tail of a version number) is replaced by i. Entries without any digit
// get a numbered build token appended.
func uniqueUA(ua string, i int) string {
	end := len(ua)
	for end > 0 && (ua[end-1] < '0' || ua[end-1] > '9') {
		end--
	}
	if end == 0 {
		return ua + " build/" + strconv.Itoa(i)
	}
	start := end
	for start > 0 && ua[start-1] >= '0' && ua[start-1] <= '9' {
		start--
	}
	return ua[:start] + strconv.Itoa(i) + ua[end:]
}

// bench measures full-pipeline throughput (regex parse + inference + Client
// Hints + bot/AI detection) in up to three passes that must not be confused:
//
//   - unique UAs: every parse sees a string that is in no cache. This is the
//     real parsing cost, and the number to compare with cache-less parsers.
//   - repeated UAs, no LRU: the corpus round-robin with the result LRU off.
//     The regex stage is still answered from the core's internal match memo,
//     so this measures the post-regex pipeline, NOT regex parsing.
//   - repeated UAs, LRU hits (when -cache > 0): the production default for
//     traffic where the same UAs repeat.
func bench(corpus []corpusEntry, n, cacheSize int) {
	runPass := func(label string, size int, unique bool) {
		parser, err := core.New(core.Config{DisableAutoUpdate: true, LRUCacheSize: size})
		if err != nil {
			fatal(err)
		}
		defer parser.Close()

		// Warmup: exercises all code paths and, for the cached pass, fills the LRU.
		for _, e := range corpus {
			parser.Parse(e.UA, e.Headers)
		}

		// Unique strings are built before the clock starts.
		var uas []string
		if unique {
			uas = make([]string, n)
			seen := make(map[string]struct{}, n)
			for i := range uas {
				uas[i] = uniqueUA(corpus[i%len(corpus)].UA, i)
				if _, dup := seen[uas[i]]; dup {
					fatal(fmt.Errorf("unique pass produced a duplicate UA: %q", uas[i]))
				}
				seen[uas[i]] = struct{}{}
			}
		}

		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		start := time.Now()
		for i := 0; i < n; i++ {
			e := corpus[i%len(corpus)]
			if unique {
				parser.Parse(uas[i], e.Headers)
			} else {
				parser.Parse(e.UA, e.Headers)
			}
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

	runPass("unique UAs (no LRU)", 0, true)
	runPass("repeated UAs (no LRU)", 0, false)
	if cacheSize > 0 {
		runPass(fmt.Sprintf("repeated UAs, LRU hits (size=%d)", cacheSize), cacheSize, false)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("process heap in use: %.1f MB\n", float64(ms.HeapInuse)/1024/1024)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
