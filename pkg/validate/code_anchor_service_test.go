package validate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunCodeAnchorsWithStore_PreservesReadOnlyValidationBehavior(t *testing.T) {
	t.Parallel()

	vaultPath := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(vaultPath, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	store, err := sqlitefixture.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{
		Path:  "docs/code.md",
		Title: "Code",
		DefinedAnchors: []codeanchor.Anchor{{
			Label:   "missing-symbol",
			Kind:    codeanchor.AnchorFunc,
			Lang:    codeanchor.LangGo,
			BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "example", Name: "Missing"},
		}},
	}))

	result := RunCodeAnchorsWithStore(context.Background(), RunContext{
		VaultPath: vaultPath,
		MaxIssues: 20,
	}, store)

	// Suite finalization converts a non-zero IssueCount to OK=false. The raw
	// check result preserves the existing runner contract here.
	require.True(t, result.OK)
	require.Empty(t, result.Error)
	require.Equal(t, 1, result.IssueCount)
	require.Equal(t, "1 anchor issues", result.Summary)
	require.Equal(t, []Issue{{
		Code:    string(codeanchor.ValidationNoMatch),
		Path:    "docs/code.md",
		Field:   "code-anchors",
		Target:  "example.Missing",
		Message: `no symbols found matching "example.Missing"`,
		Data: mustMarshal(CodeAnchorData{
			Label: "missing-symbol", Language: "go", Status: "no_match",
			Guidance: "Update the code-anchors selector, rebuild the code index if needed, then run `rzm validate code-anchors`.",
		}),
	}}, result.Issues)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, FixSafetyAgent, result.Fixes[0].Safety)

	anchors, err := store.Anchors(context.Background())
	require.NoError(t, err)
	require.Len(t, anchors, 1)
}

func TestRunCodeAnchorsWithStoreBuildsExecutableUniqueSuffixConfirmation(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(vaultPath, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "docs"), 0o755))
	content := `---
code-anchors:
  go:
    - symbol: example.Missing
---
# Code
`
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "docs", "code.md"), []byte(content), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{
		Path: "docs/code.md", Title: "Code", DefinedAnchors: []codeanchor.Anchor{{
			Label: "Missing", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo,
			BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "example", Name: "Missing"},
		}},
	}))
	require.NoError(t, store.ReplaceFileSummary(context.Background(), codeanchor.FileSummary{
		FilePath: "code/example.go", Lang: codeanchor.LangGo, ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "code/example.go", FQN: "full.example.Missing", Pkg: "full.example", Name: "Missing"}},
	}))

	runCtx := RunContext{
		VaultDef: obsidian.VaultDefinition{Path: vaultPath}, VaultPath: vaultPath,
		NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}
	result := RunCodeAnchorsWithStore(context.Background(), runCtx, store)

	require.Empty(t, result.Error)
	require.Equal(t, 1, result.IssueCount)
	require.Equal(t, "docs/code.md", result.Issues[0].Path)
	require.Equal(t, "suffix_match", result.Issues[0].Code)
	var data map[string]any
	require.NoError(t, json.Unmarshal(result.Issues[0].Data, &data))
	require.Equal(t, []any{"full.example.Missing"}, data["candidates"])
	require.Len(t, result.Fixes, 1)
	require.Equal(t, FixSafetyConfirm, result.Fixes[0].Safety)
	require.Equal(t, "replace_code_anchor_target", result.Fixes[0].Kind)
	require.Equal(t, []string{"docs/code.md"}, result.Fixes[0].AffectedPaths)
	require.Len(t, result.Fixes[0].Edits, 1)
	require.Equal(t, "example.Missing", result.Fixes[0].Edits[0].OldTarget)
	require.Equal(t, "full.example.Missing", result.Fixes[0].Edits[0].NewTarget)
	issueKey, err := StableIssueKey(CheckCodeAnchors, result.Issues[0])
	require.NoError(t, err)
	require.Equal(t, []string{issueKey}, result.Fixes[0].IssueKeys)

	plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{result})
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.Operations, 1, "confirmation action must be backed by an executable transaction operation")
	require.Contains(t, string(plan.Operations[0].Content), "full.example.Missing")
	require.NotContains(t, string(plan.Operations[0].Content), "symbol: example.Missing")
}

