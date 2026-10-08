package relevance

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
)

type linkedCorroborationStore interface {
	GraphDocEdgesWithConfidenceForPaths(ctx context.Context, paths []string) ([]semdb.GraphDocEdge, error)
}

// LinkedCorroborationRanker gives a candidate that is about the query bounded
// support for each linked candidate that is also about the query (SPEC-0120
// US2). Only links among the window's own candidates count, so a source with
// no evidence of its own, or one that is merely well linked, gains nothing.
type LinkedCorroborationRanker struct {
	Base  search.Ranker
	Store linkedCorroborationStore
}

// linkedCorroborationPerNeighbor is what the first relevant linked note adds;
// each further one adds half of what remains.
const linkedCorroborationPerNeighbor = 0.5

func (r *LinkedCorroborationRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, errors.New("missing base ranker")
	}
	if r.Store == nil || !search.IsBroadIntent(spec.Intent) || strings.TrimSpace(spec.Text) == "" {
		return r.Base.Rank(ctx, spec, candidates)
	}
	about := map[string]struct{}{}
	for _, c := range candidates {
		if path := normalizeGraphDocScorePath(c.Path); path != "" && graphDocCandidateKind(c) == "note" && aboutQuery(c.Evidence) {
			about[path] = struct{}{}
		}
	}
	if len(about) < 2 {
		return r.Base.Rank(ctx, spec, candidates)
	}
	paths := make([]string, 0, len(about))
	for path := range about {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	edges, err := r.Store.GraphDocEdgesWithConfidenceForPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	neighbors := map[string]map[string]struct{}{}
	link := func(from, to string) {
		if neighbors[from] == nil {
			neighbors[from] = map[string]struct{}{}
		}
		neighbors[from][to] = struct{}{}
	}
	for _, e := range edges {
		if e.Kind != semdb.GraphDocEdgeKindWikilink && e.Kind != semdb.GraphDocEdgeKindMarkdownLink {
			continue
		}
		src, dst := normalizeGraphDocScorePath(e.SrcPath), normalizeGraphDocScorePath(e.DstPath)
		_, srcAbout := about[src]
		_, dstAbout := about[dst]
		if src == dst || !srcAbout || !dstAbout {
			continue
		}
		link(src, dst)
		link(dst, src)
	}

	out := make([]search.Candidate, len(candidates))
	for i, c := range candidates {
		out[i] = c
		path := normalizeGraphDocScorePath(c.Path)
		if _, ok := about[path]; !ok || len(neighbors[path]) == 0 || graphDocCandidateKind(c) != "note" {
			continue
		}
		linked := make([]string, 0, len(neighbors[path]))
		for neighbor := range neighbors[path] {
			linked = append(linked, neighbor)
		}
		sort.Strings(linked)
		out[i].Evidence = append(append([]search.Evidence(nil), c.Evidence...), search.Evidence{
			Type:     "linked_corroboration",
			RawScore: 1 - math.Pow(1-linkedCorroborationPerNeighbor, float64(len(linked))),
			Source:   "graph_doc_edges",
			Details: map[string]string{
				"linked_notes": strconv.Itoa(len(linked)),
				"neighbors":    strings.Join(linked[:min(3, len(linked))], ", "),
			},
		})
	}
	return r.Base.Rank(ctx, spec, out)
}

func (r *LinkedCorroborationRanker) ApproxChannelWeights(spec search.QuerySpec) map[search.EvidenceChannel]float64 {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxChannelWeights(spec)
	}
	return search.ApproxChannelWeightsForIntent(spec.Intent)
}

// aboutQuery reports whether a candidate's own content or identity matched the
// query: lexical or semantic evidence other than what linking notes say.
func aboutQuery(evidence []search.Evidence) bool {
	own := make([]search.Evidence, 0, len(evidence))
	for _, ev := range evidence {
		if ev.Type != "link_text_match" {
			own = append(own, ev)
		}
	}
	channels := search.AggregateEvidenceScoresForRanking(own)
	return channels[search.EvidenceChannelLexical] > 0 || channels[search.EvidenceChannelSemantic] > 0
}
