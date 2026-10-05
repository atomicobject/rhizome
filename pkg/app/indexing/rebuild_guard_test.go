package indexing

import (
	"os"
	"path/filepath"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/stretchr/testify/require"
)

func TestPrepareFreshRebuildRefusesWhileARuntimeHoldsTheIndex(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))

	// The test runner is a live process that is not this one, which is exactly
	// the state the guard protects against.
	require.NoError(t, appruntime.WriteManifest(root, appruntime.InstanceManifest{
		PID:       os.Getppid(),
		VaultPath: root,
		Mode:      appruntime.ModeHeadless,
	}))

	err := PrepareFreshRebuild(root, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "rzm stop")
	require.Contains(t, err.Error(), "headless")
}

func TestPrepareFreshRebuildProceedsWithoutALiveRuntime(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))

	// No manifest at all.
	require.NoError(t, PrepareFreshRebuild(root, ""))

	// A manifest whose PID is dead is not a live runtime.
	require.NoError(t, appruntime.WriteManifest(root, appruntime.InstanceManifest{
		PID:       4194301,
		VaultPath: root,
		Mode:      appruntime.ModeHeadless,
	}))
	require.NoError(t, PrepareFreshRebuild(root, ""))
}
