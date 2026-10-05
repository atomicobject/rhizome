package indexwriter

import (
	"context"
	"fmt"

	sqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/graphdb"
)

type queueGraphScores struct {
	barrier uint64
	scores  graphdb.DerivedScores
	ack     chan error
}

// SubmitGraphScores publishes the completed global computation behind prior
// structural/semantic writes, and returns only after both score sets commit.
func (q *Writer) SubmitGraphScores(ctx context.Context, scores graphdb.DerivedScores) error {
	scores.Documents = append([]sqlite.GraphDocScore(nil), scores.Documents...)
	scores.Anchors = append([]sqlite.AnchorScore(nil), scores.Anchors...)
	ack := make(chan error, 1)
	if err := q.sendControl(ctx, queueGraphScores{barrier: q.currentSubmitBarrier(), scores: scores, ack: ack}); err != nil {
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-q.doneCh:
		select {
		case err := <-ack:
			return err
		default:
		}
		if err := q.Err(); err != nil {
			return err
		}
		return fmt.Errorf("graph score queue stopped")
	}
}
