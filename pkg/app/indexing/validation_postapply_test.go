package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationProjectionPostApplyRefresherUsesHeldLeaseAndExactPaths(t *testing.T) {
	root := writeValidationProjectionVault(t)
	path := filepath.Join(root, "notes", "one.md")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	after := []byte("---\ntype: ExampleNote\nname: Updated\n---\n# Updated\n^UpdatedBlock\n")
	plan := postApplyProjectionRepairPlan(t, "notes/one.md", before, after)

	execution, err := validate.ApplyFixPlan(context.Background(), validate.RunContext{
		VaultPath:  root,
		VaultDef:   obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteReader: &obsidian.Note{},
	}, &plan, validate.Options{
		Fix:            true,
		NonInteractive: true,
		PostApplyRefresher: ValidationProjectionPostApplyRefresher{
			VaultPath:    root,
			VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
			NoteMetadata: testNoteMetadataIndexer(t),
			NoteReader:   &obsidian.Note{},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.NotNil(t, execution.Refresh)
	assert.Equal(t, []string{"links", "markdown_targets", "metadata", "ontology"}, execution.Refresh.Domains)
	assert.Equal(t, []string{"notes/one.md"}, execution.Refresh.Paths)
	assert.NotEmpty(t, execution.Refresh.Timings)
	assert.Equal(t, after, mustReadPostApplyProjectionFile(t, path))
}

func TestValidationProjectionPostApplyRefresherDoesNotParseUnrelatedInvalidCodeAnchors(t *testing.T) {
	root := writeValidationProjectionVault(t)
	path := filepath.Join(root, "notes", "one.md")
	before := []byte(`---
type: ExampleNote
name: One
code-anchors:
  go:
    - symbol: MissingQualification
---
# One
`)
	after := []byte(`---
type: ExampleNote
name: Updated
code-anchors:
  go:
    - symbol: MissingQualification
---
# Updated
`)
	require.NoError(t, os.WriteFile(path, before, 0o644))
	plan := postApplyProjectionRepairPlan(t, "notes/one.md", before, after)

	execution, err := validate.ApplyFixPlan(context.Background(), validate.RunContext{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}, &plan, validate.Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: ValidationProjectionPostApplyRefresher{
			VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t), NoteReader: &obsidian.Note{},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, execution.Refresh)
	require.NotContains(t, execution.Refresh.Domains, string(ProjectionDomainCodeAnchors))
	assert.Equal(t, after, mustReadPostApplyProjectionFile(t, path))
}

func TestValidationProjectionPostApplyRefresherScopesCodeAnchorPathsPerTransaction(t *testing.T) {
	root, plan, afterUnrelated, afterAnchor := postApplyMixedRepairFixture(t)

	execution, err := validate.ApplyFixPlan(context.Background(), postApplyMixedRunContext(root), &plan, validate.Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: postApplyProjectionRefresher(root),
	})
	require.NoError(t, err)
	require.NotNil(t, execution.Refresh)
	require.Contains(t, execution.Refresh.Domains, string(ProjectionDomainCodeAnchors))
	assert.Equal(t, afterUnrelated, mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes", "unrelated.md")))
	assert.Equal(t, afterAnchor, mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes", "anchor.md")))
}

func TestValidationProjectionPostApplyRefresherPreservesPerTransactionScopeDuringCommittedRecovery(t *testing.T) {
	root, plan, afterUnrelated, afterAnchor := postApplyMixedRepairFixture(t)

	_, err := validate.ApplyFixPlan(context.Background(), postApplyMixedRunContext(root), &plan, validate.Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: postApplyFailingRefresher{},
	})
	require.ErrorContains(t, err, "forced post-apply failure")
	pending, err := validate.DetectPendingRepairJournals(postApplyMixedRunContext(root))
	require.NoError(t, err)
	require.NotEmpty(t, pending)

	execution, err := validate.ApplyFixPlan(context.Background(), postApplyMixedRunContext(root), &plan, validate.Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: postApplyProjectionRefresher(root),
	})
	require.NoError(t, err)
	require.NotNil(t, execution.Refresh)
	require.Contains(t, execution.Refresh.Domains, string(ProjectionDomainCodeAnchors))
	assert.Equal(t, afterUnrelated, mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes", "unrelated.md")))
	assert.Equal(t, afterAnchor, mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes", "anchor.md")))
	pending, err = validate.DetectPendingRepairJournals(postApplyMixedRunContext(root))
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestValidationProjectionPostApplyRefresherReingestsChangedCodeAnchorSource(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	path := filepath.Join(root, "notes", "one.md")
	before := []byte(`---
type: ExampleNote
name: One
code-anchors:
  go:
    - symbol: example.Missing
---
# One
`)
	after := []byte(`---
type: ExampleNote
name: One
code-anchors:
  go:
    - symbol: full.example.Missing
---
# One
`)
	require.NoError(t, os.WriteFile(path, before, 0o644))

	initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t), Target: ValidationProjectionLive,
	})
	require.NoError(t, err)
	require.NoError(t, initial.Runtime.Store.UpsertNote(ctx, codeanchor.Note{
		Path: "notes/one.md", Title: "One", DefinedAnchors: []codeanchor.Anchor{{
			Label: "Missing", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo,
			BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "example", Name: "Missing"},
		}},
	}))
	require.NoError(t, initial.Runtime.Store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "code/example.go", Lang: codeanchor.LangGo, ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{{
			Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "code/example.go",
			FQN: "full.example.Missing", Pkg: "full.example", Name: "Missing",
		}},
	}))
	beforeMatches, err := initial.Runtime.Store.UntouchedIndexDomainsSummary(ctx)
	require.NoError(t, err)
	beforeSelectors, err := initial.Runtime.Store.CodeAnchorSelectorSummary(ctx)
	require.NoError(t, err)
	beforeFreshness := codeAnchorFreshnessHash(beforeMatches.CodeAnchors, beforeSelectors)
	require.NoError(t, initial.Close())

	plan := postApplyCodeAnchorRepairPlan(t, "notes/one.md", before, after)
	execution, err := validate.ApplyFixPlan(ctx, validate.RunContext{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}, &plan, validate.Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: ValidationProjectionPostApplyRefresher{
			VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t), NoteReader: &obsidian.Note{},
		},
	})
	require.NoError(t, err)
	require.Contains(t, execution.Refresh.Domains, string(ProjectionDomainCodeAnchors))

	store, err := sqlitefixture.Open(obsidian.UnifiedIndexPath(root, ""))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	afterMatches, err := store.UntouchedIndexDomainsSummary(ctx)
	require.NoError(t, err)
	afterSelectors, err := store.CodeAnchorSelectorSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeMatches.CodeAnchors, afterMatches.CodeAnchors, "note selector repair must not masquerade as project-code generation")
	require.Equal(t, beforeSelectors.Count, afterSelectors.Count)
	require.NotEqual(t, beforeFreshness, codeAnchorFreshnessHash(afterMatches.CodeAnchors, afterSelectors))
	check := validate.RunCodeAnchorsWithStore(ctx, validate.RunContext{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}, store)
	require.Empty(t, check.Error)
	require.Zero(t, check.IssueCount, "post-apply code-anchor postcheck must read the repaired selector")
}

