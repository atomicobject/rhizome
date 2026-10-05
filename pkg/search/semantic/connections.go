package semantic

import (
	"context"
	"runtime"
	"strings"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// NoteAgg captures top scores + hits per note for semantic connections.
type NoteAgg struct {
	TopScores []float64
	TopHits   []ChunkHit
}

// ChunkHit captures a matched chunk plus the originating query chunk.
type ChunkHit struct {
	Match           embeddings.SimilarChunk
	QueryIndex      int
	QueryBreadcrumb string
	QueryHeading    string
}

// DiversifyChunkHits removes near-duplicate hits from the same section.
func DiversifyChunkHits(hits []ChunkHit, limit int) []ChunkHit {
	if limit <= 0 {
		limit = len(hits)
	}
	out := make([]ChunkHit, 0, min(limit, len(hits)))
	for _, h := range hits {
		redundant := false
		for _, picked := range out {
			sameSection := false
			if picked.Match.Breadcrumb != "" && h.Match.Breadcrumb != "" && picked.Match.Breadcrumb == h.Match.Breadcrumb {
				sameSection = true
			}
			if picked.Match.Heading != "" && h.Match.Heading != "" && picked.Match.Heading == h.Match.Heading {
				sameSection = true
			}
			if !sameSection {
				continue
			}
			diff := picked.Match.ChunkIndex - h.Match.ChunkIndex
			if diff < 0 {
				diff = -diff
			}
			if diff <= 1 {
				redundant = true
				break
			}
		}
		if redundant {
			continue
		}
		out = append(out, h)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// AggregateIntelChunkMatches aggregates note matches using intel_embeddings.
// Ontology-node chunks are the primary semantic note surface; doc-section
// matches remain accepted for old indexes and ontology-unavailable paths.
func AggregateIntelChunkMatches(
	ctx context.Context,
	store *semdb.Store,
	chunks []embeddings.StoredChunk,
	perChunkLimit int,
	skip embeddings.NoteID,
	maxScoresPerNote int,
	maxQueryChunks int,
	captureQuery bool,
	maxChunksPerNote int,
	titleForPath func(string) string,
) (map[embeddings.NoteID]*NoteAgg, int, error) {
	if perChunkLimit <= 0 {
		perChunkLimit = 10
	}
	if maxScoresPerNote <= 0 {
		maxScoresPerNote = 3
	}
	if maxQueryChunks > 0 && len(chunks) > maxQueryChunks {
		chunks = chunks[:maxQueryChunks]
	}
	agg := make(map[embeddings.NoteID]*NoteAgg)
	if len(chunks) == 0 || store == nil {
		return agg, 0, nil
	}

	sectionIndexByPath := make(map[string]map[string]int)
	var sectionMu sync.Mutex
	sectionIndexFor := func(path, sectionID string) (int, bool, error) {
		if strings.TrimSpace(path) == "" || strings.TrimSpace(sectionID) == "" {
			return -1, false, nil
		}
		sectionMu.Lock()
		idxMap, ok := sectionIndexByPath[path]
		sectionMu.Unlock()
		if !ok {
			sections, err := store.IntelDocSectionsByPath(ctx, path)
			if err != nil {
				return -1, false, err
			}
			idxMap = make(map[string]int, len(sections))
			for i, sec := range sections {
				if strings.TrimSpace(sec.SectionID) == "" {
					continue
				}
				idxMap[sec.SectionID] = i
			}
			sectionMu.Lock()
			sectionIndexByPath[path] = idxMap
			sectionMu.Unlock()
		}
		idx, ok := idxMap[sectionID]
		return idx, ok, nil
	}

	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}

	workCh := make(chan embeddings.StoredChunk, workerCount)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	var mu sync.Mutex
	totalSkipped := 0

	filters := semdb.EmbeddingSearchFilters{OwnerTypes: []string{"doc_section", "ontology_node"}}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ch := range workCh {
				select {
				case <-ctx.Done():
					select {
					case errCh <- ctx.Err():
					default:
					}
					return
				default:
				}

				matches, skipped, err := store.SearchEmbeddings(ctx, ch.Embedding, perChunkLimit, filters)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				mu.Lock()
				totalSkipped += skipped
				mu.Unlock()

				for _, m := range matches {
					noteID := embeddings.NoteID(m.Path)
					if skip != "" && noteID == skip {
						continue
					}

					chunkIndex := m.Ord
					if m.OwnerType == "doc_section" {
						chunkIndex = -1
						if idx, ok, err := sectionIndexFor(m.Path, m.OwnerID); err == nil && ok {
							chunkIndex = idx
						} else if err != nil {
							select {
							case errCh <- err:
							default:
							}
							return
						}
					}

					title := ""
					if titleForPath != nil {
						title = titleForPath(m.Path)
					}
					match := embeddings.SimilarChunk{
						NoteID:     noteID,
						Title:      title,
						ChunkIndex: chunkIndex,
						Breadcrumb: m.Breadcrumb,
						Heading:    m.Heading,
						Score:      m.Score,
					}

					mu.Lock()
					aggNote, ok := agg[noteID]
					if !ok {
						aggNote = &NoteAgg{}
						agg[noteID] = aggNote
					}
					aggNote.TopScores = addScore(aggNote.TopScores, match.Score, maxScoresPerNote)
					if maxChunksPerNote > 0 {
						hit := ChunkHit{Match: match}
						if captureQuery {
							hit.QueryIndex = ch.Index
							hit.QueryBreadcrumb = ch.Breadcrumb
							hit.QueryHeading = ch.Heading
						}
						aggNote.TopHits = insertHit(aggNote.TopHits, hit, maxChunksPerNote)
					}
					mu.Unlock()
				}
			}
		}()
	}

	for _, ch := range chunks {
		select {
		case err := <-errCh:
			close(workCh)
			wg.Wait()
			return nil, totalSkipped, err
		default:
		}
		workCh <- ch
	}
	close(workCh)
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, totalSkipped, err
	default:
	}

	return agg, totalSkipped, nil
}

