package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverRepairJournalsPreflightsEveryJournalBeforeRollback(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	firstBefore := []byte("first before\n")
	firstAfter := []byte("first after\n")
	secondBefore := []byte("second before\n")
	secondAfter := []byte("second after\n")
	firstPath := filepath.Join(root, "first.md")
	secondPath := filepath.Join(root, "second.md")
	require.NoError(t, os.WriteFile(firstPath, firstBefore, 0o640))
	require.NoError(t, os.WriteFile(secondPath, secondBefore, 0o640))

	firstOperation := repairWriteOperation(
		"operation:a", "action:first", "first.md", firstBefore, firstAfter,
	)
	firstTransactions, err := GroupRepairTransactions([]RepairOperation{firstOperation})
	require.NoError(t, err)
	require.Len(t, firstTransactions, 1)
	firstTransaction := firstTransactions[0]
	firstTransaction.Checks = []string{CheckOntology}
	first, err := prepareRepairTransaction(runCtx, "plan:recovery-order", firstTransaction, []RepairOperation{firstOperation}, false, nil)
	require.NoError(t, err)
	err = commitPreparedRepairTransaction(runCtx, first, &repairExecutionHooks{
		AfterInstall: func(_ int, _ string) error {
			return errSimulatedRepairInterruption
		},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	require.Equal(t, firstAfter, mustReadFile(t, firstPath))

	secondOperation := repairWriteOperation(
		"operation:z", "action:second", "second.md", secondBefore, secondAfter,
	)
	secondTransactions, err := GroupRepairTransactions([]RepairOperation{secondOperation})
	require.NoError(t, err)
	require.Len(t, secondTransactions, 1)
	secondTransaction := secondTransactions[0]
	secondTransaction.Checks = []string{CheckOntology}
	second, err := prepareRepairTransaction(runCtx, "plan:recovery-order", secondTransaction, []RepairOperation{secondOperation}, false, nil)
	require.NoError(t, err)
	require.NotEmpty(t, second.manifest.Entries[0].BackupPath)
	require.NoError(t, os.WriteFile(second.manifest.Entries[0].BackupPath, []byte("corrupt backup\n"), 0o640))

	_, err = recoverRepairJournals(runCtx)
	require.Error(t, err)
	assert.ErrorContains(t, err, second.manifest.TransactionID)
	assert.Equal(t, firstAfter, mustReadFile(t, firstPath),
		"a later journal failure must block every earlier rollback before vault mutation")
	assert.Equal(t, secondBefore, mustReadFile(t, secondPath))

	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 2, "both journals remain available for a later recovery attempt")
}