func TestValidationProjectionPostApplyRefresherDeletesOrphanedCodeAnchorSource(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Enabled: true},
	}))
	path := filepath.Join(root, "notes", "one.md")
	before := []byte("---\ntype: ExampleNote\nname: One\ncode-anchors:\n  go:\n    - symbol: example.Missing\n---\n# One\n")
	require.NoError(t, os.WriteFile(path, before, 0o644))

	initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t), Target: ValidationProjectionLive,
	})
	require.NoError(t, err)
	require.NoError(t, initial.Runtime.Store.UpsertNote(ctx, codeanchor.Note{
		Path: "notes/one.md", Title: "One", DefinedAnchors: []codeanchor.Anchor{{
			Label: "Missing", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo,
			BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "example", Name: "Missing"},
		}},
	}))
	require.NoError(t, initial.Close())

	plan := postApplyCodeAnchorDeletePlan(t, "notes/one.md", before)
	execution, err := validate.ApplyFixPlan(ctx, validate.RunContext{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}, &plan, validate.Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: ValidationProjectionPostApplyRefresher{
			VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t), NoteReader: &obsidian.Note{},
		},
	})
	require.NoError(t, err)
	require.Contains(t, execution.Refresh.Domains, string(ProjectionDomainCodeAnchors))

	store, err := sqlitefixture.Open(obsidian.UnifiedIndexPath(root, ""))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	anchors, err := store.Anchors(ctx)
	require.NoError(t, err)
	require.Empty(t, anchors, "deleted defining notes must not leave orphan anchors behind a fresh claim")
}

