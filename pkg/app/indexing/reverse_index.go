package indexing

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

type reverseIndexStore interface {
	IndexerVersion(ctx context.Context) (string, bool, error)
	ReverseIndexVersion(ctx context.Context) (string, bool, error)
	ReverseIndexBackfillComplete(ctx context.Context) (bool, bool, error)
	SetReverseIndexBackfillComplete(ctx context.Context, complete bool) error
	SetReverseIndexVersion(ctx context.Context, version string) error
}

// ReverseIndexStore captures the subset of store APIs needed for reverse-index
// readiness checks and lifecycle flags.
type ReverseIndexStore = reverseIndexStore

// EnsureReverseIndexReadiness verifies index/reverse-index versions and resets
// backfill state when versions are stale or missing.
func EnsureReverseIndexReadiness(ctx context.Context, store ReverseIndexStore, logf func(string, ...any)) bool {
	if store == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	warnf := func(format string, args ...any) {
		if logf != nil {
			logf(format, args...)
		}
	}

	reset := false
	if version, ok, err := store.IndexerVersion(ctx); err != nil {
		warnf("reverse-index readiness: failed to read indexer version: %v", err)
		reset = true
	} else if !ok || version != codeanchor.IndexerVersion {
		reset = true
	}

	if version, ok, err := store.ReverseIndexVersion(ctx); err != nil {
		warnf("reverse-index readiness: failed to read reverse index version: %v", err)
		reset = true
	} else if !ok || version != codeanchor.ReverseIndexVersion {
		reset = true
	}

	if !reset {
		return false
	}

	if err := store.SetReverseIndexBackfillComplete(ctx, false); err != nil {
		warnf("reverse-index readiness: failed to clear backfill flag: %v", err)
	}
	if err := store.SetReverseIndexVersion(ctx, codeanchor.ReverseIndexVersion); err != nil {
		warnf("reverse-index readiness: failed to set reverse index version: %v", err)
	}
	return true
}

// MarkReverseIndexBackfillComplete marks reverse-index backfill as complete and
// stamps the current reverse-index version.
func MarkReverseIndexBackfillComplete(ctx context.Context, store ReverseIndexStore, logf func(string, ...any)) {
	if store == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	warnf := func(format string, args ...any) {
		if logf != nil {
			logf(format, args...)
		}
	}

	if err := store.SetReverseIndexBackfillComplete(ctx, true); err != nil {
		warnf("reverse-index readiness: failed to set backfill flag: %v", err)
	}
	if err := store.SetReverseIndexVersion(ctx, codeanchor.ReverseIndexVersion); err != nil {
		warnf("reverse-index readiness: failed to set reverse index version: %v", err)
	}
}

// ShouldMarkReverseIndexComplete decides whether a run should mark reverse-index
// backfill complete.
func ShouldMarkReverseIndexComplete(ctx context.Context, store ReverseIndexStore, forceReindex bool, logf func(string, ...any)) bool {
	if store == nil {
		return false
	}
	if forceReindex {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	warnf := func(format string, args ...any) {
		if logf != nil {
			logf(format, args...)
		}
	}

	ready, ok, err := store.ReverseIndexBackfillComplete(ctx)
	if err != nil {
		warnf("reverse-index readiness: failed to read backfill flag: %v", err)
		return false
	}
	return ok && ready
}

func ensureReverseIndexReadiness(ctx context.Context, store reverseIndexStore, logf func(string, ...any)) bool {
	return EnsureReverseIndexReadiness(ctx, store, logf)
}

func markReverseIndexBackfillComplete(ctx context.Context, store reverseIndexStore, logf func(string, ...any)) {
	MarkReverseIndexBackfillComplete(ctx, store, logf)
}

func shouldMarkReverseIndexComplete(ctx context.Context, store reverseIndexStore, forceReindex bool, logf func(string, ...any)) bool {
	return ShouldMarkReverseIndexComplete(ctx, store, forceReindex, logf)
}
