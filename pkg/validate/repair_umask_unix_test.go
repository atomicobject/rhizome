//go:build !windows

package validate

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanPreservesModeDespiteRestrictiveUmask(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	after := []byte("after\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o751))
	require.NoError(t, os.Chmod(path, 0o751))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:umask", issueKey: "issue:umask",
		operations: []RepairOperation{repairWriteOperation(
			"operation:umask", "action:umask", "note.md", before, after,
		)},
	}})

	oldUmask := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(oldUmask) })
	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o751), info.Mode().Perm())
}
