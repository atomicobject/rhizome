package validate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepairJournalRootFallsBackToVaultDefinitionBasePath(t *testing.T) {
	root := t.TempDir()

	journalRoot, err := repairJournalRoot(RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root},
	})
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(vaultPaths.Root(), ".rhizome", "repair-journal", "v1"), journalRoot)
}

func TestRepairJournalRootRejectsMissingVaultRootWithoutFormattingNilError(t *testing.T) {
	_, err := repairJournalRoot(RunContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve repair journal vault")
	require.False(t, strings.Contains(err.Error(), "%!w(<nil>)"), err.Error())
}

func TestApplyFixPlanRecoversCommittedJournalAfterRefreshInterruption(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	before := []byte("before\n")
	after := []byte("after\n")
	require.NoError(t, os.WriteFile(path, before, 0o640))

	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:journal",
		issueKey: "issue:journal",
		operations: []RepairOperation{{
			ID:         "operation:journal",
			Kind:       RepairOperationWrite,
			Path:       "note.md",
			SourceHash: SourceHash(before),
			Expected:   []ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before)}},
			Content:    after,
		}},
	}})
	refresher := &failOncePostApplyRefresher{}
	runCtx := repairRunContext(t, root)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix:                true,
		NonInteractive:     true,
		PostApplyRefresher: refresher,
	})
	require.ErrorContains(t, err, "injected post-apply interruption")
	assert.Equal(t, after, mustReadFile(t, path), "committed bytes remain for forward recovery")

	journalEntries, err := filepath.Glob(filepath.Join(root, ".rhizome", "repair-journal", "v1", "*"))
	require.NoError(t, err)
	require.Len(t, journalEntries, 1)
	require.FileExists(t, filepath.Join(journalEntries[0], "manifest.json"))
	require.FileExists(t, filepath.Join(journalEntries[0], "COMMITTED"))

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix:                true,
		NonInteractive:     true,
		PostApplyRefresher: refresher,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, refresher.calls)
	assert.Equal(t, after, mustReadFile(t, path))
	assert.NoDirExists(t, journalEntries[0], "successful recovery removes durable transaction state")
}
