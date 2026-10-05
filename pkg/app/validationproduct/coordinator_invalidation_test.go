package validationproduct

import (
	"context"
	"errors"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestInvalidatedRefreshCannotPublishOrCompleteWithError(t *testing.T) {
	for _, runErr := range []error{nil, errors.New("late run failure")} {
		name := "publication"
		if runErr != nil {
			name = "error completion"
		}
		t.Run(name, func(t *testing.T) {
			store := openCoordinatorStore(t)
			started, finish := make(chan struct{}), make(chan struct{})
			coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
				close(started)
				<-finish // Deliberately ignore cancellation: the durable fence is authoritative.
				return cleanAuthoritativeRun(), runErr
			}))
			done := make(chan error, 1)
			go func() { done <- coordinator.Refresh(t.Context()) }()
			<-started
			_, err := store.MarkPublishedValidationStale(t.Context(), "reconcile failed")
			require.NoError(t, err)
			close(finish)
			require.ErrorIs(t, <-done, ErrRefreshSuperseded)
			read, err := store.GetValidationStateSnapshot(t.Context())
			require.NoError(t, err)
			require.False(t, read.HasSnapshot)
			require.Equal(t, semdb.ValidationStatusNeverRan, read.State.Status)
			require.Nil(t, coordinator.authority)
		})
	}
}

func TestInvalidationRevokesRetainedRepairAuthority(t *testing.T) {
	store := openCoordinatorStore(t)
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
		return cleanAuthoritativeRun(), nil
	}))
	require.NoError(t, coordinator.Refresh(t.Context()))
	generation := coordinator.authority.generation
	_, err := store.MarkPublishedValidationStale(t.Context(), "reconcile failed")
	require.NoError(t, err)
	_, err = coordinator.CreateRepairReview(t.Context(), generation, "", nil)
	var reviewErr *validate.RepairReviewError
	require.ErrorAs(t, err, &reviewErr)
	require.Equal(t, validate.RepairReviewErrorGenerationMismatch, reviewErr.Code)
	require.NoError(t, coordinator.Refresh(t.Context()))
	require.Greater(t, coordinator.authority.generation, generation)
	read, err := store.GetValidationStateSnapshot(t.Context())
	require.NoError(t, err)
	require.Empty(t, read.Snapshot.StaleReason)
	require.Equal(t, coordinator.authority.generation, read.State.PublishedGeneration)
}

func TestInvalidationBetweenPublicationAndAuthorityInstallation(t *testing.T) {
	store := openCoordinatorStore(t)
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
		return cleanAuthoritativeRun(), nil
	}))
	coordinator.afterPublish = func(int64) {
		_, err := store.MarkPublishedValidationStale(t.Context(), "reconcile failed")
		require.NoError(t, err)
	}
	require.ErrorIs(t, coordinator.Refresh(t.Context()), ErrRefreshSuperseded)
	require.Nil(t, coordinator.authority)
	read, err := store.GetValidationStateSnapshot(t.Context())
	require.NoError(t, err)
	require.True(t, read.HasSnapshot)
	require.Equal(t, "reconcile failed", read.Snapshot.StaleReason)
}
