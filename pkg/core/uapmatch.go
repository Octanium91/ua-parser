package core

import (
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ua-parser/uap-go/uaparser"
)

// uapMatcher runs the uap-core regex lists (browser, OS, device) over a UA
// string and returns exactly what uaparser.Parser.Parse returns, only faster.
//
// uap-core is three ordered lists of ~1,300 regexes in total and the first
// match in each list wins, so a UA that is not cached has to be tried against
// hundreds of regexes; with Go's regexp engine that costs several hundred
// microseconds. Almost every one of those regexes, however, can only match if
// some literal text is present in the UA ("Firefox/", "SM-", "bot"). The
// matcher extracts those required literals from each compiled regex once, at
// build time. At parse time it finds, in a single pass over the UA, which of
// the literals occur, and skips every regex whose requirements are not met.
// The regexes that remain are run in the original list order and decide every
// match themselves, so results are identical by construction: the prefilter
// can only say "cannot match", never "matches".
//
// The few regexes that are too large for Go to match quickly are additionally
// run in a per-UA pruned form; see uapmatch_prune.go.
//
// A uapMatcher is immutable after newUAPMatcher apart from its internal
// caches, and is safe for concurrent use. A regex hot-swap builds a new one.
type uapMatcher struct {
	uap *uaparser.Parser // owns the compiled regex lists

	// Patterns of all three lists share one index space: browser patterns
	// first, then OS (osBase), then device (deviceBase). After the real
	// patterns come pseudo-patterns that hold the requirements of individual
	// alternation branches (see prunablePattern.slots).
	osBase, deviceBase int

	// need[p] is the number of clauses pattern p requires; a pattern is run
	// only when all of them are satisfied. 0 means "always run".
	need []uint16
	// prunable[p] is non-nil for the real patterns that support branch
	// pruning.
	prunable []*prunablePattern
	// clausePattern[c] is the pattern that clause c belongs to.
	clausePattern []uint32
	// litClauses[l] lists the clauses that literal l satisfies.
	litClauses [][]uint32
	// foldClauses are the clauses containing a case-insensitive literal.
	foldClauses []uint32

	// exactIndex finds case-sensitive literals in the raw UA, foldIndex
	// case-insensitive ones (stored lowercased) in the lowercased UA.
	exactIndex, foldIndex literalIndex

	// memo remembers the regex-stage outcome per UA string. It keeps the
	// repeated-UA path cheap when the result LRU is disabled, or misses
	// because only the headers differ (uap-go's own Parse had an equivalent
	// internal cache of the same size).
	memo *lru.Cache[string, *uaparser.Client]

	// inflight coalesces concurrent parses of the same UA string: the first
	// caller runs the regex stage, the others wait for its result instead of
	// repeating the work (a burst of requests carrying one new UA costs one
	// parse, not one per request).
	inflightMu sync.Mutex
	inflight   map[string]*inflightParse

	// regexRuns counts executions of the regex stage (memo misses that were
	// not coalesced). Read by tests.
	regexRuns atomic.Uint64

	scratch sync.Pool // *matchScratch
}

// inflightParse is one regex-stage run that other goroutines may wait for.
type inflightParse struct {
	done   chan struct{}    // closed when client is set (or the run failed)
	client *uaparser.Client // nil if the run panicked
}

// uapMemoSize is the number of UA strings whose regex-stage outcome is
// remembered (same as uap-go's default internal cache size).
const uapMemoSize = 1024

// requiredLiteral is a piece of text a regex needs in order to match.
type requiredLiteral struct {
	text string
	fold bool // case-insensitive: text is lowercase, search the lowercased UA
}

// literalIndex locates a set of literals (each at least two bytes long, pure
// ASCII) inside a string in one pass: literals are bucketed by their first
// two bytes, so at each position of the input only the few literals that
// start with the two bytes found there are compared.
type literalIndex struct {
	start [1<<14 + 1]uint32 // bucket b is entries[start[b]:start[b+1]]
	// entries are sorted by bucket.
	entries []literalEntry
}

