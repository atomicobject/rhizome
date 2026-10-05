package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepairModeMatchesPlatformCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		assert.True(t, repairModeMatches(0o666, 0o600))
		assert.False(t, repairModeMatches(0o444, 0o600))
		return
	}
	assert.True(t, repairModeMatches(0o600, 0o600))
	assert.False(t, repairModeMatches(0o644, 0o600))
}

func TestApplyFixPlanPublishesCompletionArtifactAndExcludesItFromRefreshDelta(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	receipt := []byte("receipt\n")
	notePath := filepath.Join(root, "note.md")
	receiptPath := filepath.Join(root, ".rhizome", "edit-receipts", "request.json")
	require.NoError(t, os.WriteFile(notePath, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:completion", issueKey: "issue:completion",
		operations: []RepairOperation{repairWriteOperation("op:completion", "action:completion", "note.md", before, []byte("after\n"))},
	}})
	probe := &repairPostApplyProbe{t: t}

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
		CompletionArtifact: &RepairCompletionArtifact{Path: receiptPath, Content: receipt},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"note.md"}, probe.changed)
	assert.Equal(t, []byte("after\n"), mustReadFile(t, notePath))
	assert.Equal(t, receipt, mustReadFile(t, receiptPath))
	assert.True(t, repairModeMatches(mustRepairMode(t, receiptPath), 0o600))
	assert.Equal(t, 1, execution.AppliedWrites, "the internal receipt is not a repair operation")
	evidence, detectErr := DetectPendingRepairJournals(repairRunContext(t, root))
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanRollsBackCompletionArtifactWithPreparedRepair(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	notePath := filepath.Join(root, "note.md")
	receiptPath := filepath.Join(root, ".rhizome", "edit-receipts", "request.json")
	require.NoError(t, os.WriteFile(notePath, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:completion-rollback", issueKey: "issue:completion-rollback",
		operations: []RepairOperation{repairWriteOperation("op:completion-rollback", "action:completion-rollback", "note.md", before, []byte("after\n"))},
	}})
	runCtx := repairRunContext(t, root)
	injected := errSimulatedRepairInterruption

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		CompletionArtifact: &RepairCompletionArtifact{Path: receiptPath, Content: []byte("receipt\n")},
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, path string) error {
			if path == ".rhizome/edit-receipts/request.json" {
				return injected
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, injected)
	assert.Equal(t, before, mustReadFile(t, notePath))
	assert.Equal(t, []byte("receipt\n"), mustReadFile(t, receiptPath))

	dir, dirErr := repairJournalDir(runCtx, plan.Transactions[0].ID)
	require.NoError(t, dirErr)
	manifest, manifestErr := readRepairJournalManifest(dir)
	require.NoError(t, manifestErr)
	require.Len(t, manifest.Entries, 2)
	var internal repairJournalEntry
	for _, entry := range manifest.Entries {
		if entry.Internal {
			internal = entry
		}
	}
	assert.Equal(t, ".rhizome/edit-receipts/request.json", internal.Path)
	assert.Equal(t, []string{"note.md"}, manifest.Changed)
	assert.Equal(t, []string{"op:completion-rollback"}, manifest.OperationIDs)

	probe := &repairPostApplyProbe{t: t}
	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
	})
	require.NoError(t, err)
	assert.NoFileExists(t, receiptPath)
	assert.Equal(t, before, mustReadFile(t, notePath))
	assert.Equal(t, []string{"note.md"}, probe.changed)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanRecoversCommittedCompletionArtifact(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	notePath := filepath.Join(root, "note.md")
	receiptPath := filepath.Join(root, ".rhizome", "edit-receipts", "request.json")
	require.NoError(t, os.WriteFile(notePath, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:completion-forward", issueKey: "issue:completion-forward",
		operations: []RepairOperation{repairWriteOperation("op:completion-forward", "action:completion-forward", "note.md", before, []byte("after\n"))},
	}})
	runCtx := repairRunContext(t, root)
	refreshErr := errors.New("retain committed completion")

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: refreshErr},
		CompletionArtifact: &RepairCompletionArtifact{Path: receiptPath, Content: []byte("receipt\n")},
	})
	require.ErrorIs(t, err, refreshErr)
	assert.Equal(t, []byte("after\n"), mustReadFile(t, notePath))
	assert.Equal(t, []byte("receipt\n"), mustReadFile(t, receiptPath))

	probe := &repairPostApplyProbe{t: t}
	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("receipt\n"), mustReadFile(t, receiptPath))
	assert.Equal(t, []string{"note.md"}, probe.changed)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanRejectsCompletionArtifactEscapeOrExistingTarget(t *testing.T) {
	for _, test := range []struct {
		name       string
		artifact   func(root string) *RepairCompletionArtifact
		wantInErr  string
		createWant bool
	}{
		{
			name: "escape",
			artifact: func(root string) *RepairCompletionArtifact {
				return &RepairCompletionArtifact{Path: filepath.Join(filepath.Dir(root), "outside-receipt.json"), Content: []byte("receipt")}
			},
			wantInErr: "completion artifact",
		},
		{
			name: "existing target",
			artifact: func(root string) *RepairCompletionArtifact {
				return &RepairCompletionArtifact{Path: filepath.Join(root, ".rhizome", "edit-receipts", "request.json"), Content: []byte("new")}
			},
			wantInErr:  "already exists",
			createWant: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			before := []byte("before\n")
			notePath := filepath.Join(root, "note.md")
			require.NoError(t, os.WriteFile(notePath, before, 0o640))
			if test.createWant {
				receiptPath := filepath.Join(root, ".rhizome", "edit-receipts", "request.json")
				require.NoError(t, os.MkdirAll(filepath.Dir(receiptPath), 0o700))
				require.NoError(t, os.WriteFile(receiptPath, []byte("old"), 0o600))
			}
			plan := mustRepairPlan(t, []repairPlanInput{{
				actionID: "action:completion-target", issueKey: "issue:completion-target",
				operations: []RepairOperation{repairWriteOperation("op:completion-target", "action:completion-target", "note.md", before, []byte("after\n"))},
			}})

			_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
				Fix: true, NonInteractive: true, CompletionArtifact: test.artifact(root),
			})
			require.ErrorContains(t, err, test.wantInErr)
			assert.Equal(t, before, mustReadFile(t, notePath))
		})
	}
}

