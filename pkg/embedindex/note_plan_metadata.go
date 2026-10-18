package embedindex

import (
	"context"

	"sync"

	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (s *NoteSyncer) planMetadataWrites(ctx context.Context, cancel context.CancelFunc, errCh chan<- error) (submit func(embeddings.NoteFileInfo, bool) error, finish func()) {
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
		return observeNoteMetricErr(ctx, indexingperf.NoteplanMetaSubmit, func() error {
			if s.WriteQueue != nil {
				submitStarted := time.Now()
				err := s.WriteQueue.SubmitNoteMeta(ctx, info)
				indexingperf.ObserveLatency(ctx, indexingperf.NoteplanWritebackMetaSubmitWait, time.Since(submitStarted))
				return err
			}
			if metaCh != nil {
				submitStarted := time.Now()
				done := make(chan error, 1)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case metaCh <- metaWrite{info: info, done: done}:
				}
				indexingperf.ObserveLatency(ctx, indexingperf.NoteplanWritebackMetaSubmitWait, time.Since(submitStarted))
				select {
				case err := <-done:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return s.Index.UpsertNoteMeta(ctx, info)
		})
	}

	return submitNoteMeta, func() {
		if metaCh != nil {
			close(metaCh)
			metaWG.Wait()
		}
	}
}
