package finder

import (
	"math"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

const (
	// A title match scores a quarter above the same match in a path.
	titleFavorDivisor = 4
	// Notes rank ahead of code files on otherwise close scores.
	noteBonus = 8
	// Frecency adds at most this much, less than one matched character (16),
	// so it only reorders close scores.
	frecencyMargin = 12
	// Typo variants run only when the direct query matched fewer items.
	typoThreshold = 10
	typoMaxRunes  = 24
	DefaultLimit  = 50
	MaxLimit      = 200
	recentNotes   = 100
)

const (
	tierFuzzy = iota
	tierPrefix
	tierExact
)

var initAlgo = sync.OnceFunc(func() { algo.Init("default") })

var slabs = sync.Pool{New: func() any { return util.MakeSlab(100*1024, 2048) }}

// Open is one item's frecency record.
type Open struct {
	Count      int
	LastOpened time.Time
}

// Query is a finder request. An empty Text lists recent items.
type Query struct {
	Text  string
	Limit int
	Kind  Kind
	Opens map[string]Open
	Now   time.Time
}

// Result is a ranked item with highlight ranges in UTF-16 code units, the
// unit a browser string uses.
type Result struct {
	Item
	Score        int
	TitleMatches [][2]int
	PathMatches  [][2]int
}

type hit struct {
	idx, score, tier, variant int
	// rank adds the note bonus and frecency to score.
	rank int
}

// Search ranks the catalog for q. It is safe for concurrent use.
func (c *Catalog) Search(q Query) []Result {
	if c == nil {
		return nil
	}
	initAlgo()
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return c.recent(q, limit)
	}
	caseSensitive := hasUpper(text)
	if !caseSensitive {
		text = strings.ToLower(text)
	}
	pattern := algo.NormalizeRunes([]rune(text))
	hits := c.scan([][]rune{pattern}, caseSensitive, q.Kind, true)
	for i := range hits {
		hits[i].tier = c.tier(hits[i].idx, text, caseSensitive)
	}
	variants := [][]rune{pattern}
	if len(hits) < typoThreshold && len(pattern) >= 3 && len(pattern) <= typoMaxRunes {
		typo := typoVariants(pattern, caseSensitive)
		direct := make(map[int]bool, len(hits))
		for _, h := range hits {
			direct[h.idx] = true
		}
		for _, h := range c.scan(typo, caseSensitive, q.Kind, false) {
			if direct[h.idx] {
				continue
			}
			h.score = h.score * 2 / 3
			h.variant += len(variants)
			hits = append(hits, h)
		}
		variants = append(variants, typo...)
	}
	for i := range hits {
		e := &c.entries[hits[i].idx]
		hits[i].rank = hits[i].score + frecencyBoost(q.Opens[e.Path], q.Now)
		if e.Kind == KindNote {
			hits[i].rank += noteBonus
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.tier != b.tier {
			return b.tier - a.tier
		}
		if a.rank != b.rank {
			return b.rank - a.rank
		}
		ea, eb := &c.entries[a.idx], &c.entries[b.idx]
		if len(ea.Title) != len(eb.Title) {
			return len(ea.Title) - len(eb.Title)
		}
		return strings.Compare(ea.Path, eb.Path)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	slab := slabs.Get().(*util.Slab)
	defer slabs.Put(slab)
	out := make([]Result, len(hits))
	for i, h := range hits {
		e := &c.entries[h.idx]
		p := variants[h.variant]
		out[i] = Result{Item: e.Item, Score: h.score}
		if res, pos := algo.FuzzyMatchV2(caseSensitive, true, true, &e.title, p, true, slab); res.Score > 0 {
			out[i].TitleMatches = utf16Ranges(e.Title, *pos)
		}
		if res, pos := algo.FuzzyMatchV2(caseSensitive, true, true, &e.path, p, true, slab); res.Score > 0 {
			out[i].PathMatches = utf16Ranges(e.Path, *pos)
		}
	}
	return out
}

// scan matches every pattern against every entry in parallel and keeps each
// entry's best score. variant indexes patterns. Typo variants skip paths:
// titles and file names carry the intent, and long paths would multiply the
// cost of every variant.
func (c *Catalog) scan(patterns [][]rune, caseSensitive bool, kind Kind, withPath bool) []hit {
	workers := min(runtime.GOMAXPROCS(0), max(1, len(c.entries)/2048))
	chunk := (len(c.entries) + workers - 1) / workers
	parts := make([][]hit, workers)
	var wg sync.WaitGroup
	for w := range workers {
		lo, hi := w*chunk, min((w+1)*chunk, len(c.entries))
		wg.Add(1)
		go func() {
			defer wg.Done()
			slab := slabs.Get().(*util.Slab)
			defer slabs.Put(slab)
			for i := lo; i < hi; i++ {
				e := &c.entries[i]
				if kind != "" && e.Kind != kind {
					continue
				}
				best, variant := 0, 0
				for v, p := range patterns {
					if s := matchScore(e, p, caseSensitive, withPath, slab); s > best {
						best, variant = s, v
					}
				}
				if best > 0 {
					parts[w] = append(parts[w], hit{idx: i, score: best, variant: variant})
				}
			}
		}()
	}
	wg.Wait()
	return slices.Concat(parts...)
}

func matchScore(e *entry, p []rune, caseSensitive, withPath bool, slab *util.Slab) int {
	score := 0
	if withPath {
		path, _ := algo.FuzzyMatchV2(caseSensitive, true, true, &e.path, p, false, slab)
		score = path.Score
	}
	if title, _ := algo.FuzzyMatchV2(caseSensitive, true, true, &e.title, p, false, slab); title.Score > 0 {
		score = max(score, title.Score+title.Score/titleFavorDivisor)
	}
	return score
}

func (c *Catalog) tier(idx int, text string, caseSensitive bool) int {
	e := &c.entries[idx]
	title := e.lowerTitle
	if caseSensitive {
		title = e.Title
	}
	switch {
	case title == text:
		return tierExact
	case strings.HasPrefix(title, text):
		return tierPrefix
	}
	return tierFuzzy
}

// typoVariants returns the query with one adjacent pair swapped or, for
// queries of four or more runes, one rune dropped. A query of n runes yields
// at most 2n-1 variants. A case-sensitive query keeps its uppercase runes.
func typoVariants(p []rune, caseSensitive bool) [][]rune {
	seen := map[string]bool{string(p): true}
	var out [][]rune
	add := func(v []rune) {
		if key := string(v); !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	for i := 0; i+1 < len(p); i++ {
		v := slices.Clone(p)
		v[i], v[i+1] = v[i+1], v[i]
		add(v)
	}
	if len(p) >= 4 {
		for i, r := range p {
			if !caseSensitive || !unicode.IsUpper(r) {
				add(slices.Concat(p[:i:i], p[i+1:]))
			}
		}
	}
	return out
}

// frecencyValue grows with the log of the open count and halves every two
// weeks since the last open.
func frecencyValue(o Open, now time.Time) float64 {
	if o.Count <= 0 {
		return 0
	}
	ageDays := max(0, now.Sub(o.LastOpened).Hours()/24)
	return math.Log2(1+float64(o.Count)) * math.Exp2(-ageDays/14)
}

// frecencyBoost caps frecency at frecencyMargin; fifteen fresh opens reach it.
func frecencyBoost(o Open, now time.Time) int {
	return int(math.Round(min(1, frecencyValue(o, now)/4) * frecencyMargin))
}

// recent lists frecency-ranked opens, then recently changed notes.
func (c *Catalog) recent(q Query, limit int) []Result {
	type opened struct {
		idx   int
		value float64
		last  time.Time
	}
	var list []opened
	for p, o := range q.Opens {
		if idx, ok := c.byPath[p]; ok && (q.Kind == "" || c.entries[idx].Kind == q.Kind) {
			list = append(list, opened{idx, frecencyValue(o, q.Now), o.LastOpened})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		if !list[i].last.Equal(list[j].last) {
			return list[i].last.After(list[j].last)
		}
		return c.entries[list[i].idx].Path < c.entries[list[j].idx].Path
	})
	out := make([]Result, 0, limit)
	seen := map[int]bool{}
	for _, o := range list {
		if len(out) == limit {
			return out
		}
		seen[o.idx] = true
		out = append(out, Result{Item: c.entries[o.idx].Item})
	}
	if q.Kind == KindCode {
		return out
	}
	for _, idx := range c.recentIdx {
		if len(out) == limit {
			break
		}
		if !seen[idx] {
			out = append(out, Result{Item: c.entries[idx].Item})
		}
	}
	return out
}

// utf16Ranges turns fzf's rune positions into sorted, merged [start, end)
// ranges measured in UTF-16 code units.
func utf16Ranges(s string, positions []int) [][2]int {
	if len(positions) == 0 {
		return nil
	}
	want := make(map[int]bool, len(positions))
	for _, p := range positions {
		want[p] = true
	}
	var out [][2]int
	offset := 0
	for i, r := range []rune(s) {
		width := utf16.RuneLen(r)
		if width < 1 {
			width = 1
		}
		if want[i] {
			if n := len(out); n > 0 && out[n-1][1] == offset {
				out[n-1][1] += width
			} else {
				out = append(out, [2]int{offset, offset + width})
			}
		}
		offset += width
	}
	return out
}