func TestValidationProjectionPostApplyRefresherRejectsDifferentVaultBeforeProjection(t *testing.T) {
	root := writeValidationProjectionVault(t)
	otherRoot := writeValidationProjectionVault(t)
	path := filepath.Join(root, "notes", "one.md")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	after := append(append([]byte(nil), before...), '\n')
	plan := postApplyProjectionRepairPlan(t, "notes/one.md", before, after)

	_, err = validate.ApplyFixPlan(context.Background(), validate.RunContext{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
	}, &plan, validate.Options{
		Fix:            true,
		NonInteractive: true,
		PostApplyRefresher: ValidationProjectionPostApplyRefresher{
			VaultPath:    otherRoot,
			VaultDef:     obsidian.VaultDefinition{Path: otherRoot},
			NoteMetadata: testNoteMetadataIndexer(t),
		},
	})
	require.ErrorContains(t, err, "different vault")
	assert.NoFileExists(t, obsidian.UnifiedIndexPath(otherRoot, ""), "wrong-vault proof must precede projection writes")
	assert.Equal(t, after, mustReadPostApplyProjectionFile(t, path), "the frozen refresher seam runs after repair commit")
}

func TestPostApplyProjectionResultMappingIsDeterministicAndTransfersOwnership(t *testing.T) {
	closeCalls := 0
	projection := &ValidationProjectionResult{
		Runtime:      &ontology.Runtime{},
		ChangedPaths: []paths.NotePath{"z.md", "a.md", "z.md"},
		DeletedPaths: []paths.NotePath{"gone.md", "a.md"},
		Freshness: map[ProjectionDomain]ProjectionFreshness{
			ProjectionDomainOntology:   {State: ProjectionFresh},
			ProjectionDomainCode:       {State: ProjectionUntouched},
			ProjectionDomainMetadata:   {State: ProjectionFresh},
			ProjectionDomainEmbeddings: {State: ProjectionUnavailable},
			ProjectionDomainLinks:      {State: ProjectionFresh},
		},
		Timings: map[string]time.Duration{
			"zeta":  12 * time.Millisecond,
			"alpha": 3 * time.Millisecond,
		},
		closeFn: func() error {
			closeCalls++
			return nil
		},
	}

	result, err := newPostApplyProjectionResult(projection)
	require.NoError(t, err)
	assert.Same(t, projection.Runtime, result.Runtime)
	assert.Equal(t, []string{"links", "metadata", "ontology"}, result.Domains)
	assert.Equal(t, []string{"a.md", "gone.md", "z.md"}, result.Paths)
	assert.Equal(t, []validate.PostApplyTiming{
		{Phase: "alpha", DurationMs: 3},
		{Phase: "zeta", DurationMs: 12},
	}, result.Timings)
	require.NoError(t, result.Close())
	require.NoError(t, result.Close())
	assert.Equal(t, 1, closeCalls)
}

