package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fakeDedupeTracker struct {
	seen map[string]string
}

type minimalTestIndexer struct {
	lang codeanchor.Lang
}

func (m *minimalTestIndexer) IndexFile(content []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	return codeanchor.FileSummary{FilePath: path.Rel.String(), Lang: m.lang}, nil
}

func (m *minimalTestIndexer) Lang() codeanchor.Lang { return m.lang }

func (f *fakeDedupeTracker) Allow(key, fingerprint string) bool {
	if f.seen == nil {
		f.seen = make(map[string]string)
	}
	if f.seen[key] == fingerprint {
		return false
	}
	f.seen[key] = fingerprint
	return true
}

func (f *fakeDedupeTracker) MarkSent(key, fingerprint string) {
	if f.seen == nil {
		f.seen = make(map[string]string)
	}
	f.seen[key] = fingerprint
}

func (f *fakeDedupeTracker) Seen(key, fingerprint string) bool {
	if f.seen == nil {
		return false
	}
	return f.seen[key] == fingerprint
}

func TestBuildFileContext_ProximityBudgetPrefersClosest(t *testing.T) {
	root := t.TempDir()
	projectRoot := root

	// Create ancestor docs (closest first)
	closestDir := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(closestDir, 0o755))

	closestDoc := filepath.Join(closestDir, "CONTEXT.md")
	require.NoError(t, os.WriteFile(closestDoc, []byte("closest"), 0o644))

	farDoc := filepath.Join(root, "CONTEXT.md")
	require.NoError(t, os.WriteFile(farDoc, []byte("far-doc-that-should-be-dropped"), 0o644))

	target := filepath.Join(closestDir, "main.go")
	require.NoError(t, os.WriteFile(target, []byte("package main"), 0o644))

	ctx, err := BuildFileContext(target, FileContextParams{
		VaultDef:       obsidian.VaultDefinition{Path: root},
		ProjectRoot:    projectRoot,
		DocPatterns:    []string{"CONTEXT.md"},
		MaxEmptyLevels: 3,
		ContextBudget:  len("closest"), // only enough for the nearest doc
	})
	require.NoError(t, err)

	require.Equal(t, "code", ctx.FileType)
	require.Len(t, ctx.AncestorDocs, 1, "only closest doc should be included within budget")
	require.Equal(t, "closest", ctx.AncestorDocs[0].Content)
	require.True(t, ctx.Truncated, "budget exhaustion should mark truncated")
}

func TestBuildFileContext_LinkedNotes(t *testing.T) {
	root := t.TempDir()
	projectRoot := root

	// Create a note to be resolved
	notePath := filepath.Join(root, "Notes", "MyNote.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte("---\nsummary: \"stub summary\"\n---\n\n# Title\n"), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// see [[MyNote]]"), 0o644))

	codeRefs := map[string][]coderefs.CodeRef{
		"src/main.go": {
			{
				SourceFile: "src/main.go",
				Target:     "Notes/MyNote.md",
				Kind:       coderefs.RefKindWikilink,
				Line:       10,
				Snippet:    "// see [[MyNote]]",
			},
		},
	}

	ctx, err := BuildFileContext(codePath, FileContextParams{
		VaultDef:       obsidian.VaultDefinition{Path: root},
		ProjectRoot:    projectRoot,
		DocPatterns:    []string{"CONTEXT.md"},
		MaxEmptyLevels: 3,
		ContextBudget:  1000,
		CodeRefsByFile: codeRefs,
		NoteReader:     newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}),
	})
	require.NoError(t, err)

	require.Len(t, ctx.LinkedNotes, 1)
	require.Equal(t, "Notes/MyNote.md", ctx.LinkedNotes[0].Path)
	require.Equal(t, "stub summary", ctx.LinkedNotes[0].Frontmatter["summary"])
}

