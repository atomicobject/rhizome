package indexing

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

// queuedIntentStore reads the current snapshot directly and publishes its
// replacement through the same durable writer as the rest of indexing.
type queuedIntentStore struct {
	intentstore.Store
	queue *indexwriter.Writer
}

func (s queuedIntentStore) ReplaceIntentEmbeddingSnapshot(ctx context.Context, snapshot intentstore.Snapshot) error {
	if s.queue == nil {
		return fmt.Errorf("intent embedding write queue unavailable")
	}
	return s.queue.SubmitIntentEmbeddingSnapshot(ctx, snapshot)
}
