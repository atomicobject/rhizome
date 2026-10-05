package indexing

import (
	"context"
	"errors"
	"sync"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type embedAggregator struct {
	bar              ProgressBar
	mu               sync.Mutex
	totals           map[string]int
	dones            map[string]int
	lastRenderedDone int
	lastRenderedTot  int
}

func (e *embedAggregator) forPhase(phase string) func(done, total int) {
	return func(done, total int) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.totals == nil {
			e.totals = make(map[string]int)
			e.dones = make(map[string]int)
		}
		if total > 0 {
			e.totals[phase] = total
		}
		if done < 0 {
			done = 0
		}
		e.dones[phase] = done

		sumDone := 0
		sumTotal := 0
		for _, d := range e.dones {
			sumDone += d
		}
		for _, t := range e.totals {
			sumTotal += t
		}
		if sumDone == e.lastRenderedDone && sumTotal == e.lastRenderedTot {
			return
		}
		// Bulk code embedding discovers work incrementally. Suppress redraw spam
		// while total work grows but no chunks have completed yet.
		if sumDone == 0 && e.lastRenderedDone == 0 && e.lastRenderedTot > 0 && sumTotal > e.lastRenderedTot {
			e.lastRenderedTot = sumTotal
			return
		}
		e.lastRenderedDone = sumDone
		e.lastRenderedTot = sumTotal
		e.bar.Update(sumDone, sumTotal)
	}
}

type errCollector struct {
	mu  sync.Mutex
	err error
}

func (c *errCollector) capture(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil || (isCancelErr(c.err) && !isCancelErr(err)) {
		c.err = err
	}
}

func (c *errCollector) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func isCancelErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func effectiveMaxConcurrent(explicit int, provider embeddings.Provider) int {
	if explicit > 0 {
		return explicit
	}
	if provider == nil {
		return embeddings.DefaultMaxConcurrent
	}
	if d, ok := provider.(interface{ DefaultMaxConcurrency() int }); ok {
		if v := d.DefaultMaxConcurrency(); v > 0 {
			return v
		}
	}
	return embeddings.DefaultMaxConcurrent
}