func TestBuildFileContext_ExcludesByPath(t *testing.T) {
	root := t.TempDir()
	projectRoot := root

	// Docs
	docDir := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(docDir, 0o755))
	docA := filepath.Join(docDir, "CONTEXT.md")
	require.NoError(t, os.WriteFile(docA, []byte("keep me"), 0o644))
	docB := filepath.Join(root, "CONTEXT.md")
	require.NoError(t, os.WriteFile(docB, []byte("drop me"), 0o644))

	// Notes
	noteA := filepath.Join(root, "Notes", "One.md")
	noteB := filepath.Join(root, "Notes", "Two.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(noteA), 0o755))
	require.NoError(t, os.WriteFile(noteA, []byte("# One"), 0o644))
	require.NoError(t, os.WriteFile(noteB, []byte("# Two"), 0o644))

	codePath := filepath.Join(docDir, "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// see [[One]] [[Two]]"), 0o644))

	codeRefs := map[string][]coderefs.CodeRef{
		"pkg/service/main.go": {
			{SourceFile: "pkg/service/main.go", Target: "Notes/One.md", Kind: coderefs.RefKindWikilink, Line: 1},
			{SourceFile: "pkg/service/main.go", Target: "Notes/Two.md", Kind: coderefs.RefKindWikilink, Line: 1},
		},
	}

	ctx, err := BuildFileContext(codePath, FileContextParams{
		VaultDef:         obsidian.VaultDefinition{Path: root},
		ProjectRoot:      projectRoot,
		DocPatterns:      []string{"CONTEXT.md"},
		MaxEmptyLevels:   3,
		ContextBudget:    1000,
		CodeRefsByFile:   codeRefs,
		NoteReader:       &obsidian.Note{},
		ExcludeNotePaths: []string{"Notes/Two.md"},
		ExcludeDocPaths:  []string{docB},
	})
	require.NoError(t, err)

	require.Len(t, ctx.LinkedNotes, 1)
	require.Equal(t, "Notes/One.md", ctx.LinkedNotes[0].Path)
	require.Equal(t, []string{"Notes/One.md"}, ctx.ReturnedNotePaths)

	require.Len(t, ctx.AncestorDocs, 1)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	relDocA, err := vaultPaths.RelStrict(docA)
	require.NoError(t, err)
	require.Equal(t, relDocA.String(), ctx.AncestorDocs[0].Path)
	require.Equal(t, []string{relDocA.String()}, ctx.ReturnedDocPaths)
}

func TestBuildFileContext_DirectoryTargetIncludesDocsAndDirAnchors(t *testing.T) {
	root := t.TempDir()
	projectRoot := root

	// Module docs
	docDir := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(docDir, 0o755))
	docA := filepath.Join(docDir, "CONTEXT.md")
	require.NoError(t, os.WriteFile(docA, []byte("module-doc"), 0o644))
	docB := filepath.Join(root, "CONTEXT.md")
	require.NoError(t, os.WriteFile(docB, []byte("root-doc"), 0o644))

	// Dir anchor note
	notePath := filepath.Join(root, "Notes", "ServiceModule.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "Service module note"
code-anchors:
  python:
    - dir: pkg/service
---
# Service module note
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	ca := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)
	_, err = ca.IngestNoteSource(context.Background(), fileContextTestNoteSource(t, root, notePath, noteContent))
	require.NoError(t, err)

	ctx, err := BuildFileContext(docDir, FileContextParams{
		VaultDef:       obsidian.VaultDefinition{Path: root},
		ProjectRoot:    projectRoot,
		DocPatterns:    []string{"CONTEXT.md"},
		MaxEmptyLevels: 3,
		ContextBudget:  1000,
		NoteReader:     newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}),
		CodeAnchor:     ca,
	})
	require.NoError(t, err)

	// Directory target should start doc discovery in that directory.
	require.Len(t, ctx.AncestorDocs, 2)
	require.Equal(t, "module-doc", ctx.AncestorDocs[0].Content)
	require.Equal(t, "root-doc", ctx.AncestorDocs[1].Content)

	// Directory anchor should pull in the note.
	require.Len(t, ctx.LinkedNotes, 1)
	require.Equal(t, "Service module note", ctx.LinkedNotes[0].Frontmatter["summary"])
}

