package sourcetxn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryDropsOnlyExactCaseRenameLinkPair(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "unowned", true: "owned"}[owned], func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			original := filepath.Join(root, "note.md")
			target := filepath.Join(root, "Note.md")
			require.NoError(t, os.WriteFile(original, []byte("old"), 0640))
			originalInfo, err := os.Stat(original)
			require.NoError(t, err)
			targetInfo, err := os.Stat(target)
			if os.IsNotExist(err) {
				t.Skip("filesystem distinguishes case aliases")
			}
			require.NoError(t, err)
			if !os.SameFile(originalInfo, targetInfo) {
				t.Skip("filesystem keeps case spellings as distinct files")
			}
			run := writerContext{VaultPath: root, Lease: options.Lease}
			prepared, err := stageRepairFileStates(run, Manifest{TransactionID: "case-window", Checks: []string{"ontology"}, Changed: []string{"Note.md"}, Renamed: []Rename{{From: "note.md", To: "Note.md"}}}, []fileState{{Path: "Note.md", Absolute: target, OriginalPath: "note.md", OriginalExists: true, OriginalContent: []byte("old"), OriginalMode: 0640, FinalExists: true, FinalContent: []byte("new"), FinalMode: 0640}}, nil)
			require.NoError(t, err)
			casePath := prepared.Manifest.Entries[0].CasePath
			require.NoError(t, os.Link(original, casePath))
			if owned {
				prepared.Manifest.OwnedArtifacts = append(prepared.Manifest.OwnedArtifacts, casePath)
				require.NoError(t, writeRepairJournalManifest(prepared.Directory, prepared.Manifest))
			}
			writer, recovered, err := Open(root, options.Lease, WriterOptions{Role: SourceEditor})
			require.NoError(t, err)
			defer writer.Close()
			require.NoFileExists(t, casePath)
			require.Equal(t, "old", mustReadEditTransactionFile(t, original))
			for _, completion := range append(recovered.RolledBack, recovered.NoMutation...) {
				require.NoError(t, writer.Complete(completion))
			}
			require.NoError(t, CheckPending(root))
		})
	}
}
