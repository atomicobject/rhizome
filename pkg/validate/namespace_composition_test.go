package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeOverwriteCompositionPreservesBothOriginals(t *testing.T) {
	root := t.TempDir()
	old := []byte("source before\n")
	destination := []byte("target before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), old, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), destination, 0o600))
	sourceInfo, err := os.Stat(filepath.Join(root, "old.md"))
	require.NoError(t, err)
	destinationInfo, err := os.Stat(filepath.Join(root, "new.md"))
	require.NoError(t, err)
	mode := repairModeBits(sourceInfo.Mode())
	ops := []RepairOperation{
		{ID: "rename", Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md", SourceHash: SourceHash(old), SourceMode: &mode,
			DestinationState: &RepairDestinationState{Kind: RepairDestinationOccupied, Content: destination, Hash: SourceHash(destination), Mode: repairModeBits(destinationInfo.Mode())}},
		{ID: "write", Kind: RepairOperationWrite, Path: "old.md", SourceHash: SourceHash(old), SourceMode: &mode, Content: []byte("source after\n")},
	}
	states, err := composeRepairFileStates(RunContext{VaultPath: root}, ops)
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Equal(t, "old.md", states[0].rel)
	require.False(t, states[0].finalExists)
	require.Equal(t, old, states[0].originalContent)
	require.Equal(t, destination, states[1].originalContent)
	require.Equal(t, []byte("source after\n"), states[1].finalContent)
	require.Equal(t, sourceInfo.Mode(), states[1].finalMode)
	require.Equal(t, destination, mustReadFile(t, filepath.Join(root, "new.md")))

	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), []byte("external target\n"), 0o600))
	_, err = composeRepairFileStates(RunContext{VaultPath: root}, ops)
	require.ErrorContains(t, err, "destination witness changed")
}

func TestNativeCompositionRejectsSourceModeDrift(t *testing.T) {
	root := t.TempDir()
	content := []byte("source\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), content, 0o600))
	info, err := os.Stat(filepath.Join(root, "old.md"))
	require.NoError(t, err)
	mode := repairModeBits(info.Mode()) ^ 0o200
	_, err = composeRepairFileStates(RunContext{VaultPath: root}, []RepairOperation{{ID: "write", Kind: RepairOperationWrite, Path: "old.md", SourceHash: SourceHash(content), SourceMode: &mode, Content: []byte("after\n")}})
	require.ErrorContains(t, err, "source mode changed")
}

func TestGenericRepairRejectsNativeOverwriteAuthority(t *testing.T) {
	_, err := FinalizeRepairPlan(RepairPlan{Operations: []RepairOperation{{ID: "rename", Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md", SourceHash: SourceHash([]byte("source")), DestinationState: &RepairDestinationState{Kind: RepairDestinationOccupied, Content: []byte("target"), Hash: SourceHash([]byte("target")), Mode: 0o600}}}})
	require.ErrorContains(t, err, "native namespace entrypoint")
}