func TestBuildFileContext_AnchorKindsFilterOmitsDirAnchors(t *testing.T) {
	root := t.TempDir()
	projectRoot := root

	docDir := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(docDir, 0o755))
	docA := filepath.Join(docDir, "CONTEXT.md")
	require.NoError(t, os.WriteFile(docA, []byte("module-doc"), 0o644))

	notePath := filepath.Join(root, "Notes", "ServiceModule.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "Service module note"
code-anchors:
  python:
    - dir: pkg/service
---
# Service module note
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	ca := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)
	_, err = ca.IngestNoteSource(context.Background(), fileContextTestNoteSource(t, root, notePath, noteContent))
	require.NoError(t, err)

	ctx, err := BuildFileContext(docDir, FileContextParams{
		VaultDef:       obsidian.VaultDefinition{Path: root},
		ProjectRoot:    projectRoot,
		DocPatterns:    []string{"CONTEXT.md"},
		MaxEmptyLevels: 3,
		ContextBudget:  1000,
		NoteReader:     &obsidian.Note{},
		CodeAnchor:     ca,
		AnchorKinds:    []codeanchor.AnchorKind{codeanchor.AnchorFunc},
	})
	require.NoError(t, err)

	require.Len(t, ctx.LinkedNotes, 0)
}

func TestBuildFileContext_HubExpansionAddsStubNotes(t *testing.T) {
	root := t.TempDir()
	projectRoot := root

	// Notes referenced from the hub
	aPath := filepath.Join(root, "Notes", "A.md")
	bPath := filepath.Join(root, "Notes", "B.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(aPath), 0o755))
	require.NoError(t, os.WriteFile(aPath, []byte("---\nsummary: \"note a\"\n---\n\n# A\n"), 0o644))
	require.NoError(t, os.WriteFile(bPath, []byte("---\nsummary: \"note b\"\n---\n\n# B\n"), 0o644))

	// Hub note linked from code
	hubPath := filepath.Join(root, "Notes", "Hub.md")
	hubContent := "---\ntags: [type/hub]\nsummary: \"hub\"\n---\n\n# Hub\n\n- [[A]]\n- [[B]]\n"
	require.NoError(t, os.WriteFile(hubPath, []byte(hubContent), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// Docs: [[Hub]]"), 0o644))

	codeRefs := map[string][]coderefs.CodeRef{
		"src/main.go": {
			{SourceFile: "src/main.go", Target: "Notes/Hub.md", Kind: coderefs.RefKindWikilink, Line: 1, Snippet: "// Docs: [[Hub]]"},
		},
	}

	ctx, err := BuildFileContext(codePath, FileContextParams{
		VaultDef:             obsidian.VaultDefinition{Path: root},
		ProjectRoot:          projectRoot,
		DocPatterns:          []string{"CONTEXT.md"},
		MaxEmptyLevels:       3,
		ContextBudget:        1000,
		CodeRefsByFile:       codeRefs,
		NoteReader:           newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}),
		ExpandNoteLinks:      []string{"tag:type/hub"},
		ExpandNoteLinksLimit: 8,
	})
	require.NoError(t, err)

	// Should include the hub itself + its linked notes as stubs.
	require.GreaterOrEqual(t, len(ctx.LinkedNotes), 3)

	// Find A and B from the expansion.
	foundA := false
	foundB := false
	for _, ln := range ctx.LinkedNotes {
		if ln.Path == "Notes/A.md" {
			foundA = true
			require.Equal(t, "linkedFromHub", ln.Kind)
			require.Equal(t, "note a", ln.Frontmatter["summary"])
		}
		if ln.Path == "Notes/B.md" {
			foundB = true
			require.Equal(t, "linkedFromHub", ln.Kind)
			require.Equal(t, "note b", ln.Frontmatter["summary"])
		}
	}
	require.True(t, foundA)
	require.True(t, foundB)
}

func TestBuildFileContextText_UsesCodeAnchorsFromConfig(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	indexPath := filepath.Join(rhizomeDir, "db.sqlite")

	// Make Go symbol FQNs stable (avoid fallback "go:<abs-dir>" package IDs).
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.23\n"), 0o644))

	configContent := fmt.Sprintf(`notes:
  includes:
    - "**/*.md"
code:
  enabled: true
indexPath: %q
`, indexPath)
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(configContent), 0o644))

	notePath := filepath.Join(root, "Notes", "WatcherAnchor.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "Watcher anchor"
code-anchors:
  go:
    - label: watcher-anchor
      symbol: example.com/test/pkg/watcher.NewWatcher
---
# Watcher anchor
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	codePath := filepath.Join(root, "pkg", "watcher", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	codeContent := `package watcher

func NewWatcher() {}
`
	require.NoError(t, os.WriteFile(codePath, []byte(codeContent), 0o644))

	store, err := sqlitefixture.Open(indexPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)
	ctx := context.Background()
	_, err = svc.IngestNoteSource(ctx, fileContextTestNoteSource(t, root, notePath, noteContent))
	require.NoError(t, err)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, codePath, []byte(codeContent)))
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		NoteMetadata: testNoteMetadataIndexer(t),
		Files:        []string{codePath},
		BudgetChars:  4000,
		Profile:      ContextProfileCode,
	})
	require.NoError(t, err)
	require.Contains(t, text, "watcher-anchor")
	require.Contains(t, text, "Notes/WatcherAnchor.md")
	require.Contains(t, text, `source="anchor:watcher-anchor"`)
	require.NotContains(t, text, "thin_context")
}

