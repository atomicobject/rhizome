package dispatch

import (
	"context"
	"testing"
)

func TestQueueDrainQuarantinesTerminalFailureAndContinues(t *testing.T) {
	q := &Queue{}
	q.Enqueue(Operation{ID: "bad", TerminalFailure: true})
	q.Enqueue(Operation{ID: "good"})
	var uploaded []string
	if err := q.Drain(context.Background(), func(_ context.Context, op Operation) error {
		uploaded = append(uploaded, op.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(uploaded) != 1 || uploaded[0] != "good" {
		t.Fatalf("uploaded=%v", uploaded)
	}
}

func TestResolveAssignmentConflictRequiresReviewForStaleVersion(t *testing.T) {
	if got := ResolveAssignmentConflict(2, 3); got != ConflictReview {
		t.Fatalf("got %q", got)
	}
}
