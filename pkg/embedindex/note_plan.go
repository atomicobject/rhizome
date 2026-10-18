package embedindex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// Plan prepares a note embedding sync and returns a plan for Embed.
func (s *NoteSyncer) Plan(ctx context.Context) (NotePlan, error) {
	if s.Index == nil || s.Provider == nil || s.Intel == nil {
		return NotePlan{}, errors.New("note syncer requires index, provider, and intel source")
	}
	s.applyProviderCaps(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := s.ensureReady(ctx); err != nil {
		return NotePlan{}, err
	}

	// SourceHighWater is the durable freshness marker. LastSync is diagnostic
	// wall-clock data and must not drive skip decisions.
	var sourceHighWaterUnix int64
	if meta, ok, _ := s.Index.Metadata(ctx); ok && !meta.SourceHighWater.IsZero() {
		sourceHighWaterUnix = meta.SourceHighWater.Unix()
	}

	// If index supports lazy pruning, increment generation before upserting notes.
	// This marks all notes upserted in this sync with the new generation.
	// We'll decide later whether to use lazy pruning based on how many notes changed.
	lazyIndex, supportsLazy := s.Index.(embeddings.LazyPruningNoteIndex)

	sources, err := s.loadNotePlanSources(ctx, sourceHighWaterUnix)
	if err != nil {
		return NotePlan{}, err
	}
	byPath, metaByPath := sources.byPath, sources.metaByPath
	latestUpdated, changedPaths := sources.latestUpdated, sources.changedPaths
	usingMeta := sources.usingMeta
	paths, typedPaths := sources.paths, sources.typedPaths
	var sections []codeanchor.IntelDocSection

	existing, err := s.Index.ListNotes(ctx)
	if err != nil {
		return NotePlan{}, err
	}

	// Fast-path only when the durable embedding set exactly matches the current
	// source keep set. Stale extras must reach zero-task cleanup so full recovery
	// can prune embeddings for sources that can no longer be rediscovered.
	if meta, ok, _ := s.Index.Metadata(ctx); ok && !meta.SourceHighWater.IsZero() && latestUpdated > 0 {
		sourceFresh := meta.SourceHighWater.Unix() >= latestUpdated
		noteSetMatch := sameNoteSet(existing, paths)
		if sourceFresh && len(typedPaths) == 0 && noteSetMatch {
			s.progressf("No changed notes to embed")
			_ = s.Index.UpdateLastSync(ctx, time.Now())
			return NotePlan{skipEmbed: true}, nil
		}
	}

	// Decide on pruning strategy based on how many notes changed.
	// If <30% changed, use explicit set comparison (faster for incremental syncs).
	// Otherwise, use lazy pruning which requires updating metadata for all notes.
	useLazyPruning := false
	changeRatio := float64(len(changedPaths)) / float64(max(len(paths), 1))
	if supportsLazy && changeRatio >= 0.3 {
		useLazyPruning = true
		if _, err := lazyIndex.IncrementSyncGeneration(ctx); err != nil {
			return NotePlan{}, fmt.Errorf("increment sync generation: %w", err)
		}
	}

	if usingMeta {
		if _, ok := s.Intel.(noteIntelByPath); !ok {
			s.progressf("Loading intel doc sections")
			sections, err = observeNoteMetric(ctx, indexingperf.NoteplanLoadDocSections, func() ([]codeanchor.IntelDocSection, error) {
				return s.Intel.IntelDocSections(ctx)
			})
			if err != nil {
				return NotePlan{}, fmt.Errorf("read intel doc sections: %w", err)
			}
			byPath = make(map[string][]codeanchor.IntelDocSection)
			for _, sec := range sections {
				path := filepath.ToSlash(sec.Path)
				if strings.TrimSpace(path) == "" {
					continue
				}
				byPath[path] = append(byPath[path], sec)
			}
			usingMeta = false
		}
	}

	// For note metadata (title), prefer the NoteReader when available so
	// collection vaults stay consistent with agent/file-context output.
	var noteTitles map[string]string
	if s.NoteReader != nil {
		noteTitles, err = observeNoteMetric(ctx, indexingperf.NoteplanLoadTitles, func() (map[string]string, error) {
			titles := make(map[string]string, len(paths))
			for _, p := range paths {
				if title, ok := s.NoteReader.Title(p); ok {
					titles[p] = title
				}
			}
			return titles, nil
		})
		if err != nil {
			return NotePlan{}, err
		}
	}

	ids := make([]embeddings.NoteID, 0, len(paths))
	for _, path := range paths {
		ids = append(ids, embeddings.NoteID(path))
	}

	loadStart := time.Now()
	var chunkHashesByID map[embeddings.NoteID]map[int]string
	if batcher, ok := s.Index.(noteChunkHashesBatch); ok {
		hashes, err := observeNoteMetric(ctx, indexingperf.NoteplanChunkHashesBatch, func() (map[embeddings.NoteID]map[int]string, error) {
			return batcher.ChunkHashesBatch(ctx, ids)
		})
		if err != nil {
			return NotePlan{}, err
		}
		chunkHashesByID = hashes
	}
	if s.OnProgress != nil {
		s.progressf("Loaded note embedding states in %s", time.Since(loadStart).Truncate(time.Millisecond))
	}

	var totalWork atomic.Int64
	var plannedNotes atomic.Int64
	errCh := make(chan error, 1)

	submitNoteMeta, finishMeta := s.planMetadataWrites(ctx, cancel, errCh)

	var tasks []noteTask
	var tasksMu sync.Mutex

	workerCount := s.planConcurrent()
	if workerCount > len(paths) {
		workerCount = len(paths)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	buildProgressDone := make(chan struct{})
	if s.OnProgress != nil {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-buildProgressDone:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.progressf("Planned %d/%d notes (%d items)", plannedNotes.Load(), len(paths), totalWork.Load())
				}
			}
		}()
	}

	pathCh := make(chan string, workerCount*2)
	var buildWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		buildWG.Add(1)
		go func() {
			defer buildWG.Done()
			for path := range pathCh {
				var err error
				select {
				case <-ctx.Done():
					return
				default:
				}

				noteID := embeddings.NoteID(path)
				noteChanged := changedPaths[path]

				if usingMeta {
					noteMetas := metaByPath[path]
					sort.Slice(noteMetas, func(i, j int) bool {
						if noteMetas[i].StartByte == noteMetas[j].StartByte {
							return noteMetas[i].SectionID < noteMetas[j].SectionID
						}
						return noteMetas[i].StartByte < noteMetas[j].StartByte
					})

					info := buildNoteFileInfoFromMeta(noteID, path, noteMetas, noteTitles)

					// For unchanged notes: skip if not using lazy pruning (no sync_generation update needed).
					// When using lazy pruning, we still need to update metadata for ALL notes.
					if !noteChanged {
						if useLazyPruning {
							// Update metadata to preserve sync_generation for lazy pruning.
							_ = submitNoteMeta(info, true)
						}
						plannedNotes.Add(1)
						continue
					}

					if err := submitNoteMeta(info, false); err != nil {
						select {
						case errCh <- fmt.Errorf("upsert meta %s: %w", path, err):
						default:
						}
						cancel()
						return
					}

					sectionMaxBytes := s.sectionMaxBytes()

					var prevChunkHashes map[int]string
					if chunkHashesByID != nil {
						prevChunkHashes = chunkHashesByID[noteID]
					} else {
						prevChunkHashes, err = observeNoteMetric(ctx, indexingperf.NoteplanChunkHashLookup, func() (map[int]string, error) {
							return s.Index.ChunkHashes(ctx, noteID)
						})
						if err != nil {
							select {
							case errCh <- fmt.Errorf("chunk hashes %s: %w", path, err):
							default:
							}
							cancel()
							return
						}
					}

					var noteSections []codeanchor.IntelDocSection
					if byPath != nil {
						noteSections = byPath[path]
					} else if byPathLookup, ok := s.Intel.(noteIntelByPath); ok {
						scts, err := observeNoteMetric(ctx, indexingperf.NoteplanLoadDocSectionsByPath, func() ([]codeanchor.IntelDocSection, error) {
							return byPathLookup.IntelDocSectionsByPath(ctx, path)
						})
						if err != nil {
							select {
							case errCh <- fmt.Errorf("read intel doc sections %s: %w", path, err):
							default:
							}
							cancel()
							return
						}
						noteSections = scts
					} else {
						select {
						case errCh <- fmt.Errorf("read intel doc sections %s: missing intel doc section loader", path):
						default:
						}
						cancel()
						return
					}

					sort.Slice(noteSections, func(i, j int) bool {
						if noteSections[i].StartByte == noteSections[j].StartByte {
							return noteSections[i].SectionID < noteSections[j].SectionID
						}
						return noteSections[i].StartByte < noteSections[j].StartByte
					})

					allChunks, err := observeNoteMetric(ctx, indexingperf.NoteplanBuildChunks, func() ([]embeddings.ChunkInput, error) {
						return buildSectionChunks(info.Title, noteSections, sectionMaxBytes), nil
					})
					if err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
					allChunkIdx := make([]int, 0, len(allChunks))
					for _, ch := range allChunks {
						allChunkIdx = append(allChunkIdx, ch.Index)
					}

					reuseChunks, reuseVecs, embedChunks, embedTexts, err := s.planChunkUpdates(ctx, noteID, allChunks, prevChunkHashes)
					if err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}

					needsChunkCleanup := hasChunkStragglers(prevChunkHashes, allChunkIdx)
					if len(reuseChunks) == 0 && len(embedChunks) == 0 && !needsChunkCleanup {
						plannedNotes.Add(1)
						continue
					}

					work := len(embedChunks)
					plannedNotes.Add(1)
					totalWork.Add(int64(work))

					task := noteTask{
						id: noteID,
						payload: noteTaskPayload{
							info:        info,
							sections:    noteSections,
							allChunkIdx: allChunkIdx,
							reuseChunks: reuseChunks,
							reuseVecs:   reuseVecs,
							embedChunks: embedChunks,
							embedTexts:  embedTexts,
						},
					}
					tasksMu.Lock()
					tasks = append(tasks, task)
					tasksMu.Unlock()
					continue
				}
				noteSections := byPath[path]
				sort.Slice(noteSections, func(i, j int) bool {
					if noteSections[i].StartByte == noteSections[j].StartByte {
						return noteSections[i].SectionID < noteSections[j].SectionID
					}
					return noteSections[i].StartByte < noteSections[j].StartByte
				})

				info := buildNoteFileInfo(noteID, path, noteSections, noteTitles)
				if err := submitNoteMeta(info, false); err != nil {
					select {
					case errCh <- fmt.Errorf("upsert meta %s: %w", path, err):
					default:
					}
					cancel()
					return
				}

				sectionMaxBytes := s.sectionMaxBytes()

				var prevChunkHashes map[int]string
				if chunkHashesByID != nil {
					prevChunkHashes = chunkHashesByID[noteID]
				} else {
					prevChunkHashes, err = observeNoteMetric(ctx, indexingperf.NoteplanChunkHashLookup, func() (map[int]string, error) {
						return s.Index.ChunkHashes(ctx, noteID)
					})
					if err != nil {
						select {
						case errCh <- fmt.Errorf("chunk hashes %s: %w", path, err):
						default:
						}
						cancel()
						return
					}
				}

				allChunks, err := observeNoteMetric(ctx, indexingperf.NoteplanBuildChunks, func() ([]embeddings.ChunkInput, error) {
					return buildSectionChunks(info.Title, noteSections, sectionMaxBytes), nil
				})
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
				allChunkIdx := make([]int, 0, len(allChunks))
				for _, ch := range allChunks {
					allChunkIdx = append(allChunkIdx, ch.Index)
				}

				reuseChunks, reuseVecs, embedChunks, embedTexts, err := s.planChunkUpdates(ctx, noteID, allChunks, prevChunkHashes)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}

				needsChunkCleanup := hasChunkStragglers(prevChunkHashes, allChunkIdx)
				if len(reuseChunks) == 0 && len(embedChunks) == 0 && !needsChunkCleanup {
					plannedNotes.Add(1)
					continue
				}

				work := len(embedChunks)
				plannedNotes.Add(1)
				totalWork.Add(int64(work))

				task := noteTask{
					id: noteID,
					payload: noteTaskPayload{
						info:        info,
						sections:    noteSections,
						allChunkIdx: allChunkIdx,
						reuseChunks: reuseChunks,
						reuseVecs:   reuseVecs,
						embedChunks: embedChunks,
						embedTexts:  embedTexts,
					},
				}
				tasksMu.Lock()
				tasks = append(tasks, task)
				tasksMu.Unlock()
			}
		}()
	}

	go func() {
		defer close(pathCh)
		for _, path := range paths {
			select {
			case <-ctx.Done():
				return
			case pathCh <- path:
			}
		}
	}()
	buildWG.Wait()
	close(buildProgressDone)

	finishMeta()
	if s.WriteQueue != nil {
		if err := observeNoteMetricErr(ctx, indexingperf.NoteplanMetaFlushWait, func() error {
			return s.WriteQueue.FlushAndWait(ctx)
		}); err != nil {
			return NotePlan{}, err
		}
	}
	select {
	case err := <-errCh:
		if err != nil {
			return NotePlan{}, err
		}
	default:
	}

	return NotePlan{
		tasks: tasks,
		state: notePlanState{
			ids:                ids,
			typedRawPrunePaths: typedPaths,
			useLazyPruning:     useLazyPruning,
			sourceHighWater:    time.Unix(latestUpdated, 0),
		},
		TotalWork: int(totalWork.Load()),
	}, nil
}
