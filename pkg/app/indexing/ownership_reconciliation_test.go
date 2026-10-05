package indexing

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/noteownership"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnershipReconciliationTrackerNoPendingOrTransitions(t *testing.T) {
	tracker, err := noteownership.NewReconciliationTracker(0, false)
	require.NoError(t, err)

	assert.False(t, tracker.Required())
	assert.Zero(t, tracker.AcknowledgementGeneration())
	assert.Empty(t, tracker.AffectedSourcePaths())
	assert.Empty(t, tracker.AffectedAnchorIDs())
}

func TestOwnershipReconciliationTrackerStartsWithPendingGeneration(t *testing.T) {
	tracker, err := noteownership.NewReconciliationTracker(4, true)
	require.NoError(t, err)

	assert.True(t, tracker.Required())
	assert.EqualValues(t, 4, tracker.AcknowledgementGeneration())
	assert.Empty(t, tracker.AffectedSourcePaths())
	assert.Empty(t, tracker.AffectedAnchorIDs())
}

func TestOwnershipReconciliationTrackerObservesMultipleResults(t *testing.T) {
	tracker, err := noteownership.NewReconciliationTracker(0, false)
	require.NoError(t, err)

	require.NoError(t, tracker.Observe(semdb.OwnershipTransitionResult{
		AffectedSourcePaths:      []string{"beta.md", "alpha.md", "beta.md"},
		AffectedAnchorIDs:        []int64{9, 2, 9},
		ReconciliationGeneration: 3,
	}))
	require.NoError(t, tracker.Observe(semdb.OwnershipTransitionResult{
		AffectedSourcePaths:      []string{"gamma.md", "alpha.md"},
		AffectedAnchorIDs:        []int64{4, 2},
		ReconciliationGeneration: 5,
	}))

	assert.True(t, tracker.Required())
	assert.EqualValues(t, 5, tracker.AcknowledgementGeneration())
	assert.Equal(t, []string{"alpha.md", "beta.md", "gamma.md"}, tracker.AffectedSourcePaths())
	assert.Equal(t, []int64{2, 4, 9}, tracker.AffectedAnchorIDs())
}

func TestOwnershipReconciliationTrackerIgnoresZeroResult(t *testing.T) {
	tracker, err := noteownership.NewReconciliationTracker(2, true)
	require.NoError(t, err)

	require.NoError(t, tracker.Observe(semdb.OwnershipTransitionResult{}))

	assert.True(t, tracker.Required())
	assert.EqualValues(t, 2, tracker.AcknowledgementGeneration())
	assert.Empty(t, tracker.AffectedSourcePaths())
	assert.Empty(t, tracker.AffectedAnchorIDs())
}

func TestOwnershipReconciliationTrackerRejectsInvalidState(t *testing.T) {
	_, err := noteownership.NewReconciliationTracker(-1, false)
	require.Error(t, err)

	_, err = noteownership.NewReconciliationTracker(0, true)
	require.Error(t, err)

	tracker, err := noteownership.NewReconciliationTracker(3, true)
	require.NoError(t, err)
	require.NoError(t, tracker.Observe(semdb.OwnershipTransitionResult{
		ReconciliationGeneration: 4,
	}))

	err = tracker.Observe(semdb.OwnershipTransitionResult{
		ReconciliationGeneration: 3,
	})
	require.Error(t, err)
	assert.EqualValues(t, 4, tracker.AcknowledgementGeneration())

	err = tracker.Observe(semdb.OwnershipTransitionResult{
		AffectedSourcePaths: []string{"alpha.md"},
	})
	require.Error(t, err)

	err = tracker.Observe(semdb.OwnershipTransitionResult{
		TransitionedPaths: []string{"alpha.md"},
	})
	require.Error(t, err)
}