func TestPublishRepairCompletionArtifactUsesValidatedJournaledPath(t *testing.T) {
	root := t.TempDir()
	receiptPath := filepath.Join(root, ".rhizome", "edit-receipts", "request.json")
	receipt := []byte("receipt\n")
	runCtx := repairRunContext(t, root)

	require.NoError(t, PublishRepairCompletionArtifact(context.Background(), runCtx, &RepairCompletionArtifact{
		Path: receiptPath, Content: receipt,
	}))
	assert.Equal(t, receipt, mustReadFile(t, receiptPath))
	assert.True(t, repairModeMatches(mustRepairMode(t, receiptPath), 0o600))
	evidence, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	assert.Empty(t, evidence)
}

func TestPublishRepairCompletionArtifactRejectsSymlinkedInternalDirectory(t *testing.T) {
	for _, directory := range []string{".rhizome", ".rhizome/edit-receipts"} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if directory != ".rhizome" {
				require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o700))
			}
			require.NoError(t, os.Symlink(outside, filepath.Join(root, filepath.FromSlash(directory))))
			receiptPath := filepath.Join(root, ".rhizome", "edit-receipts", "request.json")
			runCtx := RunContext{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}

			err := PublishRepairCompletionArtifact(context.Background(), runCtx, &RepairCompletionArtifact{
				Path: receiptPath, Content: []byte("receipt\n"),
			})

			require.ErrorContains(t, err, "escapes vault")
			assert.NoFileExists(t, filepath.Join(outside, "request.json"))
			assert.NoFileExists(t, filepath.Join(outside, "edit-receipts", "request.json"))
			assert.NoFileExists(t, filepath.Join(outside, "index.lock"))
		})
	}
}