func fileContextTestNoteSource(t *testing.T, root, path, content string) notemeta.NoteSourceSnapshot {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	return notemeta.NewContentOnlyNoteSourceSnapshot(filepath.ToSlash(rel), content, 0)
}

func TestBuildFileContextText_WarnsForThinCodeContext(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("package main\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		NoteMetadata: testNoteMetadataIndexer(t),
		Files:        []string{codePath},
		BudgetChars:  4000,
		Profile:      ContextProfileCode,
	})
	require.NoError(t, err)
	require.Contains(t, text, "thin_context")
	require.Contains(t, text, "insufficient contextual bindings")
	require.Contains(t, text, "not absence of docs")
	require.Contains(t, text, "`rzm agent files --include-content true --input 'src/main.go'`")
	require.Contains(t, text, "`rzm agent semantic-query --path 'src/main.go' --query \"<topic>\"`")
}

func TestBuildFileContextTextReportsDescriptorOnlyHTMLAsUnsupportedNoteProjection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"notes/*.html\"]\n"), 0o644))
	target := filepath.Join(root, "notes", "Decision.HTML")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("<h1>Decision</h1>"), 0o644))

	vault := obsidian.Vault{Name: root}
	text, err := BuildFileContextText(&vault, &obsidian.Note{}, FileContextTextParams{
		Files:        []string{target},
		BudgetChars:  4000,
		NoteMetadata: descriptorOnlyHTMLNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "Unsupported projection")
	require.Contains(t, text, "format \"html\"")
	require.NotContains(t, text, "(code)")
}

func TestBuildFileContextText_WarnsForThinNoteContext(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	notePath := filepath.Join(root, "Notes", "Solo.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte("---\nsummary: \"solo summary\"\n---\n\n# Solo\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		Files:        []string{notePath},
		BudgetChars:  4000,
		Profile:      ContextProfileVault,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "thin_context")
	require.Contains(t, text, "insufficient contextual bindings")
	require.Contains(t, text, "`rzm agent files --include-content true --input 'Notes/Solo.md'`")
}

