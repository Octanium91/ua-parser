package core

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ua-parser/uap-go/uaparser"
)

// The matcher's contract is "exactly what uap-go returns". These tests check
// it against stock uaparser.Parser.Parse (same regex objects, uap-go's own
// scan loop) rather than against hand-written expectations.

// equivalenceUAs returns a varied UA set: the benchmark templates, the
// comparison corpus, hand-picked shapes that exercise long-tail device
// regexes, and mutations of all of them (case changes, non-ASCII look-alikes
// of ASCII letters, invalid UTF-8, truncation) aimed at the prefilter's
// case-folding and byte-level assumptions.
func equivalenceUAs(t testing.TB) []string {
	t.Helper()
	base := uniqueBenchUAs(2*len(benchUATemplates), benchUATemplates)

	if raw, err := os.ReadFile("../../tools/compare/corpus.json"); err == nil {
		var corpus []struct {
			UA string `json:"ua"`
		}
		if err := json.Unmarshal(raw, &corpus); err != nil {
			t.Fatalf("corpus.json: %v", err)
		}
		for _, e := range corpus {
			base = append(base, e.UA)
		}
	}

	base = append(base,
		"",
		" ",
		"Mozilla/5.0",
		"-",
		"Mozilla/5.0 (Linux; U; Android 4.0.4; en-us; L-01F Build/IMM76D) AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0 Mobile Safari/534.30",
		"Mozilla/5.0 (Linux; Android 11; Redmi Note 8 Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.45 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 9; CUBOT_X19) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.93 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 12; CPH2219) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/101.0.4951.61 Mobile Safari/537.36 OPR/69.3.3606.65458",
		"Mozilla/5.0 (Linux; Android 5.1.1; KFFOWI Build/LVY48F) AppleWebKit/537.36 (KHTML, like Gecko) Silk/68.2.6 like Chrome/68.0.3440.85 Safari/537.36",
		"Mozilla/5.0 (BlackBerry; U; BlackBerry 9900; en) AppleWebKit/534.11+ (KHTML, like Gecko) Version/7.1.0.346 Mobile Safari/534.11+",
		"Mozilla/5.0 (SymbianOS/9.4; Series60/5.0 NokiaN97-1/20.0.019; Profile/MIDP-2.1 Configuration/CLDC-1.1) AppleWebKit/525 (KHTML, like Gecko) BrowserNG/7.1.18124",
		"Nokia6230i/2.0 (03.25) Profile/MIDP-2.0 Configuration/CLDC-1.1",
		"Opera/9.80 (J2ME/MIDP; Opera Mini/9.80 (S60; SymbOS; Opera Mobi/23.348; U; en) Presto/2.5.25 Version/10.54",
		"Mozilla/5.0 (Windows Phone 10.0; Android 6.0.1; Microsoft; Lumia 950) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/52.0.2743.116 Mobile Safari/537.36 Edge/15.15254",
		"Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.2; Trident/6.0; Xbox; Xbox One)",
		"Mozilla/5.0 (Nintendo Switch; WifiWebAuthApplet) AppleWebKit/606.4 (KHTML, like Gecko) NF/6.0.1.15.4 NintendoBrowser/5.1.0.20393",
		"Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.225 Safari/537.36 CrKey/1.56.500000 DeviceType/Chromecast",
		"Roku/DVP-9.10 (519.10E04111A)",
		"AppleTV11,1/11.1",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Teams/1.5.00.22362 Chrome/85.0.4183.121 Electron/10.4.7 Safari/537.36",
		"Mozilla/5.0 (Linux; Android 10; Quest 2) AppleWebKit/537.36 (KHTML, like Gecko) OculusBrowser/16.6.0.1.52.314146309 SamsungBrowser/4.0 Chrome/91.0.4472.164 VR Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64; Tesla/2023.44.30.8) AppleWebKit/537.36 (KHTML, like Gecko) Chromium/109.0.5414.74 Chrome/109.0.5414.74 Safari/537.36",
		"Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)",
		"Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)",
		"Mozilla/5.0 (compatible; Yahoo! Slurp; http://help.yahoo.com/help/us/ysearch/slurp)",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Version/9.0 Mobile/13B143 Safari/601.1 (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)",
		"Mozilla/5.0 (compatible; MJ12bot/v1.4.8; http://mj12bot.com/)",
		"Java/1.8.0_292",
		"Wget/1.21.2",
		"Apache-HttpClient/4.5.13 (Java/11.0.15)",
		"Microsoft Office/16.0 (Windows NT 10.0; Microsoft Outlook 16.0.14326; Pro)",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:102.0) Gecko/20100101 Thunderbird/102.11.0",
		"WhatsApp/2.23.20.0 A",
		"Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/119.0.6045.105 Safari/537.36",
		"' OR '1'='1",
		"() { :; }; /bin/bash -c 'cat /etc/passwd'",
		strings.Repeat("A", 2000),
		strings.Repeat("bot ", 300),
	)

	out := make([]string, 0, len(base)*8)
	for _, ua := range base {
		out = append(out,
			ua,
			strings.ToUpper(ua),
			strings.ToLower(ua),
			// U+212A KELVIN SIGN and U+017F LONG S case-fold to k and s:
			// the regex engine treats them as those letters under (?i).
			strings.NewReplacer("k", "\u212a", "K", "\u212a", "s", "\u017f", "S", "\u017f").Replace(ua),
			ua+" \u00e9\u4e2d\u6587",            // valid non-ASCII tail
			"\xff"+ua+"\xfe\x80",                // invalid UTF-8 around
			strings.ReplaceAll(ua, " ", "\xc3"), // invalid UTF-8 inside
			ua[:len(ua)/2],
		)
	}
	return out
}

