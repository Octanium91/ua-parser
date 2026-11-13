package core

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Benchmarks
//
// Two very different paths are measured here and must not be confused:
//
//   - unique UA: the string has never been seen before, so the regex lists
//     really have to be walked. This is what a cache miss costs on diverse
//     real traffic, and it is the number that describes parsing speed.
//   - repeated UA: the same string again and again. It is answered from a
//     cache, so it says nothing about parsing speed.

// benchUAClasses is a mix of real desktop, mobile, TV, bot and tool UA
// shapes, grouped by traffic class. uniqueBenchUAs fills the placeholders:
// {A} is a browser-major-like number (100..139), {B} a small minor/patch
// number (0..99) and {C} a build-like number (1000 and up).
var benchUAClasses = []struct {
	name      string
	templates []string
}{{"desktop", []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.0.0 Safari/537.36 Edg/{A}.0.{C}.{B}",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:{A}.0) Gecko/20100101 Firefox/{A}.0.{B}",
	"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:{A}.0) Gecko/20100101 Firefox/{A}.0.{B}",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.{B} (KHTML, like Gecko) Version/17.{B} Safari/{C}.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.0.0 Safari/537.36 OPR/{A}.0.{C}.{B}",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.0.0 YaBrowser/24.{B}.{C}.0 Safari/537.36",
	"Mozilla/5.0 (X11; CrOS x86_64 {C}.{B}.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.0.0 Safari/537.36",
}}, {"mobile", []string{
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_{B} like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.{B} Mobile/15E{C} Safari/604.1",
	"Mozilla/5.0 (iPad; CPU OS 16_{B} like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/{A}.0.{C}.{B} Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 14; SM-S{C}B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 13; SM-A{C}F) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/{B}.0 Chrome/{A}.0.{C}.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 14; Pixel {B} Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 12; M{C}K6G) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 13; SM-X{C}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Safari/537.36",
	"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 13; SM-G{C}B Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/{A}.0.{C}.{B} Mobile Safari/537.36 [FB_IAB/FB4A;FBAV/4{B}.0.0.{B}.{A};]",
	"Mozilla/5.0 (Android 14; Mobile; rv:{A}.0) Gecko/{A}.0 Firefox/{A}.{B}",
}}, {"tv-console", []string{
	"Mozilla/5.0 (SMART-TV; Linux; Tizen 6.{B}) AppleWebKit/537.36 (KHTML, like Gecko) {A}.0.{C}.0/6.{B} TV Safari/537.36",
	"Mozilla/5.0 (PlayStation; PlayStation 5/{B}.{A}) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.{B} Safari/605.1.15",
}}, {"bot", []string{
	"Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{A}.0.{C}.{B} Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
	"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/{A}.0.{C}.{B} Safari/537.36",
	"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.{B}; +https://openai.com/gptbot) Chrome/{A}.0.{C}.0",
	"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.{B}; +claudebot@anthropic.com) r{C}",
	"Mozilla/5.0 (compatible; AhrefsBot/7.{B}; +http://ahrefs.com/robot/) b{C}",
	"facebookexternalhit/1.{B} (+http://www.facebook.com/externalhit_uatext.php) {C}",
}}, {"tool", []string{
	"curl/8.{B}.1 libcurl/8.{B}.1 OpenSSL/3.0.{A} zlib/1.3 build/{C}",
	"python-requests/2.{B}.{A}",
	"okhttp/4.{B}.{A}",
	"Go-http-client/1.1 (build {C})",
	"PostmanRuntime/7.{B}.{A}",
	"Dalvik/2.1.0 (Linux; U; Android 13; SM-A{C}U Build/TP1A.220624.{B})",
}}}

// benchUATemplates is every class together: the traffic mix used by the
// headline unique-UA benchmarks.
var benchUATemplates = func() []string {
	var all []string
	for _, class := range benchUAClasses {
		all = append(all, class.templates...)
	}
	return all
}()

// uniqueBenchUAs builds n distinct UA strings by cycling through the given
// templates and varying the numeric fields. It panics on a duplicate,
// so the "unique" in the benchmark names stays true when templates change.
func uniqueBenchUAs(n int, templates []string) []string {
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		round := i / len(templates)
		ua := strings.NewReplacer(
			"{A}", strconv.Itoa(100+round%40),
			"{B}", strconv.Itoa(round/40%100),
			"{C}", strconv.Itoa(1000+round),
		).Replace(templates[i%len(templates)])
		if _, dup := seen[ua]; dup {
			panic("uniqueBenchUAs: duplicate UA: " + ua)
		}
		seen[ua] = struct{}{}
		out = append(out, ua)
	}
	return out
}

