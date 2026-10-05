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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
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
	Handle        string
	Type          string // code|note
	AnchorID      string
	NodeID        string
	NodeRefJSON   string
	SourceLocator string
	NodeKind      string
	NodeType      string
	ParentNodeID  string
	NoteID        string
	Path          string
	Title         string
	Symbol        string
	FQN           string
	Kind          string
	Granularity   string
	ChunkIndex    int
	Breadcrumb    string
	Heading       string
	Score         float64

	NodeRef *ontology.NodeRef
}

type SearchStore interface {
	SearchEmbeddings(context.Context, embeddings.Embedding, int, semdb.EmbeddingSearchFilters) ([]semdb.ScoredChunk, int, error)
	IntelAnchorsByPath(context.Context, string) ([]codeanchor.IntelAnchor, error)
	IntelAnchorsByIDs(context.Context, []string) (map[string]codeanchor.IntelAnchor, error)
	OntologyNodesByIDs(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error)
	IntelDocSectionIDsByPath(context.Context, string) ([]string, error)
	IntelDocSectionsByPath(context.Context, string) ([]codeanchor.IntelDocSection, error)
	OntologyNodesByPaths(context.Context, []string) ([]codeanchor.IntelOntologyNode, error)
	IntelChunksByOwners(context.Context, []string) ([]codeanchor.IntelChunk, error)
	EmbeddingsByChunkIDs(context.Context, []string) (map[string]embeddings.Embedding, error)
}

// Searcher executes semantic retrieval across code and note intel embeddings.
// IntelStore must be available; embeddings are pulled from the unified intel_embeddings table.
type Searcher struct {
	CodeProvider embeddings.Provider
	NoteProvider embeddings.Provider
	IntelStore   SearchStore // Unified embedding store (required)

	VaultDef       obsidian.VaultDefinition
	NoteReader     obsidian.NoteReader
	OntologySchema *ontology.Schema
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
		visibleFilters.ExcludeGranularity = append(visibleFilters.ExcludeGranularity, GranularityOntologyNodeSupplemental)
		docFilters := visibleFilters
		// Supplemental chunks are generated only for ontology_node owners. Avoid
		// forcing every doc-section KNN through an irrelevant eligible-set scan;
		// all other authored granularity filters remain intact.
		docFilters.ExcludeGranularity = removeFoldedValue(docFilters.ExcludeGranularity, GranularityOntologyNodeSupplemental)
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
			supplementalFilters.Granularity = []string{GranularityOntologyNodeSupplemental}
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
		if strings.EqualFold(strings.TrimSpace(value), GranularityOntologyNodeSupplemental) {
			return true
		}
	}
	return false
}

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

