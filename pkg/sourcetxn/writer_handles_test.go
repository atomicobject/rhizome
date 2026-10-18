package sourcetxn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writerRepairRequest() RepairRequest {
	return RepairRequest{TransactionID: "writer-contract", Fingerprint: "literal-plan", Checks: []string{"ontology"}, Changed: []string{"a.md"}, Files: []Change{{Path: "a.md", OriginalPath: "a.md", Before: Snapshot{Exists: true, Content: []byte("old"), Mode: 0640}, After: Snapshot{Exists: true, Content: []byte("new"), Mode: 0640}}}}
}
func TestWriterRejectsZeroForeignAndTerminalRollbackHandles(t *testing.T) {
	root, options, _ := sourceFixture(t)
	first, _, err := Open(root, options.Lease, WriterOptions{})
	require.NoError(t, err)
	defer first.Close()
	second, _, err := Open(root, options.Lease, WriterOptions{})
	require.NoError(t, err)
	defer second.Close()
	_, err = first.Publish(nil)
	require.Error(t, err)
	require.Error(t, first.Abort(&Transaction{}))
	require.Error(t, first.Complete(Completion{}))
	tx, err := first.PrepareRepair(writerRepairRequest())
	require.NoError(t, err)
	_, err = second.Publish(tx)
	require.Error(t, err)
	require.Error(t, second.Abort(tx))
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("sentinel"), 0600))
	copied := tx.Info()
	copied.Manifest.Entries[0].StagePath = outside
	copied.Manifest.OwnedArtifacts = append(copied.Manifest.OwnedArtifacts, outside)
	completion, err := first.Publish(tx)
	require.NoError(t, err)
	require.Error(t, first.Abort(tx))
	require.Error(t, second.Complete(completion))
	require.Equal(t, "new", mustReadEditTransactionFile(t, filepath.Join(root, "a.md")))
	repeated, err := first.Publish(tx)
	require.NoError(t, err)
	require.Equal(t, completion.Info().Manifest.TransactionID, repeated.Info().Manifest.TransactionID)
	copied = completion.Info()
	copied.Manifest.OwnedArtifacts = append(copied.Manifest.OwnedArtifacts, outside)
	require.NoError(t, first.Complete(completion))
	require.NoError(t, first.Complete(completion))
	require.Equal(t, "sentinel", mustReadEditTransactionFile(t, outside))
	require.NoError(t, CheckPending(root))
	first.Close()
	require.Error(t, first.Complete(completion))
	require.Error(t, first.Abort(tx))
}
func TestWriterRequiresReopenAfterInterruption(t *testing.T) {
	root, options, _ := sourceFixture(t)
	first, _, err := Open(root, options.Lease, WriterOptions{Hooks: &Hooks{AfterInstall: func(int, string) error { return ErrSimulatedInterruption }}})
	require.NoError(t, err)
	tx, err := first.PrepareRepair(writerRepairRequest())
	require.NoError(t, err)
	_, err = first.Publish(tx)
	require.ErrorIs(t, err, ErrSimulatedInterruption)
	_, err = first.PrepareRepair(writerRepairRequest())
	require.Error(t, err)
	require.Error(t, first.Abort(tx))
	first.Close()
	next, recovered, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner, RepairPolicy: func(e RepairEvidence) error { e.Checks[0] = "mutated copy"; return nil }})
	require.NoError(t, err)
	defer next.Close()
	require.Len(t, recovered.RolledBack, 1)
	require.Equal(t, []string{"ontology"}, recovered.RolledBack[0].Info().Manifest.Checks)
	require.Equal(t, "old", mustReadEditTransactionFile(t, filepath.Join(root, "a.md")))
	require.NoError(t, next.Complete(recovered.RolledBack[0]))
	require.NoError(t, CheckPending(root))
}