func TestBuildFileContextText_WarnsWithQuotedPaths(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	codePath := filepath.Join(root, "src", "my module", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("package main\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		NoteMetadata: testNoteMetadataIndexer(t),
		Files:        []string{codePath},
		BudgetChars:  4000,
		Profile:      ContextProfileCode,
	})
	require.NoError(t, err)
	require.Contains(t, text, "`rzm agent files --include-content true --input 'src/my module/main.go'`")
	require.Contains(t, text, "`rzm agent semantic-query --path 'src/my module/main.go' --query \"<topic>\"`")
}

func TestBuildFileContextText_DoesNotWarnForLinkedNoteContext(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	aPath := filepath.Join(root, "Notes", "A.md")
	bPath := filepath.Join(root, "Notes", "B.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(aPath), 0o755))
	require.NoError(t, os.WriteFile(aPath, []byte("# A\n\n[[B]]\n"), 0o644))
	require.NoError(t, os.WriteFile(bPath, []byte("# B\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		NoteMetadata: testNoteMetadataIndexer(t),
		Files:        []string{aPath},
		BudgetChars:  4000,
		Profile:      ContextProfileVault,
	})
	require.NoError(t, err)
	require.Contains(t, text, "### Top outbound")
	require.NotContains(t, text, "thin_context")
}

func TestRenderNoteSummaryBlockNoGraph_DoesNotWarnWhenSummaryPresent(t *testing.T) {
	root := t.TempDir()
	notePath := filepath.Join(root, "Notes", "Summary.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte("---\nsummary: \"human description\"\n---\n\n# Summary\n"), 0o644))

	vaultDef := obsidian.VaultDefinition{Path: root}
	text := renderNoteSummaryBlockNoGraph(newProjectedFilesystemFactReader(t, vaultDef, &obsidian.Note{}), "Notes/Summary.md", nil)
	require.Contains(t, text, "- summary: human description")
	require.NotContains(t, text, "thin_context")
}

func TestBuildFileContextText_IncludesRationaleFromSessionStore(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	indexPath := filepath.Join(rhizomeDir, "db.sqlite")
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(fmt.Sprintf("notes:\n  includes: [\"**/*.md\"]\ncode:\n  enabled: true\nindexPath: %q\n", indexPath)), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	codeContent := `// NOTE: preserve output ordering
package main
`
	require.NoError(t, os.WriteFile(codePath, []byte(codeContent), 0o644))

	store, err := sqlitefixture.Open(indexPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{&minimalTestIndexer{lang: codeanchor.LangGo}},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)
	require.NoError(t, svc.IndexCodeFile(context.Background(), codeanchor.LangGo, codePath, []byte(codeContent)))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &noteMgr), FileContextTextParams{
		Files:        []string{codePath},
		BudgetChars:  4000,
		Profile:      ContextProfileCode,
		SessionStore: store,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "#### Rationale")
	require.Contains(t, text, `<rationale kind="note"`)
	require.Contains(t, text, "preserve output ordering")
}

func TestPrioritizeRationaleForContextPrefersWhyRationaleImportant(t *testing.T) {
	got := prioritizeRationaleForContext([]codeanchor.Rationale{
		{Kind: codeanchor.RationaleTodo, StartLine: 1},
		{Kind: codeanchor.RationaleWhy, StartLine: 20},
		{Kind: codeanchor.RationaleNote, StartLine: 2},
		{Kind: codeanchor.RationaleImportant, StartLine: 10},
	})

	require.Equal(t, []codeanchor.RationaleKind{
		codeanchor.RationaleImportant,
		codeanchor.RationaleWhy,
		codeanchor.RationaleNote,
		codeanchor.RationaleTodo,
	}, []codeanchor.RationaleKind{got[0].Kind, got[1].Kind, got[2].Kind, got[3].Kind})
}

