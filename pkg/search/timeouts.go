package search

import (
	"context"
	"time"
)

// DefaultQueryTimeout is the default ceiling for answer-oriented search calls
// that do not provide their own deadline.
const DefaultQueryTimeout = 8 * time.Second

type retrievalDeadlineKey struct{}

func retrievalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Value(retrievalDeadlineKey{}) != nil {
		return context.WithCancel(ctx)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	// Reserve time for ranking and packing once, including nested lane groups.
	reserve := min(time.Until(deadline)/4, 500*time.Millisecond)
	ctx, cancel := context.WithDeadline(ctx, deadline.Add(-max(0, reserve)))
	return context.WithValue(ctx, retrievalDeadlineKey{}, true), cancel
}
