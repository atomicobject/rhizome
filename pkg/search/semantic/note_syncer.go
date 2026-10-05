package semantic

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
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NoteIntelSource provides access to intel doc sections for semantic note embedding sync.
type NoteIntelSource interface {
	IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error)
}

// NoteIntelMetaSource provides lightweight intel doc section metadata for fast planning.
type NoteIntelMetaSource interface {
	IntelDocSectionMetas(ctx context.Context) ([]codeanchor.IntelDocSectionMeta, error)
}

type noteIntelByPath interface {
	IntelDocSectionsByPath(ctx context.Context, path string) ([]codeanchor.IntelDocSection, error)
}

type noteChunkHashesBatch interface {
	ChunkHashesBatch(ctx context.Context, ids []embeddings.NoteID) (map[embeddings.NoteID]map[int]string, error)
}

// RawNoteEmbeddingEligibilitySource decides whether paths should receive raw
// authored-section embeddings. In ontology-ready repos this should return
// false for all ontology-owned notes so ontology-node chunks become the
// primary semantic note surface for typed and untyped notes.
type RawNoteEmbeddingEligibilitySource interface {
	RawNoteEmbeddingEligibility(ctx context.Context, paths []string) (RawNoteEmbeddingEligibility, error)
}

type RawNoteEmbeddingEligibility struct {
	RawEligible map[string]bool
	TypedPaths  []string
}

// NoteMetaWriteQueue is the note-side subset of the indexing queued writer.
type NoteMetaWriteQueue interface {
	SubmitNoteMeta(context.Context, embeddings.NoteFileInfo) error
	SubmitNoteChunkSync(context.Context, embeddings.NoteChunkSync) error
	SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	FlushAndWait(context.Context) error
}

// NoteSyncer keeps the note embeddings index in sync with intel doc sections.
//
// This uses the markdown intel parser as the single source of raw chunk
// boundaries. In ontology-ready runs, RawEligibility normally suppresses raw
// doc_section embeddings and ontology-node sync owns the primary note surface.
type NoteSyncer struct {
	Index          embeddings.Index
	Provider       embeddings.Provider
	ProviderInfo   embeddings.ProviderConfig
	Intel          NoteIntelSource
	NoteReader     obsidian.NoteReader
	BatchSize      int
	MaxConcurrent  int
	PlanConcurrent int
	// EmbedGate optionally limits aggregate EmbedTexts concurrency across syncers.
	EmbedGate     chan struct{}
	EmbeddingNode *SharedEmbeddingNode
	// NoteEmbedPacker configures staged cross-note request packing.
	NoteEmbedPacker *EmbedPackerOptions
	// MaxSectionBytes caps per-section chunk embedding text (0 uses default).
	MaxSectionBytes int
	OnProgress      func(format string, args ...any)
	// OnEmbedProgress reports embedding progress as completed/total chunks.
	OnEmbedProgress func(done, total int)

	// StaleNoteThreshold controls lazy pruning of notes not seen in current sync.
	// If the fraction of stale notes is below this threshold, they're preserved
	// to avoid re-embedding costs when switching branches. Default is 0.3 (30%).
	// Set to 0 for immediate pruning (old behavior).
	StaleNoteThreshold float64

	// ChunkWriter optionally persists intel chunk records prior to embedding.
	// When set, chunk ordering is recorded in the intel spine for determinism.
	ChunkWriter ChunkWriter

	// EmbeddingWriter optionally persists embeddings to the intel spine.
	// When set, embeddings are written keyed by chunk_id after embedding.
	EmbeddingWriter EmbeddingWriter

	// WriteQueue optionally routes note/index intel writes through a single writer lane.
	WriteQueue NoteMetaWriteQueue

	// RawEligibility controls which notes receive raw authored-section
	// embeddings. Nil means all notes remain raw-eligible, which is reserved
	// for ontology-unavailable indexing paths.
	RawEligibility RawNoteEmbeddingEligibilitySource

	setupMu                sync.Mutex
	schemaReady            bool
	metaDimsKnown          bool
	setupErr               error
	freshnessMu            sync.Mutex
	pendingSourceHighWater time.Time
}

const (
	defaultSectionMaxBytes = 12000
)

func observeNoteMetric[T any](ctx context.Context, name string, fn func() (T, error)) (T, error) {
	started := time.Now()
	value, err := fn()
	indexingperf.ObserveLatency(ctx, name, time.Since(started))
	return value, err
}