type literalEntry struct {
	rest string // the literal without its first two bytes
	id   uint32 // index into uapMatcher.litClauses
}

func bucketOf(b0, b1 byte) int { return int(b0)<<7 | int(b1) }

func newLiteralIndex(lits []requiredLiteral, fold bool) literalIndex {
	var idx literalIndex
	type keyed struct {
		bucket int
		entry  literalEntry
	}
	var all []keyed
	for id, lit := range lits {
		if lit.fold == fold {
			all = append(all, keyed{bucketOf(lit.text[0], lit.text[1]), literalEntry{rest: lit.text[2:], id: uint32(id)}})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].bucket < all[j].bucket })
	idx.entries = make([]literalEntry, len(all))
	for i, k := range all {
		idx.entries[i] = k.entry
		idx.start[k.bucket+1]++
	}
	for b := 1; b < len(idx.start); b++ {
		idx.start[b] += idx.start[b-1]
	}
	return idx
}

// matchScratch is the per-parse working state.
type matchScratch struct {
	clauseHit []bool   // per clause: already counted as satisfied
	satisfied []uint16 // per pattern: number of satisfied clauses
}

func newUAPMatcher(uap *uaparser.Parser) *uapMatcher {
	m := &uapMatcher{
		uap:        uap,
		osBase:     len(uap.UA),
		deviceBase: len(uap.UA) + len(uap.OS),
	}
	var lits []requiredLiteral
	litID := map[requiredLiteral]uint32{}

	// register adds a (pseudo-)pattern with the given requirements and
	// returns its index.
	register := func(clauses [][]requiredLiteral) int {
		pattern := len(m.need)
		m.need = append(m.need, uint16(len(clauses)))
		for _, clause := range clauses {
			c := uint32(len(m.clausePattern))
			m.clausePattern = append(m.clausePattern, uint32(pattern))
			hasFold := false
			for _, lit := range clause {
				id, ok := litID[lit]
				if !ok {
					id = uint32(len(lits))
					litID[lit] = id
					lits = append(lits, lit)
					m.litClauses = append(m.litClauses, nil)
				}
				m.litClauses[id] = append(m.litClauses[id], c)
				hasFold = hasFold || lit.fold
			}
			if hasFold {
				m.foldClauses = append(m.foldClauses, c)
			}
		}
		return pattern
	}

	// Reg.String() is the exact source the regex was compiled from, flags
	// included, so the analysis sees precisely what the matcher runs.
	var regs []*regexp.Regexp
	for _, pat := range uap.UA {
		regs = append(regs, pat.Reg)
	}
	for _, pat := range uap.OS {
		regs = append(regs, pat.Reg)
	}
	for _, pat := range uap.Device {
		regs = append(regs, pat.Reg)
	}
	for _, reg := range regs {
		register(requiredClauses(reg.String()))
	}

	// Pseudo-patterns for the branches of prunable regexes go after all real
	// patterns, so real pattern indexes stay equal to list positions.
	m.prunable = make([]*prunablePattern, len(regs))
	for i, reg := range regs {
		pr, branchClauses := newPrunablePattern(reg.String(), reg.NumSubexp())
		if pr == nil {
			continue
		}
		for j, clauses := range branchClauses {
			pr.slots[j] = -1
			if len(clauses) > 0 {
				pr.slots[j] = int32(register(clauses))
			}
		}
		m.prunable[i] = pr
	}

	m.exactIndex = newLiteralIndex(lits, false)
	m.foldIndex = newLiteralIndex(lits, true)

	m.inflight = map[string]*inflightParse{}
	m.memo, _ = lru.New[string, *uaparser.Client](uapMemoSize) // only fails for size <= 0
	nClauses, nPatterns := len(m.clausePattern), len(m.need)
	m.scratch.New = func() any {
		return &matchScratch{clauseHit: make([]bool, nClauses), satisfied: make([]uint16, nPatterns)}
	}
	return m
}

