package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStabilizePendingRepairJournalsContextWaitsForWriter(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runCtx := repairRunContext(t, root)
	done := make(chan error, 1)
	go func() { done <- StabilizePendingRepairJournalsContext(ctx, runCtx) }()
	select {
	case err := <-done:
		t.Fatalf("repair recovery returned while a writer held the vault: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, release())
	require.NoError(t, <-done)
}

func TestStabilizePendingRepairJournalsRollsBackPartialConnectedWriteBeforeStartup(t *testing.T) {
	root := t.TempDir()
	firstBefore := []byte("first before\n")
	secondBefore := []byte("second before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), firstBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "second.md"), secondBefore, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:startup", issueKey: "issue:startup",
		operations: []RepairOperation{
			repairWriteOperation("op:first", "action:startup", "first.md", firstBefore, []byte("first after\n")),
			repairWriteOperation("op:second", "action:startup", "second.md", secondBefore, []byte("second after\n")),
		},
	}})
	runCtx := repairRunContext(t, root)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, ApplyOptions{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(index int, _ string) error {
			if index == 0 {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	assert.Equal(t, []byte("first after\n"), mustReadFile(t, filepath.Join(root, "first.md")))
	assert.Equal(t, secondBefore, mustReadFile(t, filepath.Join(root, "second.md")))

	require.NoError(t, StabilizePendingRepairJournals(runCtx))
	assert.Equal(t, firstBefore, mustReadFile(t, filepath.Join(root, "first.md")))
	assert.Equal(t, secondBefore, mustReadFile(t, filepath.Join(root, "second.md")))
	assert.NotEmpty(t, mustPendingRepairJournals(t, runCtx), "projection refresh still owns journal cleanup")
}

func TestStartupPreservesCommittedRepairAfterLaterSourceChanges(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprintf("removed=%t", removed), func(t *testing.T) {
			root := t.TempDir()
			runCtx := repairRunContext(t, root)
			path := filepath.Join(root, "note.md")
			before := []byte("before\n")
			require.NoError(t, os.WriteFile(path, before, 0o640))
			operation := repairWriteOperation("operation:startup", "action:startup", "note.md", before, []byte("repaired\n"))
			transactions, err := GroupRepairTransactions([]RepairOperation{operation})
			require.NoError(t, err)
			transactions[0].Checks = []string{CheckOntology}
			interrupt := false
			fixture, err := openRepairWriterFixture(t, runCtx, &repairExecutionHooks{
				AfterInstall: func(_ int, _ string) error {
					if interrupt {
						return errSimulatedRepairInterruption
					}
					return nil
				},
			})
			require.NoError(t, err)
			prepared, err := fixture.prepare("plan:startup", transactions[0], []RepairOperation{operation}, false)
			require.NoError(t, err)
			_, err = fixture.writer.Publish(prepared)
			require.NoError(t, err)
			manifest := mustReadFile(t, filepath.Join(prepared.Info().Directory, "manifest.json"))
			// A separate transaction can still be unfinished in the same crashed apply.
			unfinishedPath := filepath.Join(root, "unfinished.md")
			require.NoError(t, os.WriteFile(unfinishedPath, before, 0o640))
			unfinishedOperation := repairWriteOperation("operation:unfinished", "action:unfinished", "unfinished.md", before, []byte("partial\n"))
			unfinishedTransactions, err := GroupRepairTransactions([]RepairOperation{unfinishedOperation})
			require.NoError(t, err)
			unfinishedTransactions[0].Checks = []string{CheckOntology}
			unfinished, err := fixture.prepare("plan:startup", unfinishedTransactions[0], []RepairOperation{unfinishedOperation}, false)
			require.NoError(t, err)
			interrupt = true
			_, err = fixture.writer.Publish(unfinished)
			require.ErrorIs(t, err, errSimulatedRepairInterruption)
			fixture.close()
			require.Equal(t, []byte("partial\n"), mustReadFile(t, unfinishedPath))
			if removed {
				require.NoError(t, os.Remove(path))
			} else {
				require.NoError(t, os.WriteFile(path, before, 0o640)) // A Git restore after the repair.
			}

			for range 2 {
				require.NoError(t, StabilizePendingRepairJournalsContext(t.Context(), runCtx))
				require.NoError(t, StabilizePendingRepairJournals(runCtx))
			}
			if removed {
				assert.NoFileExists(t, path)
			} else {
				assert.Equal(t, before, mustReadFile(t, path))
			}
			assert.Equal(t, manifest, mustReadFile(t, filepath.Join(prepared.Info().Directory, "manifest.json")))
			assert.FileExists(t, filepath.Join(prepared.Info().Directory, "COMMITTED"))
			assert.FileExists(t, prepared.Info().Manifest.Entries[0].BackupPath)
			assert.Equal(t, before, mustReadFile(t, unfinishedPath))
			require.Len(t, mustPendingRepairJournals(t, runCtx), 2)
			recovery, err := openRepairWriterFixture(t, runCtx, nil)
			require.NoError(t, err)
			defer recovery.close()
			require.Empty(t, recovery.recovery.Committed, "changed committed sources must not receive a cleanup handle")
			require.Error(t, sourcetxn.CheckAdmission(root, []string{"note.md"}))
		})
	}
}
