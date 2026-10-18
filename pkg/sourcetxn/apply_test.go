package sourcetxn

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

type Options struct{ Lease *indexlock.Lease }

func apply(ctx context.Context, root string, intent Intent, options Options, hooks *Hooks) error {
	writer, _, err := Open(root, options.Lease, WriterOptions{Role: SourceEditor, Hooks: hooks})
	if writer != nil {
		defer writer.Close()
	}
	if err != nil {
		return err
	}
	return writer.Apply(ctx, intent)
}
func applySourceFixture(ctx context.Context, root string, intent Intent, options Options) error {
	return apply(ctx, root, intent, options, nil)
}
func recoverSourceFixture(root string, options Options) error {
	writer, _, err := Open(root, options.Lease, WriterOptions{Role: RuntimeRecoveryOwner})
	if writer != nil {
		writer.Close()
	}
	return err
}

func sourceFixture(t *testing.T) (string, Options, Intent) {
	t.Helper()
	root := paths.ResolveSymlinks(t.TempDir()).String()
	lease, held, err := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{})
	require.NoError(t, err)
	require.True(t, held)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	intent := Intent{Fingerprint: Hash([]byte("intent"))}
	for _, path := range []string{"a.md", "b.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("old"), 0o640))
		intent.Files = append(intent.Files, FilePatch{Path: path, Original: []byte("old"), OriginalMode: 0o640, Updated: []byte("new")})
	}
	return root, Options{Lease: lease}, intent
}

func TestSourceLeaseAndCompletePreflight(t *testing.T) {
	root, options, intent := sourceFixture(t)
	require.ErrorIs(t, applySourceFixture(context.Background(), root, intent, Options{}), indexlock.ErrLeaseNotHeld)
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), []byte("external"), 0o640))
	err := applySourceFixture(context.Background(), root, intent, options)
	require.ErrorIs(t, err, ErrSourceChanged)
	require.Equal(t, "b.md", ConflictPath(err))
	content, err := os.ReadFile(filepath.Join(root, "a.md"))
	require.NoError(t, err)
	require.Equal(t, "old", string(content))
	require.NoError(t, options.Lease.Release())
	require.ErrorIs(t, recoverSourceFixture(root, options), indexlock.ErrLeaseNotHeld)
}

func TestSourceRecoversEverySurvivingRepairHook(t *testing.T) {
	tests := []struct {
		name      string
		committed bool
		hook      func(*Hooks)
	}{
		{"journal", false, func(h *Hooks) { h.AfterJournalPublished = func(string) error { return ErrSimulatedInterruption } }},
	}
	for _, index := range []int{0, 1} {
		i := index
		tests = append(tests, struct {
			name      string
			committed bool
			hook      func(*Hooks)
		}{"before install " + string(rune('0'+i)), false, func(h *Hooks) {
			h.BeforeInstall = func(n int, _ string) error {
				if n == i {
					return ErrSimulatedInterruption
				}
				return nil
			}
		}})
		tests = append(tests, struct {
			name      string
			committed bool
			hook      func(*Hooks)
		}{"after mutation " + string(rune('0'+i)), false, func(h *Hooks) {
			h.AfterMutation = func(n int, _ string) error {
				if n == i {
					return ErrSimulatedInterruption
				}
				return nil
			}
		}})
		tests = append(tests, struct {
			name      string
			committed bool
			hook      func(*Hooks)
		}{"after install " + string(rune('0'+i)), false, func(h *Hooks) {
			h.AfterInstall = func(n int, _ string) error {
				if n == i {
					return ErrSimulatedInterruption
				}
				return nil
			}
		}})
	}
	for _, boundary := range []string{cleanupAfterArtifactRemoved, cleanupBeforeDetach, cleanupAfterDetach, cleanupAfterCommittedRemoved, cleanupAfterManifestRemoved, cleanupAfterDirectoryRemoved} {
		b := boundary
		tests = append(tests, struct {
			name      string
			committed bool
			hook      func(*Hooks)
		}{b, true, func(h *Hooks) {
			h.AfterCleanupBoundary = func(boundary, _ string) error {
				if boundary == b {
					return ErrSimulatedInterruption
				}
				return nil
			}
		}})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			var hooks Hooks
			test.hook(&hooks)
			require.ErrorIs(t, apply(context.Background(), root, intent, options, &hooks), ErrSimulatedInterruption)
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, recoverSourceFixture(root, options))
			expected := "old"
			if test.committed {
				expected = "new"
			}
			for _, file := range intent.Files {
				content, err := os.ReadFile(filepath.Join(root, file.Path))
				require.NoError(t, err)
				require.Equal(t, expected, string(content))
			}
			require.NoError(t, CheckPending(root))
		})
	}
}

