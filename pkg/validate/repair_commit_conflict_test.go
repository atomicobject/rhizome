package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/stretchr/testify/require"
)

func TestRepairCommitConflictCleansOnlyOwnedMutations(t *testing.T) {
	for _, conflictIndex := range []int{0, 1} {
		t.Run(fmt.Sprint(conflictIndex), func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"a.md", "b.md"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("old"), 0640))
			}
			run := repairRunContext(t, root)
			plan := mustRepairPlan(t, []repairPlanInput{{actionID: "action:conflict", issueKey: "issue:conflict", operations: []RepairOperation{
				repairWriteOperation("op:a", "action:conflict", "a.md", []byte("old"), []byte("new")),
				repairWriteOperation("op:b", "action:conflict", "b.md", []byte("old"), []byte("new")),
			}}})
			target := []string{"a.md", "b.md"}[conflictIndex]
			_, err := ApplyFixPlan(context.Background(), run, &plan, ApplyOptions{Fix: true, NonInteractive: true, repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
				if index == conflictIndex {
					return os.WriteFile(filepath.Join(root, target), []byte("external"), 0640)
				}
				return nil
			}}})
			require.Error(t, err)
			for _, name := range []string{"a.md", "b.md"} {
				expected := "old"
				if name == target {
					expected = "external"
				}
				require.Equal(t, []byte(expected), mustReadFile(t, filepath.Join(root, name)))
				require.NoError(t, sourcetxn.CheckAdmission(root, []string{name}))
			}
			require.NoError(t, sourcetxn.CheckPending(root))
			artifacts, err := filepath.Glob(filepath.Join(root, "*.rzm-repair-*"))
			require.NoError(t, err)
			require.Empty(t, artifacts)
		})
	}
}
