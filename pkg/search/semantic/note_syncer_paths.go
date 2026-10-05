package semantic

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// SyncPaths incrementally syncs note embeddings for specific note paths.
//
// It observes ctx between planning and embedding so a cancelled indexing job
// stops here rather than at the end of the cycle (SPEC-0104 US3).
func (s *NoteSyncer) SyncPaths(ctx context.Context, paths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, err := s.PlanPaths(ctx, paths)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.EmbedWithoutSyncMark(ctx, plan)
}

// PlanPaths prepares note embedding work for specific note paths only.
func (s *NoteSyncer) PlanPaths(ctx context.Context, paths []string) (NotePlan, error) {
	if s.Index == nil || s.Provider == nil || s.Intel == nil {
		return NotePlan{}, errors.New("note syncer requires index, provider, and intel source")
	}
	if err := s.ensureReady(ctx); err != nil {
		return NotePlan{}, err
	}

	normalized := normalizeNotePaths(paths)
	if len(normalized) == 0 {
		return NotePlan{skipEmbed: true}, nil
	}

	byPath, err := s.loadDocSectionsByPath(ctx, normalized)
	if err != nil {
		return NotePlan{}, err
	}

	var noteTitles map[string]string
	if s.NoteReader != nil {
		noteTitles, err = observeNoteMetric(ctx, "noteplan.load_titles", func() (map[string]string, error) {
			titles := make(map[string]string, len(normalized))
			for _, path := range normalized {
				if title, ok := s.NoteReader.Title(path); ok {
					titles[path] = title
				}
			}
			return titles, nil
		})
		if err != nil {
			return NotePlan{}, err
		}
	}

	liveIDs := make([]embeddings.NoteID, 0, len(normalized))
	deleteIDs := make([]embeddings.NoteID, 0)
	infoByPath := make(map[string]embeddings.NoteFileInfo, len(normalized))
	livePaths := make([]string, 0, len(normalized))
	for _, path := range normalized {
		// Between files: a cancelled job must not keep preparing work.
		if err := ctx.Err(); err != nil {
			return NotePlan{}, err
		}
		noteID := embeddings.NoteID(path)
		sections := byPath[path]
		if len(sections) == 0 {
			deleteIDs = append(deleteIDs, noteID)
			continue
		}
		sort.Slice(sections, func(i, j int) bool {
			if sections[i].StartByte == sections[j].StartByte {
				return sections[i].SectionID < sections[j].SectionID
			}
			return sections[i].StartByte < sections[j].StartByte
		})
		infoByPath[path] = buildNoteFileInfo(noteID, path, sections, noteTitles)
		liveIDs = append(liveIDs, noteID)
		livePaths = append(livePaths, path)
	}

	rawPaths, typedPaths, err := s.rawNoteEmbeddingPaths(ctx, livePaths)
	if err != nil {
		return NotePlan{}, err
	}
	indexingperf.AddCount(ctx, "noteplan.raw_eligible_paths", int64(len(rawPaths)))
	indexingperf.AddCount(ctx, "noteplan.raw_skipped_paths", int64(len(typedPaths)))
	rawSet := stringSet(rawPaths)
	if len(typedPaths) > 0 {
		filteredIDs := liveIDs[:0]
		filteredPaths := livePaths[:0]
		for _, path := range livePaths {
			if _, ok := rawSet[path]; !ok {
				continue
			}
			filteredPaths = append(filteredPaths, path)
			filteredIDs = append(filteredIDs, embeddings.NoteID(path))
		}
		livePaths = filteredPaths
		liveIDs = filteredIDs
	}

	if len(infoByPath) > 0 {
		infos := make([]embeddings.NoteFileInfo, 0, len(infoByPath))
		for _, path := range livePaths {
			infos = append(infos, infoByPath[path])
		}
		if s.WriteQueue != nil {
			for _, info := range infos {
				if err := observeNoteMetricErr(ctx, "noteplan.meta_submit", func() error {
					submitStarted := time.Now()
					err := s.WriteQueue.SubmitNoteMeta(ctx, info)
					indexingperf.ObserveLatency(ctx, "noteplan.writeback.meta.submit_wait", time.Since(submitStarted))
					return err
				}); err != nil {
					return NotePlan{}, fmt.Errorf("upsert note meta %s: %w", info.Path, err)
				}
			}
			if err := observeNoteMetricErr(ctx, "noteplan.meta_flush_wait", func() error {
				return s.WriteQueue.FlushAndWait(ctx)
			}); err != nil {
				return NotePlan{}, err
			}
		} else if batcher, ok := s.Index.(interface {
			UpsertNoteMetaBatch(context.Context, []embeddings.NoteFileInfo) error
		}); ok {
			if err := observeNoteMetricErr(ctx, "noteplan.meta_submit", func() error {
				return batcher.UpsertNoteMetaBatch(ctx, infos)
			}); err != nil {
				return NotePlan{}, fmt.Errorf("upsert note meta batch: %w", err)
			}
		} else {
			for _, info := range infos {
				if err := observeNoteMetricErr(ctx, "noteplan.meta_submit", func() error {
					return s.Index.UpsertNoteMeta(ctx, info)
				}); err != nil {
					return NotePlan{}, fmt.Errorf("upsert note meta %s: %w", info.Path, err)
				}
			}
		}
	}

	chunkHashesByID := make(map[embeddings.NoteID]map[int]string, len(liveIDs))
	if len(liveIDs) > 0 {
		if batcher, ok := s.Index.(noteChunkHashesBatch); ok {
			chunkHashesByID, err = observeNoteMetric(ctx, "noteplan.chunk_hashes_batch", func() (map[embeddings.NoteID]map[int]string, error) {
				return batcher.ChunkHashesBatch(ctx, liveIDs)
			})
			if err != nil {
				return NotePlan{}, err
			}
		}
	}

	sectionMaxBytes := s.sectionMaxBytes()
	tasks := make([]noteTask, 0, len(livePaths))
	totalWork := 0
	for _, path := range livePaths {
		noteID := embeddings.NoteID(path)
		info := infoByPath[path]
		sections := byPath[path]
		prevChunkHashes := chunkHashesByID[noteID]
		if prevChunkHashes == nil {
			prevChunkHashes, err = observeNoteMetric(ctx, "noteplan.chunk_hash_lookup", func() (map[int]string, error) {
				return s.Index.ChunkHashes(ctx, noteID)
			})
			if err != nil {
				return NotePlan{}, fmt.Errorf("chunk hashes %s: %w", path, err)
			}
		}

		allChunks, err := observeNoteMetric(ctx, "noteplan.build_chunks", func() ([]embeddings.ChunkInput, error) {
			return buildSectionChunks(info.Title, sections, sectionMaxBytes), nil
		})
		if err != nil {
			return NotePlan{}, err
		}
		allChunkIdx := make([]int, 0, len(allChunks))
		for _, chunk := range allChunks {
			allChunkIdx = append(allChunkIdx, chunk.Index)
		}
		reuseChunks, reuseVecs, embedChunks, embedTexts, err := s.planChunkUpdates(ctx, noteID, allChunks, prevChunkHashes)
		if err != nil {
			return NotePlan{}, err
		}
		needsChunkCleanup := hasChunkStragglers(prevChunkHashes, allChunkIdx)
		if len(reuseChunks) == 0 && len(embedChunks) == 0 && !needsChunkCleanup {
			continue
		}
		totalWork += len(embedChunks)
		tasks = append(tasks, noteTask{
			id: noteID,
			payload: noteTaskPayload{
				info:        info,
				sections:    sections,
				allChunkIdx: allChunkIdx,
				reuseChunks: reuseChunks,
				reuseVecs:   reuseVecs,
				embedChunks: embedChunks,
				embedTexts:  embedTexts,
			},
		})
	}

	return NotePlan{
		tasks: tasks,
		state: notePlanState{
			ids:                liveIDs,
			deleteIDs:          deleteIDs,
			typedRawPrunePaths: typedPaths,
			incremental:        true,
		},
		TotalWork: totalWork,
	}, nil
}

func normalizeNotePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func (s *NoteSyncer) loadDocSectionsByPath(ctx context.Context, paths []string) (map[string][]codeanchor.IntelDocSection, error) {
	byPath := make(map[string][]codeanchor.IntelDocSection, len(paths))
	if lookup, ok := s.Intel.(noteIntelByPath); ok {
		for _, path := range paths {
			sections, err := observeNoteMetric(ctx, "noteplan.load_doc_sections_by_path", func() ([]codeanchor.IntelDocSection, error) {
				return lookup.IntelDocSectionsByPath(ctx, path)
			})
			if err != nil {
				return nil, fmt.Errorf("read intel doc sections for %s: %w", path, err)
			}
			byPath[path] = append(byPath[path], sections...)
		}
		return byPath, nil
	}

	s.progressf("Loading intel doc sections")
	sections, err := observeNoteMetric(ctx, "noteplan.load_doc_sections", func() ([]codeanchor.IntelDocSection, error) {
		return s.Intel.IntelDocSections(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("read intel doc sections: %w", err)
	}
	want := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		want[path] = struct{}{}
	}
	for _, section := range sections {
		path := filepath.ToSlash(section.Path)
		if _, ok := want[path]; ok {
			byPath[path] = append(byPath[path], section)
		}
	}
	return byPath, nil
}