func observeNoteMetricErr(ctx context.Context, name string, fn func() error) error {
	started := time.Now()
	err := fn()
	indexingperf.ObserveLatency(ctx, name, time.Since(started))
	return err
}

// SyncPath incrementally syncs a single note's embeddings using the current intel doc sections.
// path must be repository/vault-root relative (slash-separated), matching intel_doc_sections.path.
func (s *NoteSyncer) SyncPath(ctx context.Context, path string) error {
	return s.SyncPaths(ctx, []string{path})
}

// Sync plans and embeds the raw note surface. In full indexing this is a
// compatibility path; ontology-ready note bodies are synced separately as
// ontology-node chunks.
func (s *NoteSyncer) Sync(ctx context.Context) error {
	plan, err := s.Plan(ctx)
	if err != nil {
		return err
	}
	return s.Embed(ctx, plan)
}

func (s *NoteSyncer) ensureReady(ctx context.Context) error {
	if s.Index == nil || s.Provider == nil {
		return errors.New("note syncer requires index and provider")
	}
	s.applyProviderCaps(ctx)
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupErr != nil {
		return s.setupErr
	}
	if !s.schemaReady {
		if err := s.Index.EnsureSchema(ctx); err != nil {
			s.setupErr = err
			return err
		}
		s.schemaReady = true
	}
	// Re-validate each call until the provider has reported its real dimensions.
	// Voyage learns dims from its first embedding response, so the first call may
	// pass Dimensions=0; we follow up once the value is known.
	if !s.metaDimsKnown {
		dims := s.Provider.Dimensions()
		if err := s.Index.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
			Provider:   s.ProviderInfo.Provider,
			Model:      s.ProviderInfo.Model,
			Dimensions: dims,
		}); err != nil {
			s.setupErr = err
			return err
		}
		if dims > 0 {
			s.metaDimsKnown = true
		}
	}
	return nil
}