func clientsEqual(a, b *uaparser.Client) bool {
	return reflect.DeepEqual(a.UserAgent, b.UserAgent) &&
		reflect.DeepEqual(a.Os, b.Os) &&
		reflect.DeepEqual(a.Device, b.Device)
}

// TestUAPMatcherEqualsStockParser: the matcher and stock uap-go must agree on
// every field for every UA.
func TestUAPMatcherEqualsStockParser(t *testing.T) {
	p := newTestParser(t, 0)
	m := p.uap
	for _, ua := range equivalenceUAs(t) {
		want := m.uap.Parse(ua)
		got := m.match(ua, strings.ToLower(ua))
		if !clientsEqual(got, want) {
			t.Errorf("UA %q:\n got  %+v | %+v | %+v\n want %+v | %+v | %+v",
				ua, *got.UserAgent, *got.Os, *got.Device, *want.UserAgent, *want.Os, *want.Device)
		}
	}
}

// TestUAPMatcherPrefilterIsSound checks the prefilter pattern by pattern,
// which is stricter than comparing final results: whenever a regex matches a
// UA, the prefilter must have let that regex through (also for regexes that
// are shadowed by an earlier match and therefore never decide a result).
func TestUAPMatcherPrefilterIsSound(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every regex against every test UA")
	}
	p := newTestParser(t, 0)
	m := p.uap
	sc := m.scratch.Get().(*matchScratch)
	skipped, total := 0, 0
	for _, ua := range equivalenceUAs(t) {
		m.prefilter(sc, ua, strings.ToLower(ua))
		check := func(pattern int, expr string, matches bool) {
			total++
			passes := sc.satisfied[pattern] == m.need[pattern]
			if !passes {
				skipped++
			}
			if matches && !passes {
				t.Errorf("prefilter rejected a matching regex\n UA:    %q\n regex: %s", ua, expr)
			}
		}
		for i, pat := range m.uap.UA {
			check(i, pat.Reg.String(), pat.Reg.MatchString(ua))
		}
		for i, pat := range m.uap.OS {
			check(m.osBase+i, pat.Reg.String(), pat.Reg.MatchString(ua))
		}
		for i, pat := range m.uap.Device {
			check(m.deviceBase+i, pat.Reg.String(), pat.Reg.MatchString(ua))
		}
	}
	// Guard against the prefilter silently degrading into "run everything",
	// which would be correct but would bring the old cost back.
	if skipped*10 < total*9 {
		t.Errorf("prefilter skipped only %d of %d regex runs; expected well over 90%%", skipped, total)
	}
}

// TestUAPMatcherEqualsStockParserCorpus runs the equivalence check over an
// external file with one UA per line, e.g. the 100,000-line "uas" sample that
// ships in the uap-go module:
//
//	UA_PARSER_EQUIV_CORPUS=$(go list -m -f '{{.Dir}}' github.com/ua-parser/uap-go)/uas go test ./pkg/core -run EqualsStockParserCorpus
func TestUAPMatcherEqualsStockParserCorpus(t *testing.T) {
	path := os.Getenv("UA_PARSER_EQUIV_CORPUS")
	if path == "" {
		t.Skip("UA_PARSER_EQUIV_CORPUS not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	p := newTestParser(t, 0)
	m := p.uap

	lines := make(chan string, 1024)
	var wg sync.WaitGroup
	var mu sync.Mutex
	mismatches, checked := 0, 0
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ua := range lines {
				want := m.uap.Parse(ua)
				got := m.match(ua, strings.ToLower(ua))
				mu.Lock()
				checked++
				if !clientsEqual(got, want) {
					mismatches++
					if mismatches <= 20 {
						t.Errorf("UA %q:\n got  %+v | %+v | %+v\n want %+v | %+v | %+v",
							ua, *got.UserAgent, *got.Os, *got.Device, *want.UserAgent, *want.Os, *want.Device)
					}
				}
				mu.Unlock()
			}
		}()
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		lines <- scanner.Text()
	}
	close(lines)
	wg.Wait()
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d UAs checked, %d mismatches", checked, mismatches)
}

