package query

import (
	"context"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

// Selector inventory belongs to one Execute call. A failed read remains
// retryable, and the next execution consults the current provider again.
type selectorInventory struct {
	mu     sync.Mutex
	loaded bool
	rows   []semdb.NoteMetadataRow
}

func (l *loaders) selectorMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	l.selectorInventory.mu.Lock()
	defer l.selectorInventory.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.selectorInventory.loaded {
		return l.selectorInventory.rows, nil
	}
	rows, err := l.deps.Store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	l.selectorInventory.rows = rows
	l.selectorInventory.loaded = true
	return rows, nil
}
