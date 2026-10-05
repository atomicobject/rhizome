package validate

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticSnapshotPublishesBlockedRepairRecovery(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "recovery.db"))
	require.NoError(t, err)
	defer store.Close()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	result := blockedRepairJournalResult(RunContext{}, []string{CheckBrokenLinks}, nil, "")
	require.Equal(t, 1, result.ErrorCount)
	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{VaultIdentity: "vault", Generation: generation, CheckOutcomes: []DiagnosticCheckOutcome{{Check: CheckBrokenLinks, Outcome: CheckOutcomeBlocked, Summary: "Recover interrupted repairs first"}}})
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)

	state, err := store.GetValidationState(ctx)
	require.NoError(t, err)
	require.Equal(t, generation, state.PublishedGeneration)
	stored, ok, err := store.GetPublishedValidationSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, generation, stored.Generation)
	require.Equal(t, semdb.ValidationCompletionIncomplete, stored.Completion)
	require.Zero(t, stored.ErrorCount, "a blocked recovery is not an invented diagnostic error")
	require.Len(t, stored.Checks, 1)
	require.Equal(t, semdb.ValidationCheckOutcomeBlocked, stored.Checks[0].Outcome)
	require.Equal(t, "Recover interrupted repairs first", stored.Checks[0].Summary)
}
