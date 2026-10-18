package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCanceledBatchRetainsDebtAndRestartMatchesFresh(t *testing.T) {
	t.Parallel()
	root, fresh := t.TempDir(), t.TempDir()
	for _, path := range []string{root, fresh} {
		writeIndexingParityWorkload(t, path, 2, 2)
	}
	definition, err := repoconfig.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := false
	opts.afterPreparedOwnershipCommit = func(context.Context) error { reached = true; cancel(); return ctx.Err() }
	require.ErrorIs(t, runUnifiedCoreForTest(t, ctx, opts), context.Canceled)
	require.True(t, reached)
	store, closeStore, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	unready, err := store.HasUnreadyDerivedWork(context.Background())
	require.NoError(t, err)
	require.True(t, unready, "interrupted publication must retain durable inactive obligations")
	_, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.True(t, pending)
	closeStore()
	opts.afterPreparedOwnershipCommit = nil
	require.NoError(t, runUnifiedCoreForTest(t, context.Background(), opts))
	store, closeStore, err = indexstore.Open(root, indexstore.Options{Access: indexstore.ReadOnly, RequireEnabled: false})
	require.NoError(t, err)
	recovered, err := store.GetValidationStateSnapshot(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, recovered.Snapshot.RepairPlanFingerprint)
	closeStore()
	// A second invocation in the same vault must retain the source authority
	// that the interrupted run recovered, even though it republishes validation.
	require.NoError(t, runUnifiedCoreForTest(t, context.Background(), opts))
	freshDefinition, err := repoconfig.LoadDefinitionFromPath(fresh)
	require.NoError(t, err)
	require.NoError(t, runUnifiedCoreForTest(t, context.Background(), UnifiedOptions{VaultPath: fresh, VaultDef: freshDefinition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}))
	store, closeStore, err = indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	defer closeStore()
	freshStore, closeFresh, err := indexstore.Open(fresh, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	defer closeFresh()
	got, err := canonicalWorkloadSnapshot(t, context.Background(), store, root)
	require.NoError(t, err)
	want, err := canonicalWorkloadSnapshot(t, context.Background(), freshStore, fresh)
	require.NoError(t, err)
	requireParitySnapshotCoverage(t, want)
	require.Equal(t, want, got)
	require.NotEmpty(t, want["dependencies"])
	require.NotEmpty(t, want["go-relationships"])
	current, err := store.GetValidationStateSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, recovered.Snapshot.RepairPlanFingerprint, current.Snapshot.RepairPlanFingerprint)
	require.NoError(t, addPublishedValidationSnapshot(t, context.Background(), store, root, got))
	require.NoError(t, addPublishedValidationSnapshot(t, context.Background(), freshStore, fresh, want))
	require.Equal(t, want["published-validation"], got["published-validation"])
	debt, err := workloadDerivedDebt(context.Background(), store)
	require.NoError(t, err)
	require.Zero(t, debt)
	_, pending, err = store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.False(t, pending)
}