// TestRequiredClauses pins the literal analysis on shapes that occur in
// uap-core, including the ones where a naive analysis would be unsound.
func TestRequiredClauses(t *testing.T) {
	lit := func(s string) requiredLiteral { return requiredLiteral{text: s} }
	fold := func(s string) requiredLiteral { return requiredLiteral{text: s, fold: true} }
	cases := []struct {
		expr string
		want [][]requiredLiteral
	}{
		{`(Firefox)/(\d+)\.(\d+)`, [][]requiredLiteral{{lit("Firefox/")}}},
		// Optional parts require nothing.
		{`(Chrome)?/(\d+)`, nil},
		{`(?:Mobile|)Safari`, [][]requiredLiteral{{lit("Safari")}}},
		{`x*y?`, nil},
		// The parser factors common prefixes; the analysis must put them back.
		{`(WebCrunch|WebZIP)/`, [][]requiredLiteral{{lit("WebCrunch/"), lit("WebZIP/")}}},
		{`bots?/\d`, [][]requiredLiteral{{lit("bot/"), lit("bots/")}}},
		// The parser turns [Bb] into a case-insensitive "b".
		{`[Bb]ot`, [][]requiredLiteral{{fold("bot")}}},
		{`[Bb]ot|[XY]z`, [][]requiredLiteral{{fold("bot"), lit("Xz"), lit("Yz")}}},
		// A literal that contains another alternative adds nothing.
		{`(bot|bingbot|spider)`, [][]requiredLiteral{{lit("bot"), lit("spider")}}},
		// Case-insensitive literals are stored lowercased.
		{`(?i)(Android|iPhone) OS`, [][]requiredLiteral{{fold("android os"), fold("iphone os")}}},
		// Repetition: the body is required, but not as one exact string.
		{`(?:ab)+c`, [][]requiredLiteral{{lit("ab")}}},
		{`(?:ab){2,3}`, [][]requiredLiteral{{lit("ab")}}},
		{`(?:ab){0,3}cd`, [][]requiredLiteral{{lit("cd")}}},
		// Both sides of a gap are required.
		{`Android.{0,20}Build/`, [][]requiredLiteral{{lit("Android")}, {lit("Build/")}}},
		// Alternation of two-part branches: every combination is implied.
		{`(?:AA.*BB|CC)`, [][]requiredLiteral{{lit("AA"), lit("CC")}, {lit("BB"), lit("CC")}}},
		// One-byte literals are not worth checking.
		{`a.b`, nil},
		// Non-ASCII literals are never used.
		{`caf\x{e9}`, nil},
	}
	for _, tc := range cases {
		got := requiredClauses(tc.expr)
		if !sameClauses(got, tc.want) {
			t.Errorf("%s:\n got  %v\n want %v", tc.expr, got, tc.want)
		}
	}
}

// sameClauses compares CNF formulas ignoring clause and literal order.
func sameClauses(a, b [][]requiredLiteral) bool {
	key := func(cs [][]requiredLiteral) map[string]int {
		out := map[string]int{}
		for _, c := range cs {
			parts := make([]string, 0, len(c))
			for _, l := range c {
				s := l.text
				if l.fold {
					s = "(?i)" + s
				}
				parts = append(parts, s)
			}
			// Order-insensitive within a clause.
			for i := range parts {
				for j := i + 1; j < len(parts); j++ {
					if parts[j] < parts[i] {
						parts[i], parts[j] = parts[j], parts[i]
					}
				}
			}
			out[strings.Join(parts, "\x00")]++
		}
		return out
	}
	return reflect.DeepEqual(key(a), key(b))
}