func TestBuildFileContextText_EmbedsLinkedNoteBodyByDefault(t *testing.T) {
	root := t.TempDir()

	// Minimal local config so BuildFileContextText treats this as a code repo and can find doc patterns.
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	notePath := filepath.Join(root, "Notes", "Doc.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "short summary"
---

# Doc

This is the detailed body.
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// Docs: [[Doc]]\npackage main\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	noteCache := obsidian.BuildNotePathCache([]string{"Notes/Doc.md"})
	codeBytes, err := os.ReadFile(codePath)
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)
	refs, err := coderefs.ScanFile(rel.String(), codeBytes, noteCache)
	require.NoError(t, err)
	codeRefs := map[string][]coderefs.CodeRef{rel.String(): refs}

	text, err := BuildFileContextText(&vault, newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &noteMgr), FileContextTextParams{
		Files:          []string{codePath},
		BudgetChars:    8000,
		Profile:        ContextProfileCode,
		CodeRefsByFile: codeRefs,
		NoteMetadata:   testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, `<doc kind="note" path="Notes/Doc.md"`)
	require.Contains(t, text, "This is the detailed body.")
}

func TestBuildFileContextText_IncludesPreciseOntologyNodeFromCoderef(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(filepath.Join(rhizomeDir, "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "ontology", "schema.graphql"), []byte(`
type ProductSpec @node(paths: ["docs/spec.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "spec.md"), []byte(`# Spec

## Stories

### Story A

^story-a
summary:: Preserve exact node context
`), 0o644))
	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// WHY: implements [[spec#^story-a]]\npackage main\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	noteCache := obsidian.BuildNotePathCache([]string{"docs/spec.md"})
	codeBytes, err := os.ReadFile(codePath)
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)
	refs, err := coderefs.ScanFile(rel.String(), codeBytes, noteCache)
	require.NoError(t, err)

	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		NoteMetadata:   testNoteMetadataIndexer(t),
		Files:          []string{codePath},
		BudgetChars:    8000,
		Profile:        ContextProfileCode,
		CodeRefsByFile: map[string][]coderefs.CodeRef{rel.String(): refs},
		SessionStore:   store,
	})
	require.NoError(t, err)
	require.Contains(t, text, `kind="ontology-node"`)
	require.Contains(t, text, `locator="docs/spec.md#^story-a"`)
	require.Contains(t, text, `wikilink="[[spec#^story-a]]"`)
	require.Contains(t, text, "Preserve exact node context")
}

func TestBuildFileContextText_ExcludesOntologyNodeForExcludedLinkedNote(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(filepath.Join(rhizomeDir, "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "ontology", "schema.graphql"), []byte(`
type ProductSpec @node(paths: ["docs/spec.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "spec.md"), []byte(`# Spec

## Stories

### Story A

^story-a
summary:: Should be excluded with note
`), 0o644))
	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// WHY: implements [[spec#^story-a]]\npackage main\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	noteCache := obsidian.BuildNotePathCache([]string{"docs/spec.md"})
	codeBytes, err := os.ReadFile(codePath)
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)
	refs, err := coderefs.ScanFile(rel.String(), codeBytes, noteCache)
	require.NoError(t, err)

	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		Files:            []string{codePath},
		BudgetChars:      8000,
		Profile:          ContextProfileCode,
		CodeRefsByFile:   map[string][]coderefs.CodeRef{rel.String(): refs},
		SessionStore:     store,
		NoteMetadata:     testNoteMetadataIndexer(t),
		ExcludeNotePaths: []string{"docs/spec.md"},
	})
	require.NoError(t, err)
	require.NotContains(t, text, `kind="ontology-node"`)
	require.NotContains(t, text, "Should be excluded with note")
}

func TestBuildFileContextText_ApplyRepairsEmbeddedNodeLinkTarget(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type ProductSpec @node(paths: ["docs/spec.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	specPath := filepath.Join(root, "docs", "spec.md")
	require.NoError(t, os.WriteFile(specPath, []byte(`# Spec

## Stories

### Story A
summary:: Repair exact node context
`), 0o644))
	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// WHY: implements [[spec#Story A]]\npackage main\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	noteCache := obsidian.BuildNotePathCache([]string{"docs/spec.md"})
	codeBytes, err := os.ReadFile(codePath)
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)
	refs, err := coderefs.ScanFile(rel.String(), codeBytes, noteCache)
	require.NoError(t, err)

	params := FileContextTextParams{
		Files:            []string{codePath},
		BudgetChars:      8000,
		Profile:          ContextProfileCode,
		CodeRefsByFile:   map[string][]coderefs.CodeRef{rel.String(): refs},
		SessionStore:     store,
		NoteMetadata:     testNoteMetadataIndexer(t),
		EnsureLinkTarget: ontology.EnsureLinkTargetApply,
		ApplyLinkTargets: func(ctx context.Context, schemaHash string, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
			schema, err := ontology.LoadSchema(root)
			require.NoError(t, err)
			require.Equal(t, schema.Hash, schemaHash)
			def, err := vault.Definition()
			require.NoError(t, err)
			result, err := (&ontology.NodeLinkService{VaultDef: def, NoteReader: &noteMgr, Schema: schema}).LinkTargets(ctx, request)
			if err != nil {
				return result, err
			}
			require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, def, &noteMgr, store, []string{"docs/spec.md"}, nil))
			_, err = ontology.SyncPublishedPaths(ctx, testNoteMetadataIndexer(t), def, &noteMgr, store, nil, []string{"docs/spec.md"}, nil)
			return result, err
		},
	}
	text, err := BuildFileContextText(&vault, &noteMgr, params)
	require.NoError(t, err)
	require.Contains(t, text, `wikilink="[[spec#^`)
	updated, err := os.ReadFile(specPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), "^userstory-story-a-")
	params.ApplyLinkTargets = func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		return ontology.LinkTargetResult{Applied: true}, fmt.Errorf("source applied; index convergence incomplete")
	}
	_, err = BuildFileContextText(&vault, &noteMgr, params)
	require.ErrorContains(t, err, "source applied; index convergence incomplete")
	params.ApplyLinkTargets = nil
	_, err = BuildFileContextText(&vault, &noteMgr, params)
	require.ErrorContains(t, err, "explicit live writer")
	params.OntologyRuntimeProvider = &countingOntologyRuntimeProvider{
		runtime: &ontology.Runtime{Store: store, Schema: &ontology.Schema{Hash: "partial"}},
		err:     fmt.Errorf("partial runtime load failed"),
	}
	params.ApplyLinkTargets = func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		t.Fatal("partial runtime must not reach source apply")
		return ontology.LinkTargetResult{}, nil
	}
	_, err = BuildFileContextText(&vault, &noteMgr, params)
	require.ErrorContains(t, err, "partial runtime load failed")

}

func TestBuildFileContextText_SkipsEmbeddedNoteWhenDeduped(t *testing.T) {
	root := t.TempDir()

	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	notePath := filepath.Join(root, "Notes", "Doc.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "short summary"
---

# Doc

This is the detailed body.
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// Docs: [[Doc]]\npackage main\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	noteCache := obsidian.BuildNotePathCache([]string{"Notes/Doc.md"})
	codeBytes, err := os.ReadFile(codePath)
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)
	refs, err := coderefs.ScanFile(rel.String(), codeBytes, noteCache)
	require.NoError(t, err)
	codeRefs := map[string][]coderefs.CodeRef{rel.String(): refs}

	tracker := &fakeDedupeTracker{}
	params := FileContextTextParams{
		NoteMetadata:   testNoteMetadataIndexer(t),
		Files:          []string{codePath},
		BudgetChars:    8000,
		Profile:        ContextProfileCode,
		CodeRefsByFile: codeRefs,
		Dedupe:         tracker,
	}
	reader := newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &noteMgr)
	first, err := BuildFileContextText(&vault, reader, params)
	require.NoError(t, err)
	require.Contains(t, first, "This is the detailed body.")
	second, err := BuildFileContextText(&vault, reader, params)
	require.NoError(t, err)
	require.NotContains(t, second, "This is the detailed body.")
	require.Contains(t, second, "short summary")
}

func TestBuildFileContextText_FallsBackToSummaryWhenBudgetTight(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	notePath := filepath.Join(root, "Notes", "Doc.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "short summary"
---

# Doc

This is the detailed body that should not fit.
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	codePath := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// Docs: [[Doc]]\npackage main\n"), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	noteCache := obsidian.BuildNotePathCache([]string{"Notes/Doc.md"})
	codeBytes, err := os.ReadFile(codePath)
	require.NoError(t, err)
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)
	refs, err := coderefs.ScanFile(rel.String(), codeBytes, noteCache)
	require.NoError(t, err)
	codeRefs := map[string][]coderefs.CodeRef{rel.String(): refs}

	text, err := BuildFileContextText(&vault, newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Path: root}, &noteMgr), FileContextTextParams{
		NoteMetadata:   testNoteMetadataIndexer(t),
		Files:          []string{codePath},
		BudgetChars:    1400,
		Profile:        ContextProfileCode,
		CodeRefsByFile: codeRefs,
	})
	require.NoError(t, err)
	require.NotContains(t, text, `<doc kind="note" path="Notes/Doc.md"`)
	require.Contains(t, text, "short summary")
}

