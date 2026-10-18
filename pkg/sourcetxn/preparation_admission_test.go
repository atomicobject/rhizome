package sourcetxn

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefusedPreparationDoesNotCreateSourceOrReceiptParents(t *testing.T) {
	for _, reason := range []string{"admission", "source conflict"} {
		t.Run(reason, func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			writer, _, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner})
			require.NoError(t, err)
			defer writer.Close()
			request := RepairRequest{TransactionID: "refused", Fingerprint: "reviewed", Changed: []string{"new/nested/note.md"}, Files: []Change{{Path: "new/nested/note.md", Before: Snapshot{}, After: Snapshot{Exists: true, Content: []byte("created"), Mode: 0640}}}, Completion: &CompletionArtifact{Path: filepath.Join(root, repairCompletionDirectory, "refused.json"), Content: []byte("receipt")}}
			if reason == "admission" {
				evidence := filepath.Join(root, "evidence")
				require.NoError(t, os.WriteFile(evidence, []byte("preserved"), 0600))
				require.NoError(t, recordQuarantine(root, evidence, []string{"new/nested/note.md"}, nil, errors.New("review required")))
			} else {
				request.Files = append(request.Files, Change{Path: "a.md", OriginalPath: "a.md", Before: Snapshot{Exists: true, Content: []byte("stale"), Mode: 0640}, After: Snapshot{Exists: true, Content: []byte("new"), Mode: 0640}})
				request.Changed = append(request.Changed, "a.md")
			}
			tx, err := writer.PrepareRepair(request)
			require.Error(t, err)
			require.Nil(t, tx)
			require.NoDirExists(t, filepath.Join(root, "new"))
			require.NoDirExists(t, filepath.Join(root, repairCompletionDirectory))
			require.Equal(t, "old", mustReadEditTransactionFile(t, filepath.Join(root, "a.md")))
		})
	}
}
