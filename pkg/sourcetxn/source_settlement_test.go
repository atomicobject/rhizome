package sourcetxn

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func unpublishedPartialArtifact(t *testing.T) (string, Options, Intent, string, string) {
	t.Helper()
	root, options, intent := sourceFixture(t)
	var dir string
	require.ErrorIs(t, apply(context.Background(), root, intent, options, &Hooks{AfterJournalPublished: func(path string) error { dir = path; return ErrSimulatedInterruption }}), ErrSimulatedInterruption)
	m, err := readRepairJournalManifest(dir)
	require.NoError(t, err)
	artifact := m.Entries[0].BackupPath
	require.NoError(t, os.WriteFile(artifact, []byte("partial"), 0600))
	return root, options, intent, dir, artifact
}
func TestUnpublishedSettlementSurvivesCrashAndLaterLegitimateEdit(t *testing.T) {
	for _, boundary := range []string{"before", "after"} {
		t.Run(boundary, func(t *testing.T) {
			root, options, intent, dir, artifact := unpublishedPartialArtifact(t)
			hooks := &Hooks{}
			if boundary == "before" {
				hooks.BeforeSourceSettlement = func(string) error { return ErrSimulatedInterruption }
			} else {
				hooks.AfterSourceSettlement = func(string) error { return ErrSimulatedInterruption }
			}
			writer, _, err := Open(root, options.Lease, WriterOptions{Role: SourceEditor, Hooks: hooks})
			require.ErrorIs(t, err, ErrSimulatedInterruption)
			if writer != nil {
				writer.Close()
			}
			if boundary == "before" {
				require.Error(t, CheckAdmission(root, []string{"a.md"}))
			} else {
				require.NoError(t, CheckAdmission(root, []string{"a.md"}))
			}
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, CheckPending(root))
			content, err := os.ReadFile(artifact)
			require.NoError(t, err)
			require.Equal(t, "partial", string(content))
			require.FileExists(t, filepath.Join(dir, sourceSettledMarker))
			require.NoError(t, applySourceFixture(context.Background(), root, Intent{Fingerprint: "later edit", Files: []FilePatch{{Path: "a.md", Original: []byte("old"), OriginalMode: 0640, Updated: []byte("legitimate")}}}, options))
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, CheckPending(root))
			content, err = os.ReadFile(filepath.Join(root, "a.md"))
			require.NoError(t, err)
			require.Equal(t, "legitimate", string(content))
			content, err = os.ReadFile(artifact)
			require.NoError(t, err)
			require.Equal(t, "partial", string(content))
			require.Equal(t, "old", mustReadEditTransactionFile(t, filepath.Join(root, intent.Files[1].Path)))
		})
	}
}
func TestUnpublishedSettlementRetainsAmbiguousSources(t *testing.T) {
	root, options, _, dir, artifact := unpublishedPartialArtifact(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), []byte("external"), 0640))
	require.NoError(t, recoverSourceFixture(root, options))
	require.Error(t, CheckAdmission(root, []string{"a.md"}))
	require.NoFileExists(t, filepath.Join(dir, sourceSettledMarker))
	require.Equal(t, "partial", mustReadEditTransactionFile(t, artifact))
	require.Equal(t, "external", mustReadEditTransactionFile(t, filepath.Join(root, "b.md")))
}
