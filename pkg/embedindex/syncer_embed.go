package embedindex

// Docs:
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"context"
	"errors"
	"fmt"

	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// Embed executes a prepared sync plan.
func (s *Syncer) Embed(ctx context.Context, plan SyncPlan) error {
	return s.embed(ctx, plan, true)
}

func (s *Syncer) EmbedWithoutSyncMark(ctx context.Context, plan SyncPlan) error {
	return s.embed(ctx, plan, false)
}

func (s *Syncer) embed(ctx context.Context, plan SyncPlan, updateLastSync bool) error {
	if plan.skipEmbed {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Persist intel chunks prior to embedding if a writer is configured.
	if s.ChunkWriter != nil && len(plan.tasks) > 0 {
		// With a queued writer, chunk rows and subsequent embedding rows share the
		// same FIFO lane, so an eager FlushAndWait here only serializes overlap.
		if err := s.persistIntelChunks(ctx, plan.tasks); err != nil {
			return fmt.Errorf("persist intel chunks: %w", err)
		}
	}

	itemWriter := func(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
		if s.WriteQueue != nil {
			return s.WriteQueue.SubmitCodeItemEmbedding(ctx, codeindex.ItemEmbeddingUpsert{
				AnchorID:  anchorID,
				Hash:      hash,
				Embedding: vec,
			})
		}
		return s.Index.UpsertItemEmbedding(ctx, anchorID, hash, vec)
	}
	// High-volume indexing writes must batch intel spine persistence; per-item
	// UpsertEmbeddings calls turn one sync pass into thousands of tiny writes.
	intelTarget := s.EmbeddingWriter
	if s.WriteQueue != nil {
		intelTarget = queueEmbeddingWriter{queue: s.WriteQueue}
	}
	var writebackBatcher *codeWritebackBatcher
	if s.WriteQueue != nil {
		writebackBatcher = newCodeWritebackBatcher(ctx, s.WriteQueue, s.WriteQueue.CodeWritebackBatchConfig())
		itemWriter = func(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
			return writebackBatcher.SubmitCodeItemEmbedding(ctx, anchorID, hash, vec)
		}
	}
	var intelWriter intelEmbeddingSubmitter
	if intelTarget != nil && writebackBatcher == nil {
		if s.WriteQueue != nil {
			intelWriter = directIntelEmbeddingSubmitter{ctx: ctx, writer: intelTarget}
		} else {
			intelWriter = newBatchedIntelEmbeddingWriter(ctx, intelTarget, intelEmbeddingBatchWriterOptions{
				Label:      "code",
				OnError:    func(error) { cancel() },
				OnProgress: s.progressf,
			})
		}
	}
	closeIntelWriter := func() error {
		if intelWriter == nil {
			return nil
		}
		return intelWriter.Close()
	}
	drainWriteQueue := func() error {
		if writebackBatcher != nil {
			if err := writebackBatcher.Close(); err != nil {
				return fmt.Errorf("close code writeback batcher: %w", err)
			}
			writebackBatcher = nil
		}
		if s.WriteQueue != nil {
			if err := s.WriteQueue.FlushAndWait(context.WithoutCancel(ctx)); err != nil {
				return fmt.Errorf("flush code write queue: %w", err)
			}
		}
		return nil
	}
	batchErrCh := make(chan error, 1)
	var writeCh chan itemEmbedWrite
	var writeWG sync.WaitGroup
	hasBatchWriter := false
	if s.WriteQueue == nil {
		if batcher, ok := s.Index.(interface {
			UpsertItemEmbeddingBatch(ctx context.Context, items []codeindex.ItemEmbeddingUpsert) error
		}); ok {
			writeCh = make(chan itemEmbedWrite, itemEmbeddingBatchSize*2)
			writeWG.Add(1)
			hasBatchWriter = true
			go func() {
				defer writeWG.Done()
				var batch []codeindex.ItemEmbeddingUpsert
				flush := func() {
					if len(batch) == 0 {
						return
					}
					if err := batcher.UpsertItemEmbeddingBatch(ctx, batch); err != nil {
						select {
						case batchErrCh <- err:
						default:
						}
						cancel()
					}
					batch = batch[:0]
				}
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						flush()
						return
					case item, ok := <-writeCh:
						if !ok {
							flush()
							return
						}
						if len(item.vec) == 0 {
							continue
						}
						batch = append(batch, codeindex.ItemEmbeddingUpsert{AnchorID: item.id, Hash: item.hash, Embedding: item.vec})
						if len(batch) >= itemEmbeddingBatchSize {
							flush()
						}
					case <-ticker.C:
						flush()
					}
				}
			}()

			itemWriter = func(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case writeCh <- itemEmbedWrite{id: anchorID, hash: hash, vec: vec}:
					return nil
				}
			}
		}
	}

	totalWork := plan.TotalWork
	totalTasks := len(plan.tasks)
	if totalTasks > 0 {
		if plan.CacheHits > 0 {
			s.progressf("Embedding %d items (%d from cache, batch size %d)", totalWork, plan.CacheHits, s.batchSize())
		} else {
			s.progressf("Embedding %d items (batch size %d, max concurrency %d)", totalWork, s.batchSize(), s.maxConcurrent())
		}
		if s.OnEmbedProgress != nil && totalWork > 0 {
			s.OnEmbedProgress(0, totalWork)
		}
		err := s.processTasksPipelined(ctx, plan, itemWriter, intelWriter, writebackBatcher)
		if err != nil {
			if hasBatchWriter {
				close(writeCh)
				writeWG.Wait()
			}
			return errors.Join(fmt.Errorf("process code embedding tasks: %w", err), drainWriteQueue(), closeIntelWriter())
		}
	} else {
		s.progressf("No embeddings to update (index is up to date)")
	}
	if hasBatchWriter {
		close(writeCh)
		writeWG.Wait()
	}
	if err := drainWriteQueue(); err != nil {
		return err
	}
	if err := closeIntelWriter(); err != nil {
		return fmt.Errorf("close code intel writer: %w", err)
	}
	select {
	case err := <-batchErrCh:
		if err != nil {
			return err
		}
	default:
	}

	// Provider may have learned its output dimensions during this batch (Voyage
	// reports them with the first response). Re-run the validator so the meta
	// row reflects reality before the next sync cycle.
	_ = s.ensureReady(ctx)
	s.progressf("Sync complete")
	if updateLastSync {
		return s.markPlanFreshness(ctx, plan, true)
	}
	return s.markPlanFreshness(ctx, plan, false)
}