func TestPostApplyProjectionExactPathsIncludeRenameEndpoints(t *testing.T) {
	root := t.TempDir()
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)

	exact, err := postApplyProjectionExactPaths(
		vaultPaths,
		[]string{"notes/changed.md", "notes/moved.md"},
		[]validate.PathRename{{From: "notes/old.md", To: "notes/moved.md"}},
		[]string{"notes/deleted.md", "notes/old.md"},
	)
	require.NoError(t, err)
	assert.Equal(t, []paths.NotePath{"notes/changed.md", "notes/moved.md"}, exact.Changed)
	assert.Equal(t, []paths.NotePath{"notes/deleted.md", "notes/old.md"}, exact.Deleted)
}

func TestPostApplyProjectionResultMappingClosesOwnerWhenRuntimeMissing(t *testing.T) {
	closeErr := errors.New("close failed")
	closeCalls := 0
	projection := &ValidationProjectionResult{
		closeFn: func() error {
			closeCalls++
			return closeErr
		},
	}

	_, err := newPostApplyProjectionResult(projection)
	require.ErrorContains(t, err, "prepared validation runtime is absent")
	require.ErrorIs(t, err, closeErr)
	assert.Equal(t, 1, closeCalls)
}

func postApplyProjectionRepairPlan(t *testing.T, path string, before, after []byte) validate.RepairPlan {
	t.Helper()
	const (
		actionID = "action:validation-projection-postapply"
		issueKey = "issue:validation-projection-postapply"
	)
	plan, err := validate.FinalizeRepairPlan(validate.RepairPlan{
		Actions: []validate.FixAction{{
			ID:        actionID,
			Check:     validate.CheckOntology,
			Kind:      "validation_projection_postapply_test",
			Safety:    validate.FixSafetySafe,
			Title:     "Update validation projection fixture",
			IssueKeys: []string{issueKey},
		}},
		Operations: []validate.RepairOperation{{
			ID:         "operation:validation-projection-postapply",
			ActionID:   actionID,
			IssueKey:   issueKey,
			Kind:       validate.RepairOperationWrite,
			Path:       path,
			SourceHash: validate.SourceHash(before),
			Expected: []validate.ExpectedText{{
				StartByte: 0,
				EndByte:   len(before),
				Text:      string(before),
			}},
			Content: after,
		}},
	})
	require.NoError(t, err)
	return plan
}

func postApplyCodeAnchorRepairPlan(t *testing.T, path string, before, after []byte) validate.RepairPlan {
	t.Helper()
	const (
		actionID = "action:validation-projection-code-anchor-postapply"
		issueKey = "issue:validation-projection-code-anchor-postapply"
	)
	plan, err := validate.FinalizeRepairPlan(validate.RepairPlan{
		Actions: []validate.FixAction{{
			ID: actionID, Check: validate.CheckCodeAnchors, IssueCode: "suffix_match",
			Kind: validate.FixKindReplaceCodeAnchorTarget, Safety: validate.FixSafetySafe,
			Title: "Repair code anchor", IssueKeys: []string{issueKey},
		}},
		Operations: []validate.RepairOperation{{
			ID: "operation:validation-projection-code-anchor-postapply", ActionID: actionID,
			IssueKey: issueKey, RequiredChecks: []string{validate.CheckCodeAnchors},
			Kind: validate.RepairOperationWrite, Path: path, SourceHash: validate.SourceHash(before),
			Expected: []validate.ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before)}},
			Content:  after,
		}},
	})
	require.NoError(t, err)
	return plan
}

