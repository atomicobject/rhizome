package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestValidationProjectionPostApplyRefresherSelectsExactSources(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, tc := range []struct {
			name, from, to string
			content        []byte
			includes       []string
			excludes       []string
			noteOwned      bool
		}{
			{name: "markdown", from: "notes/old.md", to: "notes/new.md", content: []byte("# Moving\n"), noteOwned: true},
			{name: "png", from: "notes/old.png", to: "notes/new.png", content: []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 255}},
			{name: "pdf", from: "notes/old.pdf", to: "notes/new.pdf", content: []byte("%PDF-1.4\nsynthetic attachment\n")},
			{name: "ignored markdown", from: "notes/old.md", to: "ignored/new.md", content: []byte("# Moving\n"), excludes: []string{"ignored/"}},
			{name: "unselected markdown", from: "notes/old.md", to: "outside/new.md", content: []byte("# Moving\n"), includes: []string{"notes/*.md"}},
			{name: "system context", from: "notes/old.md", to: "excluded/CONTEXT.md", content: []byte("# Moving\n"), includes: []string{"notes/*.md"}, excludes: []string{"excluded/"}, noteOwned: true},
			{name: "selected html", from: "notes/old.html", to: "notes/new.html", content: []byte("<title>Moving</title><p id=\"target\">Visible fixture</p>"), includes: []string{"notes/*.md", "notes/*.html"}, noteOwned: true},
		} {
			t.Run(fmt.Sprintf("warm=%t/%s", warm, tc.name), func(t *testing.T) {
				ctx := context.Background()
				root := writeValidationProjectionVault(t)
				definition := obsidian.VaultDefinition{Path: root, Excludes: tc.excludes, Links: obsidian.LinkTypeBoth}
				if tc.includes != nil {
					definition.Path, definition.Root, definition.Includes = "", root, tc.includes
				}
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, tc.to)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, tc.from), tc.content, 0o644))
				// An empty admitted anchor set must not load unrelated declarations.
				require.NoError(t, os.WriteFile(filepath.Join(root, "notes/unrelated.md"), []byte("---\ncode-anchors:\n  go:\n    - symbol: MissingQualification\n---\n# Unrelated\n"), 0o644))
				reader := &obsidian.Note{}
				indexer := testNoteMetadataIndexer(t)
				if warm {
					initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{VaultPath: root, VaultDef: definition, NoteMetadata: indexer, NoteReader: reader})
					require.NoError(t, err)
					require.NoError(t, initial.Close())
				}
				before := mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes/one.md"))
				after := []byte("---\ntype: ExampleNote\nname: Updated\n---\n# Updated\n[[" + filepath.Base(tc.to) + "]]\n")
				plan := postApplyProjectionRepairPlan(t, "notes/one.md", before, after)
				plan.Operations = append(plan.Operations, validate.RepairOperation{
					ID: "operation:move-source", ActionID: plan.Actions[0].ID, IssueKey: plan.Actions[0].IssueKeys[0],
					Kind: validate.RepairOperationRename, Path: tc.from, DestinationPath: tc.to, SourceHash: validate.SourceHash(tc.content),
				})
				plan.Fingerprint = ""
				plan, err := validate.FinalizeRepairPlan(plan)
				require.NoError(t, err)
				runCtx := validate.RunContext{VaultPath: root, VaultDef: definition, NoteMetadata: indexer, NoteReader: reader}
				execution, err := validate.ApplyFixPlan(ctx, runCtx, &plan, validate.Options{
					Fix: true, NonInteractive: true,
					PostApplyRefresher: ValidationProjectionPostApplyRefresher{VaultPath: root, VaultDef: definition, NoteMetadata: indexer, NoteReader: reader},
				})
				require.NoError(t, err)
				require.NoFileExists(t, filepath.Join(root, tc.from))
				require.Equal(t, tc.content, mustReadPostApplyProjectionFile(t, filepath.Join(root, tc.to)))
				require.NotNil(t, execution.Refresh)
				require.ElementsMatch(t, []string{tc.from, tc.to, "notes/one.md"}, execution.Refresh.Paths)
				require.Contains(t, execution.Refresh.Domains, string(ProjectionDomainCodeAnchors))
				pending, err := validate.DetectPendingRepairJournals(runCtx)
				require.NoError(t, err)
				require.Empty(t, pending)
				store, cleanup, err := obsidian.OpenIntelStore(root, false)
				require.NoError(t, err)
				defer cleanup()
				rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{tc.from, tc.to, "notes/one.md"})
				require.NoError(t, err)
				require.NotContains(t, rows, tc.from)
				require.Equal(t, "Updated", rows["notes/one.md"].Title)
				_, projected := rows[tc.to]
				require.Equal(t, tc.noteOwned, projected)
				nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/one.md"})
				require.NoError(t, err)
				require.NotEmpty(t, nodes)
				if tc.name == "selected html" {
					require.Equal(t, "html", rows[tc.to].FormatID)
					targets, err := store.CurrentNoteFragmentTargets(ctx, []string{tc.to}, "", "")
					require.NoError(t, err)
					require.NotEmpty(t, targets)
				}
				if tc.name == "markdown" {
					edges, err := store.GraphDocNoteEdgesForPaths(ctx, []string{"notes/one.md"})
					require.NoError(t, err)
					require.Len(t, edges, 1)
					require.Equal(t, tc.to, edges[0].DstPath)
				}
			})
		}
	}
}