// Plan prepares a note embedding sync and returns a plan for Embed.
func (s *NoteSyncer) Plan(ctx context.Context) (NotePlan, error) {
	if s.Index == nil || s.Provider == nil || s.Intel == nil {
		return NotePlan{}, errors.New("note syncer requires index, provider, and intel source")
	}
	s.applyProviderCaps(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var err error

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

	var sections []codeanchor.IntelDocSection
	var metaSections []codeanchor.IntelDocSectionMeta
	var byPath map[string][]codeanchor.IntelDocSection
	var metaByPath map[string][]codeanchor.IntelDocSectionMeta
	var latestUpdated int64
	changedPaths := make(map[string]bool) // Tracks notes with updated_at > lastSyncUnix
	usingMeta := false
	if metaSource, ok := s.Intel.(NoteIntelMetaSource); ok {
		s.progressf("Loading intel doc section metadata")
		metaSections, err = observeNoteMetric(ctx, "noteplan.load_doc_section_meta", func() ([]codeanchor.IntelDocSectionMeta, error) {
			return metaSource.IntelDocSectionMetas(ctx)
		})
		if err != nil {
			return NotePlan{}, fmt.Errorf("read intel doc sections: %w", err)
		}
		metaByPath = make(map[string][]codeanchor.IntelDocSectionMeta)
		for _, sec := range metaSections {
			path := filepath.ToSlash(sec.Path)
			if strings.TrimSpace(path) == "" {
				continue
			}
			if sec.UpdatedAt > latestUpdated {
				latestUpdated = sec.UpdatedAt
			}
			if sec.UpdatedAt > sourceHighWaterUnix {
				changedPaths[path] = true
			}
			metaByPath[path] = append(metaByPath[path], sec)
		}
		usingMeta = true
	} else {
		s.progressf("Loading intel doc sections")
		sections, err = observeNoteMetric(ctx, "noteplan.load_doc_sections", func() ([]codeanchor.IntelDocSection, error) {
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
			if sec.UpdatedAt > latestUpdated {
				latestUpdated = sec.UpdatedAt
			}
			if sec.UpdatedAt > sourceHighWaterUnix {
				changedPaths[path] = true
			}
			byPath[path] = append(byPath[path], sec)
		}
	}

	var paths []string
	if usingMeta {
		paths = make([]string, 0, len(metaByPath))
		for p := range metaByPath {
			paths = append(paths, p)
		}
	} else {
		paths = make([]string, 0, len(byPath))
		for p := range byPath {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	rawPaths, typedPaths, err := s.rawNoteEmbeddingPaths(ctx, paths)
	if err != nil {
		return NotePlan{}, err
	}
	paths = rawPaths
	indexingperf.AddCount(ctx, "noteplan.raw_eligible_paths", int64(len(rawPaths)))
	indexingperf.AddCount(ctx, "noteplan.raw_skipped_paths", int64(len(typedPaths)))

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
			sections, err = observeNoteMetric(ctx, "noteplan.load_doc_sections", func() ([]codeanchor.IntelDocSection, error) {
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
		noteTitles, err = observeNoteMetric(ctx, "noteplan.load_titles", func() (map[string]string, error) {
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
		hashes, err := observeNoteMetric(ctx, "noteplan.chunk_hashes_batch", func() (map[embeddings.NoteID]map[int]string, error) {
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

	type metaWrite struct {
		info embeddings.NoteFileInfo
		done chan error
	}

	metaBatch, hasMetaBatch := s.Index.(interface {
		UpsertNoteMetaBatch(ctx context.Context, infos []embeddings.NoteFileInfo) error
	})
	var metaCh chan metaWrite
	var metaWG sync.WaitGroup
	if hasMetaBatch && s.WriteQueue == nil {
		metaCh = make(chan metaWrite, 256)
		metaWG.Add(1)
		go func() {
			defer metaWG.Done()
			var batch []embeddings.NoteFileInfo
			var pending []chan error
			flush := func() error {
				if len(batch) == 0 {
					return nil
				}
				err := metaBatch.UpsertNoteMetaBatch(ctx, batch)
				for _, done := range pending {
					done <- err
				}
				batch = batch[:0]
				pending = pending[:0]
				return err
			}
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					_ = flush()
					return
				case item, ok := <-metaCh:
					if !ok {
						if err := flush(); err != nil {
							select {
							case errCh <- err:
							default:
							}
							cancel()
						}
						return
					}
					batch = append(batch, item.info)
					pending = append(pending, item.done)
					if len(batch) >= 200 {
						if err := flush(); err != nil {
							select {
							case errCh <- err:
							default:
							}
							cancel()
							return
						}
					}
				case <-ticker.C:
					if err := flush(); err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
				}
			}
		}()
	}
	submitNoteMeta := func(info embeddings.NoteFileInfo, ignoreErr bool) error {
		return observeNoteMetricErr(ctx, "noteplan.meta_submit", func() error {
			if s.WriteQueue != nil {
				submitStarted := time.Now()
				err := s.WriteQueue.SubmitNoteMeta(ctx, info)
				indexingperf.ObserveLatency(ctx, "noteplan.writeback.meta.submit_wait", time.Since(submitStarted))
				return err
			}
			if metaCh != nil {
				done := make(chan error, 1)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case metaCh <- metaWrite{info: info, done: done}:
				}
				return <-done
			}
			return s.Index.UpsertNoteMeta(ctx, info)
		})
	}

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
						prevChunkHashes, err = observeNoteMetric(ctx, "noteplan.chunk_hash_lookup", func() (map[int]string, error) {
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
						scts, err := observeNoteMetric(ctx, "noteplan.load_doc_sections_by_path", func() ([]codeanchor.IntelDocSection, error) {
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

					allChunks, err := observeNoteMetric(ctx, "noteplan.build_chunks", func() ([]embeddings.ChunkInput, error) {
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
					prevChunkHashes, err = observeNoteMetric(ctx, "noteplan.chunk_hash_lookup", func() (map[int]string, error) {
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

				allChunks, err := observeNoteMetric(ctx, "noteplan.build_chunks", func() ([]embeddings.ChunkInput, error) {
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

	if metaCh != nil {
		close(metaCh)
		metaWG.Wait()
	}
	if s.WriteQueue != nil {
		if err := observeNoteMetricErr(ctx, "noteplan.meta_flush_wait", func() error {
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

// Embed executes a prepared note sync plan.
func (s *NoteSyncer) Embed(ctx context.Context, plan NotePlan) error {
	return s.embed(ctx, plan, true)
}

func (s *NoteSyncer) EmbedWithoutSyncMark(ctx context.Context, plan NotePlan) error {
	return s.embed(ctx, plan, false)
}

func (s *NoteSyncer) MarkLastSync(ctx context.Context) error {
	if err := s.markPendingSourceHighWater(ctx); err != nil {
		return err
	}
	if generationCommitter, ok := s.Index.(interface {
		CommitSyncGeneration(context.Context) error
	}); ok {
		if err := generationCommitter.CommitSyncGeneration(ctx); err != nil {
			return err
		}
	}
	return s.Index.UpdateLastSync(ctx, time.Now())
}

func (s *NoteSyncer) recordPendingSourceHighWater(ts time.Time) {
	if ts.IsZero() || ts.Unix() <= 0 {
		return
	}
	s.freshnessMu.Lock()
	defer s.freshnessMu.Unlock()
	if s.pendingSourceHighWater.IsZero() || ts.After(s.pendingSourceHighWater) {
		s.pendingSourceHighWater = ts
	}
}

func (s *NoteSyncer) markPendingSourceHighWater(ctx context.Context) error {
	s.freshnessMu.Lock()
	ts := s.pendingSourceHighWater
	s.pendingSourceHighWater = time.Time{}
	s.freshnessMu.Unlock()
	if ts.IsZero() || ts.Unix() <= 0 {
		return nil
	}
	return s.Index.UpdateSourceHighWater(ctx, ts)
}

func (s *NoteSyncer) markPlanFreshness(ctx context.Context, plan NotePlan, updateLastSync bool) error {
	if updateLastSync {
		if !plan.state.sourceHighWater.IsZero() && plan.state.sourceHighWater.Unix() > 0 {
			if err := s.Index.UpdateSourceHighWater(ctx, plan.state.sourceHighWater); err != nil {
				return err
			}
		}
		return s.MarkLastSync(ctx)
	}
	s.recordPendingSourceHighWater(plan.state.sourceHighWater)
	return nil
}

func (s *NoteSyncer) embed(ctx context.Context, plan NotePlan, updateLastSync bool) error {
	if plan.skipEmbed {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Persist intel chunks prior to embedding if a writer is configured.
	if s.ChunkWriter != nil && len(plan.tasks) > 0 {
		// With a queued writer, note intel chunks and later embedding rows stay in
		// order on the same FIFO lane; flushing here only removes overlap.
		if err := s.persistNoteIntelChunks(ctx, plan.tasks); err != nil {
			return fmt.Errorf("persist note intel chunks: %w", err)
		}
	}

	lazyIndex, _ := s.Index.(embeddings.LazyPruningNoteIndex)

	if len(plan.tasks) == 0 {
		s.progressf("No changed notes to embed")
		if err := s.applyNotePlanCleanup(ctx, lazyIndex, plan.state); err != nil {
			return err
		}
		return s.markPlanFreshness(ctx, plan, updateLastSync)
	}

	if plan.TotalWork > 0 {
		s.progressf("Embedding %d changed notes (%d items)", len(plan.tasks), plan.TotalWork)
	} else {
		s.progressf("Applying note chunk updates for %d notes", len(plan.tasks))
	}
	if plan.TotalWork > 0 && s.OnEmbedProgress != nil {
		s.OnEmbedProgress(0, plan.TotalWork)
	}

	// Multi-note sync must batch intel embedding persistence; per-note writes
	// reintroduce the tiny-transaction regression in unified indexing.
	intelTarget := s.EmbeddingWriter
	if s.WriteQueue != nil {
		intelTarget = queueEmbeddingWriter{queue: s.WriteQueue}
	}
	intelWriter := newBatchedIntelEmbeddingWriter(ctx, intelTarget, intelEmbeddingBatchWriterOptions{
		Label:      "notes",
		OnError:    func(error) { cancel() },
		OnProgress: s.progressf,
	})
	if err := s.processTasksPipelined(ctx, plan, intelWriter); err != nil {
		if intelWriter != nil {
			_ = intelWriter.Close()
		}
		return err
	}
	if intelWriter != nil {
		if err := intelWriter.Close(); err != nil {
			return err
		}
	}
	if s.WriteQueue != nil {
		if err := observeNoteMetricErr(ctx, "noteembed.writeback.flush_wait", func() error {
			return s.WriteQueue.FlushAndWait(ctx)
		}); err != nil {
			return err
		}
	}

	if err := s.applyNotePlanCleanup(ctx, lazyIndex, plan.state); err != nil {
		return err
	}
	// Provider may have learned its output dimensions during this batch (Voyage
	// reports them with the first response). Re-run the validator so the meta
	// row reflects reality before the next sync cycle.
	_ = s.ensureReady(ctx)
	return s.markPlanFreshness(ctx, plan, updateLastSync)
}

func (s *NoteSyncer) applyNotePlanCleanup(ctx context.Context, lazyIndex embeddings.LazyPruningNoteIndex, state notePlanState) error {
	if err := s.pruneTypedRawNoteSurfaces(ctx, state.typedRawPrunePaths); err != nil {
		return err
	}
	if state.incremental {
		for _, id := range state.deleteIDs {
			if err := s.Index.DeleteNote(ctx, id); err != nil {
				return fmt.Errorf("delete note %s: %w", id, err)
			}
		}
		return nil
	}
	return s.pruneStaleNotes(ctx, lazyIndex, state.useLazyPruning, state.ids)
}

// pruneStaleNotes removes notes not seen in current sync, using lazy pruning if supported.
func (s *NoteSyncer) pruneStaleNotes(ctx context.Context, lazyIndex embeddings.LazyPruningNoteIndex, supportsLazy bool, ids []embeddings.NoteID) error {
	if supportsLazy {
		threshold := s.StaleNoteThreshold
		if threshold == 0 {
			threshold = 0.3 // Default: preserve notes if stale fraction < 30%
		}
		if pruned, err := lazyIndex.PruneStaleNotes(ctx, threshold); err != nil {
			return fmt.Errorf("prune stale notes: %w", err)
		} else if pruned > 0 {
			s.progressf("Pruned %d stale notes (above %.0f%% threshold)", pruned, threshold*100)
		}
	} else {
		if err := s.Index.DeleteNotesNotIn(ctx, ids); err != nil {
			return fmt.Errorf("delete removed notes: %w", err)
		}
	}
	return nil
}

func (s *NoteSyncer) rawNoteEmbeddingPaths(ctx context.Context, paths []string) ([]string, []string, error) {
	paths = normalizeNotePaths(paths)
	if len(paths) == 0 {
		return nil, nil, nil
	}
	if s.RawEligibility == nil {
		return paths, nil, nil
	}
	eligibility, err := s.RawEligibility.RawNoteEmbeddingEligibility(ctx, paths)
	if err != nil {
		return nil, nil, fmt.Errorf("load raw note embedding eligibility: %w", err)
	}
	typedSet := stringSet(eligibility.TypedPaths)
	raw := make([]string, 0, len(paths))
	typed := make([]string, 0, len(eligibility.TypedPaths))
	for _, path := range paths {
		if _, ok := typedSet[path]; ok {
			typed = append(typed, path)
			continue
		}
		if eligible, ok := eligibility.RawEligible[path]; ok && !eligible {
			typed = append(typed, path)
			continue
		}
		raw = append(raw, path)
	}
	return raw, normalizeNotePaths(typed), nil
}

func (s *NoteSyncer) pruneTypedRawNoteSurfaces(ctx context.Context, paths []string) error {
	paths = normalizeNotePaths(paths)
	if len(paths) == 0 {
		return nil
	}
	for _, path := range paths {
		if err := s.Index.DeleteNote(ctx, embeddings.NoteID(path)); err != nil {
			return fmt.Errorf("delete raw typed-note embeddings %s: %w", path, err)
		}
	}
	if s.ChunkWriter == nil {
		indexingperf.AddCount(ctx, "noteplan.raw_chunks_pruned", 0)
		return nil
	}
	byPath, err := s.loadDocSectionsByPath(ctx, paths)
	if err != nil {
		return err
	}
	ownerIDs := make([]string, 0)
	for _, path := range paths {
		for _, section := range byPath[path] {
			if strings.TrimSpace(section.SectionID) == "" {
				continue
			}
			ownerIDs = append(ownerIDs, section.SectionID)
		}
	}
	ownerIDs = normalizeNotePaths(ownerIDs)
	indexingperf.AddCount(ctx, "noteplan.raw_chunks_pruned", int64(len(ownerIDs)))
	if len(ownerIDs) == 0 {
		return nil
	}
	if s.WriteQueue != nil {
		if err := s.WriteQueue.SubmitIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, nil); err != nil {
			return err
		}
		return s.WriteQueue.FlushAndWait(ctx)
	}
	if familyWriter, ok := s.ChunkWriter.(ChunkFamilyWriter); ok {
		return familyWriter.ReplaceIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, nil)
	}
	return s.ChunkWriter.ReplaceIntelChunks(ctx, ownerIDs, nil)
}

func sameNoteSet(existing []embeddings.NoteFileInfo, paths []string) bool {
	if len(existing) != len(paths) {
		return false
	}
	have := make(map[string]struct{}, len(existing))
	for _, n := range existing {
		if string(n.ID) == "" {
			return false
		}
		have[string(n.ID)] = struct{}{}
	}
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			return false
		}
		if _, ok := have[p]; !ok {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func buildNoteFileInfo(id embeddings.NoteID, path string, sections []codeanchor.IntelDocSection, titles map[string]string) embeddings.NoteFileInfo {
	maxUpdated := int64(0)
	size := int64(0)
	for _, sec := range sections {
		if sec.UpdatedAt > maxUpdated {
			maxUpdated = sec.UpdatedAt
		}
		size += int64(len(sec.Content))
	}
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if t := titles[path]; t != "" {
		title = t
	}
	return embeddings.NoteFileInfo{
		ID:    id,
		Path:  path,
		Title: title,
		Size:  size,
		Mtime: time.Unix(maxUpdated, 0),
	}
}

func buildNoteFileInfoFromMeta(id embeddings.NoteID, path string, sections []codeanchor.IntelDocSectionMeta, titles map[string]string) embeddings.NoteFileInfo {
	maxUpdated := int64(0)
	size := int64(0)
	for _, sec := range sections {
		if sec.UpdatedAt > maxUpdated {
			maxUpdated = sec.UpdatedAt
		}
		if sec.EndByte > size {
			size = sec.EndByte
		}
	}
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if t := titles[path]; t != "" {
		title = t
	}
	return embeddings.NoteFileInfo{
		ID:    id,
		Path:  path,
		Title: title,
		Size:  size,
		Mtime: time.Unix(maxUpdated, 0),
	}
}

func buildSectionChunks(noteTitle string, sections []codeanchor.IntelDocSection, maxBytes int) []embeddings.ChunkInput {
	// Build hierarchical breadcrumbs based on heading levels.
	type levelTitle struct {
		level int64
		title string
	}
	var stack []levelTitle

	out := make([]embeddings.ChunkInput, 0, len(sections))
	for idx, sec := range sections {
		title := strings.TrimSpace(sec.Title)
		level := sec.Level
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		if title != "" {
			stack = append(stack, levelTitle{level: level, title: title})
		}

		breadcrumbParts := make([]string, 0, 1+len(stack))
		if strings.TrimSpace(noteTitle) != "" {
			breadcrumbParts = append(breadcrumbParts, strings.TrimSpace(noteTitle))
		}
		for _, lt := range stack {
			if lt.title != "" {
				breadcrumbParts = append(breadcrumbParts, lt.title)
			}
		}
		breadcrumb := strings.Join(breadcrumbParts, " > ")

		text := buildChunkText(noteTitle, title, sec.Content, maxBytes)
		out = append(out, embeddings.NewChunkInput(idx, text, breadcrumb, title))
	}
	return out
}

func buildChunkText(noteTitle, heading, body string, maxBytes int) string {
	var b strings.Builder
	if strings.TrimSpace(noteTitle) != "" {
		b.WriteString("# ")
		b.WriteString(strings.TrimSpace(noteTitle))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(heading) != "" {
		b.WriteString("## ")
		b.WriteString(strings.TrimSpace(heading))
		b.WriteString("\n\n")
	}
	body = strings.TrimSpace(body)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	text := b.String()
	if maxBytes <= 0 {
		maxBytes = defaultSectionMaxBytes
	}
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}
	return strings.TrimSpace(text)
}

func hasChunkStragglers(prev map[int]string, desired []int) bool {
	if len(prev) == 0 {
		return false
	}
	if len(prev) != len(desired) {
		// Quick signal: any mismatch in count means we likely have stale or missing chunk rows.
		return true
	}
	desiredSet := make(map[int]struct{}, len(desired))
	for _, idx := range desired {
		desiredSet[idx] = struct{}{}
	}
	for idx := range prev {
		if _, ok := desiredSet[idx]; !ok {
			return true
		}
	}
	return false
}

func (s *NoteSyncer) planChunkUpdates(ctx context.Context, noteID embeddings.NoteID, desired []embeddings.ChunkInput, prevHashes map[int]string) ([]embeddings.ChunkInput, []embeddings.Embedding, []embeddings.ChunkInput, []string, error) {
	indexingperf.AddCount(ctx, "noteplan.chunks.desired", int64(len(desired)))
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "noteplan.plan_chunk_updates", time.Since(started))
	}()
	// Fast-path: if all desired indices match hashes and we don't need to update breadcrumbs/headings,
	// we can skip loading stored embeddings (expensive) and return no work.
	allMatch := true
	for _, ch := range desired {
		if prevHashes[ch.Index] != ch.Hash {
			allMatch = false
			break
		}
	}
	if allMatch {
		indexingperf.AddCount(ctx, "noteplan.reuse.same_position", int64(len(desired)))
		// Without loading stored chunks, we conservatively assume metadata is stable.
		return nil, nil, nil, nil, nil
	}

	storedChunks, err := observeNoteMetric(ctx, "noteplan.load_stored_chunks", func() ([]embeddings.StoredChunk, error) {
		return s.Index.NoteChunks(ctx, noteID)
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load stored chunks %s: %w", noteID, err)
	}
	byIndex := make(map[int]embeddings.StoredChunk, len(storedChunks))
	for _, ch := range storedChunks {
		byIndex[ch.Index] = ch
	}

	// Build a best-effort hash -> embedding map so we can reuse embeddings when chunk indices shift.
	embByHash := make(map[string]embeddings.Embedding, len(prevHashes))
	for idx, hash := range prevHashes {
		if hash == "" {
			continue
		}
		if _, ok := embByHash[hash]; ok {
			continue
		}
		if stored, ok := byIndex[idx]; ok && len(stored.Embedding) > 0 {
			embByHash[hash] = stored.Embedding
		}
	}
	for _, ch := range desired {
		if _, ok := embByHash[ch.Hash]; ok {
			continue
		}
		cacheLookupStarted := time.Now()
		cached, ok, err := s.Index.EmbeddingByHash(ctx, ch.Hash)
		indexingperf.ObserveLatency(ctx, "noteplan.cache_lookup", time.Since(cacheLookupStarted))
		if err == nil && ok && len(cached) > 0 {
			indexingperf.AddCount(ctx, "noteplan.hash_cache.hit", 1)
			embByHash[ch.Hash] = cached
		} else if err != nil {
			indexingperf.AddCount(ctx, "noteplan.hash_cache.error", 1)
			return nil, nil, nil, nil, err
		} else {
			indexingperf.AddCount(ctx, "noteplan.hash_cache.miss", 1)
		}
	}

	var reuseChunks []embeddings.ChunkInput
	var reuseVecs []embeddings.Embedding
	var embedChunks []embeddings.ChunkInput
	var embedTexts []string

	for _, want := range desired {
		prevHash := prevHashes[want.Index]
		if prevHash == want.Hash {
			// Hash matches at this index. If the stored metadata differs, rewrite without re-embedding.
			if stored, ok := byIndex[want.Index]; ok && len(stored.Embedding) > 0 {
				indexingperf.AddCount(ctx, "noteplan.reuse.same_position", 1)
				if stored.Breadcrumb != want.Breadcrumb || stored.Heading != want.Heading {
					indexingperf.AddCount(ctx, "noteplan.reuse.metadata_rewrite", 1)
					reuseChunks = append(reuseChunks, stripChunkText(want))
					reuseVecs = append(reuseVecs, stored.Embedding)
				}
				continue
			}
			// Hash row exists but we couldn't load an embedding (dims mismatch or missing row) – re-embed.
			indexingperf.AddCount(ctx, "noteplan.embed.reason.missing_vector", 1)
			embedChunks = append(embedChunks, want)
			embedTexts = append(embedTexts, want.Text)
			continue
		}

		// Index/hash mismatch: try to reuse an existing embedding by hash (supports reordering/inserts).
		if vec := embByHash[want.Hash]; len(vec) > 0 {
			indexingperf.AddCount(ctx, "noteplan.reuse.content_hash", 1)
			reuseChunks = append(reuseChunks, stripChunkText(want))
			reuseVecs = append(reuseVecs, vec)
			continue
		}

		indexingperf.AddCount(ctx, "noteplan.embed.reason.new_hash", 1)
		embedChunks = append(embedChunks, want)
		embedTexts = append(embedTexts, want.Text)
	}

	indexingperf.AddCount(ctx, "noteplan.chunks.rewrite", int64(len(reuseChunks)))
	indexingperf.AddCount(ctx, "noteplan.chunks.embed", int64(len(embedChunks)))
	return reuseChunks, reuseVecs, embedChunks, embedTexts, nil
}

func stripChunkText(in embeddings.ChunkInput) embeddings.ChunkInput {
	in.Text = ""
	return in
}

func (s *NoteSyncer) sectionMaxBytes() int {
	if s.MaxSectionBytes > 0 {
		return s.MaxSectionBytes
	}
	return defaultSectionMaxBytes
}

func (s *NoteSyncer) planConcurrent() int {
	if s.PlanConcurrent > 0 {
		return s.PlanConcurrent
	}
	return defaultPlanConcurrency()
}

func (s *NoteSyncer) maxConcurrent() int {
	if s.MaxConcurrent > 0 {
		return s.MaxConcurrent
	}
	return EffectiveMaxConcurrent(s.Provider, 0)
}

func (s *NoteSyncer) progressf(format string, args ...any) {
	if s.OnProgress != nil {
		s.OnProgress(format, args...)
	}
}

func (s *NoteSyncer) applyProviderCaps(ctx context.Context) {
	if s.Provider == nil {
		return
	}
	if s.MaxSectionBytes > 0 {
		return
	}
	ctxProvider, ok := s.Provider.(embeddings.ContextTokenProvider)
	if !ok {
		return
	}
	tokens, err := ctxProvider.ContextTokens(ctx)
	if err != nil && tokens == 0 {
		s.progressf("Ollama context probe failed: %v", err)
		return
	}
	if tokens <= 0 {
		return
	}
	capBytes := tokensToBytes(tokens)
	sectionMax := min(defaultSectionMaxBytes, capBytes)
	if s.MaxSectionBytes == 0 {
		s.MaxSectionBytes = sectionMax
	}
	if err != nil {
		s.progressf("Ollama context probe failed (%v); using fallback %d tokens", err, tokens)
	}
}

func tokensToBytes(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	const tokenByteMultiplier = 3
	if tokens > (int(^uint(0)>>1))/tokenByteMultiplier {
		return int(^uint(0) >> 1)
	}
	return tokens * tokenByteMultiplier
}

func buildIntelChunksForSections(noteTitle string, sections []codeanchor.IntelDocSection, now int64) (ownerIDs []string, chunks []codeanchor.IntelChunk) {
	if len(sections) == 0 {
		return nil, nil
	}
	// Keep ordering stable and consistent with embedding chunk indices.
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].StartByte == sections[j].StartByte {
			return sections[i].SectionID < sections[j].SectionID
		}
		return sections[i].StartByte < sections[j].StartByte
	})
	desired := buildSectionChunks(noteTitle, sections, 0)
	ownerIDs = make([]string, 0, len(sections))
	chunks = make([]codeanchor.IntelChunk, 0, len(sections))
	for i, sec := range sections {
		secID := strings.TrimSpace(sec.SectionID)
		if secID == "" {
			continue
		}
		ownerIDs = append(ownerIDs, secID)
		var breadcrumb, heading, hash string
		if i >= 0 && i < len(desired) {
			breadcrumb = desired[i].Breadcrumb
			heading = desired[i].Heading
			hash = desired[i].Hash
		}
		if hash == "" {
			hash = strings.TrimSpace(sec.Fingerprint)
		}
		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID:     codeanchor.IntelChunkID(secID, 0, "section"),
			OwnerID:     secID,
			OwnerType:   "doc_section",
			ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection,
			Ord:         0,
			Granularity: "section",
			Breadcrumb:  breadcrumb,
			Heading:     heading,
			ContentHash: hash,
			StartByte:   sec.StartByte,
			EndByte:     sec.EndByte,
			UpdatedAt:   now,
		})
	}
	return ownerIDs, chunks
}

// persistNoteIntelChunks records one deterministic chunk per doc section for notes being synced.
func (s *NoteSyncer) persistNoteIntelChunks(ctx context.Context, tasks []noteTask) error {
	if s.ChunkWriter == nil {
		return nil
	}

	now := time.Now().Unix()
	ownerSet := make(map[string]struct{}, 256)
	var ownerIDs []string
	var chunks []codeanchor.IntelChunk
	for _, task := range tasks {
		ids, cs := buildIntelChunksForSections(task.payload.info.Title, task.payload.sections, now)
		for _, id := range ids {
			if _, ok := ownerSet[id]; ok {
				continue
			}
			ownerSet[id] = struct{}{}
			ownerIDs = append(ownerIDs, id)
		}
		chunks = append(chunks, cs...)
	}
	if s.WriteQueue != nil {
		return s.WriteQueue.SubmitIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, chunks)
	}
	if familyWriter, ok := s.ChunkWriter.(ChunkFamilyWriter); ok {
		return familyWriter.ReplaceIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, chunks)
	}
	return s.ChunkWriter.ReplaceIntelChunks(ctx, ownerIDs, chunks)
}
