// graph_doc_score_ranker.go decorates a base Ranker with graph-derived evidence
// (HITS authority/hub, community membership). The decorator pattern allows stacking
// multiple graph-aware rankers (e.g., GraphAnchorScoreRanker + GraphDocScoreRanker).
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Graph (Hub)](docs/hubs/Graph (Hub).md)
package relevance

import (
	"context"
	"errors"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type graphDocScoreStore interface {
	GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semdb.GraphDocScore, error)
	GraphDocEdgesWithConfidenceForPaths(ctx context.Context, paths []string) ([]semdb.GraphDocEdge, error)
}

// GraphDocScoreRanker decorates candidates with graph-derived evidence (HITS + communities)
// before delegating to the underlying ranker.
type GraphDocScoreRanker struct {
	Base  search.Ranker
	Store graphDocScoreStore
}

func (r *GraphDocScoreRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, errors.New("missing base ranker")
	}
	if r.Store == nil || len(candidates) == 0 {
		return r.Base.Rank(ctx, spec, candidates)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	paths, seedPaths := collectCandidateAndSeedPaths(spec, candidates)
	if len(paths) == 0 {
		return r.Base.Rank(ctx, spec, candidates)
	}

	scores, err := r.Store.GraphDocScoresByPaths(ctx, paths)
	if err != nil {
		return nil, err
	}

	seedCommunities := make(map[string]struct{})
	for _, sp := range seedPaths {
		if sc, ok := scores[sp]; ok && strings.TrimSpace(sc.Community) != "" {
			seedCommunities[sc.Community] = struct{}{}
		}
	}

	// Build seed path set for edge-confidence lookup.
	seedSet := make(map[string]struct{}, len(seedPaths))
	for _, sp := range seedPaths {
		seedSet[sp] = struct{}{}
	}

	// Fetch confidence for edges incident to all paths (candidates + seeds).
	// avgEdgeConfidence[candidatePath] = average confidence_score of edges connecting it to seeds.
	avgEdgeConfidence := make(map[string]float64)
	if len(seedPaths) > 0 {
		edgeRows, edgeErr := r.Store.GraphDocEdgesWithConfidenceForPaths(ctx, paths)
		if edgeErr == nil && len(edgeRows) > 0 {
			// For each candidate, collect confidence scores of edges that touch a seed.
			confScores := make(map[string][]float64)
			for _, e := range edgeRows {
				candidatePath := ""
				srcPath := normalizeGraphDocScorePath(e.SrcPath)
				dstPath := normalizeGraphDocScorePath(e.DstPath)
				if _, isSeed := seedSet[srcPath]; isSeed {
					if _, alsoSeed := seedSet[dstPath]; !alsoSeed {
						candidatePath = dstPath
					}
				} else if _, isSeed := seedSet[dstPath]; isSeed {
					candidatePath = srcPath
				}
				if candidatePath == "" {
					continue
				}
				score := e.ConfidenceScore
				if score == 0 {
					score = 1.0
				}
				confScores[candidatePath] = append(confScores[candidatePath], score)
			}
			for path, scores := range confScores {
				if len(scores) == 0 {
					continue
				}
				sum := 0.0
				for _, s := range scores {
					sum += s
				}
				avgEdgeConfidence[path] = sum / float64(len(scores))
			}
		}
	}

	augmented := make([]search.Candidate, 0, len(candidates))
	for _, c := range candidates {
		path := normalizeGraphDocScorePath(c.Path)
		kind := graphDocCandidateKind(c)
		if path == "" {
			augmented = append(augmented, c)
			continue
		}
		sc, ok := scores[path]
		if ok && (kind == "" || sc.DocType == kind) {
			if sc.Authority > 0 {
				c.Evidence = append(c.Evidence, search.Evidence{
					Type:     "graph_hits_authority",
					RawScore: clamp01(sc.Authority),
					Source:   "graph_doc_scores",
					Details: map[string]string{
						"doc_type": sc.DocType,
					},
				})
			}
			if sc.Hub > 0 {
				c.Evidence = append(c.Evidence, search.Evidence{
					Type:     "graph_hits_hub",
					RawScore: clamp01(sc.Hub),
					Source:   "graph_doc_scores",
					Details: map[string]string{
						"doc_type": sc.DocType,
					},
				})
			}
			if sc.Community != "" && len(seedCommunities) > 0 {
				if _, inSeed := seedCommunities[sc.Community]; inSeed {
					// same_community: 0.7 is a tuned boost for candidates in the same graph community
					// as a seed. Lower than authority (1.0 max) to avoid overwhelming direct matches.
					c.Evidence = append(c.Evidence, search.Evidence{
						Type:     "same_community",
						RawScore: 0.7,
						Source:   "graph_doc_scores",
						Details: map[string]string{
							"community": sc.Community,
						},
					})
				}
			}
		}
		// graph_edge_confidence: additive signal based on average confidence_score of
		// edges connecting this candidate to the query seeds.
		if avg, hasConf := avgEdgeConfidence[path]; hasConf {
			c.Evidence = append(c.Evidence, search.Evidence{
				Type:     "graph_edge_confidence",
				RawScore: clamp01(avg),
				Source:   "graph_doc_edges",
			})
		}
		augmented = append(augmented, c)
	}

	return r.Base.Rank(ctx, spec, augmented)
}

func (r *GraphDocScoreRanker) ApproxChannelWeights(spec search.QuerySpec) map[search.EvidenceChannel]float64 {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxChannelWeights(spec)
	}
	return search.ApproxChannelWeightsForIntent(spec.Intent)
}

func (r *GraphDocScoreRanker) ApproxMaxPerOwner(spec search.QuerySpec) int {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxMaxPerOwner(spec)
	}
	return 1
}

func collectCandidateAndSeedPaths(spec search.QuerySpec, candidates []search.Candidate) (paths []string, seedPaths []string) {
	seen := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		p := normalizeGraphDocScorePath(c.Path)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	for _, s := range spec.Seeds {
		p := normalizeGraphDocScoreSeedPath(s)
		if p == "" {
			continue
		}
		seedPaths = append(seedPaths, p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	return paths, seedPaths
}

func normalizeGraphDocScoreSeedPath(seed knowledge.Handle) string {
	switch seed.Kind {
	case knowledge.KindNote, knowledge.KindNoteChunk:
		return normalizeGraphDocScorePath(seed.ID)
	case knowledge.KindNodeChunk:
		owner := seed.Owner()
		if owner.Kind == knowledge.KindNote && strings.TrimSpace(owner.ID) != "" {
			return normalizeGraphDocScorePath(owner.ID)
		}
		if len(seed.Fragments) > 0 && strings.TrimSpace(seed.Fragments[0]) != "" {
			return normalizeGraphDocScorePath(seed.Fragments[0])
		}
		return ""
	case knowledge.KindFile:
		return normalizeGraphDocScorePath(seed.ID)
	default:
		return ""
	}
}

func normalizeGraphDocScorePath(path string) string {
	return search.NormalizeLocalityPath(strings.ReplaceAll(path, `\`, "/"))
}

func graphDocCandidateKind(c search.Candidate) string {
	switch c.Handle.Kind {
	case knowledge.KindNote, knowledge.KindNoteChunk, knowledge.KindNodeChunk:
		return "note"
	case knowledge.KindFile, knowledge.KindAnchor, knowledge.KindCodeChunk:
		return "code"
	}
	switch c.Type {
	case "note":
		return "note"
	case "code", "file", "anchor":
		return "code"
	default:
		return ""
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