func postApplyCodeAnchorDeletePlan(t *testing.T, path string, before []byte) validate.RepairPlan {
	t.Helper()
	const (
		actionID = "action:validation-projection-code-anchor-delete"
		issueKey = "issue:validation-projection-code-anchor-delete"
	)
	plan, err := validate.FinalizeRepairPlan(validate.RepairPlan{
		Actions: []validate.FixAction{{
			ID: actionID, Check: validate.CheckCodeAnchors, IssueCode: "no_match",
			Kind: "delete_code_anchor_source", Safety: validate.FixSafetySafe,
			Title: "Delete code anchor source", IssueKeys: []string{issueKey},
		}},
		Operations: []validate.RepairOperation{{
			ID: "operation:validation-projection-code-anchor-delete", ActionID: actionID,
			IssueKey: issueKey, RequiredChecks: []string{validate.CheckCodeAnchors},
			Kind: validate.RepairOperationDelete, Path: path, SourceHash: validate.SourceHash(before),
			Expected: []validate.ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before)}},
		}},
	})
	require.NoError(t, err)
	return plan
}

func postApplyMixedRepairFixture(t *testing.T) (string, validate.RepairPlan, []byte, []byte) {
	t.Helper()
	root := writeValidationProjectionVault(t)
	beforeUnrelated := []byte(`---
type: ExampleNote
name: Unrelated
code-anchors:
  go:
    - symbol: MissingQualification
---
# Unrelated
`)
	afterUnrelated := []byte(`---
type: ExampleNote
name: Updated
code-anchors:
  go:
    - symbol: MissingQualification
---
# Updated
`)
	beforeAnchor := []byte(`---
type: ExampleNote
name: Anchor
code-anchors:
  go:
    - symbol: example.Missing
---
# Anchor
`)
	afterAnchor := []byte(`---
type: ExampleNote
name: Anchor
code-anchors:
  go:
    - symbol: full.example.Missing
---
# Anchor
`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "unrelated.md"), beforeUnrelated, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "anchor.md"), beforeAnchor, 0o644))

	plan, err := validate.FinalizeRepairPlan(validate.RepairPlan{
		Actions: []validate.FixAction{
			{ID: "action:unrelated", Check: validate.CheckOntology, Kind: "update_unrelated", Safety: validate.FixSafetySafe, IssueKeys: []string{"issue:unrelated"}},
			{ID: "action:anchor", Check: validate.CheckCodeAnchors, Kind: validate.FixKindReplaceCodeAnchorTarget, Safety: validate.FixSafetySafe, IssueKeys: []string{"issue:anchor"}},
		},
		Operations: []validate.RepairOperation{
			{
				ID: "operation:unrelated", ActionID: "action:unrelated", IssueKey: "issue:unrelated",
				RequiredChecks: []string{validate.CheckOntology}, Kind: validate.RepairOperationWrite,
				Path: "notes/unrelated.md", SourceHash: validate.SourceHash(beforeUnrelated),
				Expected: []validate.ExpectedText{{StartByte: 0, EndByte: len(beforeUnrelated), Text: string(beforeUnrelated)}}, Content: afterUnrelated,
			},
			{
				ID: "operation:anchor", ActionID: "action:anchor", IssueKey: "issue:anchor",
				RequiredChecks: []string{validate.CheckCodeAnchors}, Kind: validate.RepairOperationWrite,
				Path: "notes/anchor.md", SourceHash: validate.SourceHash(beforeAnchor),
				Expected: []validate.ExpectedText{{StartByte: 0, EndByte: len(beforeAnchor), Text: string(beforeAnchor)}}, Content: afterAnchor,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, plan.Transactions, 2)
	return root, plan, afterUnrelated, afterAnchor
}

func postApplyMixedRunContext(root string) validate.RunContext {
	return validate.RunContext{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}
}

func postApplyProjectionRefresher(root string) ValidationProjectionPostApplyRefresher {
	return ValidationProjectionPostApplyRefresher{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexerForHelper(), NoteReader: &obsidian.Note{},
	}
}

type postApplyFailingRefresher struct{}

func (postApplyFailingRefresher) Refresh(
	context.Context,
	*validate.IndexLockLease,
	[]string,
	[]validate.PathRename,
	[]string,
) (validate.PostApplyRefreshResult, error) {
	return validate.PostApplyRefreshResult{}, errors.New("forced post-apply failure")
}

func mustReadPostApplyProjectionFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}
