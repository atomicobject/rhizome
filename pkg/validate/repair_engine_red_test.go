package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanRejectsStaleSourceWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	original := []byte("before\n")
	require.NoError(t, os.WriteFile(path, original, 0o644))

	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:stale",
		issueKey: "issue:stale",
		operations: []RepairOperation{{
			ID:         "operation:stale",
			Kind:       RepairOperationWrite,
			Path:       "note.md",
			SourceHash: SourceHash(original),
			Expected:   []ExpectedText{{StartByte: 0, EndByte: len(original), Text: string(original)}},
			Content:    []byte("after\n"),
		}},
	}})
	require.NoError(t, os.WriteFile(path, []byte("changed after planning\n"), 0o644))

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix:            true,
		NonInteractive: true,
		Checks:         []string{CheckViews},
		ReplanCommand:  "rzm validate fix views --scope-note note.md",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "stale")
	require.NotNil(t, execution)
	assert.Empty(t, execution.Applied)
	assert.Equal(t, "rzm validate fix views --scope-note note.md", execution.ReplanCommand,
		"a stale rejection must direct the caller to its own scoped replan command")
	assert.Equal(t, []byte("changed after planning\n"), mustReadFile(t, path))
}

func TestApplyFixPlanRejectsConnectedTransactionWhenOneSourceIsStaleBeforeMutation(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.md")
	secondPath := filepath.Join(root, "second.md")
	firstBefore := []byte("first before\n")
	secondBefore := []byte("second before\n")
	require.NoError(t, os.WriteFile(firstPath, firstBefore, 0o644))
	require.NoError(t, os.WriteFile(secondPath, secondBefore, 0o644))

	// A shared identity deliberately places both files in one transaction. The
	// second stale precondition must reject the whole transaction during
	// preflight, before the first operation is staged or journaled.
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:multi-file",
		issueKey: "issue:multi-file",
		operations: []RepairOperation{
			{
				ID:         "operation:first",
				Kind:       RepairOperationWrite,
				Path:       "first.md",
				SourceHash: SourceHash(firstBefore),
				Expected:   []ExpectedText{{StartByte: 0, EndByte: len(firstBefore), Text: string(firstBefore)}},
				Identities: []string{"node:shared"},
				Content:    []byte("first after\n"),
			},
			{
				ID:         "operation:second",
				Kind:       RepairOperationWrite,
				Path:       "second.md",
				SourceHash: SourceHash([]byte("not the on-disk source\n")),
				Expected:   []ExpectedText{{StartByte: 0, EndByte: len(secondBefore), Text: string(secondBefore)}},
				Identities: []string{"node:shared"},
				Content:    []byte("second after\n"),
			},
		},
	}})

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix:            true,
		NonInteractive: true,
	})

	require.ErrorContains(t, err, "stale repair operation operation:second")
	require.NotNil(t, execution)
	assert.Empty(t, execution.Applied)
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "failed_stale", execution.Transactions[0].Status)
	assert.Equal(t, firstBefore, mustReadFile(t, firstPath))
	assert.Equal(t, secondBefore, mustReadFile(t, secondPath))
	assert.NoDirExists(t, filepath.Join(root, ".rhizome", "repair-journal"), "preflight rejection must not publish a journal")
}

func TestApplyFixPlanPreservesRawBytesAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "raw.md")
	before := []byte{0xff, 'a', '\r', '\n', 0x00, 'b'}
	after := []byte{0xff, 'A', '\r', '\n', 0x00, 'b'}
	require.NoError(t, os.WriteFile(path, before, 0o644))
	require.NoError(t, os.Chmod(path, 0o751))
	wantMode := mustRepairMode(t, path)

	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:raw",
		issueKey: "issue:raw",
		operations: []RepairOperation{{
			ID:         "operation:raw",
			Kind:       RepairOperationWrite,
			Path:       "raw.md",
			SourceHash: SourceHash(before),
			Expected:   []ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before)}},
			Content:    after,
		}},
	}})

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix:            true,
		NonInteractive: true,
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"action:raw"}, execution.Applied)
	assert.Equal(t, after, mustReadFile(t, path))
	assert.Equal(t, wantMode, mustRepairMode(t, path))
}

