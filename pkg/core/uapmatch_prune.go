package core

import (
	"regexp"
	"regexp/syntax"
	"sync"
)

// Branch pruning: the second half of the uapMatcher speed-up.
//
// A handful of uap-core regexes are one enormous alternation of crawler
// names ("(bot|BUbiNG|zao|...|GPTBot)", 150-200 alternatives). The literal
// prefilter cannot skip them for a bot UA, because they really do match, and
// they are too large for Go's fast backtracking matcher (limited to 500
// program instructions), so every run costs 100-200 microseconds in the
// slower NFA simulation.
//
// For one given UA, though, almost all of those alternatives are dead: a
// branch whose required literal is not in the UA cannot match anywhere in
// it. Deleting branches that can never match does not change what a regex
// matches or captures: the engine explores alternatives in priority order
// and picks the first that succeeds, and removing candidates that always
// fail leaves both the surviving candidates and their order untouched. So
// the matcher runs a variant of the regex that keeps only the live branches
// (compiled on first use and cached per set of live branches). The variant
// is small, gets the fast matcher, and returns exactly the same submatches.
//
// Preconditions, checked when the matcher is built: the pruned branches
// contain no capture groups (so group numbering is unchanged) and a variant
// must report the same number of groups as the original, else the original
// regex is used.

const (
	// goBacktrackLimit mirrors regexp's maxBacktrackProg: programs above
	// this size never use the fast backtracker. Only such regexes are worth
	// pruning.
	goBacktrackLimit = 500
	// minPrunableBranches: smaller alternations are not worth the bookkeeping.
	minPrunableBranches = 8
	// maxPrunedVariants bounds the per-regex cache of compiled variants.
	// Once full, unseen branch sets fall back to the original regex.
	maxPrunedVariants = 256
)

// prunablePattern is a regex with one large alternation whose branches can
// be pruned per UA.
type prunablePattern struct {
	source    string // exact source of the original regex
	numSubexp int    // capture groups of the original
	path      []int  // child indexes from the syntax-tree root to the alternation
	// slots[j] is the pseudo-pattern (index into uapMatcher.need) that
	// carries branch j's required literals, or -1 if the branch has none and
	// is therefore always kept.
	slots []int32

	mu sync.RWMutex
	// variants maps a set of live branches (bitset as string) to the
	// compiled pruned regex; a nil value means "use the original".
	variants map[string]*regexp.Regexp
}

// newPrunablePattern reports whether expr qualifies for branch pruning and,
// if so, returns it together with the requirements of each branch (nil for a
// branch that has none).
func newPrunablePattern(expr string, numSubexp int) (*prunablePattern, [][][]requiredLiteral) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil, nil
	}
	// Simplify may rewrite nodes, so size is measured on a separate parse.
	if sized, err := syntax.Parse(expr, syntax.Perl); err != nil {
		return nil, nil
	} else if prog, err := syntax.Compile(sized.Simplify()); err != nil || len(prog.Inst) <= goBacktrackLimit {
		return nil, nil
	}

	path, alt := largestAlternation(re)
	if alt == nil || len(alt.Sub) < minPrunableBranches {
		return nil, nil
	}
	branchClauses := make([][][]requiredLiteral, len(alt.Sub))
	prunable := false
	for j, branch := range alt.Sub {
		if hasCapture(branch) {
			return nil, nil
		}
		branchClauses[j] = usableClauses(analyze(branch))
		prunable = prunable || len(branchClauses[j]) > 0
	}
	if !prunable {
		return nil, nil
	}
	return &prunablePattern{
		source:    expr,
		numSubexp: numSubexp,
		path:      path,
		slots:     make([]int32, len(alt.Sub)),
		variants:  map[string]*regexp.Regexp{},
	}, branchClauses
}

// largestAlternation returns the alternation node with the most branches and
// the path of child indexes leading to it.
func largestAlternation(root *syntax.Regexp) (path []int, alt *syntax.Regexp) {
	var walk func(re *syntax.Regexp, at []int)
	walk = func(re *syntax.Regexp, at []int) {
		if re.Op == syntax.OpAlternate && (alt == nil || len(re.Sub) > len(alt.Sub)) {
			alt = re
			path = append([]int(nil), at...)
		}
		for i, sub := range re.Sub {
			walk(sub, append(at, i))
		}
	}
	walk(root, nil)
	return path, alt
}

func hasCapture(re *syntax.Regexp) bool {
	if re.Op == syntax.OpCapture {
		return true
	}
	for _, sub := range re.Sub {
		if hasCapture(sub) {
			return true
		}
	}
	return false
}

// variant returns the pruned regex for the UA whose prefilter state is in
// sc, or nil when the original regex must be used.
func (p *prunablePattern) variant(m *uapMatcher, sc *matchScratch) *regexp.Regexp {
	var buf [32]byte
	key := buf[:0]
	if n := (len(p.slots) + 7) / 8; n <= len(buf) {
		key = buf[:n]
	} else {
		key = make([]byte, n)
	}
	live := 0
	for j, slot := range p.slots {
		if slot < 0 || sc.satisfied[slot] == m.need[slot] {
			key[j>>3] |= 1 << (j & 7)
			live++
		}
	}
	// Nothing to prune, or no branch looks alive. The latter cannot produce
	// a match through the alternation, but the alternation may be optional,
	// so the original regex decides.
	if live == len(p.slots) || live == 0 {
		return nil
	}

	p.mu.RLock()
	re, ok := p.variants[string(key)]
	p.mu.RUnlock()
	if ok {
		return re
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if re, ok := p.variants[string(key)]; ok {
		return re
	}
	if len(p.variants) >= maxPrunedVariants {
		return nil
	}
	re = p.compile(key)
	p.variants[string(key)] = re
	return re
}

// compile builds the regex that keeps only the branches set in live. It
// returns nil if the result cannot be trusted to be equivalent.
func (p *prunablePattern) compile(live []byte) *regexp.Regexp {
	root, err := syntax.Parse(p.source, syntax.Perl)
	if err != nil {
		return nil
	}
	alt := root
	for _, i := range p.path {
		if i >= len(alt.Sub) {
			return nil
		}
		alt = alt.Sub[i]
	}
	if alt.Op != syntax.OpAlternate || len(alt.Sub) != len(p.slots) {
		return nil
	}
	kept := make([]*syntax.Regexp, 0, len(alt.Sub))
	for j, branch := range alt.Sub {
		if live[j>>3]&(1<<(j&7)) != 0 {
			kept = append(kept, branch)
		}
	}
	alt.Sub = kept // order preserved; a one-branch alternation is valid

	re, err := regexp.Compile(root.String())
	if err != nil || re.NumSubexp() != p.numSubexp {
		return nil
	}
	return re
}
