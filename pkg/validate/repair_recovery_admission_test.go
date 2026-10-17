package validate

import (
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverRepairJournalsQuarantinesUnsafePathsBeforeIndependentRollback(t *testing.T) {
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
	fixture, err := openRepairWriterFixture(t, runCtx, nil)
	require.NoError(t, err)
	first, err := fixture.prepare("plan:recovery-order", firstTransaction, []RepairOperation{firstOperation}, false)
	require.NoError(t, err)
	_, err = fixture.writer.Publish(first)
	require.NoError(t, err)
	require.Equal(t, firstAfter, mustReadFile(t, firstPath))

	secondOperation := repairWriteOperation(
		"operation:z", "action:second", "second.md", secondBefore, secondAfter,
	)
	secondTransactions, err := GroupRepairTransactions([]RepairOperation{secondOperation})
	require.NoError(t, err)
	require.Len(t, secondTransactions, 1)
	secondTransaction := secondTransactions[0]
	secondTransaction.Checks = []string{CheckOntology}
	second, err := fixture.prepare("plan:recovery-order", secondTransaction, []RepairOperation{secondOperation}, false)
	require.NoError(t, err)
	fixture.close()
	// Model a crash after source installation but before the first transaction's
	// terminal marker persisted, alongside independent damaged preparation.
	require.NoError(t, os.Remove(filepath.Join(first.Info().Directory, "COMMITTED")))
	require.NotEmpty(t, second.Info().Manifest.Entries[0].BackupPath)
	require.NoError(t, os.WriteFile(second.Info().Manifest.Entries[0].BackupPath, []byte("corrupt backup\n"), 0o640))

	recovery, err := openRepairWriterFixture(t, runCtx, nil)
	require.NoError(t, err)
	defer recovery.close()
	assert.Equal(t, firstBefore, mustReadFile(t, firstPath),
		"a quarantined unrelated path must not block a fully preflighted independent rollback")
	assert.Equal(t, secondBefore, mustReadFile(t, secondPath))

	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 2, "both journals remain available for convergence or review")
	require.Error(t, sourcetxn.CheckAdmission(root, []string{"second.md"}))
	require.NoError(t, sourcetxn.CheckAdmission(root, []string{"unrelated.md"}))
}
