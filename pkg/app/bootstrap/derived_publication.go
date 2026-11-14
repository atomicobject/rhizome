package bootstrap

import (
	"context"
	"errors"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Publication batch bounds keep each lane job short enough for explicit
// indexing and watcher batches to interleave with a large derived pass.
const (
	derivedPublishBatchResults = 128
	derivedPublishBatchBytes   = 16 << 20
	derivedPublishBatchWait    = 250 * time.Millisecond
)

type derivedResult interface {
	RetainedBytes() int
	SourcePath() string
}

// computeAndPublish gives compute a sink that hands results to one publisher.
// The publisher publishes bounded batches, each inside one lease, so a pass
// admits one lane job and one witness check per batch rather than per result.
// A sink call blocks while the buffer is full and returns the publisher's
// error once a batch fails; a failed batch also cancels compute's context so
// the pass stops calling the provider. It returns after the publisher has
// drained, with the tickets whose results were all published; a member that
// dropped while a batch held its result is left out, so its ticket survives
// the final acknowledgement and its retry rebuilds it. A non-nil cache records
// the vectors of every result left unpublished (failed or superseded
// publication, cancelled compute, or an owner a failed compute left
// incomplete), so the retry or replacement does not resend that text.
func computeAndPublish[R derivedResult](s *derivedScheduler, ctx context.Context, tickets []codeanchor.DerivedWork, compute func(context.Context, func(context.Context, R) error) error, publish func(context.Context, R) error, cache func(context.Context, ...R) error) ([]codeanchor.DerivedWork, error) {
	computeCtx, stopCompute := context.WithCancel(ctx)
	defer stopCompute()
	// shortcut: buffered results are bounded by count; add a byte bound if owners grow large.
	results := make(chan R, derivedPublishBatchResults)
	failed := make(chan struct{})
	done := make(chan struct{})
	var publishErr error
	var unpublishedMu sync.Mutex
	var unpublished []R
	keep := func(results ...R) {
		unpublishedMu.Lock()
		unpublished = append(unpublished, results...)
		unpublishedMu.Unlock()
	}
	go func() {
		defer close(done)
		for open := true; open; {
			first, ok := <-results
			if !ok {
				return
			}
			batch, bytes := []R{first}, first.RetainedBytes()
			timer := time.NewTimer(derivedPublishBatchWait)
		fill:
			for len(batch) < derivedPublishBatchResults && bytes < derivedPublishBatchBytes {
				select {
				case next, ok := <-results:
					if !ok {
						open = false
						break fill
					}
					batch, bytes = append(batch, next), bytes+next.RetainedBytes()
				case <-timer.C:
					break fill
				}
			}
			timer.Stop()
			if _, err := s.withLease(ctx, tickets, false, "derived_publication", func(ctx context.Context, live []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error) {
				for _, result := range batch {
					if live[0].Path != "" && !slices.ContainsFunc(live, func(member codeanchor.DerivedWork) bool { return member.Path == result.SourcePath() }) {
						keep(result)
						continue
					}
					if err := publish(ctx, result); err != nil {
						return live, err
					}
				}
				return live, nil
			}); err != nil {
				publishErr = err
				keep(batch...)
				stopCompute()
				close(failed)
				return
			}
		}
	}()
	err := compute(computeCtx, func(sinkCtx context.Context, result R) error {
		select {
		case <-failed:
			keep(result)
			return publishErr
		default:
		}
		select {
		case results <- result:
			return nil
		case <-failed:
			keep(result)
			return publishErr
		case <-sinkCtx.Done():
			keep(result)
			return sinkCtx.Err()
		}
	})
	close(results)
	<-done
	for result := range results {
		keep(result)
	}
	var incomplete *embedindex.IncompleteDerivedError[R]
	if errors.As(err, &incomplete) {
		keep(incomplete.Results...)
	}
	if cache != nil && len(unpublished) > 0 {
		if cacheErr := s.inLane(context.WithoutCancel(ctx), "derived_vector_cache", func(ctx context.Context) error { return cache(ctx, unpublished...) }); cacheErr != nil && ctx.Err() == nil {
			watcherEvent(ctx, slog.LevelWarn, "derived.vector_cache_failed", append(derivedAttrs(tickets[0]), slog.String("error", cacheErr.Error()))...)
		}
	}
	if publishErr != nil {
		return nil, publishErr
	}
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(tickets), func(ticket codeanchor.DerivedWork) bool {
		return slices.ContainsFunc(unpublished, func(result R) bool { return result.SourcePath() == ticket.Path })
	}), nil
}
