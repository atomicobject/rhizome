package validate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func snapshotRepairTestTree(t *testing.T, root string) []string {
	t.Helper()
	var snapshot []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := fmt.Sprintf("%s|%v", filepath.ToSlash(rel), info.Mode())
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item += "|" + SourceHash(content)
		}
		snapshot = append(snapshot, item)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(snapshot)
	return snapshot
}

func mustRepairMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode() & (os.ModePerm | repairSpecialMode)
}

// applyCheckFixThroughPlan applies one action from a real check result
// through BuildRepairPlan and ApplyFixPlan. Confirmation is interactive and
// accepted, so the action keeps its original safety tier; an agent-required
// action is never applied and fails the Applied assertion.
func applyCheckFixThroughPlan(t *testing.T, runCtx RunContext, check CheckResult, action FixAction) {
	t.Helper()
	check.Fixes = []FixAction{action}
	plan, err := BuildRepairPlan(t.Context(), runCtx, []CheckResult{check})
	require.NoError(t, err)
	exec, err := ApplyFixPlan(t.Context(), runCtx, plan, Options{
		Fix: true, Confirm: func(string) (bool, error) { return true, nil },
	})
	require.NoError(t, err)
	require.Equal(t, []string{action.ID}, exec.Applied)
}