func TestLegacyQuarantineRetainsAllEvidenceAndEnforcesAdmission(t *testing.T) {
	root, options, _ := sourceFixture(t)
	legacy := filepath.Join(root, ".rhizome", "edit-journal")
	require.NoError(t, os.MkdirAll(legacy, 0o700))
	backup := filepath.Join(root, ".a.md.rhizome-backup-test")
	temp := filepath.Join(root, "a.md.rhizome-write-test")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(temp, []byte("new"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("external"), 0o640))
	journal := filepath.Join(legacy, "legacy.json")
	require.NoError(t, writeEditWriteJournal(journal, editWriteJournal{Version: 1, Phase: "prepared", TransactionID: "legacy", Entries: []editWriteJournalEntry{{NotePath: "a.md", BackupPath: backup, TempPath: temp, OldFingerprint: legacyFingerprint("old"), NewFingerprint: legacyFingerprint("new")}}}))
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, recoverSourceFixture(root, options))
	require.FileExists(t, journal)
	require.FileExists(t, backup)
	require.FileExists(t, temp)
	require.ErrorContains(t, CheckAdmission(root, []string{"a.md"}), "review required")
	require.NoError(t, CheckAdmission(root, []string{"b.md"}))
	content, err := os.ReadFile(filepath.Join(root, "a.md"))
	require.NoError(t, err)
	require.Equal(t, "external", string(content))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "unreadable.json"), []byte("{"), 0o600))
	require.NoError(t, recoverSourceFixture(root, options))
	require.Error(t, CheckAdmission(root, []string{"b.md"}))
}

func TestSourceDoesNotPublishAfterLeaseReleaseInHook(t *testing.T) {
	root, options, intent := sourceFixture(t)
	err := apply(context.Background(), root, intent, options, &Hooks{BeforeInstall: func(int, string) error { return options.Lease.Release() }})
	require.ErrorIs(t, err, indexlock.ErrLeaseNotHeld)
	for _, file := range intent.Files {
		content, err := os.ReadFile(filepath.Join(root, file.Path))
		require.NoError(t, err)
		require.Equal(t, "old", string(content))
	}
	require.Error(t, CheckPending(root), "prepared evidence remains until a real lease recovers it")
}

func TestSourceNoopStillChecksItsWitnessWithoutCreatingEvidence(t *testing.T) {
	root, options, intent := sourceFixture(t)
	intent.Files = intent.Files[:1]
	intent.Files[0].Updated = []byte("old")
	require.NoError(t, applySourceFixture(context.Background(), root, intent, options))
	require.NoError(t, CheckPending(root))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("external"), 0640))
	require.ErrorIs(t, applySourceFixture(context.Background(), root, intent, options), ErrSourceChanged)
}

func TestPreparedSourceCannotStageArtifactsOutsideItsVault(t *testing.T) {
	root, options, _ := sourceFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	_, err := stageRepairFileStates(writerContext{VaultPath: root, Lease: options.Lease}, Manifest{Version: 1, Source: true, TransactionID: "outside", Changed: []string{"a.md"}}, []fileState{{Path: "a.md", Absolute: outside, OriginalPath: "a.md", OriginalExists: true, OriginalContent: []byte("old"), OriginalMode: 0640, FinalExists: true, FinalContent: []byte("new"), FinalMode: 0640}}, nil)
	require.Error(t, err)
	matches, err := filepath.Glob(outside + ".rzm-repair-*")
	require.NoError(t, err)
	require.Empty(t, matches)
	require.NoError(t, CheckPending(root))
}
