package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterruptedRepairReportsLaterUnselectedTransactionAsSkipped(t *testing.T) {
	root := t.TempDir()
	firstBefore := []byte("first before\n")
	laterBefore := []byte("later before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), firstBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "later.md"), laterBefore, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{
		{
			actionID: "action:first", issueKey: "issue:first",
			operations: []RepairOperation{repairWriteOperation(
				"a-interrupted", "action:first", "first.md", firstBefore, []byte("first after\n"),
			)},
		},
		{
			actionID: "action:later-confirm", issueKey: "issue:later-confirm",
			operations: []RepairOperation{repairWriteOperation(
				"z-unselected", "action:later-confirm", "later.md", laterBefore, []byte("later after\n"),
			)},
		},
	})
	plan.Actions[1].Safety = FixSafetyConfirm
	plan, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, ApplyOptions{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, _ string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	assert.Contains(t, execution.Skipped, "action:later-confirm")
	assert.NotContains(t, execution.Failed, "action:later-confirm")
	for _, transaction := range execution.Transactions {
		if transaction.OperationIDs[0] == "z-unselected" {
			assert.Equal(t, "skipped", transaction.Status)
		}
	}
	assert.Equal(t, laterBefore, mustReadFile(t, filepath.Join(root, "later.md")))
}
