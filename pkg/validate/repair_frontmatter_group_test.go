package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequiredListRepairsShareOneFrontmatterOperation(t *testing.T) {
	const body = "# One\n\nBody café with `tags: [example]`.\n"
	for _, tc := range []struct{ name, prefix, after string }{
		{"existing frontmatter", "---\ntype: Spec\n# Keep this comment.\n---\n", "---\ntype: Spec\n# Keep this comment.\nowners: []\ntags: []\n---\n" + body},
		{"without frontmatter", "", "---\nowners: []\ntags: []\n---\n" + body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.prefix + body
			root, _, result := runPreparedOntologySuite(t, map[string]string{
				".rhizome/ontology/schema.graphql": `type Spec @node(paths: ["specs/*.md"]) { tags: [String!]! owners: [String!]! }`,
				"specs/one.md":                     before,
			})
			plan := result.FixPlan
			require.NotNil(t, plan)
			require.Len(t, plan.Actions, 2)
			require.Len(t, plan.Operations, 1)
			op := plan.Operations[0]
			assert.Equal(t, tc.after, string(op.Content))
			assert.Equal(t, SourceHash([]byte(before)), op.SourceHash)
			assert.Equal(t, []string{plan.Actions[0].ID, plan.Actions[1].ID}, op.ActionIDs)
			assert.Equal(t, plan.IssueKeys, op.IssueKeys)
			for _, action := range plan.Actions {
				assert.Equal(t, []string{op.ID}, action.OperationIDs)
				assert.Equal(t, "missing_required_field", action.IssueCode)
			}
			require.Len(t, plan.Transactions, 1)
			require.Empty(t, plan.Transactions[0].Conflicts)
			runCtx := repairRunContext(t, root)
			_, err := buildRepairReviewSelection(result, runCtx, 1, plan.Fingerprint, []string{plan.Actions[0].ID})
			reviewErr := assertRepairReviewError(t, err, RepairReviewErrorTransactionIncomplete)
			assert.Equal(t, op.ActionIDs, reviewErr.RequiredActionIDs)
			partial, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, ApplySelection: []string{plan.Actions[0].ID}})
			require.NoError(t, err)
			assert.Empty(t, partial.Applied)
			assert.Equal(t, op.ActionIDs, partial.Skipped)
			assert.Equal(t, before, string(mustReadFile(t, filepath.Join(root, "specs/one.md"))))
			pending, err := DetectPendingRepairJournals(runCtx)
			require.NoError(t, err)
			assert.Empty(t, pending)
			execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, NonInteractive: true})
			require.NoError(t, err)
			assert.Equal(t, op.ActionIDs, execution.Applied)
			assert.Equal(t, tc.after, string(mustReadFile(t, filepath.Join(root, "specs/one.md"))))
		})
	}
}

func TestFrontmatterGroupComposesAliasesAndIndependentLinkEdits(t *testing.T) {
	const before = "# Note\n\n[[old]] and untouched café.\n"
	const after = "---\naliases: [Alpha, Beta]\ntags: []\n---\n# Note\n\n[[new]] and untouched café.\n"
	first := repairPlannerAction("action:a", "issue:a")
	first.Edits = []FixEdit{
		{Kind: FixKindAppendAlias, NotePath: "note.md", Value: "Beta"},
		{Kind: FixKindRewriteLinkTarget, NotePath: "note.md", StartByte: 10, EndByte: 13, Value: "new"},
	}
	second := repairPlannerAction("action:b", "issue:b")
	second.Edits = []FixEdit{
		{Kind: FixKindAppendAlias, NotePath: "note.md", Value: "Alpha"},
		{Kind: FixKindAppendAlias, NotePath: "note.md", Value: "Beta"},
		{Kind: FixKindSetFrontmatter, NotePath: "note.md", Property: "tags", Values: []string{}},
	}
	root, runCtx, plan := frontmatterRepairPlan(t, before, second, first)
	reordered, err := buildRepairPlanFromActions(t.Context(), runCtx, []FixAction{first, second}, NewOntologyOperationAdapter())
	require.NoError(t, err)
	assert.Equal(t, plan.Fingerprint, reordered.Fingerprint)
	require.Len(t, plan.Operations, 2)
	require.Len(t, plan.Transactions, 1)
	require.Empty(t, plan.Transactions[0].Conflicts)
	assert.Len(t, plan.Actions[0].OperationIDs, 2, "a mixed action retains both distinct operations")
	assert.Len(t, plan.Actions[1].OperationIDs, 1)
	for _, operation := range plan.Operations {
		assert.Equal(t, SourceHash([]byte(before)), operation.SourceHash)
	}
	execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, NonInteractive: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"action:a", "action:b"}, execution.Applied)
	assert.Equal(t, after, string(mustReadFile(t, filepath.Join(root, "note.md"))))
}

func TestFrontmatterGroupCompatibleDuplicateAssignments(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		edit                FixEdit
	}{
		{"same list", "# Body\n", "---\ntags: []\n---\n# Body\n", FixEdit{Kind: FixKindSetFrontmatter, Property: "tags", Values: []string{}}},
		{"same archive transition", "---\ntype: EffortNote\nstatus: complete\n---\n# History\n", "---\ntype: EffortNote\nstatus: archived\n---\n# History\n", FixEdit{Kind: FixKindSetFrontmatter, Property: "status", Value: "archived"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := []FixAction{repairPlannerAction("action:a", "issue:a"), repairPlannerAction("action:b", "issue:b")}
			for index := range actions {
				edit := tc.edit
				edit.NotePath = "note.md"
				actions[index].Edits = []FixEdit{edit}
			}
			root, runCtx, plan := frontmatterRepairPlan(t, tc.before, actions...)
			require.Len(t, plan.Operations, 1)
			assert.Equal(t, []string{"action:a", "action:b"}, plan.Operations[0].ActionIDs)
			execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, NonInteractive: true})
			require.NoError(t, err)
			assert.Equal(t, []string{"action:a", "action:b"}, execution.Applied)
			assert.Equal(t, tc.after, string(mustReadFile(t, filepath.Join(root, "note.md"))))
		})
	}
}

