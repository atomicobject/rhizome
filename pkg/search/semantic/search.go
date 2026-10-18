// Package semantic provides unified semantic search over note and code embeddings.
//
// The primary entry point is Searcher.Search, which:
//  1. Embeds the query text (preferring the code provider when available)
//  2. Runs vector similarity search against unified intel_embeddings
//  3. Filters and returns code + note chunks (anchors/doc sections)
//
// Chunking is handled separately by ChunkBuilder in chunker.go (for indexing).
//
// Docs: [Embeddings (Hub)](docs/hubs/Embeddings (Hub).md), [Search (Hub)](docs/hubs/Search (Hub).md)
package semantic

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"reflect"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/search"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// SearchFilters narrows semantic search results.
type SearchFilters struct {
	Types        []string // code | note
	Langs        []string
	PathPrefixes []string
	Granularity  []string
	// NoteTypes are resolved types of the owning note. This intentionally
	// differs from EmbeddingSearchFilters.OntologyTypeNames: an embedded node
	// may have a different type from its owning note, while its note chunks
	// still belong in a note-type search.
	NoteTypes    []string
	ExactSymbols []string
	TestsOnly    bool
	ExcludeTests bool
}

// SearchRequest represents a semantic search query.
type SearchRequest struct {
	QueryText  string
	SeedHandle string
	Filters    SearchFilters
	K          int
}

// SurveyNodesByTypeRequest ranks ontology-node chunks inside a typed slice.
type SurveyNodesByTypeRequest struct {
	QueryTerms   []string
	TypeNames    []string
	PathPrefixes []string
	Granularity  []string
	K            int
}

// Result represents a unified semantic search result.
type Result struct {
	search.SourceRef
	Handle string
	Type   string // code|note
	Score  float64
}

type SearchStore interface {
	SearchEmbeddings(context.Context, embeddings.Embedding, int, semdb.EmbeddingSearchFilters) ([]semdb.ScoredChunk, int, error)
	IntelAnchorsByPath(context.Context, string) ([]codeanchor.IntelAnchor, error)
	IntelAnchorsByIDs(context.Context, []string) (map[string]codeanchor.IntelAnchor, error)
	OntologyNodesByIDs(context.Context, []string) (map[string]readmodel.NodeRow, error)
	IntelDocSectionIDsByPath(context.Context, string) ([]string, error)
	IntelDocSectionsByPath(context.Context, string) ([]codeanchor.IntelDocSection, error)
	OntologyNodesByPaths(context.Context, []string) ([]readmodel.NodeRow, error)
	IntelChunksByOwners(context.Context, []string) ([]codeanchor.IntelChunk, error)
	EmbeddingsByChunkIDs(context.Context, []string) (map[string]embeddings.Embedding, error)
}

// Searcher executes semantic retrieval across code and note intel embeddings.
// IntelStore must be available; embeddings are pulled from the unified intel_embeddings table.
type Searcher struct {
	CodeProvider embeddings.Provider
	NoteProvider embeddings.Provider
	IntelStore   SearchStore // Unified embedding store (required)

	VaultDef   obsidian.VaultDefinition
	NoteReader obsidian.NoteReader
}

// HasIntelStore reports whether the searcher has a concrete intel store.
func (s *Searcher) HasIntelStore() bool {
	if s == nil {
		return false
	}
	return hasSearchStore(s.IntelStore)
}

func hasSearchStore(store SearchStore) bool {
	if store == nil {
		return false
	}
	v := reflect.ValueOf(store)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !v.IsNil()
	default:
		return true
	}
}

// Search runs semantic search against the unified intel_embeddings store.
func (s *Searcher) Search(ctx context.Context, req SearchRequest) ([]Result, error) {
	if strings.TrimSpace(req.QueryText) == "" {
		return nil, errors.New("query text is required")
	}
	limit := req.K
	if limit <= 0 {
		limit = 25
	}
	if !s.HasIntelStore() {
		return nil, errors.New("semantic search requires intel store")
	}
	if s.CodeProvider == nil && s.NoteProvider == nil {
		return nil, errors.New("no embedding provider available")
	}
	req.K = limit
	return s.searchWithIntelStore(ctx, req)
}