// prefilter fills sc for one UA: afterwards pattern p can only match if
// sc.satisfied[p] == m.need[p]. lower must be strings.ToLower(line).
func (m *uapMatcher) prefilter(sc *matchScratch, line, lower string) {
	clear(sc.clauseHit)
	clear(sc.satisfied)

	m.markPresent(sc, &m.exactIndex, line)
	if isASCII(line) {
		m.markPresent(sc, &m.foldIndex, lower)
	} else {
		// Case-insensitive literals are compared against a lowercased copy
		// of the UA. That is only equivalent to the regex engine's Unicode
		// case folding when the input is pure ASCII ("(?i)k" also matches
		// the Kelvin sign U+212A), so for the rare non-ASCII UA those
		// literals are not used as evidence: every clause that has one
		// counts as satisfied and the regexes decide.
		for _, c := range m.foldClauses {
			m.satisfy(sc, c)
		}
	}
}

// parse returns the browser, OS and device that uap-core's lists yield for
// line; lower must be strings.ToLower(line). The returned Client is shared
// with the memo and must not be modified.
func (m *uapMatcher) parse(line, lower string) *uaparser.Client {
	if c, ok := m.memo.Get(line); ok {
		return c
	}

	m.inflightMu.Lock()
	if call, ok := m.inflight[line]; ok {
		// Another goroutine is parsing this very UA right now: wait for it.
		m.inflightMu.Unlock()
		<-call.done
		if call.client != nil {
			return call.client
		}
		return m.match(line, lower) // the other run failed; do it ourselves
	}
	// Re-check under the lock: a run that finished between the lookup above
	// and here has already stored its result.
	if c, ok := m.memo.Get(line); ok {
		m.inflightMu.Unlock()
		return c
	}
	call := &inflightParse{done: make(chan struct{})}
	m.inflight[line] = call
	m.inflightMu.Unlock()

	defer func() {
		m.inflightMu.Lock()
		delete(m.inflight, line)
		m.inflightMu.Unlock()
		close(call.done)
	}()

	c := m.match(line, lower)
	m.memo.Add(line, c) // before the in-flight entry is removed
	call.client = c
	return c
}

// match runs the regex stage for one UA, bypassing memo and coalescing.
func (m *uapMatcher) match(line, lower string) *uaparser.Client {
	m.regexRuns.Add(1)

	sc := m.scratch.Get().(*matchScratch)
	m.prefilter(sc, line, lower)

	c := &uaparser.Client{
		UserAgent: new(uaparser.UserAgent),
		Os:        new(uaparser.Os),
		Device:    new(uaparser.Device),
	}

	// The three loops mirror uaparser.Parser.ParseUserAgent / ParseOs /
	// ParseDevice exactly: patterns in list order, each pattern's own Match
	// fills the struct, the first one that yields a non-empty family wins,
	// "Other" when none does.
	for i, pat := range m.uap.UA {
		if sc.satisfied[i] != m.need[i] {
			continue
		}
		if re := m.pruned(sc, i); re != nil {
			fillUserAgent(c.UserAgent, re, line, pat.FamilyReplacement, pat.V1Replacement, pat.V2Replacement, pat.V3Replacement)
		} else {
			pat.Match(line, c.UserAgent)
		}
		if c.UserAgent.Family != "" {
			break
		}
	}
	if c.UserAgent.Family == "" {
		c.UserAgent.Family = "Other"
	}

	for i, pat := range m.uap.OS {
		if sc.satisfied[m.osBase+i] != m.need[m.osBase+i] {
			continue
		}
		if re := m.pruned(sc, m.osBase+i); re != nil {
			fillOS(c.Os, re, line, pat.OSReplacement, pat.V1Replacement, pat.V2Replacement, pat.V3Replacement, pat.V4Replacement)
		} else {
			pat.Match(line, c.Os)
		}
		if c.Os.Family != "" {
			break
		}
	}
	if c.Os.Family == "" {
		c.Os.Family = "Other"
	}

	for i, pat := range m.uap.Device {
		if sc.satisfied[m.deviceBase+i] != m.need[m.deviceBase+i] {
			continue
		}
		if re := m.pruned(sc, m.deviceBase+i); re != nil {
			fillDevice(c.Device, re, line, pat.DeviceReplacement, pat.BrandReplacement, pat.ModelReplacement)
		} else {
			pat.Match(line, c.Device)
		}
		if c.Device.Family != "" {
			break
		}
	}
	if c.Device.Family == "" {
		c.Device.Family = "Other"
	}

	m.scratch.Put(sc)
	return c
}

