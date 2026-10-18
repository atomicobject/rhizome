package semantic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// SurveyNodesByType runs independent note embeddings for each topic, unions the
// type-filtered ontology-node chunks, and ranks one result per ontology node.
//
// WHY: this is intentionally separate from Searcher.Search because broad
// semantic-query wants chunk diversity, while typed roots need the
// [[ontology-graphql-query-contract#^SPEC-0042-US5]] node-level survey contract.
func (s *Searcher) SurveyNodesByType(ctx context.Context, req SurveyNodesByTypeRequest) ([]Result, error) {
	terms := normalizeSurveyStrings(req.QueryTerms)
	if len(terms) == 0 {
		return nil, errors.New("at least one query term is required")
	}
	typeNames := normalizeSurveyStrings(req.TypeNames)
	if len(typeNames) == 0 {
		return nil, errors.New("at least one ontology type name is required")
	}
	limit := req.K
	if limit <= 0 {
		limit = 100
	}
	if !s.HasIntelStore() {
		return nil, errors.New("semantic search requires intel store")
	}
	if s.NoteProvider == nil {
		return nil, errors.New("no note embedding provider available")
	}

	baseFilters := semdb.EmbeddingSearchFilters{
		PathPrefixes:      req.PathPrefixes,
		Granularity:       req.Granularity,
		OwnerTypes:        []string{"ontology_node"},
		OntologyTypeNames: typeNames,
	}
	candidateLimit := limit * 8
	if candidateLimit < limit+200 {
		candidateLimit = limit + 200
	}
	if candidateLimit < 200 {
		candidateLimit = 200
	}

	byNode := make(map[string]semdb.ScoredChunk)
	for _, term := range terms {
		vec, err := embedOnce(ctx, s.NoteProvider, term)
		if err != nil {
			return nil, fmt.Errorf("embed query term: %w", err)
		}
		found, _, err := s.IntelStore.SearchEmbeddings(ctx, vec, candidateLimit, baseFilters)
		if err != nil {
			return nil, fmt.Errorf("search intel embeddings: %w", err)
		}
		for _, sc := range found {
			if strings.TrimSpace(sc.OwnerID) == "" {
				continue
			}
			current, ok := byNode[sc.OwnerID]
			if !ok || betterSurveyChunk(sc, current) {
				byNode[sc.OwnerID] = sc
			}
		}
	}
	if len(byNode) == 0 {
		return nil, nil
	}

	scored := make([]semdb.ScoredChunk, 0, len(byNode))
	for _, sc := range byNode {
		scored = append(scored, sc)
	}
	sortSurveyChunks(scored)
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return s.resultsFromScoredChunks(ctx, scored)
}

func normalizeSurveyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func betterSurveyChunk(candidate, current semdb.ScoredChunk) bool {
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	if candidate.Path != current.Path {
		return candidate.Path < current.Path
	}
	if candidate.OwnerID != current.OwnerID {
		return candidate.OwnerID < current.OwnerID
	}
	return candidate.ChunkID < current.ChunkID
}

func sortSurveyChunks(scored []semdb.ScoredChunk) {
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].Path != scored[j].Path {
			return scored[i].Path < scored[j].Path
		}
		if scored[i].OwnerID != scored[j].OwnerID {
			return scored[i].OwnerID < scored[j].OwnerID
		}
		return scored[i].ChunkID < scored[j].ChunkID
	})
}
