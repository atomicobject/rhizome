package unifiedsearch

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
)

func CoalesceNoteResults(results []search.RankedResult) []DisplayItem {
	type group struct {
		itemIdx int
		best    search.RankedResult
		merged  search.Candidate
		chunks  []search.RankedResult
	}

	var out []DisplayItem
	byOwner := map[string]*group{}

	emitNonNote := func(r search.RankedResult) {
		out = append(out, DisplayItem{Primary: r})
	}

	for _, r := range results {
		if r.Type != "note" || r.Owner.String() == "" {
			emitNonNote(r)
			continue
		}
		key := r.Owner.String()
		g := byOwner[key]
		if g == nil {
			out = append(out, DisplayItem{Primary: r})
			g = &group{itemIdx: len(out) - 1, best: r, merged: r.Candidate}
			byOwner[key] = g
		} else {
			if r.FinalScore > g.best.FinalScore {
				g.best = r
			}
			g.merged = search.MergeCandidate(g.merged, r.Candidate)
		}
		g.chunks = append(g.chunks, r)
	}

	for _, g := range byOwner {
		sort.SliceStable(g.chunks, func(i, j int) bool {
			if g.chunks[i].FinalScore != g.chunks[j].FinalScore {
				return g.chunks[i].FinalScore > g.chunks[j].FinalScore
			}
			return g.chunks[i].Handle.String() < g.chunks[j].Handle.String()
		})
		best := g.best
		best.Candidate = g.merged
		best.FinalScore = g.best.FinalScore

		var withContext []search.RankedResult
		for _, r := range g.chunks {
			if strings.TrimSpace(r.Breadcrumb) == "" && strings.TrimSpace(r.Heading) == "" {
				continue
			}
			withContext = append(withContext, r)
		}
		if len(withContext) > 3 {
			withContext = withContext[:3]
		}

		out[g.itemIdx].Primary = best
		out[g.itemIdx].NoteChunks = withContext
	}

	return out
}
