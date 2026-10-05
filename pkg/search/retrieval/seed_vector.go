package retrieval

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
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
	nodesByID := make(map[string]codeanchor.IntelOntologyNode)
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
				title = noteTitleFromPath(notePath)
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
				Handle:      handle,
				Owner:       owner,
				Type:        "note",
				Path:        notePath,
				Title:       title,
				NoteID:      notePath,
				ChunkIndex:  chunkIndex,
				Breadcrumb:  m.Breadcrumb,
				Heading:     m.Heading,
				Granularity: m.Granularity,
			}
			if m.OwnerType == "ontology_node" {
				entry.cand.NodeID = m.OwnerID
				entry.cand.Kind = "ontology_node"
				if hasNode {
					entry.cand.NodeRefJSON = node.NodeRefJSON
					entry.cand.SourceLocator = node.SourceLocator
					entry.cand.NodeKind = node.NodeKind
					entry.cand.NodeType = node.TypeName
					entry.cand.ParentNodeID = node.ParentNodeID
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

func hasCompleteOntologyNodeIdentity(node codeanchor.IntelOntologyNode) bool {
	return strings.TrimSpace(node.NodeID) != "" &&
		strings.TrimSpace(node.NotePath) != "" &&
		strings.TrimSpace(node.NodeRefJSON) != "" &&
		strings.TrimSpace(node.SourceLocator) != ""
}

func (r *SeedVectorRetriever) retrieveCodeSeeds(ctx context.Context, seeds []knowledge.Handle, perChunkLimit, maxChunks int, queryFilters search.Filters) ([]search.Candidate, error) {
	store := r.Semantic.IntelStore
	if store == nil {
		return nil, nil
	}

	var anchorSeeds []string
	var fileSeeds []string
	anchorCache := make(map[string]codeanchor.IntelAnchor)

	for _, seed := range seeds {
		switch seed.Kind {
		case knowledge.KindAnchor:
			id := strings.TrimSpace(seed.ID)
			if id != "" {
				anchorSeeds = append(anchorSeeds, id)
			}
		case knowledge.KindFile:
			if hasFragment(seed, "dirseed") {
				continue
			}
			p := normalizeCodePath(filepath.ToSlash(strings.TrimSpace(seed.ID)))
			if p != "" {
				fileSeeds = append(fileSeeds, p)
			}
		}
	}

	for _, seed := range fileSeeds {
		if ctx.Err() != nil {
			break
		}
		anchors, err := store.IntelAnchorsByPath(ctx, seed)
		if err != nil {
			return nil, err
		}
		for _, anchor := range anchors {
			if strings.TrimSpace(anchor.AnchorID) == "" {
				continue
			}
			anchorSeeds = append(anchorSeeds, anchor.AnchorID)
			anchorCache[anchor.AnchorID] = anchor
		}
	}

	if len(anchorSeeds) == 0 {
		return nil, nil
	}

	uniqueSeeds := make([]string, 0, len(anchorSeeds))
	seenSeed := make(map[string]struct{}, len(anchorSeeds))
	for _, id := range anchorSeeds {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seenSeed[id]; ok {
			continue
		}
		seenSeed[id] = struct{}{}
		uniqueSeeds = append(uniqueSeeds, id)
	}

	maxSeedAnchors := 12
	if maxChunks > 0 {
		maxSeedAnchors = min(maxSeedAnchors, max(1, maxChunks/2))
	}
	if len(uniqueSeeds) > maxSeedAnchors {
		uniqueSeeds = uniqueSeeds[:maxSeedAnchors]
	}

	chunks, err := store.IntelChunksByOwners(ctx, uniqueSeeds)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, nil
	}

	primary := make(map[string]codeanchor.IntelChunk)
	for _, c := range chunks {
		if strings.TrimSpace(c.OwnerID) == "" {
			continue
		}
		cur, ok := primary[c.OwnerID]
		if !ok || c.Ord < cur.Ord {
			primary[c.OwnerID] = c
		}
	}

	chunkIDs := make([]string, 0, len(primary))
	for _, c := range primary {
		chunkIDs = append(chunkIDs, c.ChunkID)
	}
	embeddingsByChunk, err := store.EmbeddingsByChunkIDs(ctx, chunkIDs)
	if err != nil {
		return nil, err
	}

	perItemLimit := perChunkLimit
	if perItemLimit < 25 {
		perItemLimit = 25
	}
	if perItemLimit > 250 {
		perItemLimit = 250
	}

	type agg struct {
		cand   search.Candidate
		scores []float64
	}
	best := make(map[string]*agg)

	fetchAnchors := func(ids []string) error {
		if len(ids) == 0 {
			return nil
		}
		fetched, err := store.IntelAnchorsByIDs(ctx, ids)
		if err != nil {
			return err
		}
		for id, anchor := range fetched {
			anchorCache[id] = anchor
		}
		return nil
	}

	applyMatches := func(matches []semdb.ScoredChunk) error {
		var missing []string
		for _, m := range matches {
			if _, ok := anchorCache[m.OwnerID]; !ok {
				missing = append(missing, m.OwnerID)
			}
		}
		if err := fetchAnchors(missing); err != nil {
			return err
		}
		for _, m := range matches {
			anchor := anchorCache[m.OwnerID]
			handle := knowledge.AnchorHandle(m.OwnerID)
			entry, ok := best[m.OwnerID]
			if !ok {
				entry = &agg{}
				best[m.OwnerID] = entry
			}
			path := m.Path
			if anchor.Path != "" {
				path = anchor.Path
			}
			entry.cand = search.Candidate{
				Handle:     handle,
				Owner:      handle,
				Type:       "code",
				Path:       path,
				Title:      anchor.Symbol,
				Symbol:     anchor.Symbol,
				FQN:        anchor.FQN,
				Kind:       anchor.Kind,
				ChunkIndex: -1,
				AnchorID:   m.OwnerID,
			}
			entry.scores = addScore(entry.scores, m.Score, 5)
		}
		return nil
	}

	filters := semdb.EmbeddingSearchFilters{
		OwnerTypes:   []string{"anchor"},
		PathPrefixes: queryFilters.PathPrefixes,
		NoteTypes:    queryFilters.NoteTypes,
		ExactSymbols: queryFilters.ExactSymbols,
		TestsOnly:    queryFilters.TestsOnly,
		ExcludeTests: queryFilters.ExcludeTests,
	}
	var seedEmbeds []embeddings.Embedding
	for _, seed := range uniqueSeeds {
		if ctx.Err() != nil {
			break
		}
		chunk, ok := primary[seed]
		if !ok {
			continue
		}
		emb := embeddingsByChunk[chunk.ChunkID]
		if len(emb) == 0 {
			continue
		}
		seedEmbeds = append(seedEmbeds, emb)

		matches, _, err := store.SearchEmbeddings(ctx, emb, perItemLimit, filters)
		if err != nil {
			return nil, err
		}
		if err := applyMatches(matches); err != nil {
			return nil, err
		}
	}

	if centroid := averageEmbeddings(seedEmbeds); centroid != nil && ctx.Err() == nil {
		matches, _, err := store.SearchEmbeddings(ctx, centroid, perItemLimit, filters)
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
		cand.Evidence = []search.Evidence{literalEvidence("code_vector_similarity", score)}
		results = append(results, cand)
	}
	results = sortCandidates(results, perChunkLimit)
	return results, nil
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

func noteTitleFromPath(p string) string {
	base := filepath.Base(strings.TrimSpace(p))
	return strings.TrimSuffix(base, filepath.Ext(base))
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
