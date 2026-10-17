package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// SeedVectorRetriever runs vector similarity using seed entities (notes/files/anchors) instead of free-text queries.
// It complements the text-driven VectorRetriever so seeded searches (e.g., files/dirs only) can still surface
// relevant results.
type SeedVectorRetriever struct {
	Semantic         *semantic.Searcher
	MaxChunksPerSeed int
}

func (r *SeedVectorRetriever) Name() string { return "seed_vector" }

func (r *SeedVectorRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if len(spec.Seeds) == 0 {
		return nil, nil
	}
	if r.Semantic == nil {
		return nil, fmt.Errorf("seed vector retriever missing semantic backend")
	}
	if r.Semantic.IntelStore == nil {
		return nil, nil
	}

	limit := spec.Limits.Total
	if limit <= 0 {
		limit = 25
	}
	perChunkLimit := limit * 4
	if perChunkLimit < limit {
		perChunkLimit = limit
	}
	maxChunks := r.MaxChunksPerSeed
	if maxChunks <= 0 {
		maxChunks = 32
	}

	var out []search.Candidate

	if spec.Filters.AllowsType("note") {
		if noteResults, err := r.retrieveNoteSeeds(ctx, spec.Seeds, perChunkLimit, maxChunks, spec.Filters); err != nil {
			return nil, err
		} else {
			out = append(out, noteResults...)
		}
	}

	if spec.Filters.AllowsType("code") {
		if codeResults, err := r.retrieveCodeSeeds(ctx, spec.Seeds, perChunkLimit, maxChunks, spec.Filters); err != nil {
			return nil, err
		} else {
			out = append(out, codeResults...)
		}
	}

	return out, nil
}

func (r *SeedVectorRetriever) retrieveNoteSeeds(ctx context.Context, seeds []knowledge.Handle, perChunkLimit, maxChunks int, queryFilters search.Filters) ([]search.Candidate, error) {
	store := r.Semantic.IntelStore
	if store == nil {
		return nil, nil
	}

	type noteTarget struct {
		all     bool
		indexes map[int]struct{}
	}

	targets := make(map[string]*noteTarget)
	for _, seed := range seeds {
		switch seed.Kind {
		case knowledge.KindNote, knowledge.KindNoteChunk:
			notePath, ok := cleanTypedNotePath(seed.ID)
			if !ok {
				continue
			}
			noteID := embeddings.NoteID(notePath)
			path := string(noteID)
			target, ok := targets[path]
			if !ok {
				target = &noteTarget{indexes: make(map[int]struct{})}
				targets[path] = target
			}
			if seed.Kind == knowledge.KindNote {
				target.all = true
				target.indexes = nil
				continue
			}
			if target.all {
				continue
			}
			if len(seed.Fragments) == 0 {
				continue
			}
			idxStr := strings.TrimSpace(seed.Fragments[0])
			if idxStr == "" {
				continue
			}
			if idxVal, err := strconv.Atoi(idxStr); err == nil {
				target.indexes[idxVal] = struct{}{}
			}
		}
	}

	if len(targets) == 0 {
		return nil, nil
	}

	var queries []embeddings.StoredChunk
	var seedEmbeds []embeddings.Embedding
	for path, target := range targets {
		indexes := target.indexes
		if target.all {
			indexes = nil
		}
		chunks, err := semantic.NoteSemanticChunks(ctx, store, path, indexes, maxChunks)
		if err != nil {
			return nil, err
		}
		queries = append(queries, chunks...)
	}
	if len(queries) == 0 {
		return nil, nil
	}
	for _, q := range queries {
		seedEmbeds = append(seedEmbeds, q.Embedding)
	}
	nodesByID := make(map[string]readmodel.NodeRow)
	loadNodes := func(matches []semdb.ScoredChunk) error {
		nodeIDs := make([]string, 0)
		seen := make(map[string]struct{})
		for _, m := range matches {
			if m.OwnerType != "ontology_node" || strings.TrimSpace(m.OwnerID) == "" {
				continue
			}
			if _, ok := nodesByID[m.OwnerID]; ok {
				continue
			}
			if _, ok := seen[m.OwnerID]; ok {
				continue
			}
			seen[m.OwnerID] = struct{}{}
			nodeIDs = append(nodeIDs, m.OwnerID)
		}
		if len(nodeIDs) == 0 {
			return nil
		}
		nodes, err := store.OntologyNodesByIDs(ctx, nodeIDs)
		if err != nil {
			return err
		}
		for id, node := range nodes {
			nodesByID[id] = node
		}
		return nil
	}

	titleByPath := make(map[string]string)

	type agg struct {
		cand   search.Candidate
		scores []float64
	}
	best := make(map[string]*agg)

	applyMatches := func(matches []semdb.ScoredChunk) error {
		if err := loadNodes(matches); err != nil {
			return err
		}
		for _, m := range matches {
			notePath, ok := cleanTypedNotePath(m.Path)
			if !ok {
				continue
			}
			owner := knowledge.NoteHandle(notePath)
			handle := owner
			chunkIndex := m.Ord
			node, hasNode := nodesByID[m.OwnerID]
			if m.OwnerType == "ontology_node" {
				if !hasCompleteOntologyNodeIdentity(node) {
					continue
				}
				if nodePath, ok := cleanTypedNotePath(node.NotePath); ok {
					notePath = nodePath
					owner = knowledge.NoteHandle(notePath)
				}
				handle = knowledge.NodeChunkHandle(m.OwnerID, notePath, m.Granularity, chunkIndex)
			} else if chunkIndex >= 0 {
				handle = knowledge.NoteChunkHandle(notePath, chunkIndex)
			}
			title, okTitle := titleByPath[notePath]
			if !okTitle {
				title = paths.NoteTitle(strings.TrimSpace(notePath))
				titleByPath[notePath] = title
			}
			if hasNode && strings.TrimSpace(node.Title) != "" {
				title = node.Title
			}
			key := handle.String()
			if key == "" {
				continue
			}
			entry, ok := best[key]
			if !ok {
				entry = &agg{}
				best[key] = entry
			}
			entry.cand = search.Candidate{
				SourceRef: search.SourceRef{
					Path:        notePath,
					Title:       title,
					NoteID:      notePath,
					ChunkIndex:  chunkIndex,
					Breadcrumb:  m.Breadcrumb,
					Heading:     m.Heading,
					Granularity: m.Granularity,
				},
				Handle: handle,
				Owner:  owner,
				Type:   "note",
			}
			if m.OwnerType == "ontology_node" {
				entry.cand.NodeID = m.OwnerID
				entry.cand.Kind = "ontology_node"
				if hasNode {
					entry.cand.ParentNodeID = node.ParentNodeID
					var ref ontology.NodeRef
					if err := json.Unmarshal([]byte(node.NodeRefJSON), &ref); err == nil && !ref.IsZero() {
						entry.cand.NodeRef = &ref
					}
				}
			}
			entry.scores = addScore(entry.scores, m.Score, 5)
		}
		return nil
	}

	filters := semdb.EmbeddingSearchFilters{
		OwnerTypes:   []string{"doc_section", "ontology_node"},
		PathPrefixes: queryFilters.PathPrefixes,
		NoteTypes:    queryFilters.NoteTypes,
		ExactSymbols: queryFilters.ExactSymbols,
		TestsOnly:    queryFilters.TestsOnly,
		ExcludeTests: queryFilters.ExcludeTests,
	}
	for _, q := range queries {
		if ctx.Err() != nil {
			break
		}
		matches, _, err := store.SearchEmbeddings(ctx, q.Embedding, perChunkLimit, filters)
		if err != nil {
			return nil, err
		}
		if err := applyMatches(matches); err != nil {
			return nil, err
		}
	}
	if centroid := averageEmbeddings(seedEmbeds); centroid != nil && ctx.Err() == nil {
		matches, _, err := store.SearchEmbeddings(ctx, centroid, perChunkLimit, filters)
		if err == nil {
			if err := applyMatches(matches); err != nil {
				return nil, err
			}
		}
	}

	results := make([]search.Candidate, 0, len(best))
	for _, a := range best {
		score := average(a.scores)
		cand := a.cand
		cand.Evidence = []search.Evidence{literalEvidence("note_vector_similarity", score)}
		results = append(results, cand)
	}
	results = sortCandidates(results, perChunkLimit)
	return results, nil
}

