package indexwriter

import (
	"context"
	"fmt"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// SubmitGraphScores publishes the completed global computation behind prior
// structural/semantic writes, and returns only after both score sets commit.
func (q *Writer) SubmitGraphScores(ctx context.Context, scores semdb.DerivedGraphScores) error {
	scores.Documents = append([]semdb.GraphDocScore(nil), scores.Documents...)
	scores.Anchors = append([]semdb.AnchorScore(nil), scores.Anchors...)
	return submitErrorControl(ctx, q, func(q *Writer) error {
		if q.handlers.ApplyGraphScores == nil {
			return fmt.Errorf("graph scores queue not initialized")
		}
		return q.handlers.ApplyGraphScores(q.ctx, scores)
	})
}