func addScore(scores []float64, score float64, maxScores int) []float64 {
	if maxScores <= 0 {
		maxScores = 3
	}
	scores = append(scores, score)
	sortFloatDesc(scores)
	if len(scores) > maxScores {
		scores = scores[:maxScores]
	}
	return scores
}

func insertHit(top []ChunkHit, hit ChunkHit, limit int) []ChunkHit {
	for i, h := range top {
		if h.Match.ChunkIndex == hit.Match.ChunkIndex {
			if h.Match.Score >= hit.Match.Score {
				return top
			}
			top[i] = hit
			sortChunkHits(top)
			if limit > 0 && len(top) > limit {
				top = top[:limit]
			}
			return top
		}
	}
	pos := sortChunkHitInsert(top, hit)
	top = append(top, ChunkHit{})
	copy(top[pos+1:], top[pos:])
	top[pos] = hit
	if limit > 0 && len(top) > limit {
		top = top[:limit]
	}
	return top
}

func sortFloatDesc(vals []float64) {
	for i := 1; i < len(vals); i++ {
		j := i
		for j > 0 && vals[j-1] < vals[j] {
			vals[j-1], vals[j] = vals[j], vals[j-1]
			j--
		}
	}
}

func sortChunkHits(hits []ChunkHit) {
	for i := 1; i < len(hits); i++ {
		j := i
		for j > 0 && hits[j-1].Match.Score < hits[j].Match.Score {
			hits[j-1], hits[j] = hits[j], hits[j-1]
			j--
		}
	}
}

func sortChunkHitInsert(hits []ChunkHit, hit ChunkHit) int {
	lo := 0
	hi := len(hits)
	for lo < hi {
		mid := (lo + hi) / 2
		if hits[mid].Match.Score < hit.Match.Score {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}
