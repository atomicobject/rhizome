package sqlite

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidationInvalidationFencesRunningGeneration(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "without_snapshot"
		if retained {
			name = "retained_snapshot"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := currentSchemaTestDBPath(t, "invalidation.db")
			store, err := Open(path)
			require.NoError(t, err)
			defer func() { _ = store.Close() }()
			var publishedGeneration int64
			if retained {
				publishedGeneration, err = store.SetValidationRunning(ctx)
				require.NoError(t, err)
				ok, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(publishedGeneration, 1))
				require.NoError(t, err)
				require.True(t, ok)
			}
			running, err := store.SetValidationRunning(ctx)
			require.NoError(t, err)
			_, err = store.MarkPublishedValidationStale(ctx, "reconcile failed")
			require.NoError(t, err)
			ok, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(running, 2))
			require.NoError(t, err)
			require.False(t, ok, "invalidated run must not publish")
			ok, err = store.SetValidationError(ctx, running, "late failure", 10)
			require.NoError(t, err)
			require.False(t, ok, "invalidated run must not replace lifecycle with error")
			require.NoError(t, store.Close())
			store, err = Open(path)
			require.NoError(t, err)
			read, err := store.GetValidationStateSnapshot(ctx)
			require.NoError(t, err)
			require.Greater(t, read.State.Generation, running)
			require.Equal(t, publishedGeneration, read.State.PublishedGeneration)
			require.Equal(t, retained, read.HasSnapshot)
			if retained {
				require.Equal(t, ValidationStatusOK, read.State.Status)
				require.Equal(t, "reconcile failed", read.Snapshot.StaleReason)
				require.Equal(t, 1, read.Snapshot.IssueCount)
			} else {
				require.Equal(t, ValidationStatusNeverRan, read.State.Status)
			}
			next, err := store.SetValidationRunning(ctx)
			require.NoError(t, err)
			ok, err = store.PublishValidationSnapshot(ctx, validationStoreFixture(next, 3))
			require.NoError(t, err)
			require.True(t, ok)
			read, err = store.GetValidationStateSnapshot(ctx)
			require.NoError(t, err)
			require.Equal(t, next, read.State.PublishedGeneration)
			require.Empty(t, read.Snapshot.StaleReason)
		})
	}
}
