package codeanchor

import "context"

// WithBatchIndexing marks a context as belonging to a batch indexing run (e.g. `rzm index`).
//
// Batch runs already recompute anchor scopes after ingest, so they can skip per-file dirty
// tracking/invalidation work that exists primarily to support incremental live updates.
func WithBatchIndexing(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, batchIndexingKey{}, true)
}

type batchIndexingKey struct{}

func isBatchIndexing(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(batchIndexingKey{}).(bool)
	return v
}

// WithForceReindex disables incremental short-circuiting (mtime/hash checks).
// Intended for one-off rebuilds after schema/version changes.
func WithForceReindex(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, forceReindexKey{}, true)
}

type forceReindexKey struct{}

func isForceReindex(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(forceReindexKey{}).(bool)
	return v
}
