package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeOverwriteRollbackOriginalPairNeverMutatesEndpoints(t *testing.T) {
	for _, recreated := range []bool{false, true} {
		for _, equal := range []bool{false, true} {
			t.Run(string([]byte{'0' + byteBool(recreated), '0' + byteBool(equal)}), func(t *testing.T) {
				runCtx, manifest := namespaceRollbackFixture(t, equal)
				if recreated {
					require.NoError(t, os.Rename(filepath.Join(runCtx.VaultPath, "old.md"), filepath.Join(runCtx.VaultPath, "retired.md")))
					require.NoError(t, os.WriteFile(filepath.Join(runCtx.VaultPath, "old.md"), []byte("source\n"), 0o600))
				}
				beforeSource, err := os.Stat(filepath.Join(runCtx.VaultPath, "old.md"))
				require.NoError(t, err)
				beforeDestination, err := os.Stat(filepath.Join(runCtx.VaultPath, "new.md"))
				require.NoError(t, err)
				plan, err := buildRepairRollbackPlan(runCtx, manifest)
				require.NoError(t, err)
				for _, state := range plan.states {
					require.Equal(t, "none", state.action)
				}
				mutated, err := executeRepairRollbackPlan(runCtx, manifest, plan)
				require.NoError(t, err)
				require.False(t, mutated)
				require.NoError(t, verifyNamespaceOriginalFiles(runCtx, manifest))
				afterSource, err := os.Stat(filepath.Join(runCtx.VaultPath, "old.md"))
				require.NoError(t, err)
				afterDestination, err := os.Stat(filepath.Join(runCtx.VaultPath, "new.md"))
				require.NoError(t, err)
				require.True(t, os.SameFile(beforeSource, afterSource))
				require.True(t, os.SameFile(beforeDestination, afterDestination))
			})
		}
	}
}

func TestNativeOverwriteRollbackRefusesUnknownPairBeforeMutation(t *testing.T) {
	for _, change := range []string{"final", "source", "mode", "type"} {
		t.Run(change, func(t *testing.T) {
			runCtx, manifest := namespaceRollbackFixture(t, false)
			switch change {
			case "final":
				require.NoError(t, os.WriteFile(filepath.Join(runCtx.VaultPath, "new.md"), []byte("source\n"), 0o600))
			case "source":
				require.NoError(t, os.WriteFile(filepath.Join(runCtx.VaultPath, "old.md"), []byte("recreated\n"), 0o600))
			case "mode":
				require.NoError(t, os.Chmod(filepath.Join(runCtx.VaultPath, "old.md"), 0o400))
				t.Cleanup(func() { _ = os.Chmod(filepath.Join(runCtx.VaultPath, "old.md"), 0o600) })
			case "type":
				require.NoError(t, os.Rename(filepath.Join(runCtx.VaultPath, "old.md"), filepath.Join(runCtx.VaultPath, "saved.md")))
				require.NoError(t, os.Mkdir(filepath.Join(runCtx.VaultPath, "old.md"), 0o700))
			}
			_, err := buildRepairRollbackPlan(runCtx, manifest)
			require.Error(t, err)
			require.FileExists(t, filepath.Join(runCtx.VaultPath, "new.md"))
		})
	}
}

func namespaceRollbackFixture(t *testing.T, equal bool) (RunContext, repairJournalManifest) {
	t.Helper()
	root := t.TempDir()
	source, destination := []byte("source\n"), []byte("destination\n")
	if equal {
		destination = source
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), source, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), destination, 0o600))
	sourceInfo, err := os.Stat(filepath.Join(root, "old.md"))
	require.NoError(t, err)
	destinationInfo, err := os.Stat(filepath.Join(root, "new.md"))
	require.NoError(t, err)
	return RunContext{VaultPath: root}, repairJournalManifest{
		Namespace: &namespaceJournalPurpose{}, Renamed: []PathRename{{From: "old.md", To: "new.md"}},
		Entries: []repairJournalEntry{
			{Path: "old.md", OriginalPath: "old.md", OriginalExists: true, OriginalHash: SourceHash(source), OriginalMode: repairModeBits(sourceInfo.Mode())},
			{Path: "new.md", OriginalPath: "new.md", OriginalExists: true, OriginalHash: SourceHash(destination), OriginalMode: repairModeBits(destinationInfo.Mode()), FinalExists: true, FinalHash: SourceHash(source), FinalMode: repairModeBits(sourceInfo.Mode())},
		},
	}
}

func byteBool(value bool) byte {
	if value {
		return 1
	}
	return 0
}
