package sourcetxn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitConflictRestoresOnlyInstalledFiles(t *testing.T) {
	for _, changedIndex := range []int{0, 1} {
		t.Run(fmt.Sprint(changedIndex), func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			target := intent.Files[changedIndex].Path
			err := apply(context.Background(), root, intent, options, &Hooks{BeforeInstall: func(index int, _ string) error {
				if index == changedIndex {
					return os.WriteFile(filepath.Join(root, target), []byte("external"), 0640)
				}
				return nil
			}})
			require.ErrorIs(t, err, ErrSourceChanged)
			require.Equal(t, target, ConflictPath(err))
			for _, file := range intent.Files {
				content, readErr := os.ReadFile(filepath.Join(root, file.Path))
				require.NoError(t, readErr)
				expected := "old"
				if file.Path == target {
					expected = "external"
				}
				require.Equal(t, expected, string(content), file.Path)
				require.NoError(t, CheckAdmission(root, []string{file.Path}))
			}
			require.NoError(t, CheckPending(root))
			artifacts, err := filepath.Glob(filepath.Join(root, "*.rzm-repair-*"))
			require.NoError(t, err)
			require.Empty(t, artifacts)
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, CheckPending(root))
		})
	}
}

func TestRecoveryRemovesUnpublishedPrepareDirectories(t *testing.T) {
	for _, withTemporaryManifest := range []bool{false, true} {
		t.Run(fmt.Sprint(withTemporaryManifest), func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			dir, err := repairJournalDir(writerContext{VaultPath: root}, "unpublished")
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(dir, 0755))
			if withTemporaryManifest {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json.tmp"), []byte("{partial"), 0600))
			}
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoDirExists(t, dir)
			require.NoError(t, CheckPending(root))
		})
	}
}

func TestRecoveryReconcilesRemovedQuarantineEvidence(t *testing.T) {
	root, options, _ := sourceFixture(t)
	evidence := filepath.Join(root, ".rhizome", "edit-journal", "bad.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(evidence), 0700))
	require.NoError(t, os.WriteFile(evidence, []byte("{"), 0600))
	require.NoError(t, recoverSourceFixture(root, options))
	require.Error(t, CheckPending(root))
	require.NoError(t, os.Remove(evidence))
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, CheckPending(root))
	records, err := quarantineRecords(root)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestRecoveryAdoptsExactArtifactsAndPreservesUnknownArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		mode          os.FileMode
		recoverable   bool
	}{
		{"exact", "old", 0640, true}, {"different bytes", "external", 0640, false}, {"different mode", "old", 0600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			var dir string
			require.ErrorIs(t, apply(context.Background(), root, intent, options, &Hooks{AfterJournalPublished: func(path string) error { dir = path; return ErrSimulatedInterruption }}), ErrSimulatedInterruption)
			manifest, err := readRepairJournalManifest(dir)
			require.NoError(t, err)
			artifact := manifest.Entries[0].BackupPath
			if tc.name == "different mode" && runtime.GOOS == "windows" {
				tc.mode = 0440
			}
			require.NoError(t, os.WriteFile(artifact, []byte(tc.content), tc.mode))
			require.NoError(t, recoverSourceFixture(root, options))
			if tc.recoverable {
				require.NoError(t, CheckPending(root))
				require.NoFileExists(t, artifact)
			} else {
				require.NoError(t, CheckAdmission(root, []string{"a.md"}))
				require.FileExists(t, artifact)
				pending, err := detectPendingRepairJournals(writerContext{VaultPath: root})
				require.NoError(t, err)
				require.Len(t, pending, 1)
				require.Equal(t, "source_settled", pending[0].State)
				content, err := os.ReadFile(artifact)
				require.NoError(t, err)
				require.Equal(t, tc.content, string(content))
			}
			content, err := os.ReadFile(filepath.Join(root, "a.md"))
			require.NoError(t, err)
			require.Equal(t, "old", string(content))
		})
	}
}

