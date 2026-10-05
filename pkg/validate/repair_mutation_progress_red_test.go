package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanRollsBackMutationWhenEntryFinishingFails(t *testing.T) {
	root := t.TempDir()
	firstBefore := []byte("first before\n")
	secondBefore := []byte("second before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a-first.md"), firstBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "z-second.md"), secondBefore, 0o600))
	plan := mustRepairPlan(t, []repairPlanInput{
		{actionID: "action:mutation-first", issueKey: "issue:mutation-first", operations: []RepairOperation{
			repairWriteOperation("op:mutation-first", "action:mutation-first", "a-first.md", firstBefore, []byte("first after\n")),
		}},
		{actionID: "action:mutation-second", issueKey: "issue:mutation-second", operations: []RepairOperation{
			repairWriteOperation("op:mutation-second", "action:mutation-second", "z-second.md", secondBefore, []byte("second after\n")),
		}},
	})
	injected := errors.New("entry finishing failed after mutation")
	fired := false

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterMutation: func(_ int, path string) error {
			if !fired && path == "a-first.md" {
				fired = true
				return injected
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, injected)
	assert.Equal(t, firstBefore, mustReadFile(t, filepath.Join(root, "a-first.md")), "mutation must roll back")
	assert.Equal(t, []byte("second after\n"), mustReadFile(t, filepath.Join(root, "z-second.md")))
	statuses := map[string]string{}
	for _, transaction := range execution.Transactions {
		for _, path := range transaction.AffectedPaths {
			statuses[path] = transaction.Status
		}
	}
	assert.Equal(t, "failed", statuses["a-first.md"])
	assert.Equal(t, "applied", statuses["z-second.md"])
}
