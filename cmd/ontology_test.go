package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestPrintOntologyRuntimeError_AllowsNilRuntime(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	err := errors.New("boom")
	require.ErrorIs(t, printOntologyRuntimeError(cmd, nil, err), err)
}

func TestOntologyQueryRuntimeCodePathsRejectEscapes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing.go"), []byte("package pkg\n"), 0o644))
	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live: &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
	}}

	rel, abs, err := provider.runtimeCodePath("pkg/thing.go")
	require.NoError(t, err)
	require.Equal(t, "pkg/thing.go", rel)
	expectedAbs, err := filepath.EvalSymlinks(filepath.Join(root, "pkg", "thing.go"))
	require.NoError(t, err)
	require.Equal(t, expectedAbs, filepath.FromSlash(abs))

	_, _, err = provider.runtimeCodePath("../outside.go")
	require.Error(t, err)
	require.Contains(t, err.Error(), "inside the vault")
}

func TestOntologyQueryRuntimeNotePathsPreserveMixedCaseAuthoredExtensions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live: &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
	}}

	for _, rel := range []string{"notes/Decision.MD", "notes/Reference.HTML"} {
		t.Run(rel, func(t *testing.T) {
			absPath := filepath.Join(root, filepath.FromSlash(rel))
			require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
			require.NoError(t, os.WriteFile(absPath, []byte("note"), 0o644))

			gotRel, gotAbs, err := provider.runtimeNotePath(rel)
			require.NoError(t, err)
			require.Equal(t, rel, gotRel)
			expectedAbs, err := filepath.EvalSymlinks(absPath)
			require.NoError(t, err)
			require.Equal(t, expectedAbs, filepath.FromSlash(gotAbs))
		})
	}
}

func TestOntologyQueryRuntimeDocsForCodeRejectsEscapedPathBeforeRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live: &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
	}}

	pack, err := provider.DocsForCode(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "../outside.go"})
	require.Error(t, err)
	require.Empty(t, pack.Docs)
	require.Empty(t, pack.Notes)
}

func TestOntologyQueryRuntimeDocsForCodeUsesIndexedCoderefs(t *testing.T) {
	anchorNote := `---
summary: anchored guidance
code-anchors:
  go:
    - label: thing-guidance
      glob: pkg/*.go
---
# Anchored guidance
`
	vault := setupAgentTestVault(t, map[string]string{
		"pkg/thing.go":     "package pkg\n\n// See [[Decision]].\n",
		"docs/Decision.md": "---\nsummary: indexed decision\n---\n\n# Decision\n",
		"docs/Anchored.md": anchorNote,
	})
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "pkg/thing.go", []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: "pkg/thing.go",
		DstKind: "note",
		DstPath: "docs/Decision.md",
	}}))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{{
			Path: "docs/Decision.md", Title: "Decision", ContentHash: "decision", Mtime: 1, Size: 1,
		}},
	}))
	anchorService := codeanchor.NewServiceWithOptions(
		store,
		nil,
		codeanchor.WithBasePath(vault.path),
		codeanchor.WithWriteAccess(),
	)
	_, err = anchorService.IngestNoteSource(ctx, testNoteSourceSnapshot(t, vault.path, filepath.Join(vault.path, "docs", "Anchored.md"), anchorNote))
	require.NoError(t, err)
	require.NoError(t, anchorService.RecomputeAnchorScopes(ctx))
	require.NoError(t, store.Close())

	live, err := bootstrap.NewLiveRuntime(ctx, bootstrap.IndexedReadOnlyRuntimeOptions(bootstrap.LiveOptions{
		VaultName: vault.name,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, live.Close()) })
	require.NoError(t, live.WaitForCodeIndex(ctx))

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live:       live,
		noteReader: &obsidian.Note{},
	}}
	pack, err := provider.DocsForCode(ctx, ontologyquery.CodeRuntimeRequest{Path: "pkg/thing.go", First: 10})
	require.NoError(t, err)
	require.Contains(t, pack.Notes, ontologyquery.RuntimePath{
		Path:    "docs/Decision.md",
		Title:   "Decision",
		Kind:    "wikilink",
		Snippet: "// See [[Decision]].",
		Line:    3,
	})
	require.Contains(t, pack.Notes, ontologyquery.RuntimePath{
		Path:  "docs/Anchored.md",
		Title: "Anchored",
		Kind:  "anchor:thing-guidance",
	})
}

func TestOntologyQueryRuntimeTestsForCodeRejectsEscapedPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live: &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
	}}

	pack, err := provider.TestsForCode(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "../outside.go"})
	require.NoError(t, err)
	require.False(t, pack.Available)
	require.Equal(t, "invalid_code_path", pack.Warnings[0].Code)
	require.Empty(t, pack.Tests)
}