// pruned returns the per-UA pruned form of pattern p, or nil when the
// original regex has to be used (the normal case).
func (m *uapMatcher) pruned(sc *matchScratch, p int) *regexp.Regexp {
	if pr := m.prunable[p]; pr != nil {
		return pr.variant(m, sc)
	}
	return nil
}

// fillUserAgent, fillOS and fillDevice do for a pruned regex what the Match
// methods of the uap-go patterns do for the original one: run the regex and
// expand the replacement templates of the pattern from the submatches. They
// must stay line-for-line equivalent to those methods (user_agent.go, os.go,
// device.go in uap-go); TestUAPMatcherEqualsStockParser guards that.
func fillUserAgent(ua *uaparser.UserAgent, re *regexp.Regexp, line, family, v1, v2, v3 string) {
	matches := re.FindStringSubmatchIndex(line)
	if len(matches) > 0 {
		ua.Family = string(re.ExpandString(nil, family, line, matches))
		ua.Major = string(re.ExpandString(nil, v1, line, matches))
		ua.Minor = string(re.ExpandString(nil, v2, line, matches))
		ua.Patch = string(re.ExpandString(nil, v3, line, matches))
	}
}

func fillOS(os *uaparser.Os, re *regexp.Regexp, line, family, v1, v2, v3, v4 string) {
	matches := re.FindStringSubmatchIndex(line)
	if len(matches) > 0 {
		os.Family = string(re.ExpandString(nil, family, line, matches))
		os.Major = string(re.ExpandString(nil, v1, line, matches))
		os.Minor = string(re.ExpandString(nil, v2, line, matches))
		os.Patch = string(re.ExpandString(nil, v3, line, matches))
		os.PatchMinor = string(re.ExpandString(nil, v4, line, matches))
	}
}

func fillDevice(dvc *uaparser.Device, re *regexp.Regexp, line, family, brand, model string) {
	matches := re.FindStringSubmatchIndex(line)
	if len(matches) == 0 {
		return
	}
	dvc.Family = strings.TrimSpace(string(re.ExpandString(nil, family, line, matches)))
	dvc.Brand = strings.TrimSpace(string(re.ExpandString(nil, brand, line, matches)))
	dvc.Model = strings.TrimSpace(string(re.ExpandString(nil, model, line, matches)))
}

// markPresent finds every literal of idx that occurs in s and marks the
// clauses it satisfies.
func (m *uapMatcher) markPresent(sc *matchScratch, idx *literalIndex, s string) {
	for i := 0; i+1 < len(s); i++ {
		b0, b1 := s[i], s[i+1]
		if b0|b1 >= 0x80 {
			continue // literals are pure ASCII
		}
		bucket := bucketOf(b0, b1)
		lo, hi := idx.start[bucket], idx.start[bucket+1]
		if lo == hi {
			continue
		}
		tail := s[i+2:]
		for _, e := range idx.entries[lo:hi] {
			if strings.HasPrefix(tail, e.rest) {
				for _, c := range m.litClauses[e.id] {
					m.satisfy(sc, c)
				}
			}
		}
	}
}