func TestFrontmatterGroupPreservesConflictingAssignments(t *testing.T) {
	const before = "---\nstatus: draft\n---\n# Untouched\n"
	for _, tc := range []struct {
		name        string
		left, right FixEdit
	}{
		{"different values including no-op", FixEdit{Kind: FixKindSetFrontmatter, Property: "status", Value: "draft"}, FixEdit{Kind: FixKindSetFrontmatter, Property: "status", Value: "active"}},
		{"case-insensitive property", FixEdit{Kind: FixKindSetFrontmatter, Property: "Status", Value: "draft"}, FixEdit{Kind: FixKindSetFrontmatter, Property: "status", Value: "active"}},
		{"scalar versus empty list", FixEdit{Kind: FixKindSetFrontmatter, Property: "tags", Value: ""}, FixEdit{Kind: FixKindSetFrontmatter, Property: "tags", Values: []string{}}},
		{"different lists", FixEdit{Kind: FixKindSetFrontmatter, Property: "tags", Values: []string{"One"}}, FixEdit{Kind: FixKindSetFrontmatter, Property: "tags", Values: []string{"Two"}}},
		{"set aliases versus append", FixEdit{Kind: FixKindSetFrontmatter, Property: "aliases", Values: []string{"Set"}}, FixEdit{Kind: FixKindAppendAlias, Value: "Append"}},
		{"append versus set aliases", FixEdit{Kind: FixKindAppendAlias, Value: "Append"}, FixEdit{Kind: FixKindSetFrontmatter, Property: "Aliases", Values: []string{"Set"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := []FixAction{repairPlannerAction("action:a", "issue:a"), repairPlannerAction("action:b", "issue:b")}
			for index, edit := range []FixEdit{tc.left, tc.right} {
				edit.NotePath = "note.md"
				actions[index].Edits = []FixEdit{edit}
			}
			root, runCtx, plan := frontmatterRepairPlan(t, before, actions...)
			require.Len(t, plan.Operations, 1)
			assert.Equal(t, before, string(plan.Operations[0].Content))
			assert.Equal(t, []string{"action:a", "action:b"}, plan.Operations[0].ActionIDs)
			assert.Equal(t, []string{"issue:a", "issue:b"}, plan.Operations[0].IssueKeys)
			require.Len(t, plan.Transactions, 1)
			require.NotEmpty(t, plan.Transactions[0].Conflicts)
			assert.Equal(t, RepairConflictEditOverlap, plan.Transactions[0].Conflicts[0].Kind)
			execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, NonInteractive: true})
			require.NoError(t, err)
			assert.Empty(t, execution.Applied)
			assert.Equal(t, "skipped_conflict", execution.Transactions[0].Status)
			assert.Equal(t, before, string(mustReadFile(t, filepath.Join(root, "note.md"))))
		})
	}
}

func TestFrontmatterGroupRetainsReviewAndSourceAuthority(t *testing.T) {
	const before = "---\ntype: Spec\n---\n# Original body\n"
	actions := []FixAction{repairPlannerAction("action:a", "issue:a"), repairPlannerAction("action:b", "issue:b")}
	for index, property := range []string{"tags", "owners"} {
		actions[index].Edits = []FixEdit{{Kind: FixKindSetFrontmatter, NotePath: "note.md", Property: property, Values: []string{}}}
	}
	root, runCtx, plan := frontmatterRepairPlan(t, before, actions...)
	require.Len(t, plan.Operations, 1)
	changed := *plan
	changed.Operations = append([]RepairOperation(nil), plan.Operations...)
	changed.Operations[0].Content = []byte("different output")
	fingerprint, err := RepairPlanFingerprint(changed)
	require.NoError(t, err)
	assert.NotEqual(t, plan.Fingerprint, fingerprint)
	_, err = ApplyFixPlan(t.Context(), runCtx, &changed, ApplyOptions{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "fingerprint changed")
	assert.Equal(t, before, string(mustReadFile(t, filepath.Join(root, "note.md"))))
	changed.Operations[0] = plan.Operations[0]
	changed.Operations[0].IssueKeys = []string{"issue:a"}
	_, err = FinalizeRepairPlan(changed)
	require.ErrorContains(t, err, "issue membership must equal")
	changed.Operations[0] = plan.Operations[0]
	changed.Operations[0].ActionIDs = []string{"action:a"}
	_, err = FinalizeRepairPlan(changed)
	require.ErrorContains(t, err, "is not owned by an associated action")
	const external = "---\ntype: Spec\n---\n# User changed only the body\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte(external), 0o644))
	execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "source hash changed")
	assert.Empty(t, execution.Applied)
	assert.Equal(t, external, string(mustReadFile(t, filepath.Join(root, "note.md"))))
	pending, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func frontmatterRepairPlan(t *testing.T, before string, actions ...FixAction) (string, RunContext, *RepairPlan) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte(before), 0o644))
	runCtx := repairRunContext(t, root)
	plan, err := buildRepairPlanFromActions(t.Context(), runCtx, actions, NewOntologyOperationAdapter())
	require.NoError(t, err)
	return root, runCtx, plan
}