func TestOntologyQueryRuntimeCodeForNoteFallsBackToAuthoredAnchors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	notePath := filepath.Join(root, "docs", "code-anchors", "runtime.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
code-anchors:
  go:
    - label: runtime.execute
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.executor.resolveRuntimeRoot
    - label: runtime.files
      glob: pkg/ontology/query/*.go
---
# Runtime anchors
`), 0o644))

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live:              &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
		noteFormatRuntime: &formats,
	}}

	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{
		Path:  "docs/code-anchors/runtime.md",
		First: 10,
	})
	require.NoError(t, err)
	require.True(t, pack.Available)
	require.Equal(t, "docs/code-anchors/runtime.md", pack.NormalizedPath)
	require.Contains(t, pack.Code, ontologyquery.RuntimePath{
		Path:   "github.com/atomicobject/rhizome/pkg/ontology/query.executor.resolveRuntimeRoot",
		Kind:   "function",
		Reason: "runtime.execute",
	})
	require.Contains(t, pack.Code, ontologyquery.RuntimePath{
		Path:   "pkg/ontology/query/*.go",
		Kind:   "glob",
		Reason: "runtime.files",
	})
	require.Equal(t, "code_anchor_scope_unavailable", pack.Warnings[0].Code)
}

func TestOntologyQueryRuntimeCodeForNoteWarnsWhenNoAnchorsFound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	notePath := filepath.Join(root, "docs", "notes", "plain.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte("# Plain note\n\nNo code anchors here.\n"), 0o644))

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live:              &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
		noteFormatRuntime: &formats,
	}}

	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{
		Path:  "docs/notes/plain.md",
		First: 10,
	})
	require.NoError(t, err)
	require.True(t, pack.Available)
	require.Equal(t, "docs/notes/plain.md", pack.NormalizedPath)
	require.Empty(t, pack.Code)
	require.Len(t, pack.Warnings, 1)
	require.Equal(t, "code_anchors_not_found", pack.Warnings[0].Code)
	require.Equal(t, "docs/notes/plain.md", pack.Warnings[0].Path)
}

func TestOntologyQueryRuntimeCodeForNoteFailsClosedWithoutFormatRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	notePath := filepath.Join(root, "notes", "runtime.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
code-anchors:
  go:
    - label: must-not-default-to-markdown
      glob: pkg/**/*.go
---
`), 0o644))

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live: &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
	}}
	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "notes/runtime.md"})
	require.NoError(t, err)
	require.Empty(t, pack.Code)
	require.Len(t, pack.Warnings, 1)
	require.Equal(t, "note_format_unavailable", pack.Warnings[0].Code)
	require.Contains(t, pack.Warnings[0].Message, "note format runtime is unavailable")
}

func TestOntologyQueryRuntimeCodeForNoteFailsClosedWithInvalidFormatRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	notePath := filepath.Join(root, "notes", "runtime.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
code-anchors:
  go:
    - label: must-not-default-to-markdown
      glob: pkg/**/*.go
---
`), 0o644))
	invalid := noteformat.Runtime{}

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live:              &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Path: root}},
		noteFormatRuntime: &invalid,
	}}
	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "notes/runtime.md"})
	require.NoError(t, err)
	require.Empty(t, pack.Code)
	require.Len(t, pack.Warnings, 1)
	require.Equal(t, "note_format_unavailable", pack.Warnings[0].Code)
}

func TestOntologyQueryRuntimeCodeForNoteDoesNotParseConfiguredHTMLFallback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	notePath := filepath.Join(root, "notes", "runtime.html")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
code-anchors:
  go:
    - label: must-not-parse
      glob: pkg/**/*.go
---
`), 0o644))
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live:              &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.html"}}},
		noteFormatRuntime: &formats,
	}}
	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "notes/runtime.html"})
	require.NoError(t, err)
	require.Empty(t, pack.Code)
	require.Equal(t, "code_anchor_fallback_unsupported_format", pack.Warnings[0].Code)
	require.Contains(t, pack.Warnings[0].Message, `format "html"`)
}

func TestOntologyQueryRuntimeCodeForNoteDoesNotParseFutureProjectableFormat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	notePath := filepath.Join(root, "notes", "runtime.future")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
code-anchors:
  go:
    - label: must-not-parse
      glob: pkg/**/*.go
---
`), 0o644))
	descriptor := futureNoteDescriptor()
	projector := &futureNoteProjector{descriptor: descriptor}
	registry, err := noteformat.NewRegistry(markdown.New(), projector)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{
		live:              &bootstrap.LiveRuntime{VaultDef: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.future"}}},
		noteFormatRuntime: &formats,
	}}
	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "notes/runtime.future"})
	require.NoError(t, err)
	require.Empty(t, pack.Code)
	require.Equal(t, "code_anchor_fallback_unsupported_format", pack.Warnings[0].Code)
	require.Contains(t, pack.Warnings[0].Message, `format "future"`)
}

type futureNoteProjector struct {
	descriptor noteformat.Descriptor
}

func (p *futureNoteProjector) Descriptor() noteformat.Descriptor { return p.descriptor }

func (p *futureNoteProjector) Project(_ noteformat.AuthoredSource) (noteformat.Projection, error) {
	return noteformat.NewProjectionWithFacts(
		p.descriptor.ProviderVersion,
		p.descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		nil,
		p.descriptor.Capabilities,
		noteformat.ProjectionFacts{},
	)
}

func futureNoteDescriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "future",
		Extensions:        []string{".future"},
		ProviderVersion:   "future-provider-v1",
		ProjectionVersion: "future-projection-v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityAuthoredLinkExtraction,
		),
	}
}

func TestOntologyQueryRuntimeAuthoringGuideRequiresType(t *testing.T) {
	t.Parallel()

	provider := ontologyQueryRuntimeProviders{rt: &ontologyQueryRuntime{}}
	guide, err := provider.AuthoringGuide(context.Background(), ontologyquery.OntologyAuthoringGuideRequest{Type: ""})
	require.NoError(t, err)
	require.False(t, guide.Available)
	require.Equal(t, "type_required", guide.Warnings[0].Code)
}