func TestBuildFileContextText_IncludesOntologySummaryForTypedNotes(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(filepath.Join(rhizomeDir, "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "ontology", "schema.graphql"), []byte(`
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["people/*.md"], keyField: "name") {
  name: String!
  team: Team @link(inverse: "members")
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "teams"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "teams", "Eng.md"), []byte(`---
type: Team
name: Eng
members:
  - people/Alice.md
---
`), 0o644))
	notePath := filepath.Join(root, "people", "Alice.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
type: Person
name: Alice
team: teams/Eng.md
---

[[Eng]]
`), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		Files:        []string{notePath},
		BudgetChars:  4000,
		Profile:      ContextProfileVault,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "### Ontology")
	require.Contains(t, text, "- type: Person")
	require.Contains(t, text, "- fields: name!, team")
	require.Contains(t, text, "### Ontology neighbors")
	require.Contains(t, text, "- team [field]: teams/Eng.md")
}

func TestBuildFileContextText_SurfacesOntologyDescriptionsAndContextInclude(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(filepath.Join(rhizomeDir, "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "ontology", "schema.graphql"), []byte(`
"""
Team hub note.
Owns the members list and the team runbooks.
"""
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  """Always read members before modifying team structure."""
  members: [Person!] @link(inverse: "team", contextInclude: true)
}

