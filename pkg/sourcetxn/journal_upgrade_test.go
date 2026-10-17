package sourcetxn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiteralRepairV1PreparedCommittedAndCleanupRemainRecoverable(t *testing.T) {
	for _, state := range []string{"prepared", "committed", "cleanup"} {
		t.Run(state, func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			run := writerContext{VaultPath: root, Lease: options.Lease}
			id := "old-v1-" + state
			dir, err := repairJournalDir(run, id)
			require.NoError(t, err)
			if state == "cleanup" {
				dir = filepath.Join(filepath.Dir(dir), repairJournalCleanupPrefix+filepath.Base(dir))
			}
			require.NoError(t, os.MkdirAll(dir, 0700))
			if state == "cleanup" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("unreadable terminal reporting"), 0600))
				require.NoError(t, recoverSourceFixture(root, options))
				require.NoDirExists(t, dir)
				return
			}
			abs := filepath.Join(root, "a.md")
			short := strings.TrimPrefix(Hash([]byte(id)), "sha256:")[:12]
			backup := repairArtifactPath(abs, short, 0, "backup")
			stage := repairArtifactPath(abs, short, 0, "stage")
			require.NoError(t, os.WriteFile(backup, []byte("old"), 0640))
			require.NoError(t, os.WriteFile(stage, []byte("new"), 0640))
			require.NoError(t, os.WriteFile(abs, []byte("new"), 0640))
			// This is the pre-extraction v1 wire shape, not a re-encoding of Manifest.
			encoded := fmt.Sprintf(`{"version":1,"transactionId":%q,"planFingerprint":"old-plan","createdAt":"2026-10-16T00:00:00Z","changed":["a.md"],"checks":["ontology"],"ownedArtifacts":[%q,%q],"entries":[{"path":"a.md","originalPath":"a.md","originalExists":true,"originalMode":416,"originalHash":%q,"finalExists":true,"finalMode":416,"finalHash":%q,"backupPath":%q,"stagePath":%q}]}`, id, backup, stage, Hash([]byte("old")), Hash([]byte("new")), backup, stage)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(encoded), 0600))
			if state == "committed" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "COMMITTED"), []byte("committed\n"), 0600))
				require.NoError(t, os.WriteFile(abs, []byte("later"), 0640))
			}
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, recoverSourceFixture(root, options))
			content, err := os.ReadFile(abs)
			require.NoError(t, err)
			expected := "old"
			if state == "committed" {
				expected = "later"
			}
			require.Equal(t, expected, string(content))
			require.FileExists(t, filepath.Join(dir, "manifest.json"), "repair refresh/postcheck owns its evidence")
			require.Error(t, CheckAdmission(root, []string{"a.md"}))
			require.NoError(t, CheckAdmission(root, []string{"b.md"}))
		})
	}
}

func TestLiteralNamespaceV2WireShapeRetainsGitReceiptAndPurpose(t *testing.T) {
	// Terminal metadata is reporting only, but its complete wire shape must round-trip.
	literal := `{"version":2,"transactionId":"old-native","planFingerprint":"old-plan","createdAt":"2026-10-16T00:00:00Z","operationIds":["move"],"renamed":[{"from":"a.md","to":"b.md"}],"entries":[],"namespace":{"version":1,"settledDecision":"committed","summary":{"count":1},"moves":[{"from":"a.md","to":"b.md"}],"gitMoves":[{"from":"a.md","to":"b.md"}],"git":{"gitDir":"/fixture/.git","indexPath":"/fixture/.git/index","lockPath":"/fixture/.git/index.lock","rawOriginal":{"exists":true,"hash":"sha256:original","mode":420},"rollback":{"path":"/fixture/.git/index.rollback","hash":"sha256:rollback","mode":420},"candidate":{"path":"/fixture/.git/index.candidate","hash":"sha256:candidate","mode":420},"token":"native-token","lockHash":"sha256:lock","lockMode":384}}}`
	var manifest Manifest
	require.NoError(t, json.Unmarshal([]byte(literal), &manifest))
	require.Equal(t, 2, manifest.Version)
	require.Equal(t, "committed", manifest.Namespace.SettledDecision)
	require.Equal(t, "native-token", manifest.Namespace.Git.Token)
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.JSONEq(t, literal, string(encoded))
}

func TestRepairQuarantinePreservesUnexpectedBytesAndAllowsUnrelatedWrites(t *testing.T) {
	root, options, _ := sourceFixture(t)
	run := writerContext{VaultPath: root, Lease: options.Lease}
	prepared, err := stageRepairFileStates(run, Manifest{Version: 1, TransactionID: "repair-quarantine", Checks: []string{"ontology"}, Changed: []string{"a.md"}}, []fileState{{Path: "a.md", Absolute: filepath.Join(root, "a.md"), OriginalPath: "a.md", OriginalExists: true, OriginalContent: []byte("old"), OriginalMode: 0640, FinalExists: true, FinalContent: []byte("new"), FinalMode: 0640}}, nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("external"), 0640))
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, recoverSourceFixture(root, options))
	require.FileExists(t, prepared.Manifest.Entries[0].BackupPath)
	require.FileExists(t, prepared.Manifest.Entries[0].StagePath)
	require.FileExists(t, filepath.Join(prepared.Directory, "manifest.json"))
	require.ErrorContains(t, CheckAdmission(root, []string{"a.md"}), "review required")
	require.NoError(t, CheckAdmission(root, []string{"b.md"}))
	evidence, err := detectPendingRepairJournals(run)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, "quarantined", evidence[0].State)
}

func TestUnreadableJournalBlocksEvenOtherJournalRecovery(t *testing.T) {
	root, options, _ := sourceFixture(t)
	run := writerContext{VaultPath: root, Lease: options.Lease}
	prepared, err := stageRepairFileStates(run, Manifest{Version: 1, TransactionID: "valid-before-corrupt", Checks: []string{"ontology"}, Changed: []string{"a.md"}}, []fileState{{Path: "a.md", Absolute: filepath.Join(root, "a.md"), OriginalPath: "a.md", OriginalExists: true, OriginalContent: []byte("old"), OriginalMode: 0640, FinalExists: true, FinalContent: []byte("new"), FinalMode: 0640}}, nil)
	require.NoError(t, err)
	require.ErrorIs(t, commitPreparedRepairTransaction(run, prepared, &Hooks{AfterInstall: func(int, string) error { return ErrSimulatedInterruption }}), ErrSimulatedInterruption)
	bad := filepath.Join(filepath.Dir(prepared.Directory), "unreadable")
	require.NoError(t, os.Mkdir(bad, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(bad, "manifest.json"), []byte("{"), 0600))
	require.NoError(t, recoverSourceFixture(root, options))
	content, err := os.ReadFile(filepath.Join(root, "a.md"))
	require.NoError(t, err)
	require.Equal(t, "new", string(content), "unknown authority forbids even a valid-looking rollback")
	require.FileExists(t, prepared.Manifest.Entries[0].BackupPath)
	require.Error(t, CheckAdmission(root, []string{"b.md"}))
}