func (s *Searcher) resultsFromScoredChunks(ctx context.Context, scored []semdb.ScoredChunk) ([]Result, error) {
	ownerTypes := make(map[string]string, len(scored))
	for _, sc := range scored {
		if _, ok := ownerTypes[sc.OwnerID]; !ok {
			ownerTypes[sc.OwnerID] = sc.OwnerType
		}
	}
	anchors, _ := s.IntelStore.IntelAnchorsByIDs(ctx, func() []string {
		var ids []string
		for id, ot := range ownerTypes {
			if ot == "anchor" {
				ids = append(ids, id)
			}
		}
		return ids
	}())
	var nodeIDs []string
	for id, ot := range ownerTypes {
		if ot == "ontology_node" {
			nodeIDs = append(nodeIDs, id)
		}
	}
	nodes, err := s.IntelStore.OntologyNodesByIDs(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	sectionIndexByPath := make(map[string]map[string]int)
	getSectionIndex := func(path, sectionID string) (int, bool) {
		if strings.TrimSpace(path) == "" || strings.TrimSpace(sectionID) == "" {
			return -1, false
		}
		idxMap, ok := sectionIndexByPath[path]
		if !ok {
			sectionIDs, err := s.IntelStore.IntelDocSectionIDsByPath(ctx, path)
			if err != nil || len(sectionIDs) == 0 {
				return -1, false
			}
			idxMap = make(map[string]int, len(sectionIDs))
			for i, sectionID := range sectionIDs {
				if strings.TrimSpace(sectionID) == "" {
					continue
				}
				idxMap[sectionID] = i
			}
			sectionIndexByPath[path] = idxMap
		}
		idx, ok := idxMap[sectionID]
		return idx, ok
	}

	results := make([]Result, 0, len(scored))
	for _, sc := range scored {
		chunkIndex := sc.Ord
		if sc.OwnerType == "doc_section" {
			if idx, ok := getSectionIndex(sc.Path, sc.OwnerID); ok {
				chunkIndex = idx
			} else {
				chunkIndex = -1
			}
		}
		result := Result{
			Path:        sc.Path,
			Breadcrumb:  sc.Breadcrumb,
			Heading:     sc.Heading,
			Granularity: sc.Granularity,
			ChunkIndex:  chunkIndex,
			Score:       sc.Score,
		}
		if sc.OwnerType == "anchor" {
			result.Type = "code"
			result.AnchorID = sc.OwnerID
			if anchor, ok := anchors[sc.OwnerID]; ok {
				result.Symbol = anchor.Symbol
				result.FQN = anchor.FQN
				result.Kind = anchor.Kind
				result.Path = anchor.Path
			}
			// Build handle for code chunks
			result.Handle = knowledge.CodeChunkHandle(sc.OwnerID, sc.Granularity, chunkIndex).String()
		} else if sc.OwnerType == "doc_section" {
			result.Type = "note"
			result.NoteID = sc.Path
			result.Title = sc.Heading
			// Build handle for note chunks
			if chunkIndex >= 0 {
				result.Handle = knowledge.NoteChunkHandle(sc.Path, chunkIndex).String()
			} else {
				result.Handle = knowledge.NoteHandle(sc.Path).String()
			}
		} else if sc.OwnerType == "ontology_node" {
			node, ok := nodes[sc.OwnerID]
			if !ok || strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.NotePath) == "" || strings.TrimSpace(node.NodeRefJSON) == "" || strings.TrimSpace(node.SourceLocator) == "" {
				continue
			}
			result.Type = "note"
			result.NodeID = sc.OwnerID
			result.NoteID = sc.Path
			result.Title = sc.Heading
			result.Kind = "ontology_node"
			result.NodeRefJSON = node.NodeRefJSON
			result.SourceLocator = node.SourceLocator
			result.NodeKind = node.NodeKind
			result.NodeType = displayOntologyNodeType(node.TypeName)
			result.ParentNodeID = node.ParentNodeID
			var ref ontology.NodeRef
			if err := json.Unmarshal([]byte(node.NodeRefJSON), &ref); err == nil && !ref.IsZero() {
				result.NodeRef = &ref
			}
			result.Path = node.NotePath
			result.NoteID = node.NotePath
			if strings.TrimSpace(node.Title) != "" {
				result.Title = node.Title
			}
			result.Handle = knowledge.NodeChunkHandle(sc.OwnerID, result.NoteID, sc.Granularity, chunkIndex).String()
		}
		results = append(results, result)
	}
	return results, nil
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

func displayOntologyNodeType(typeName string) string {
	if ontology.IsFallbackNoteTypeName(typeName) {
		return "untyped note"
	}
	if ontology.IsFallbackSectionTypeName(typeName) {
		return "untyped note section"
	}
	return typeName
}

func interleaveScoredChunksByOwner(scored []semdb.ScoredChunk, limit int) []semdb.ScoredChunk {
	if limit <= 0 || len(scored) <= limit {
		return scored
	}
	type bucket struct {
		key   string
		items []semdb.ScoredChunk
	}
	byKey := make(map[string]int, len(scored))
	buckets := make([]bucket, 0, len(scored))
	for _, sc := range scored {
		key := scoredChunkDiversityKey(sc)
		idx, ok := byKey[key]
		if !ok {
			idx = len(buckets)
			byKey[key] = idx
			buckets = append(buckets, bucket{key: key})
		}
		buckets[idx].items = append(buckets[idx].items, sc)
	}
	sort.SliceStable(buckets, func(i, j int) bool {
		left := buckets[i].items[0]
		right := buckets[j].items[0]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		return buckets[i].key < buckets[j].key
	})
	out := make([]semdb.ScoredChunk, 0, limit)
	for len(out) < limit {
		added := false
		for i := range buckets {
			if len(buckets[i].items) == 0 {
				continue
			}
			out = append(out, buckets[i].items[0])
			buckets[i].items = buckets[i].items[1:]
			added = true
			if len(out) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return out
}

func scoredChunkDiversityKey(sc semdb.ScoredChunk) string {
	if (sc.OwnerType == "doc_section" || sc.OwnerType == "ontology_node") && strings.TrimSpace(sc.Path) != "" {
		return "note:" + sc.Path
	}
	return sc.OwnerType + ":" + sc.OwnerID
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