// persistIntelChunks converts semantic chunks to intel chunks and persists them.
func (s *Syncer) persistIntelChunks(ctx context.Context, tasks []codeTask) error {
	if s.ChunkWriter == nil {
		return nil
	}

	now := time.Now().Unix()
	var ownerIDs []string
	var chunks []codeanchor.IntelChunk

	for _, task := range tasks {
		ownerID := string(task.id)
		ownerIDs = append(ownerIDs, ownerID)
		if len(task.payload.chunks) == 0 && task.payload.ownerKind != "" {
			indexingperf.AddCount(ctx, indexingperf.CodeChunksPruned.Select(sanitizeMetricPart(task.payload.ownerKind)), 1)
		}
		for _, sc := range task.payload.chunks {
			chunks = append(chunks, codeanchor.IntelChunk{
				ChunkID:     codeanchor.IntelChunkID(ownerID, sc.Input.Index, sc.Input.Granularity),
				OwnerID:     ownerID,
				OwnerType:   "anchor",
				Ord:         sc.Input.Index,
				Granularity: sc.Input.Granularity,
				Breadcrumb:  sc.Input.Breadcrumb,
				Heading:     sc.Input.Heading,
				ContentHash: sc.Input.Hash,
				StartByte:   int64(sc.Input.StartByte),
				EndByte:     int64(sc.Input.EndByte),
				UpdatedAt:   now,
			})
		}
	}

	if s.WriteQueue != nil {
		return s.WriteQueue.SubmitIntelChunks(ctx, ownerIDs, chunks)
	}
	return s.ChunkWriter.ReplaceIntelChunks(ctx, ownerIDs, chunks)
}
