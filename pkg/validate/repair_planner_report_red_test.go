package validate

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanPlainRepairGroupsKeepsOverlappingActionsConflictedAndIndependent(t *testing.T) {
	root := t.TempDir()
	sharedBefore := []byte("0123456789\n")
	independentBefore := []byte("independent\n")
	require.NoError(t, writeRepairTestFile(root, "shared.md", sharedBefore))
	require.NoError(t, writeRepairTestFile(root, "independent.md", independentBefore))
	runCtx := repairRunContext(t, root)

	operations, err := planPlainRepairGroups(runCtx, []plainRepairEdit{
		{
			action: repairPlannerAction("action:overlap-a", "issue:overlap-a"),
			edit: FixEdit{
				Kind: FixKindRewriteLinkTarget, NotePath: "shared.md",
				StartByte: 2, EndByte: 7, Value: "AAAAA",
			},
		},
		{
			action: repairPlannerAction("action:overlap-b", "issue:overlap-b"),
			edit: FixEdit{
				Kind: FixKindRewriteLinkTarget, NotePath: "shared.md",
				StartByte: 4, EndByte: 9, Value: "BBBBB",
			},
		},
		{
			action: repairPlannerAction("action:independent", "issue:independent"),
			edit: FixEdit{
				Kind: FixKindRewriteLinkTarget, NotePath: "independent.md",
				StartByte: 0, EndByte: 3, Value: "IND",
			},
		},
	})
	require.NoError(t, err)

	transactions, err := GroupRepairTransactions(operations)
	require.NoError(t, err)
	require.Len(t, transactions, 2, "same-path edits should connect without absorbing independent work")
	sort.Slice(transactions, func(i, j int) bool {
		return transactions[i].AffectedPaths[0] < transactions[j].AffectedPaths[0]
	})
	assert.Equal(t, []string{"independent.md"}, transactions[0].AffectedPaths)
	assert.Empty(t, transactions[0].Conflicts)
	assert.Equal(t, []string{"shared.md"}, transactions[1].AffectedPaths)
	require.NotEmpty(t, transactions[1].Conflicts)
	assert.Equal(t, RepairConflictEditOverlap, transactions[1].Conflicts[0].Kind)
}

func TestPlanOntologyRepairGroupsKeepsPreviewConflictLocal(t *testing.T) {
	root := t.TempDir()
	conflictBefore := []byte("# Conflict\n")
	okBefore := []byte("# OK\n")
	require.NoError(t, writeRepairTestFile(root, "conflict.md", conflictBefore))
	require.NoError(t, writeRepairTestFile(root, "ok.md", okBefore))
	runCtx := repairRunContext(t, root)
	previewer := ontologyPreviewerFunc(func(
		_ context.Context,
		_ RunContext,
		_ *ontology.Schema,
		request OntologyPreviewRequest,
	) (OntologyPreviewResult, error) {
		path := request.Edits[0].Ref.NotePath
		if path == "conflict.md" {
			return OntologyPreviewResult{Conflicts: []OntologyPreviewConflict{{
				Kind: ontology.ConflictKindCollectionDrift, NotePath: path,
				Message: "concurrent semantic edit",
			}}}, nil
		}
		operation := repairWriteOperation(
			request.OperationID, request.ActionIDs[0], path, okBefore, []byte("# OK\n^ok\n"),
		)
		operation.ActionIDs = append([]string(nil), request.ActionIDs...)
		operation.IssueKey = request.IssueKeys[0]
		operation.IssueKeys = append([]string(nil), request.IssueKeys...)
		return OntologyPreviewResult{Operations: []RepairOperation{operation}}, nil
	})

	operations, supplemental, err := planOntologyRepairGroups(
		context.Background(), runCtx, &ontology.Schema{}, previewer,
		[]structuredRepairEdit{
			{
				action: repairPlannerAction("action:conflict", "issue:conflict"),
				edit:   FixEdit{Kind: FixKindEnsureBlockID, NotePath: "conflict.md", BlockID: "conflict"},
			},
			{
				action: repairPlannerAction("action:ok", "issue:ok"),
				edit:   FixEdit{Kind: FixKindEnsureBlockID, NotePath: "ok.md", BlockID: "ok"},
			},
		},
	)
	require.NoError(t, err, "one preview conflict must not abort independent planning")
	assert.Empty(t, supplemental)

	transactions, err := GroupRepairTransactions(operations)
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	statuses := map[string]bool{}
	for _, transaction := range transactions {
		statuses[transaction.AffectedPaths[0]] = len(transaction.Conflicts) > 0
	}
	assert.Equal(t, map[string]bool{"conflict.md": true, "ok.md": false}, statuses)
}

