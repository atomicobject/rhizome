package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRunUnifiedCoreValidationCancellationWithoutDebtFails(t *testing.T) {
	t.Parallel()

	root, definition := touchParityVault(t)
	options := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}
	require.NoError(t, runUnifiedCoreForTest(t, context.Background(), options))
	store, cleanup, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := false
	finished := false
	options.ProgressBar = cancelValidationProgress{
		label: func(label string) {
			if label == "Validating index" {
				_, pending, err := store.PendingOwnershipReconciliation(context.Background())
				require.NoError(t, err)
				require.False(t, pending, "exercise the no-reconciliation completion path")
				entered = true
				cancel()
			}
		},
		update: func(done, total int) {
			if done == total {
				finished = true
			}
		},
	}
	err = runUnifiedCoreForTest(t, ctx, options)
	require.True(t, entered)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, finished, "canceled validation must not publish successful completion")
}

type cancelValidationProgress struct {
	label  func(string)
	update func(int, int)
}

func (p cancelValidationProgress) Println(string)         {}
func (p cancelValidationProgress) SetLabel(label string)  { p.label(label) }
func (p cancelValidationProgress) Update(done, total int) { p.update(done, total) }
