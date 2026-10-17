package query

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func (l *loaders) selectorMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	return l.scope.MetadataInventory(ctx)
}