func TestRunCodeAnchorsWithStoreBindsTwoSuffixRepairsToTheirExactIssues(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(vaultPath, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "docs", "code.md"), []byte(`---
code-anchors:
  go:
    - label: First
      symbol: short.First
    - label: Second
      symbol: short.Second
---
# Code
`), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{
		Path: "docs/code.md", Title: "Code", DefinedAnchors: []codeanchor.Anchor{
			{Label: "First", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo, BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "short", Name: "First"}},
			{Label: "Second", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo, BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "short", Name: "Second"}},
		},
	}))
	require.NoError(t, store.ReplaceFileSummary(context.Background(), codeanchor.FileSummary{
		FilePath: "code/example.go", Lang: codeanchor.LangGo, ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{
			{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "code/example.go", FQN: "full.short.First", Pkg: "full.short", Name: "First"},
			{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "code/example.go", FQN: "full.short.Second", Pkg: "full.short", Name: "Second"},
		},
	}))

	result := RunCodeAnchorsWithStore(context.Background(), RunContext{
		VaultPath: vaultPath, VaultDef: obsidian.VaultDefinition{Path: vaultPath}, NoteReader: &obsidian.Note{},
	}, store)

	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Fixes, 2)
	for index := range result.Issues {
		require.Equal(t, FixSafetyConfirm, result.Fixes[index].Safety)
		key, keyErr := StableIssueKey(CheckCodeAnchors, result.Issues[index])
		require.NoError(t, keyErr)
		require.Equal(t, []string{key}, result.Fixes[index].IssueKeys)
		require.NotContains(t, result.Fixes[index].IssueKeys[0], "issue:action:v1:")
	}
	require.NotEqual(t, result.Fixes[0].IssueKeys, result.Fixes[1].IssueKeys)
}

func TestRunCodeAnchorsWithStoreKeepsEachDefiningNoteAsExactRepairOwner(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(vaultPath, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "docs"), 0o755))
	content := []byte("---\ncode-anchors:\n  go:\n    - symbol: short.Missing\n---\n# Code\n")
	for _, path := range []string{"docs/one.md", "docs/two.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(vaultPath, filepath.FromSlash(path)), content, 0o644))
	}
	store, err := sqlitefixture.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	anchor := codeanchor.Anchor{Label: "Missing", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo, BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "short", Name: "Missing"}}
	for _, path := range []string{"docs/one.md", "docs/two.md"} {
		require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{Path: path, Title: path, DefinedAnchors: []codeanchor.Anchor{anchor}}))
	}
	require.NoError(t, store.ReplaceFileSummary(context.Background(), codeanchor.FileSummary{
		FilePath: "code/example.go", Lang: codeanchor.LangGo, ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "code/example.go", FQN: "full.short.Missing", Pkg: "full.short", Name: "Missing"}},
	}))

	result := RunCodeAnchorsWithStore(context.Background(), RunContext{
		VaultPath: vaultPath, VaultDef: obsidian.VaultDefinition{Path: vaultPath}, NoteReader: &obsidian.Note{},
	}, store)

	require.Equal(t, 2, result.IssueCount)
	require.Equal(t, []string{"docs/one.md", "docs/two.md"}, []string{result.Issues[0].Path, result.Issues[1].Path})
	require.Len(t, result.Fixes, 2)
	for index, path := range []string{"docs/one.md", "docs/two.md"} {
		require.Equal(t, FixSafetyConfirm, result.Fixes[index].Safety, "%+v", result.Fixes[index])
		require.Equal(t, []string{path}, result.Fixes[index].AffectedPaths)
		key, keyErr := StableIssueKey(CheckCodeAnchors, result.Issues[index])
		require.NoError(t, keyErr)
		require.Equal(t, []string{key}, result.Fixes[index].IssueKeys)
	}
}

func TestRunCodeAnchorsWithStoreReportsSourceOwnershipStoreErrorsAsCheckErrors(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(vaultPath, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	store, err := sqlitefixture.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{
		Path: "docs/code.md", DefinedAnchors: []codeanchor.Anchor{{
			Label: "Missing", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo,
			BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "example", Name: "Missing"},
		}},
	}))
	_, err = store.DB().ExecContext(context.Background(), "DROP TABLE note_anchors")
	require.NoError(t, err)

	result := RunCodeAnchorsWithStore(context.Background(), RunContext{VaultPath: vaultPath}, store)

	require.False(t, result.OK)
	require.ErrorContains(t, errors.New(result.Error), "load code-anchor source paths")
	require.Empty(t, result.Issues)
	require.Empty(t, result.Fixes)
}
