package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinalizeRepairPlanRequiresAuthorizedOperationIssueMembership(t *testing.T) {
	actions := []FixAction{
		{ID: "action-a", Check: CheckAliases, Safety: FixSafetySafe, IssueKeys: []string{"issue-a"}},
		{ID: "action-b", Check: CheckOntology, Safety: FixSafetySafe, IssueKeys: []string{"issue-b"}},
	}
	base := RepairOperation{
		ID: "operation", ActionIDs: []string{"action-a", "action-b"}, IssueKeys: []string{"issue-a", "issue-b"},
		Kind: RepairOperationWrite, Path: "note.md", SourceHash: SourceHash([]byte("before")),
		Expected: []ExpectedText{{StartByte: 0, EndByte: 6, Text: "before", Replacement: "after"}}, Content: []byte("after"),
	}

	finalized, err := FinalizeRepairPlan(RepairPlan{Actions: actions, Operations: []RepairOperation{base}})
	require.NoError(t, err)
	require.Equal(t, []string{"issue-a", "issue-b"}, finalized.Operations[0].IssueKeys)

	missing := base
	missing.IssueKeys = nil
	_, err = FinalizeRepairPlan(RepairPlan{Actions: actions, Operations: []RepairOperation{missing}})
	require.ErrorContains(t, err, "requires issue membership")

	foreign := base
	foreign.IssueKeys = []string{"issue-a", "issue-foreign"}
	_, err = FinalizeRepairPlan(RepairPlan{Actions: actions, Operations: []RepairOperation{foreign}})
	require.ErrorContains(t, err, "is not owned by an associated action")

	partial := base
	partial.IssueKeys = []string{"issue-a"}
	_, err = FinalizeRepairPlan(RepairPlan{Actions: actions, Operations: []RepairOperation{partial}})
	require.ErrorContains(t, err, "must equal its associated actions")
}