// satisfy records that clause c holds for the current UA.
func (m *uapMatcher) satisfy(sc *matchScratch, c uint32) {
	if !sc.clauseHit[c] {
		sc.clauseHit[c] = true
		sc.satisfied[m.clausePattern[c]]++
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// minRequiredLiteralLen is the shortest literal worth checking: one-byte
// literals ("/", " ", "(") are present in nearly every UA and filter nothing.
const minRequiredLiteralLen = 2

// Bounds that keep the analysis small; exceeding one only makes the result
// less selective, never wrong.
const (
	maxExactSet         = 512 // strings in one exact set
	maxExactProduct     = 64  // strings produced by concatenating two exact sets
	maxClassLiterals    = 4   // characters in a class that is expanded ("[Bb]")
	maxClauseDistribute = 16  // clauses produced by distributing an alternation
)

// requiredClauses derives, from a regex source, a set of clauses that every
// match must satisfy (see patternFilter). It returns nil when the regex cannot
// be parsed or has no usable required literal; nil is always safe.
func requiredClauses(expr string) [][]requiredLiteral {
	re, err := syntax.Parse(expr, syntax.Perl) // same flags as regexp.Compile
	if err != nil {
		return nil
	}
	return usableClauses(analyze(re))
}

// usableClauses turns analysis output into the clauses worth checking.
func usableClauses(li literalInfo) [][]requiredLiteral {
	var out [][]requiredLiteral
	for _, clause := range li.asClauses() {
		clause = dropImpliedLiterals(clause)
		if shortestLiteral(clause) >= minRequiredLiteralLen {
			out = append(out, clause)
		}
	}
	// Most selective first (longest literals, fewest alternatives), so a
	// regex that cannot match is usually rejected by its first clause.
	sort.SliceStable(out, func(i, j int) bool { return moreSelective(out[i], out[j]) })
	return out
}

// literalInfo is what is known about the text a regex node consumes.
//
// When exact is set, the node matches only strings from the finite set
// "set" (e.g. `bots?/` matches exactly "bot/" or "bots/"). Otherwise
// "clauses" holds requirements every match fulfils (possibly none).
type literalInfo struct {
	exact   bool
	set     []requiredLiteral
	clauses [][]requiredLiteral
}

var emptyExact = literalInfo{exact: true, set: []requiredLiteral{{}}}

// asClauses turns the info into requirements: an exact set is one clause
// ("one of these strings is in the input"), unless it contains the empty
// string, in which case it requires nothing.
func (li literalInfo) asClauses() [][]requiredLiteral {
	if !li.exact {
		return li.clauses
	}
	if len(li.set) == 0 {
		return nil
	}
	for _, lit := range li.set {
		if lit.text == "" {
			return nil
		}
	}
	return [][]requiredLiteral{li.set}
}

// analyze walks the regex syntax tree. Soundness rests on one property per
// node kind; only text a node MUST consume is ever reported:
//
//   - literal, small character class: matches exactly those strings;
//   - empty-width assertions (^, $, \b): match exactly the empty string;
//   - x?: exactly what x matches, or the empty string;
//   - concatenation: adjacent exact sets concatenate into longer exact
//     strings; everything else contributes its requirements (AND);
//   - alternation: one branch matches, so the union of exact sets, or one
//     requirement per branch OR-ed together (nothing if a branch has none);
//   - capture: same as its content; x+ and x{n,} with n >= 1: x is matched at
//     least once, so the requirements of x hold (but the match is not exact);
//   - anything else (x*, large classes, "."): nothing is known.
func analyze(re *syntax.Regexp) literalInfo {
	switch re.Op {
	case syntax.OpLiteral:
		if lit, ok := literalOf(re); ok {
			return literalInfo{exact: true, set: []requiredLiteral{lit}}
		}

	case syntax.OpCharClass:
		if set, ok := classLiterals(re); ok {
			return literalInfo{exact: true, set: set}
		}

	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return emptyExact

	case syntax.OpCapture:
		return analyze(re.Sub[0])

	case syntax.OpQuest:
		if sub := analyze(re.Sub[0]); sub.exact && len(sub.set) < maxExactSet {
			set := append(append([]requiredLiteral{}, sub.set...), requiredLiteral{})
			return literalInfo{exact: true, set: uniqueLiterals(set)}
		}

	case syntax.OpPlus:
		return literalInfo{clauses: analyze(re.Sub[0]).asClauses()}

	case syntax.OpRepeat:
		if re.Min >= 1 {
			return literalInfo{clauses: analyze(re.Sub[0]).asClauses()}
		}

	case syntax.OpConcat:
		acc := emptyExact // exact run being accumulated
		var clauses [][]requiredLiteral
		flushed := false
		for _, sub := range re.Sub {
			si := analyze(sub)
			if si.exact && len(acc.set)*len(si.set) <= maxExactProduct {
				acc = literalInfo{exact: true, set: crossLiterals(acc.set, si.set)}
				continue
			}
			clauses = append(clauses, acc.asClauses()...)
			flushed = true
			if si.exact {
				acc = si
			} else {
				clauses = append(clauses, si.clauses...)
				acc = emptyExact
			}
		}
		if !flushed {
			return acc
		}
		return literalInfo{clauses: append(clauses, acc.asClauses()...)}

	case syntax.OpAlternate:
		subs := make([]literalInfo, len(re.Sub))
		allExact, total := true, 0
		for i, sub := range re.Sub {
			subs[i] = analyze(sub)
			allExact = allExact && subs[i].exact
			total += len(subs[i].set)
		}
		if allExact && total <= maxExactSet {
			var set []requiredLiteral
			for _, si := range subs {
				set = append(set, si.set...)
			}
			return literalInfo{exact: true, set: uniqueLiterals(set)}
		}
		return literalInfo{clauses: alternateClauses(subs)}
	}
	return literalInfo{}
}

// alternateClauses combines the requirements of alternation branches.
// (A1 AND A2) OR (B1) is equivalent to (A1 OR B1) AND (A2 OR B1); that
// distribution is done in full while it stays small, otherwise only the most
// selective clause of each branch is kept, which is weaker but still implied.
func alternateClauses(branches []literalInfo) [][]requiredLiteral {
	perBranch := make([][][]requiredLiteral, len(branches))
	product := 1
	for i, b := range branches {
		perBranch[i] = b.asClauses()
		if len(perBranch[i]) == 0 {
			return nil // this branch can match without any known literal
		}
		if product <= maxClauseDistribute {
			product *= len(perBranch[i])
		}
	}
	if product > maxClauseDistribute {
		var union []requiredLiteral
		for _, cl := range perBranch {
			union = append(union, bestClause(cl)...)
		}
		return [][]requiredLiteral{uniqueLiterals(union)}
	}
	out := [][]requiredLiteral{nil}
	for _, cl := range perBranch {
		next := make([][]requiredLiteral, 0, len(out)*len(cl))
		for _, partial := range out {
			for _, c := range cl {
				merged := append(append([]requiredLiteral{}, partial...), c...)
				next = append(next, uniqueLiterals(merged))
			}
		}
		out = next
	}
	return out
}

// literalOf converts a literal node into a requiredLiteral. Only pure-ASCII
// literals are used: for those a byte-wise substring test is exactly what the
// regex engine requires, on any input (valid UTF-8 or not).
func literalOf(re *syntax.Regexp) (requiredLiteral, bool) {
	if len(re.Rune) == 0 {
		return requiredLiteral{}, false
	}
	buf := make([]byte, len(re.Rune))
	for i, r := range re.Rune {
		if r <= 0 || r >= 0x80 {
			return requiredLiteral{}, false
		}
		buf[i] = byte(r)
	}
	lit := requiredLiteral{text: string(buf), fold: re.Flags&syntax.FoldCase != 0}
	if lit.fold {
		lit.text = lowerASCII(lit.text)
	}
	return lit, true
}

// classLiterals expands a small pure-ASCII character class such as "[Bb]"
// into its members. Case-insensitive classes are already expanded by the
// regex parser (both cases are listed), so members are case-sensitive.
func classLiterals(re *syntax.Regexp) ([]requiredLiteral, bool) {
	var set []requiredLiteral
	for i := 0; i+1 < len(re.Rune); i += 2 {
		lo, hi := re.Rune[i], re.Rune[i+1]
		if lo <= 0 || hi >= 0x80 || int(hi-lo)+1+len(set) > maxClassLiterals {
			return nil, false
		}
		for r := lo; r <= hi; r++ {
			set = append(set, requiredLiteral{text: string([]byte{byte(r)})})
		}
	}
	return set, len(set) > 0
}

// crossLiterals concatenates every string of a with every string of b. A
// result that involves a case-insensitive part is case-insensitive as a
// whole (lowercased), which is weaker than the regex but still implied by it.
func crossLiterals(a, b []requiredLiteral) []requiredLiteral {
	out := make([]requiredLiteral, 0, len(a)*len(b))
	for _, x := range a {
		for _, y := range b {
			switch {
			case x.text == "":
				out = append(out, y)
			case y.text == "":
				out = append(out, x)
			case x.fold || y.fold:
				out = append(out, requiredLiteral{text: lowerASCII(x.text + y.text), fold: true})
			default:
				out = append(out, requiredLiteral{text: x.text + y.text})
			}
		}
	}
	return uniqueLiterals(out)
}

func uniqueLiterals(lits []requiredLiteral) []requiredLiteral {
	seen := make(map[requiredLiteral]struct{}, len(lits))
	out := lits[:0:0]
	for _, lit := range lits {
		if _, dup := seen[lit]; !dup {
			seen[lit] = struct{}{}
			out = append(out, lit)
		}
	}
	return out
}

// dropImpliedLiterals removes from an OR-clause every literal that contains
// another literal of the same clause: if "bingbot" is in the UA then so is
// "bot", so checking "bot" alone decides the clause. The clause must already
// be free of duplicates.
func dropImpliedLiterals(clause []requiredLiteral) []requiredLiteral {
	out := clause[:0:0]
	for i, lit := range clause {
		implied := false
		for j, other := range clause {
			if i == j || other.text == "" {
				continue
			}
			switch {
			case other.fold == lit.fold:
				// Equal texts cannot occur here (no duplicates), so this is
				// a strict containment and cannot be mutual.
				implied = strings.Contains(lit.text, other.text)
			case other.fold:
				// A case-insensitive literal covers every casing of the
				// same text; a case-sensitive one never covers a
				// case-insensitive one.
				implied = strings.Contains(lowerASCII(lit.text), other.text)
			}
			if implied {
				break
			}
		}
		if !implied {
			out = append(out, lit)
		}
	}
	return out
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// bestClause picks the most selective clause of a non-empty list.
func bestClause(clauses [][]requiredLiteral) []requiredLiteral {
	best := clauses[0]
	for _, c := range clauses[1:] {
		if moreSelective(c, best) {
			best = c
		}
	}
	return best
}

// moreSelective orders clauses by how likely they are to reject a UA: a
// longer shortest-literal first, then fewer alternatives.
func moreSelective(a, b []requiredLiteral) bool {
	la, lb := shortestLiteral(a), shortestLiteral(b)
	if la != lb {
		return la > lb
	}
	return len(a) < len(b)
}

func shortestLiteral(clause []requiredLiteral) int {
	n := 0
	for i, lit := range clause {
		if i == 0 || len(lit.text) < n {
			n = len(lit.text)
		}
	}
	return n
}
