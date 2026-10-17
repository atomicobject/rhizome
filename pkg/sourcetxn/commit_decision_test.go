package sourcetxn

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUncertainCommitDecisionNeverStartsRollback(t *testing.T) {
	for _, source := range []bool{true, false} {
		name := "repair"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			var dir string
			rollbackCalls := 0
			fault := errors.New("directory synchronization failed after commit marker publication")
			hooks := &Hooks{
				AfterJournalPublished:      func(path string) error { dir = path; return nil },
				afterCommitMarkerPublished: func() error { return fault },
				BeforeRollbackMutation: func(int, string) error {
					rollbackCalls++
					if rollbackCalls == 2 {
						return ErrSimulatedInterruption
					}
					return nil
				},
			}
			writer, _, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner, Hooks: hooks})
			require.NoError(t, err)
			if source {
				err = writer.Apply(context.Background(), intent)
			} else {
				request := writerRepairRequest()
				request.Changed = append(request.Changed, "b.md")
				request.Files = append(request.Files, Change{Path: "b.md", OriginalPath: "b.md", Before: Snapshot{Exists: true, Content: []byte("old"), Mode: 0640}, After: Snapshot{Exists: true, Content: []byte("new"), Mode: 0640}})
				tx, prepareErr := writer.PrepareRepair(request)
				require.NoError(t, prepareErr)
				_, err = writer.Publish(tx)
			}
			require.ErrorIs(t, err, fault)
			var pending *RecoveryRequiredError
			require.ErrorAs(t, err, &pending)
			require.Zero(t, rollbackCalls, "a published commit decision cannot authorize rollback")
			require.Equal(t, "committed\n", mustReadEditTransactionFile(t, filepath.Join(dir, "COMMITTED")))
			m, readErr := readRepairJournalManifest(dir)
			require.NoError(t, readErr)
			for _, entry := range m.Entries {
				require.Equal(t, "new", mustReadEditTransactionFile(t, filepath.Join(root, entry.Path)))
				require.Equal(t, "old", mustReadEditTransactionFile(t, entry.BackupPath))
				require.NoFileExists(t, entry.StagePath)
			}
			require.Error(t, CheckPending(root))
			_, err = writer.PrepareRepair(writerRepairRequest())
			require.Error(t, err, "uncertain writer requires reopen")
			writer.Close()
			next, recovered, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner})
			require.NoError(t, err)
			if !source {
				require.Len(t, recovered.Committed, 1)
				require.NoError(t, next.Verify(recovered.Committed[0]))
				require.NoError(t, next.Complete(recovered.Committed[0]))
			}
			next.Close()
			require.NoError(t, CheckPending(root))
			require.NoDirExists(t, dir)
			for _, name := range []string{"a.md", "b.md"} {
				require.Equal(t, "new", mustReadEditTransactionFile(t, filepath.Join(root, name)))
			}
		})
	}
}
