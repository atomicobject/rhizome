package validate

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// Checks that read Markdown through the note-metadata provider never see a
// non-canonical source path: provider admission rejects it before any finding
// or stable key exists. Each row pairs the rejection with the same content
// under a valid path that does produce the finding.
func TestMarkdownProviderChecksRejectNonCanonicalSourcePath(t *testing.T) {
	tests := []struct {
		name    string
		run     func(RunContext) CheckResult
		invalid map[string]string
		valid   map[string]string
	}{
		{
			name: "fragile external",
			run: func(runCtx RunContext) CheckResult {
				return RunFragileExternal(context.Background(), runCtx, Options{})
			},
			invalid: map[string]string{"../source.md": "[[target#Missing Heading]]\n", "target.md": "# Target\n\n## Current Heading\n"},
			valid:   map[string]string{"source.md": "[[target#Missing Heading]]\n", "target.md": "# Target\n\n## Current Heading\n"},
		},
		{
			name: "orphan block ids",
			run: func(runCtx RunContext) CheckResult {
				return RunOrphanBlockIDs(context.Background(), runCtx)
			},
			invalid: map[string]string{"../owner.md": "# Owner\n\n^orphan\n"},
			valid:   map[string]string{"owner.md": "# Owner\n\n^orphan\n"},
		},
		{
			name: "code frontmatter",
			run: func(runCtx RunContext) CheckResult {
				return RunCodeFrontmatter(context.Background(), runCtx)
			},
			invalid: map[string]string{"../source.md": "---\ncode-anchors: [\n---\n"},
			valid:   map[string]string{"source.md": "---\ncode-anchors: [\n---\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.run(stableIssueKeyRunContext(t, tt.invalid, true))
			require.False(t, result.OK)
			require.Contains(t, result.Error, "canonical note path")
			require.Empty(t, result.Issues)
			require.Empty(t, result.Fixes)

			control := tt.run(stableIssueKeyRunContext(t, tt.valid, true))
			require.Empty(t, control.Error)
			require.Positive(t, control.IssueCount)
		})
	}
}

// Broken-links and link-hygiene enumerate the supplied reader directly, so a
// non-canonical path reaches StableIssueKey, which must fail the whole check.
func TestChecksFailClosedWhenStableIssueKeyCannotNormalizePath(t *testing.T) {
	tests := []struct {
		name    string
		run     func(RunContext) CheckResult
		invalid map[string]string
		valid   map[string]string
	}{
		{
			name:    "link hygiene",
			run:     RunLinkHygiene,
			invalid: map[string]string{"../source.md": "[[target.md]]\n", "target.md": "# Target\n"},
			valid:   map[string]string{"source.md": "[[target.md]]\n", "target.md": "# Target\n"},
		},
		{
			name: "broken links",
			run: func(runCtx RunContext) CheckResult {
				return RunBrokenLinks(runCtx, Options{})
			},
			invalid: map[string]string{"../source.md": "[[missing]]\n"},
			valid:   map[string]string{"source.md": "[[missing]]\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.run(stableIssueKeyRunContext(t, tt.invalid, false))
			require.False(t, result.OK)
			require.Contains(t, result.Error, "identify")
			require.Empty(t, result.Issues)
			require.Empty(t, result.Fixes)

			control := tt.run(stableIssueKeyRunContext(t, tt.valid, false))
			require.Empty(t, control.Error)
			require.Positive(t, control.IssueCount)
			require.NotEmpty(t, control.Fixes)
		})
	}
}

func stableIssueKeyRunContext(t *testing.T, notes map[string]string, withMetadata bool) RunContext {
	t.Helper()
	root := t.TempDir()
	runCtx := RunContext{
		VaultDef:  obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath: root, NoteReader: stableIssueKeyErrorNoteReader{notes: notes}, MaxIssues: 20,
	}
	if withMetadata {
		runCtx.NoteMetadata = testNoteMetadata(t)
	}
	return runCtx
}