// TestUAPMatcherPrunedVariantsMatchOriginal checks branch pruning at the
// lowest level: for every prunable regex and every test UA, the pruned
// variant must return exactly the submatch indexes of the original regex.
func TestUAPMatcherPrunedVariantsMatchOriginal(t *testing.T) {
	p := newTestParser(t, 0)
	m := p.uap

	var regs []*regexp.Regexp
	for _, pat := range m.uap.UA {
		regs = append(regs, pat.Reg)
	}
	for _, pat := range m.uap.OS {
		regs = append(regs, pat.Reg)
	}
	for _, pat := range m.uap.Device {
		regs = append(regs, pat.Reg)
	}

	prunable := 0
	for _, pr := range m.prunable {
		if pr != nil {
			prunable++
		}
	}
	if prunable == 0 {
		t.Fatal("no regex qualified for branch pruning; the bot path would be slow again")
	}

	sc := m.scratch.Get().(*matchScratch)
	used := 0
	for _, ua := range equivalenceUAs(t) {
		m.prefilter(sc, ua, strings.ToLower(ua))
		for i, pr := range m.prunable {
			if pr == nil {
				continue
			}
			variant := pr.variant(m, sc)
			if variant == nil {
				continue
			}
			used++
			got, want := variant.FindStringSubmatchIndex(ua), regs[i].FindStringSubmatchIndex(ua)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("pruned variant differs\n UA:      %q\n regex:   %s\n variant: %s\n got %v want %v",
					ua, regs[i], variant, got, want)
			}
		}
	}
	if used == 0 {
		t.Error("no pruned variant was ever used by the test UAs")
	}
	t.Logf("%d prunable regexes, %d variant runs compared", prunable, used)
}

// TestUAPMatcherCoalescesConcurrentParses: goroutines that ask for a UA
// while it is already being parsed must wait for that run and share its
// result instead of running the regex stage again.
func TestUAPMatcherCoalescesConcurrentParses(t *testing.T) {
	p := newTestParser(t, 0)
	m := p.uap
	const ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.7390.55 Safari/537.36 coalesce-test"

	// Pose as a parse of ua that is in progress.
	call := &inflightParse{done: make(chan struct{})}
	m.inflightMu.Lock()
	m.inflight[ua] = call
	m.inflightMu.Unlock()

	const waiters = 32
	results := make(chan *uaparser.Client, waiters)
	for i := 0; i < waiters; i++ {
		go func() { results <- m.parse(ua, strings.ToLower(ua)) }()
	}

	select {
	case <-results:
		t.Fatal("a caller returned while the same UA was still being parsed")
	case <-time.After(100 * time.Millisecond):
	}
	if runs := m.regexRuns.Load(); runs != 0 {
		t.Fatalf("regex stage ran %d times while a parse of the same UA was in flight", runs)
	}

	// Finish the in-flight parse the way parse() does.
	want := m.match(ua, strings.ToLower(ua))
	m.memo.Add(ua, want)
	call.client = want
	m.inflightMu.Lock()
	delete(m.inflight, ua)
	m.inflightMu.Unlock()
	close(call.done)

	for i := 0; i < waiters; i++ {
		if got := <-results; got != want {
			t.Fatalf("waiter %d did not receive the shared result", i)
		}
	}
	if runs := m.regexRuns.Load(); runs != 1 {
		t.Errorf("regex stage ran %d times for %d concurrent requests, want 1", runs, waiters+1)
	}
}

// TestParseSameUAConcurrently drives the public API from many goroutines
// with one never-seen UA per round; every caller must get the same answer.
func TestParseSameUAConcurrently(t *testing.T) {
	p := newTestParser(t, 0)
	uas := uniqueBenchUAs(200, benchUATemplates)
	for _, ua := range uas {
		want := p.computeResult(ua+" ref", map[string]string{}, p.corrections.Load())
		var wg sync.WaitGroup
		got := make([]*Result, 16)
		for i := range got {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got[i] = p.Parse(ua, nil)
			}()
		}
		wg.Wait()
		for i := range got {
			if !reflect.DeepEqual(got[i], got[0]) {
				t.Fatalf("UA %q: goroutine %d got a different result", ua, i)
			}
		}
		if got[0].Browser.Name != want.Browser.Name || got[0].OS.Name != want.OS.Name {
			t.Fatalf("UA %q: coalesced result %s/%s differs from a solo parse %s/%s",
				ua, got[0].Browser.Name, got[0].OS.Name, want.Browser.Name, want.OS.Name)
		}
	}
	if runs := p.uap.regexRuns.Load(); runs > uint64(2*len(uas)) {
		t.Errorf("regex stage ran %d times for %d distinct UAs (plus %d reference parses)", runs, len(uas), len(uas))
	}
}