func TestApplyFixPlanIsolatesConflictingTransaction(t *testing.T) {
	root := t.TempDir()
	conflictPath := filepath.Join(root, "conflict.md")
	independentPath := filepath.Join(root, "independent.md")
	conflictBefore := []byte("conflict before\n")
	independentBefore := []byte("independent before\n")
	require.NoError(t, os.WriteFile(conflictPath, conflictBefore, 0o644))
	require.NoError(t, os.WriteFile(independentPath, independentBefore, 0o644))

	plan := mustRepairPlan(t, []repairPlanInput{
		{
			actionID: "action:conflict-a",
			issueKey: "issue:conflict-a",
			operations: []RepairOperation{{
				ID:         "operation:conflict-a",
				Kind:       RepairOperationWrite,
				Path:       "conflict.md",
				SourceHash: SourceHash(conflictBefore),
				Expected:   []ExpectedText{{StartByte: 0, EndByte: 8, Text: "conflict", Replacement: "first"}},
				Content:    []byte("first before\n"),
			}},
		},
		{
			actionID: "action:conflict-b",
			issueKey: "issue:conflict-b",
			operations: []RepairOperation{{
				ID:         "operation:conflict-b",
				Kind:       RepairOperationWrite,
				Path:       "conflict.md",
				SourceHash: SourceHash(conflictBefore),
				Expected:   []ExpectedText{{StartByte: 0, EndByte: 8, Text: "conflict", Replacement: "second"}},
				Content:    []byte("second before\n"),
			}},
		},
		{
			actionID: "action:independent",
			issueKey: "issue:independent",
			operations: []RepairOperation{{
				ID:         "operation:independent",
				Kind:       RepairOperationWrite,
				Path:       "independent.md",
				SourceHash: SourceHash(independentBefore),
				Expected:   []ExpectedText{{StartByte: 0, EndByte: len(independentBefore), Text: string(independentBefore)}},
				Content:    []byte("independent after\n"),
			}},
		},
	})

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix:            true,
		NonInteractive: true,
	})

	require.NoError(t, err)
	assert.Contains(t, execution.Applied, "action:independent")
	assert.NotContains(t, execution.Applied, "action:conflict-a")
	assert.NotContains(t, execution.Applied, "action:conflict-b")
	assert.Contains(t, execution.Skipped, "action:conflict-a")
	assert.Contains(t, execution.Skipped, "action:conflict-b")
	assert.Equal(t, conflictBefore, mustReadFile(t, conflictPath))
	assert.Equal(t, []byte("independent after\n"), mustReadFile(t, independentPath))
}

type repairPlanInput struct {
	actionID   string
	issueKey   string
	operations []RepairOperation
}

func mustRepairPlan(t *testing.T, inputs []repairPlanInput) RepairPlan {
	t.Helper()
	plan := RepairPlan{}
	for _, input := range inputs {
		plan.Actions = append(plan.Actions, FixAction{
			ID:        input.actionID,
			Check:     CheckOntology,
			Kind:      "repair_test",
			Safety:    FixSafetySafe,
			Title:     input.actionID,
			IssueKeys: []string{input.issueKey},
		})
		for _, operation := range input.operations {
			operation.ActionID = input.actionID
			operation.IssueKey = input.issueKey
			plan.Operations = append(plan.Operations, operation)
		}
	}
	finalized, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)
	return finalized
}

func repairRunContext(t *testing.T, root string) RunContext {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	return RunContext{
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}

type failOncePostApplyRefresher struct {
	calls int
}

func (r *failOncePostApplyRefresher) Refresh(
	_ context.Context,
	lease *IndexLockLease,
	_ []string,
	_ []PathRename,
	_ []string,
) (PostApplyRefreshResult, error) {
	if err := lease.RequireHeld(); err != nil {
		return PostApplyRefreshResult{}, err
	}
	r.calls++
	if r.calls == 1 {
		return PostApplyRefreshResult{}, errors.New("injected post-apply interruption")
	}
	result := NewPostApplyRefreshResult(&ontology.Runtime{}, repairNoopCloseOwner{})
	result.Domains = []string{"ontology"}
	return result, nil
}