func TestQueryRecipeFixBuilderFailsClosedWhenStableIssueKeyCannotNormalizePath(t *testing.T) {
	actions, err := buildQueryRecipeFixes([]queryrecipe.Issue{{
		Code: "query_recipe_compile_error", Path: "../outside.graphql", Message: "invalid recipe",
	}})
	require.Error(t, err)
	require.Empty(t, actions)

	control, err := buildQueryRecipeFixes([]queryrecipe.Issue{{
		Code: "query_recipe_compile_error", Path: "recipes/inside.graphql", Message: "invalid recipe",
	}})
	require.NoError(t, err)
	require.Len(t, control, 1)
	require.Len(t, control[0].IssueKeys, 1)
}

func TestFinalizeCheckResultFailsClosedWhenStableIssueKeyCannotNormalizePath(t *testing.T) {
	finalize := func(path string) CheckResult {
		return finalizeCheckResult(CheckViews, CheckResult{
			OK:         true,
			IssueCount: 1,
			Issues:     []Issue{{Code: "view_error", Path: path}},
			Fixes:      []FixAction{{ID: "unsafe-without-key"}},
		}, time.Second, 20)
	}

	result := finalize("../outside.md")
	require.False(t, result.OK)
	require.Contains(t, result.Error, "identify views issue")
	require.Zero(t, result.IssueCount)
	require.Empty(t, result.Issues)
	require.Empty(t, result.Fixes)
	require.Empty(t, result.fullIssues)
	require.Empty(t, result.allIssueKeys)

	control := finalize("views/inside.md")
	require.Empty(t, control.Error)
	require.Equal(t, 1, control.IssueCount)
	require.Len(t, control.Issues, 1)
	require.Len(t, control.Fixes, 1)
	require.Len(t, control.allIssueKeys, 1)
}

func TestIdentifierOutputsFailClosedWhenStableIssueKeyCannotNormalizePath(t *testing.T) {
	t.Run("alias mirror", func(t *testing.T) {
		issue, fix, err := identifierAliasMirrorOutput(identifierAliasMirrorMissing{
			Path: "../outside.md", Type: "Spec", FieldName: "specId", Source: "specid", Value: "SPEC-0001",
		})
		require.Error(t, err)
		require.Empty(t, issue)
		require.Empty(t, fix)
	})

	invalid := identifierreconcile.Claim{
		Node:  identifierreconcile.CanonicalNodeKey{NotePath: "../outside.md", TypeName: "Spec", IdentifierField: "specId"},
		Value: "SPEC-0001", Kind: identifierreconcile.ClaimPreferred,
	}
	valid := identifierreconcile.Claim{
		Node:  identifierreconcile.CanonicalNodeKey{NotePath: "specs/inside.md", TypeName: "Spec", IdentifierField: "specId"},
		Value: "SPEC-0001", Kind: identifierreconcile.ClaimPreferred,
	}

	t.Run("planned collision", func(t *testing.T) {
		issues, fixes, bindings, err := plannedIdentifierCollisionOutput(&identifierreconcile.Plan{
			Collisions: []identifierreconcile.PlannedCollision{{
				Key: "identifier-collision:v1:test", Value: "SPEC-0001",
				Kind: identifierreconcile.CollisionPreferredPreferred, Keeper: valid,
				Losers: []identifierreconcile.PlannedLoser{{Claim: invalid, Replacement: "SPEC-0002"}},
			}},
		}, identifierRuntimeInventory{})
		require.Error(t, err)
		require.Empty(t, issues)
		require.Empty(t, fixes)
		require.Empty(t, bindings)
	})

	t.Run("manual collision", func(t *testing.T) {
		issues, fixes, err := manualIdentifierCollisionOutput([]identifierreconcile.Claim{invalid, valid})
		require.Error(t, err)
		require.Empty(t, issues)
		require.Empty(t, fixes)
	})
}

type stableIssueKeyErrorNoteReader struct {
	obsidian.NoteReader
	notes map[string]string
}

func (r stableIssueKeyErrorNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	paths := make([]string, 0, len(r.notes))
	for path := range r.notes {
		paths = append(paths, path)
	}
	return paths, nil
}

func (r stableIssueKeyErrorNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	return r.notes[path], nil
}

func (stableIssueKeyErrorNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Unix(1, 0), nil
}

var _ obsidian.NoteReader = stableIssueKeyErrorNoteReader{}