type Person @node(paths: ["people/*.md"], keyField: "name") {
  name: String!
  team: Team @link(inverse: "members")
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "teams"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "teams", "Eng.md"), []byte(`---
type: Team
name: Eng
members:
  - people/Alice.md
---
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "people"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "people", "Alice.md"), []byte(`---
type: Person
name: Alice
team: teams/Eng.md
---

# Alice

Alice leads the incident response rotation for the Eng team.
`), 0o644))

	vault := obsidian.Vault{Name: root}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		Files:        []string{filepath.Join(root, "teams", "Eng.md")},
		BudgetChars:  6000,
		Profile:      ContextProfileVault,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "- type: Team")
	require.Contains(t, text, "members(ctx)")
	require.Contains(t, text, "> Team hub note.")
	require.Contains(t, text, "> Owns the members list and the team runbooks.")
	require.Contains(t, text, "- members (ctx) [field]: people/Alice.md")
	// contextInclude must actually embed the target note body, not just
	// annotate the neighbor list. Agents need the docs in the payload.
	require.Contains(t, text, "#### Ontology context (ctx)")
	require.Contains(t, text, `<doc kind="note" path="people/Alice.md"`)
	require.Contains(t, text, `source="ontology:members"`)
	require.Contains(t, text, "Alice leads the incident response rotation")
}
