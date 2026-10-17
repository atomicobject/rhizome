package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/stretchr/testify/require"
)

func TestNativeOverwriteRollbackOriginalPairNeverMutatesEndpoints(t *testing.T) {
	for _, recreated := range []bool{false, true} {
		for _, equal := range []bool{false, true} {
			t.Run(string([]byte{'0' + byteBool(recreated), '0' + byteBool(equal)}), func(t *testing.T) {
				runCtx := namespaceRollbackFixture(t, equal)
				if recreated {
					require.NoError(t, os.Rename(filepath.Join(runCtx.VaultPath, "old.md"), filepath.Join(runCtx.VaultPath, "retired.md")))
					require.NoError(t, os.WriteFile(filepath.Join(runCtx.VaultPath, "old.md"), []byte("source\n"), 0o600))
				}
				beforeSource, err := os.Stat(filepath.Join(runCtx.VaultPath, "old.md"))
				require.NoError(t, err)
				beforeDestination, err := os.Stat(filepath.Join(runCtx.VaultPath, "new.md"))
				require.NoError(t, err)
				recovery, err := openRepairWriterFixture(t, runCtx, nil)
				require.NoError(t, err)
				defer recovery.close()
				require.Len(t, recovery.recovery.Native, 1)
				require.Equal(t, repairJournalRestored, recovery.recovery.Native[0].Info().State)
				require.Equal(t, "source\n", string(mustReadFile(t, filepath.Join(runCtx.VaultPath, "old.md"))))
				wantDestination := "destination\n"
				if equal {
					wantDestination = "source\n"
				}
				require.Equal(t, wantDestination, string(mustReadFile(t, filepath.Join(runCtx.VaultPath, "new.md"))))
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
			runCtx := namespaceRollbackFixture(t, false)
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
			recovery, _ := openRepairWriterFixture(t, runCtx, nil)
			defer recovery.close()
			require.Empty(t, recovery.recovery.Native)
			require.Error(t, sourcetxn.CheckAdmission(runCtx.VaultPath, []string{"old.md", "new.md"}))
			require.FileExists(t, filepath.Join(runCtx.VaultPath, "new.md"))
		})
	}
}

func namespaceRollbackFixture(t *testing.T, equal bool) RunContext {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
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
	run := namespaceTestRunContext(root)
	mode := repairModeBits(sourceInfo.Mode())
	plan, err := compileNamespacePlan(run, NamespaceMutationPlan{Operations: []RepairOperation{{
		Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md", SourceHash: SourceHash(source), SourceMode: &mode,
		DestinationState: &RepairDestinationState{Kind: RepairDestinationOccupied, Content: destination, Hash: SourceHash(destination), Mode: repairModeBits(destinationInfo.Mode())},
	}}})
	require.NoError(t, err)
	fixture, err := openRepairWriterFixture(t, run, nil)
	require.NoError(t, err)
	_, err = fixture.prepareNamespace(context.Background(), plan, true)
	require.NoError(t, err)
	fixture.close()
	return run
}

func byteBool(value bool) byte {
	if value {
		return 1
	}
	return 0
}