func TestMergedMultiActionRepairReportsEveryIssueKey(t *testing.T) {
	root := t.TempDir()
	before := []byte("# Note\n")
	require.NoError(t, writeRepairTestFile(root, "note.md", before))
	runCtx := repairRunContext(t, root)
	actions := []FixAction{
		{
			ID: "action:safe", Check: CheckViews, Kind: "append", Safety: FixSafetySafe,
			Title: "safe", IssueKeys: []string{"issue:safe"},
			Edits: []FixEdit{{Kind: FixKindAddSectionScaffold, NotePath: "note.md", Property: "H2", Value: "Safe"}},
		},
		{
			ID: "action:confirm", Check: CheckViews, Kind: "append", Safety: FixSafetyConfirm,
			Title: "confirm", IssueKeys: []string{"issue:confirm"},
			Edits: []FixEdit{{Kind: FixKindAddSectionScaffold, NotePath: "note.md", Property: "H2", Value: "Confirm"}},
		},
	}
	plan, err := buildRepairPlanFromActions(context.Background(), runCtx, actions, NewOntologyOperationAdapter())
	require.NoError(t, err)

	execution, err := ApplyFixPlan(context.Background(), runCtx, plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"action:safe", "action:confirm"}, execution.Skipped)
	assert.Equal(t, []string{"issue:confirm", "issue:safe"}, execution.RemainingIssueKeys)
	assert.Equal(t, before, mustReadFile(t, repairTestPath(root, "note.md")))
}

func TestRepairDeltaForEditThenRenameMarksDestinationChanged(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, writeRepairTestFile(root, "old.md", before))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:move-edit", issueKey: "issue:move-edit",
		operations: []RepairOperation{
			repairWriteOperation("op-write", "action:move-edit", "old.md", before, []byte("after\n")),
			{
				ID: "op-rename", Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
				SourceHash: SourceHash(before),
			},
		},
	}})
	probe := &repairPostApplyProbe{t: t}

	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"new.md"}, probe.changed)
	assert.NotContains(t, probe.changed, "old.md")
	assert.Equal(t, []PathRename{{From: "old.md", To: "new.md"}}, probe.renamed)
}

func TestRepairInterruptionReportsLaterTransactionsNotAttempted(t *testing.T) {
	root := t.TempDir()
	firstBefore := []byte("first\n")
	secondBefore := []byte("second\n")
	require.NoError(t, writeRepairTestFile(root, "first.md", firstBefore))
	require.NoError(t, writeRepairTestFile(root, "second.md", secondBefore))
	plan := mustRepairPlan(t, []repairPlanInput{
		{
			actionID: "action:first", issueKey: "issue:first",
			operations: []RepairOperation{repairWriteOperation(
				"a-interrupted", "action:first", "first.md", firstBefore, []byte("first after\n"),
			)},
		},
		{
			actionID: "action:second", issueKey: "issue:second",
			operations: []RepairOperation{repairWriteOperation(
				"z-not-attempted", "action:second", "second.md", secondBefore, []byte("second after\n"),
			)},
		},
	})

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, _ string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	require.Len(t, execution.Transactions, 2)
	statusByOperation := map[string]string{}
	for _, transaction := range execution.Transactions {
		statusByOperation[transaction.OperationIDs[0]] = transaction.Status
	}
	assert.Equal(t, "interrupted", statusByOperation["a-interrupted"])
	assert.Equal(t, "not_attempted", statusByOperation["z-not-attempted"])
	assert.Contains(t, execution.RemainingIssueKeys, "issue:second")
}

type ontologyPreviewerFunc func(
	context.Context,
	RunContext,
	*ontology.Schema,
	OntologyPreviewRequest,
) (OntologyPreviewResult, error)

func (f ontologyPreviewerFunc) Preview(
	ctx context.Context,
	runCtx RunContext,
	schema *ontology.Schema,
	request OntologyPreviewRequest,
) (OntologyPreviewResult, error) {
	return f(ctx, runCtx, schema, request)
}

func repairPlannerAction(id, issueKey string) FixAction {
	return FixAction{
		ID: id, Check: CheckOntology, Kind: "repair_test", Safety: FixSafetySafe,
		Title: id, IssueKeys: []string{issueKey},
	}
}

func writeRepairTestFile(root, path string, content []byte) error {
	return os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), content, 0o640)
}

func repairTestPath(root, path string) string {
	return filepath.Join(root, filepath.FromSlash(path))
}
