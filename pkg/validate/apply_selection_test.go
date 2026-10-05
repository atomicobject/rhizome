package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectRepairActionsWithApplySelection(t *testing.T) {
	actions := []FixAction{
		{ID: "safe", Safety: FixSafetySafe, IssueKeys: []string{"issue:v1:safe"}},
		{ID: "confirm", Safety: FixSafetyConfirm, IssueKeys: []string{"issue:v1:confirm"}},
		{ID: "agent", Safety: FixSafetyAgent, IssueKeys: []string{"issue:v1:agent"}},
	}
	failConfirm := func(string) (bool, error) {
		t.Fatal("Confirm must not be consulted for an explicit selection")
		return false, nil
	}
	tests := []struct {
		name      string
		selection []string
		want      map[string]bool
		wantErr   string
	}{
		{
			name:      "action id selects confirmation action and skips unselected safe action",
			selection: []string{"confirm"},
			want:      map[string]bool{"safe": false, "confirm": true, "agent": false},
		},
		{
			name:      "issue key selects its action",
			selection: []string{" issue:v1:safe "},
			want:      map[string]bool{"safe": true, "confirm": false, "agent": false},
		},
		{
			name:      "unknown entries are named",
			selection: []string{"confirm", "missing-b", "missing-a"},
			wantErr:   "not found in the current plan: missing-a, missing-b",
		},
		{
			name:      "agent_required action is rejected",
			selection: []string{"issue:v1:agent"},
			wantErr:   "agent_required and have no deterministic edits: agent",
		},
		{
			name:      "blank selection is rejected",
			selection: []string{" "},
			wantErr:   "selection is empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected, err := selectRepairActions(actions, Options{ApplySelection: tt.selection, Confirm: failConfirm})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, selected)
		})
	}
}

func TestApplyFixPlanAppliesExactlySelectedConfirmationAction(t *testing.T) {
	root := t.TempDir()
	confirmBefore, safeBefore := []byte("confirm before\n"), []byte("safe before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "confirm.md"), confirmBefore, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "safe.md"), safeBefore, 0o644))
	plan := selectionTestPlan(t, confirmBefore, safeBefore)

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true, ApplySelection: []string{"issue:confirm"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"action:confirm"}, execution.Applied)
	assert.Contains(t, execution.Skipped, "action:safe")
	assert.Equal(t, []byte("confirm after\n"), mustReadFile(t, filepath.Join(root, "confirm.md")))
	assert.Equal(t, safeBefore, mustReadFile(t, filepath.Join(root, "safe.md")))
}

func TestApplyFixPlanRejectsUnknownSelectionBeforeMutation(t *testing.T) {
	root := t.TempDir()
	confirmBefore, safeBefore := []byte("confirm before\n"), []byte("safe before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "confirm.md"), confirmBefore, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "safe.md"), safeBefore, 0o644))
	plan := selectionTestPlan(t, confirmBefore, safeBefore)

	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true, ApplySelection: []string{"action:confirm", "action:gone"},
	})

	require.ErrorContains(t, err, "action:gone")
	assert.Equal(t, confirmBefore, mustReadFile(t, filepath.Join(root, "confirm.md")))
	assert.Equal(t, safeBefore, mustReadFile(t, filepath.Join(root, "safe.md")))
	assert.NoDirExists(t, filepath.Join(root, ".rhizome", "repair-journal"))
}

func TestApplyRepairSessionRejectsSelectionWithoutReviewedWork(t *testing.T) {
	root := t.TempDir()
	_, _, err := ApplyRepairSession(context.Background(), Result{}, repairRunContext(t, root), Options{
		Fix: true, NonInteractive: true, ApplySelection: []string{"action:any"},
	})
	require.ErrorContains(t, err, "not found in the current plan: action:any")
}

func selectionTestPlan(t *testing.T, confirmBefore, safeBefore []byte) RepairPlan {
	t.Helper()
	plan := mustRepairPlan(t, []repairPlanInput{
		{actionID: "action:confirm", issueKey: "issue:confirm", operations: []RepairOperation{
			repairWriteOperation("op-confirm", "action:confirm", "confirm.md", confirmBefore, []byte("confirm after\n")),
		}},
		{actionID: "action:safe", issueKey: "issue:safe", operations: []RepairOperation{
			repairWriteOperation("op-safe", "action:safe", "safe.md", safeBefore, []byte("safe after\n")),
		}},
	})
	for i := range plan.Actions {
		if plan.Actions[i].ID == "action:confirm" {
			plan.Actions[i].Safety = FixSafetyConfirm
			plan.Actions[i].Question = "Apply confirm?"
		}
	}
	finalized, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)
	return finalized
}