// benchUniquePoolSize is far larger than any cache in the pipeline, so by the
// time the pool wraps around every entry has long been evicted and each parse
// is still a genuine miss.
const benchUniquePoolSize = 1 << 16

const benchFixedUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"

// BenchmarkParseUniqueUA is the honest parsing-speed number: every iteration
// parses a UA string that is in no cache (result LRU disabled, pool far
// larger than any internal memo).
func BenchmarkParseUniqueUA(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	defer p.Close()
	uas := uniqueBenchUAs(benchUniquePoolSize, benchUATemplates)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(uas[i%len(uas)], nil)
	}
}

// BenchmarkParseUniqueUAByClass breaks the unique-UA cost down by traffic
// class. Bots are the expensive class: their UAs are matched by uap-core's
// largest alternation regexes, which the prefilter cannot skip.
func BenchmarkParseUniqueUAByClass(b *testing.B) {
	for _, class := range benchUAClasses {
		b.Run(class.name, func(b *testing.B) {
			p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
			defer p.Close()
			// {A} and {B} alone give 4,000 distinct fillings per template.
			uas := uniqueBenchUAs(min(benchUniquePoolSize, 4000*len(class.templates)), class.templates)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p.Parse(uas[i%len(uas)], nil)
			}
		})
	}
}

// BenchmarkParseUniqueUAWithLRU is the same miss path with the result LRU
// enabled (the default in every shipped driver): a miss additionally pays for
// building the cache key and inserting/evicting an entry.
func BenchmarkParseUniqueUAWithLRU(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 1000})
	defer p.Close()
	uas := uniqueBenchUAs(benchUniquePoolSize, benchUATemplates)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(uas[i%len(uas)], nil)
	}
}

// BenchmarkParseUniqueUAParallel runs the miss path from all CPUs at once.
func BenchmarkParseUniqueUAParallel(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	defer p.Close()
	uas := uniqueBenchUAs(benchUniquePoolSize, benchUATemplates)
	var next atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Parse(uas[next.Add(1)%uint64(len(uas))], nil)
		}
	})
}

// BenchmarkParseRepeatedUANoLRU parses ONE fixed UA with the result LRU
// disabled. It does not measure regex parsing of unseen strings; see
// BenchmarkParseUniqueUA for that. Formerly named BenchmarkParse.
func BenchmarkParseRepeatedUANoLRU(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	defer p.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(benchFixedUA, nil)
	}
}

// BenchmarkParseRepeatedUA parses ONE fixed UA with the result LRU enabled:
// a pure cache hit (key build + lookup + copy). Formerly
// BenchmarkParseWithCache.
func BenchmarkParseRepeatedUA(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 1000})
	defer p.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(benchFixedUA, nil)
	}
}

// BenchmarkParseRepeatedUAWithHeaders is the repeated-UA, no-LRU path with
// Client Hints applied on every call. Formerly BenchmarkParseWithHeaders.
func BenchmarkParseRepeatedUAWithHeaders(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	defer p.Close()
	headers := map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"15.0.0"`,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(benchFixedUA, headers)
	}
}

// BenchmarkParseRepeatedUAParallel is the LRU cache-hit path under
// contention. Formerly BenchmarkParseParallel.
func BenchmarkParseRepeatedUAParallel(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 1000})
	defer p.Close()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Parse(benchFixedUA, nil)
		}
	})
}

// BenchmarkParseUniqueUABurst models a burst: all CPUs ask for the same
// never-seen UA at the same moment (e.g. many requests of one new client
// arriving together), round after round. Concurrent parses of one UA are
// coalesced, so a round costs about one parse rather than one per CPU.
// Reported time is per round.
func BenchmarkParseUniqueUABurst(b *testing.B) {
	p, _ := New(Config{DisableAutoUpdate: true, LRUCacheSize: 0})
	defer p.Close()
	uas := uniqueBenchUAs(benchUniquePoolSize, benchUATemplates)
	workers := runtime.GOMAXPROCS(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ua := uas[i%len(uas)]
		var wg sync.WaitGroup
		wg.Add(workers)
		for w := 0; w < workers; w++ {
			go func() {
				defer wg.Done()
				p.Parse(ua, nil)
			}()
		}
		wg.Wait()
	}
	b.ReportMetric(float64(p.uap.regexRuns.Load())/float64(b.N), "regex-runs/round")
}