func hasCompleteOntologyNodeIdentity(node readmodel.NodeRow) bool {
	return strings.TrimSpace(node.NodeID) != "" &&
		strings.TrimSpace(node.NotePath) != "" &&
		strings.TrimSpace(node.NodeRefJSON) != "" &&
		strings.TrimSpace(node.SourceLocator) != ""
}

func hasFragment(h knowledge.Handle, frag string) bool {
	frag = strings.TrimSpace(frag)
	if frag == "" {
		return false
	}
	for _, f := range h.Fragments {
		if strings.TrimSpace(f) == frag {
			return true
		}
	}
	return false
}

func normalizeCodePath(p string) string {
	return string(paths.NormalizeCode(filepath.ToSlash(strings.TrimSpace(p))))
}

func literalEvidence(typ string, score float64) search.Evidence {
	return search.Evidence{Type: typ, RawScore: score, Source: "seed_vector"}
}

func scoreOf(evs []search.Evidence) float64 {
	best := 0.0
	for _, ev := range evs {
		if ev.RawScore > best {
			best = ev.RawScore
		}
	}
	return best
}

func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func sortCandidates(c []search.Candidate, limit int) []search.Candidate {
	sort.SliceStable(c, func(i, j int) bool {
		si := scoreOf(c[i].Evidence)
		sj := scoreOf(c[j].Evidence)
		if si != sj {
			return si > sj
		}
		return c[i].Handle.String() < c[j].Handle.String()
	})
	if limit > 0 && len(c) > limit {
		return c[:limit]
	}
	return c
}

func averageEmbeddings(vecs []embeddings.Embedding) embeddings.Embedding {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	if dim == 0 {
		return nil
	}
	out := make(embeddings.Embedding, dim)
	for _, v := range vecs {
		if len(v) != dim {
			continue
		}
		for i, val := range v {
			out[i] += val
		}
	}
	count := float32(len(vecs))
	for i := range out {
		out[i] /= count
	}
	return out
}

func addScore(scores []float64, score float64, maxScores int) []float64 {
	if maxScores <= 0 {
		maxScores = 3
	}
	scores = append(scores, score)
	sort.Slice(scores, func(i, j int) bool { return scores[i] > scores[j] })
	if len(scores) > maxScores {
		scores = scores[:maxScores]
	}
	return scores
}