func TestApplyRejectsFilesystemAliasesBeforeStaging(t *testing.T) {
	root, options, _ := sourceFixture(t)
	const nfc = "caf\u00e9.md"
	const nfd = "cafe\u0301.md"
	require.NoError(t, os.WriteFile(filepath.Join(root, nfc), []byte("old"), 0640))
	first, err := os.Stat(filepath.Join(root, nfc))
	require.NoError(t, err)
	second, err := os.Stat(filepath.Join(root, nfd))
	if os.IsNotExist(err) {
		t.Skip("filesystem distinguishes Unicode normalization forms")
	}
	require.NoError(t, err)
	if !os.SameFile(first, second) {
		t.Skip("filesystem keeps normalization forms as separate files")
	}
	intent := Intent{Fingerprint: "unicode-alias", Files: []FilePatch{
		{Path: nfc, Original: []byte("old"), OriginalMode: 0640, Updated: []byte("first")},
		{Path: nfd, Original: []byte("old"), OriginalMode: 0640, Updated: []byte("second")},
	}}
	require.ErrorContains(t, applySourceFixture(context.Background(), root, intent, options), "duplicate")
	content, err := os.ReadFile(filepath.Join(root, nfc))
	require.NoError(t, err)
	require.Equal(t, "old", string(content))
	states := []fileState{}
	for _, file := range intent.Files {
		states = append(states, fileState{Path: file.Path, Absolute: filepath.Join(root, file.Path), OriginalPath: file.Path, OriginalExists: true, OriginalContent: []byte("old"), OriginalMode: 0640, FinalExists: true, FinalContent: file.Updated, FinalMode: 0640})
	}
	_, err = stageRepairFileStates(writerContext{VaultPath: root, Lease: options.Lease}, Manifest{Source: true, TransactionID: "alias-states", Changed: []string{nfc, nfd}}, states, nil)
	require.ErrorContains(t, err, "duplicate")
	require.NoError(t, CheckPending(root))
}

func TestRecoveryPropagatesUnreadableQuarantine(t *testing.T) {
	root, options, _ := sourceFixture(t)
	dir, err := quarantineRoot(root)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unreadable.json"), []byte("{"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "edit-journal"), 0700))
	run := writerContext{VaultPath: root, Lease: options.Lease}
	require.Error(t, recoverLegacy(run))
	_, err = outsideQuarantine(run, nil)
	require.Error(t, err)
	require.Error(t, recoverSourceFixture(root, options))
}

func TestFailedRollbackIsReportedAsRequiringRecovery(t *testing.T) {
	root, options, intent := sourceFixture(t)
	err := apply(context.Background(), root, intent, options, &Hooks{BeforeInstall: func(index int, _ string) error {
		if index == 1 {
			return os.WriteFile(filepath.Join(root, "a.md"), []byte("external after first install"), 0640)
		}
		return nil
	}})
	var unresolved *RecoveryRequiredError
	require.ErrorAs(t, err, &unresolved)
	require.Error(t, CheckPending(root))
	content, err := os.ReadFile(filepath.Join(root, "a.md"))
	require.NoError(t, err)
	require.Equal(t, "external after first install", string(content))
	content, err = os.ReadFile(filepath.Join(root, "b.md"))
	require.NoError(t, err)
	require.Equal(t, "old", string(content))
}

func TestCommitPreconditionsIdentifyTheirSource(t *testing.T) {
	for _, tc := range []string{"backup ownership", "stage ownership", "absolute path"} {
		t.Run(tc, func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			run := writerContext{VaultPath: root, Lease: options.Lease}
			prepared, err := stageRepairFileStates(run, Manifest{Source: true, TransactionID: "precondition-path", Changed: []string{"a.md"}}, []fileState{{Path: "a.md", Absolute: filepath.Join(root, "a.md"), OriginalPath: "a.md", OriginalExists: true, OriginalContent: []byte("old"), OriginalMode: 0640, FinalExists: true, FinalContent: []byte("new"), FinalMode: 0640}}, nil)
			require.NoError(t, err)
			switch tc {
			case "backup ownership":
				prepared.Manifest.OwnedArtifacts = []string{prepared.Manifest.Entries[0].StagePath}
			case "stage ownership":
				prepared.Manifest.OwnedArtifacts = []string{prepared.Manifest.Entries[0].BackupPath}
			case "absolute path":
				prepared.Files[0].Absolute = filepath.Join(root, "different.md")
			}
			err = commitPreparedRepairTransaction(run, prepared, nil)
			var precondition CommitPreconditionError
			require.ErrorAs(t, err, &precondition)
			require.Equal(t, "a.md", precondition.Path)
			content, err := os.ReadFile(filepath.Join(root, "a.md"))
			require.NoError(t, err)
			require.Equal(t, "old", string(content))
		})
	}
}
