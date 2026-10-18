package indexing

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

type reverseIndexStore interface {
	IndexerVersion(ctx context.Context) (string, bool, error)
	ReverseIndexVersion(ctx context.Context) (string, bool, error)
}

// reverseIndexNeedsRebuild reads version evidence before queued invalidation.
func reverseIndexNeedsRebuild(ctx context.Context, store reverseIndexStore, logf func(string, ...any)) bool {
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

	return reset
}