func TestValidationProjectionPostApplyRefresherKeepsEligibleSourceErrors(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		ctx := context.Background()
		root := writeValidationProjectionVault(t)
		request := ValidationProjectionRequest{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t)}
		initial, err := RefreshValidationProjection(ctx, request)
		require.NoError(t, err)
		require.NoError(t, initial.Close())
		request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/missing.md"}}
		request.refreshNoteAnchors = true
		_, err = RefreshValidationProjection(ctx, request)
		require.ErrorContains(t, err, "read note notes/missing.md")
	})
	t.Run("invalid anchor source", func(t *testing.T) {
		ctx := context.Background()
		root := writeValidationProjectionVault(t)
		before := mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes/one.md"))
		after := []byte("---\ntype: ExampleNote\nname: One\ncode-anchors:\n  go:\n    - symbol: MissingQualification\n---\n# One\n")
		plan := postApplyCodeAnchorRepairPlan(t, "notes/one.md", before, after)
		runCtx := postApplyMixedRunContext(root)
		_, err := validate.ApplyFixPlan(ctx, runCtx, &plan, validate.Options{
			Fix: true, NonInteractive: true, PostApplyRefresher: postApplyProjectionRefresher(root),
		})
		require.ErrorContains(t, err, "extract code-anchor source notes/one.md")
		require.Equal(t, after, mustReadPostApplyProjectionFile(t, filepath.Join(root, "notes/one.md")))
		pending, err := validate.DetectPendingRepairJournals(runCtx)
		require.NoError(t, err)
		require.NotEmpty(t, pending)
	})
}

func TestValidationProjectionPostApplyRefresherRetiresRejectedCurrentPath(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	definition := obsidian.VaultDefinition{Path: root}
	indexer := testNoteMetadataIndexer(t)
	initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{VaultPath: root, VaultDef: definition, NoteMetadata: indexer})
	require.NoError(t, err)
	require.NoError(t, initial.Runtime.Store.UpsertNote(ctx, codeanchor.Note{
		Path: "notes/one.md", Title: "One", DefinedAnchors: []codeanchor.Anchor{{Label: "retired", Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangGo}},
	}))
	require.NoError(t, initial.Close())

	definition.Excludes = []string{"notes/one.md"}
	result, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath: root, VaultDef: definition, NoteMetadata: indexer,
		ExactPaths: &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}, refreshNoteAnchors: true,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, result.Close()) }()
	require.Equal(t, []paths.NotePath{"notes/one.md"}, result.ChangedPaths)
	rows, err := result.Runtime.Store.AllNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.NotContains(t, rows, "notes/one.md")
	nodes, err := result.Runtime.Store.OntologyNodesByPaths(ctx, []string{"notes/one.md"})
	require.NoError(t, err)
	require.Empty(t, nodes)
	anchors, err := result.Runtime.Store.Anchors(ctx)
	require.NoError(t, err)
	require.Empty(t, anchors)
}
