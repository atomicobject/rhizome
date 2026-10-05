package bootstrap

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
)

func (w *unifiedSemanticWatcher) structuralWriter(ctx context.Context) *indexwriter.Writer {
	return indexwriter.New(ctx, indexcore.BindWriterHandlers(indexwriter.Handlers{}, w.noteSvc, w.intelStore))
}
