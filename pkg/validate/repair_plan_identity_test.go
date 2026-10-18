package validate

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRepairPlanFingerprintIsStableAcrossInputOrdering(t *testing.T) {
	left := RepairPlan{
		IssueKeys: []string{"issue:v1:b", "issue:v1:a"},
		Operations: []RepairOperation{
			{ID: "op-b", Kind: RepairOperationWrite, Path: "notes/b.md", SourceHash: "hash-b"},
			{ID: "op-a", Kind: RepairOperationRename, Path: "notes/a.md", DestinationPath: "notes/c.md", SourceHash: "hash-a"},
		},
	}
	right := RepairPlan{
		IssueKeys:  []string{"issue:v1:a", "issue:v1:b"},
		Operations: []RepairOperation{left.Operations[1], left.Operations[0]},
	}

	leftFingerprint, err := RepairPlanFingerprint(left)
	require.NoError(t, err)
	rightFingerprint, err := RepairPlanFingerprint(right)
	require.NoError(t, err)

	assert.Equal(t, leftFingerprint, rightFingerprint)
}

func TestRepairPlanFingerprintIncludesStableActionKeysAndSourceHashes(t *testing.T) {
	base := RepairPlan{
		Actions: []FixAction{{ID: "action:v1:a"}},
		Operations: []RepairOperation{{
			ID:         "op-a",
			ActionID:   "action:v1:a",
			IssueKey:   "issue:v1:a",
			Kind:       RepairOperationWrite,
			Path:       "notes/a.md",
			SourceHash: "sha256:one",
		}},
	}
	baseFingerprint, err := RepairPlanFingerprint(base)
	require.NoError(t, err)

	actionChanged := base
	actionChanged.Actions = []FixAction{{ID: "action:v1:b"}}
	actionFingerprint, err := RepairPlanFingerprint(actionChanged)
	require.NoError(t, err)

	sourceChanged := base
	sourceChanged.Operations = append([]RepairOperation(nil), base.Operations...)
	sourceChanged.Operations[0].SourceHash = "sha256:two"
	sourceFingerprint, err := RepairPlanFingerprint(sourceChanged)
	require.NoError(t, err)

	assert.NotEqual(t, baseFingerprint, actionFingerprint)
	assert.NotEqual(t, baseFingerprint, sourceFingerprint)
}

func TestFinalizeRepairPlanDerivesMembershipAndRejectsMissingPreconditions(t *testing.T) {
	plan := RepairPlan{
		IssueKeys: []string{"issue:v1:a"},
		Actions: []FixAction{{
			ID:        "action:v1:a",
			Check:     CheckOntology,
			IssueKeys: []string{"issue:v1:a"},
		}},
		Operations: []RepairOperation{{
			ID:         "op-a",
			ActionID:   "action:v1:a",
			IssueKey:   "issue:v1:a",
			Kind:       RepairOperationWrite,
			Path:       "notes/a.md",
			SourceHash: "sha256:one",
		}},
	}
	finalized, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)
	assert.NotEmpty(t, finalized.Fingerprint)
	require.Len(t, finalized.Transactions, 1)
	assert.Equal(t, []string{"op-a"}, finalized.Actions[0].OperationIDs)

	plan.Operations[0].SourceHash = ""
	_, err = FinalizeRepairPlan(plan)
	require.Error(t, err)
	assert.ErrorContains(t, err, "source hash")
}