// searchWithIntelStore performs unified search using intel_embeddings.
func (s *Searcher) searchWithIntelStore(ctx context.Context, req SearchRequest) ([]Result, error) {
	embedder := QueryEmbedder{
		CodeProvider: s.CodeProvider,
		NoteProvider: s.NoteProvider,
	}
	embs, err := embedder.Embed(ctx, req.QueryText)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	baseFilters := semdb.EmbeddingSearchFilters{
		PathPrefixes: req.Filters.PathPrefixes,
		Granularity:  req.Filters.Granularity,
		NoteTypes:    req.Filters.NoteTypes,
		ExactSymbols: req.Filters.ExactSymbols,
		TestsOnly:    req.Filters.TestsOnly,
		ExcludeTests: req.Filters.ExcludeTests,
	}

	var scored []semdb.ScoredChunk
	var searched bool
	candidateLimit := req.K * 8
	if candidateLimit < req.K+32 {
		candidateLimit = req.K + 32
	}
	if candidateLimit < 50 {
		candidateLimit = 50
	}
	mergeScores := func(items []semdb.ScoredChunk) {
		if len(items) == 0 {
			return
		}
		scored = append(scored, items...)
	}
	searchFilteredResults := func(query embeddings.Embedding, filters semdb.EmbeddingSearchFilters, weight float64, ownerTypes ...string) ([]semdb.ScoredChunk, error) {
		if len(query) == 0 {
			return nil, nil
		}
		filters.OwnerTypes = append(filters.OwnerTypes, ownerTypes...)
		found, _, err := s.IntelStore.SearchEmbeddings(ctx, query, candidateLimit, filters)
		if err != nil {
			return nil, fmt.Errorf("search intel embeddings: %w", err)
		}
		if weight != 1 {
			for i := range found {
				found[i].Score *= weight
			}
		}
		searched = true
		return found, nil
	}
	searchFiltered := func(query embeddings.Embedding, filters semdb.EmbeddingSearchFilters, weight float64, ownerTypes ...string) error {
		found, err := searchFilteredResults(query, filters, weight, ownerTypes...)
		if err != nil {
			return err
		}
		mergeScores(found)
		return nil
	}
	search := func(query embeddings.Embedding, ownerTypes ...string) error {
		return searchFiltered(query, baseFilters, 1, ownerTypes...)
	}

	if allowsType(req.Filters, "code") {
		if err := search(embs.Code, "anchor"); err != nil {
			return nil, err
		}
	}
	if allowsType(req.Filters, "note") {
		visibleFilters := baseFilters
		visibleFilters.ExcludeGranularity = append(visibleFilters.ExcludeGranularity, embeddings.GranularityOntologyNodeSupplemental)
		docFilters := visibleFilters
		// Supplemental chunks are generated only for ontology_node owners. Avoid
		// forcing every doc-section KNN through an irrelevant eligible-set scan;
		// all other authored granularity filters remain intact.
		docFilters.ExcludeGranularity = removeFoldedValue(docFilters.ExcludeGranularity, embeddings.GranularityOntologyNodeSupplemental)
		docVisible, err := searchFilteredResults(embs.Note, docFilters, 1, "doc_section")
		if err != nil {
			return nil, err
		}
		nodeVisible, err := searchFilteredResults(embs.Note, visibleFilters, 1, "ontology_node")
		if err != nil {
			return nil, err
		}
		visible := mergeVisibleNoteCandidates(docVisible, nodeVisible, candidateLimit)
		mergeScores(visible)
		if allowsSupplementalGranularity(req.Filters.Granularity) {
			supplementalFilters := baseFilters
			supplementalFilters.Granularity = []string{embeddings.GranularityOntologyNodeSupplemental}
			if err := searchFiltered(embs.Note, supplementalFilters, supplementalSemanticWeight, "ontology_node"); err != nil {
				return nil, err
			}
		}
	}

	if !searched {
		return nil, errors.New("no embedding provider available for requested types")
	}

	if len(scored) == 0 {
		return nil, nil
	}

	sortScoredChunks(scored)
	if req.K > 0 && len(scored) > req.K {
		scored = interleaveScoredChunksByOwner(scored, req.K)
	}
	return s.resultsFromScoredChunks(ctx, scored)
}

func sortScoredChunks(scored []semdb.ScoredChunk) {
	if len(scored) < 2 {
		return
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].ChunkID < scored[j].ChunkID
	})
}

func mergeVisibleNoteCandidates(docSections, ontologyNodes []semdb.ScoredChunk, limit int) []semdb.ScoredChunk {
	out := make([]semdb.ScoredChunk, 0, min(len(docSections)+len(ontologyNodes), limit))
	out = append(out, docSections...)
	out = append(out, ontologyNodes...)
	sortScoredChunks(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func removeFoldedValue(values []string, removed string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), removed) {
			continue
		}
		out = append(out, value)
	}
	return out
}

const supplementalSemanticWeight = 0.78

func allowsSupplementalGranularity(filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, value := range filters {
		if strings.EqualFold(strings.TrimSpace(value), embeddings.GranularityOntologyNodeSupplemental) {
			return true
		}
	}
	return false
}

func allowsType(filters SearchFilters, t string) bool {
	if len(filters.Types) == 0 {
		return true
	}
	t = strings.ToLower(strings.TrimSpace(t))
	for _, allowed := range filters.Types {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		switch allowed {
		case "notes", "doc_section", "note_chunk":
			allowed = "note"
		case "anchor", "code_chunk":
			allowed = "code"
		}
		if allowed == t {
			return true
		}
	}
	return false
}
