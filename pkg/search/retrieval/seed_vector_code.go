package retrieval

import (
	"context"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

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
				SourceRef: search.SourceRef{
					Path:       path,
					Title:      anchor.Symbol,
					Symbol:     anchor.Symbol,
					FQN:        anchor.FQN,
					Kind:       anchor.Kind,
					ChunkIndex: -1,
					AnchorID:   m.OwnerID,
				},
				Handle: handle,
				Owner:  handle,
				Type:   "code",
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
