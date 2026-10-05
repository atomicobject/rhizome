package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	vaultignore "github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/stretchr/testify/require"
)

func TestFileReadEndpointsConfineResolvedPathsToVault(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ignored"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "inside.md"), []byte("# Inside\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "page.html"), []byte("<h1>Inside HTML</h1>\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored", "secret.md"), []byte("# Ignored secret\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outer, "outside.md"), []byte("# Outside secret\n"), 0o644))

	vaultDef := obsidian.VaultDefinition{Root: root}
	catalog, err := NewFileCatalog(vaultDef, nil, testNoteMetadataIndexer(t))
	require.NoError(t, err)
	srv := &Server{
		cfg:     Config{VaultPath: root, VaultDef: vaultDef, NoteMetadata: testNoteMetadataIndexer(t)},
		runtime: &Runtime{Ignore: vaultignore.NewMatcher([]string{"ignored/"})},
		catalog: catalog,
	}

	file, err := srv.readFileView(t.Context(), "notes/page.html")
	require.NoError(t, err)
	require.Equal(t, "<h1>Inside HTML</h1>\n", file.Content)

	for _, input := range []string{"notes/inside.md", "/notes/inside.md"} {
		file, err := srv.readFileView(t.Context(), input)
		require.NoError(t, err, input)
		require.Equal(t, "# Inside\n", file.Content, input)

		rendered, err := srv.readRenderedNote(t.Context(), input)
		require.NoError(t, err, input)
		require.Equal(t, "# Inside\n", rendered.Content, input)
	}

	for _, input := range []string{"../outside.md", "//outside.md", `C:\outside.md`} {
		_, err = srv.readFileView(t.Context(), input)
		require.ErrorIs(t, err, paths.ErrOutsideVault, input)
		_, err = srv.readRenderedNote(t.Context(), input)
		require.ErrorIs(t, err, paths.ErrOutsideVault, input)
	}

	absOutside := filepath.Join(outer, "outside.md")
	_, err = srv.readFileView(t.Context(), absOutside)
	require.Error(t, err)
	_, err = srv.readRenderedNote(t.Context(), absOutside)
	require.Error(t, err)

	t.Run("symlinks", func(t *testing.T) {
		if err := os.Symlink("inside.md", filepath.Join(root, "notes", "inside-link.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.Symlink(filepath.Join(outer, "outside.md"), filepath.Join(root, "notes", "outside-link.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.Symlink(filepath.Join("..", "ignored", "secret.md"), filepath.Join(root, "notes", "ignored-link.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		file, err := srv.readFileView(t.Context(), "notes/inside-link.md")
		require.NoError(t, err)
		require.Equal(t, "# Inside\n", file.Content)
		rendered, err := srv.readRenderedNote(t.Context(), "notes/inside-link.md")
		require.NoError(t, err)
		require.Equal(t, "# Inside\n", rendered.Content)

		input := "notes/outside-link.md"
		_, err = srv.readFileView(t.Context(), input)
		require.ErrorIs(t, err, paths.ErrOutsideVault, input)
		_, err = srv.readRenderedNote(t.Context(), input)
		require.ErrorIs(t, err, paths.ErrOutsideVault, input)

		_, err = srv.readFileView(t.Context(), "notes/ignored-link.md")
		require.ErrorContains(t, err, "path is ignored")
		_, err = srv.readRenderedNote(t.Context(), "notes/ignored-link.md")
		require.ErrorContains(t, err, "path is ignored")
	})
}

func TestListTreeConfinesResolvedDirectoriesToVault(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(outer, "outside"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "inside.md"), []byte("# Inside\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outer, "outside", "secret.md"), []byte("# Outside secret\n"), 0o644))

	srv := &Server{cfg: Config{VaultPath: root}}
	for _, input := range []string{"", ".", "/"} {
		_, err := srv.listTree(t.Context(), input, 20)
		require.NoError(t, err, input)
	}
	_, err := srv.listTree(t.Context(), "../outside", 20)
	require.ErrorIs(t, err, paths.ErrOutsideVault)

	t.Run("symlinks", func(t *testing.T) {
		if err := os.Symlink("notes", filepath.Join(root, "inside-link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.Symlink(filepath.Join(outer, "outside"), filepath.Join(root, "outside-link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err := srv.listTree(t.Context(), "inside-link", 20)
		require.NoError(t, err)
		_, err = srv.listTree(t.Context(), "outside-link", 20)
		require.ErrorIs(t, err, paths.ErrOutsideVault)
	})
}

func TestListTreeFiltersSymlinkEntriesByResolvedTarget(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "vault")
	for _, dir := range []string{"notes", "shared", "ignored", "container"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(outer, "outside-dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "inside.md"), []byte("# Inside\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "shared", "child.md"), []byte("# Child\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored", "secret.md"), []byte("# Secret\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outer, "outside.md"), []byte("# Outside\n"), 0o644))

	vaultDef := obsidian.VaultDefinition{Root: root}
	catalog, err := NewFileCatalog(vaultDef, nil, testNoteMetadataIndexer(t))
	require.NoError(t, err)
	srv := &Server{
		cfg:     Config{VaultPath: root, VaultDef: vaultDef},
		runtime: &Runtime{Ignore: vaultignore.NewMatcher([]string{"ignored/"})},
		catalog: catalog,
	}

	t.Run("symlink entries", func(t *testing.T) {
		links := [][2]string{
			{"inside.md", filepath.Join(root, "notes", "allowed.md")},
			{filepath.Join("..", "shared"), filepath.Join(root, "notes", "allowed-dir")},
			{filepath.Join("..", "ignored", "secret.md"), filepath.Join(root, "notes", "ignored.md")},
			{filepath.Join(outer, "outside.md"), filepath.Join(root, "notes", "outside.md")},
			{filepath.Join(outer, "outside-dir"), filepath.Join(root, "notes", "outside-dir")},
			{"missing.md", filepath.Join(root, "notes", "broken.md")},
			{filepath.Join("..", "ignored", "secret.md"), filepath.Join(root, "container", "ignored.md")},
			{filepath.Join(outer, "outside.md"), filepath.Join(root, "container", "outside.md")},
			{"missing.md", filepath.Join(root, "container", "broken.md")},
		}
		for _, link := range links {
			if err := os.Symlink(link[0], link[1]); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}

		notes, err := srv.listTree(t.Context(), "notes", 20)
		require.NoError(t, err)
		entryPaths := make([]string, 0, len(notes.Entries))
		entriesByPath := make(map[string]TreeEntry, len(notes.Entries))
		for _, entry := range notes.Entries {
			entryPaths = append(entryPaths, entry.Path)
			entriesByPath[entry.Path] = entry
		}
		require.ElementsMatch(t, []string{
			"notes/allowed-dir",
			"notes/allowed.md",
			"notes/inside.md",
		}, entryPaths)
		require.Equal(t, "dir", entriesByPath["notes/allowed-dir"].Kind)
		require.True(t, entriesByPath["notes/allowed-dir"].HasChildren)
		require.Equal(t, "note", entriesByPath["notes/allowed.md"].Kind)

		rootTree, err := srv.listTree(t.Context(), "", 20)
		require.NoError(t, err)
		for _, entry := range rootTree.Entries {
			if entry.Path == "container" {
				require.False(t, entry.HasChildren)
				return
			}
		}
		t.Fatal("container tree entry missing")
	})
}

func TestVaultReadPathRequiresConfiguredRoot(t *testing.T) {
	srv := &Server{}

	_, err := srv.listTree(t.Context(), "", 20)
	require.ErrorContains(t, err, "vault root is required")
	_, err = srv.readFileView(t.Context(), "notes/inside.md")
	require.ErrorContains(t, err, "vault root is required")
	_, err = srv.readRenderedNote(t.Context(), "notes/inside.md")
	require.ErrorContains(t, err, "vault root is required")
}

func TestReadFileViewUsesCurrentConfiguredMarkdownProjectionFacts(t *testing.T) {
	root := t.TempDir()
	rel := "notes/Decision.MD"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("---\ntitle: parsed title\n---\n[[missing]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "target.md"), []byte("# Target\n"), 0o644))

	descriptor := fileViewMarkdownDescriptor()
	metadata, err := noteformat.NewMetadataValue("from projection")
	require.NoError(t, err)
	projection, err := noteformat.NewProjectionWithFacts(
		descriptor.ProviderVersion,
		descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		nil,
		descriptor.Capabilities,
		noteformat.ProjectionFacts{
			Title: &noteformat.TitleFact{Value: "projection title"},
			RootMetadata: []noteformat.RootMetadataFact{{
				Key:   "source",
				Value: metadata,
			}},
			Links: []noteformat.UnresolvedAuthoredLinkFact{{
				Resolution:    noteformat.LinkResolutionNoteReference,
				Syntax:        "wikilink",
				Subtype:       "basic",
				ResolverInput: "target",
				Target:        "target",
				Path:          "target",
			}},
		},
	)
	require.NoError(t, err)
	projector := &fileViewProjector{descriptor: descriptor, projection: projection}
	registry, err := noteformat.NewRegistry(projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	vaultDef := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}}
	catalog, err := NewFileCatalog(vaultDef, nil, indexer)
	require.NoError(t, err)
	srv := &Server{
		cfg:     Config{VaultPath: root, VaultDef: vaultDef, NoteMetadata: indexer},
		catalog: catalog,
	}

	got, err := srv.readFileView(t.Context(), rel)
	require.NoError(t, err)
	require.Equal(t, 1, projector.calls)
	require.Equal(t, paths.NotePath(rel), projector.lastPath)
	require.Equal(t, "note", got.Kind)
	require.Equal(t, "projection title", got.Title)
	require.Equal(t, map[string]interface{}{"source": "from projection"}, got.Frontmatter)
	require.Equal(t, []ResolvedLink{{Target: "notes/target.md", Text: "target", Kind: "wikilink"}}, got.Links)
}

func TestReadFileViewRejectsConfiguredDescriptorOnlyHTML(t *testing.T) {
	root := t.TempDir()
	rel := "notes/Decision.HTML"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("---\ntitle: markdown-looking input\n---\n[[target]]\n"), 0o644))

	vaultDef := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.html"}}
	catalog, err := NewFileCatalog(vaultDef, nil, descriptorOnlyHTMLNoteMetadataIndexer(t))
	require.NoError(t, err)
	srv := &Server{
		cfg:     Config{VaultPath: root, VaultDef: vaultDef},
		catalog: catalog,
	}

	_, err = srv.readFileView(t.Context(), rel)
	require.ErrorContains(t, err, "format \"html\"")
	require.ErrorContains(t, err, "file view projection is not supported")
	require.Nil(t, srv.resolveMarkdownLinksCompat(rel, "[[target]]"))
	require.Nil(t, srv.markdownEmbedsCompat(rel, "![[target]]"))
	require.Nil(t, srv.markdownAliasesForNotePathCompat(t.Context(), rel))
}

func TestRenderedNoteUsesFutureProviderFactsWithoutMarkdownCompatibilityParsing(t *testing.T) {
	root := t.TempDir()
	const rel = "notes/Decision.future"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("---\ntitle: raw Markdown title\n---\n[[raw-only]]\n![[raw-embed]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "target.future"), []byte("future target"), 0o644))

	descriptor := futureFileViewDescriptor()
	metadata, err := noteformat.NewMetadataValue("from future projection")
	require.NoError(t, err)
	projection, err := noteformat.NewProjectionWithFacts(
		descriptor.ProviderVersion,
		descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		nil,
		descriptor.Capabilities,
		noteformat.ProjectionFacts{
			Title: &noteformat.TitleFact{Value: "future projection title"},
			RootMetadata: []noteformat.RootMetadataFact{{
				Key:   "source",
				Value: metadata,
			}},
			Links: []noteformat.UnresolvedAuthoredLinkFact{{
				Resolution:    noteformat.LinkResolutionNoteReference,
				Syntax:        "future-link",
				Subtype:       "basic",
				ResolverInput: "target",
				Target:        "target",
				Path:          "target",
			}},
		},
	)
	require.NoError(t, err)
	projector := &fileViewProjector{descriptor: descriptor, projection: projection}
	registry, err := noteformat.NewRegistry(markdown.New(), projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	vaultDef := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.future"}}
	catalog, err := NewFileCatalog(vaultDef, nil, indexer)
	require.NoError(t, err)
	srv := &Server{
		cfg:     Config{VaultPath: root, VaultDef: vaultDef, NoteMetadata: indexer},
		catalog: catalog,
	}

	got, err := srv.readRenderedNote(t.Context(), rel)
	require.NoError(t, err)
	require.Equal(t, 1, projector.calls)
	require.Equal(t, "future projection title", got.Title)
	require.Equal(t, map[string]interface{}{"source": "from future projection"}, got.Frontmatter)
	require.Equal(t, "---\ntitle: raw Markdown title\n---\n[[raw-only]]\n![[raw-embed]]\n", got.Rendered)
	require.Equal(t, []ResolvedLink{{Target: "notes/target.future", Text: "target", Kind: "wikilink"}}, got.Links)
	require.Empty(t, got.Embeds, "raw Markdown embed syntax must not be parsed for a future format")
	require.Nil(t, srv.resolveMarkdownLinksCompat(rel, "[[raw-only]]"))
	require.Nil(t, srv.markdownEmbedsCompat(rel, "![[raw-embed]]"))
	require.Nil(t, srv.markdownAliasesForNotePathCompat(t.Context(), rel))
}

type fileViewProjector struct {
	descriptor noteformat.Descriptor
	projection noteformat.Projection
	calls      int
	lastPath   paths.NotePath
}

func (p *fileViewProjector) Descriptor() noteformat.Descriptor {
	return p.descriptor
}

func (p *fileViewProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.calls++
	p.lastPath = source.Path()
	return p.projection, nil
}

func fileViewMarkdownDescriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "markdown",
		Extensions:        []string{".md"},
		ProviderVersion:   "file-view-provider-v1",
		ProjectionVersion: "file-view-projection-v1",
		OwnershipPolicy:   noteformat.OwnershipDefault,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityAuthoredLinkExtraction,
		),
	}
}

func futureFileViewDescriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "future-web",
		Extensions:        []string{".future"},
		ProviderVersion:   "future-web-provider-v1",
		ProjectionVersion: "future-web-projection-v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityAuthoredLinkExtraction,
		),
	}
}

func TestNotePathCacheStaysHotUntilInvalidated(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "first.md"), []byte("# First\n"), 0o644))

	srv := &Server{
		cfg: Config{
			VaultPath:    root,
			VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
			NoteMetadata: testNoteMetadataIndexer(t),
		},
	}

	first := srv.notePathCache(t.Context())
	resolved, ok := first.ResolveNote("first")
	require.True(t, ok)
	require.Equal(t, "notes/first.md", resolved)

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "second.md"), []byte("# Second\n"), 0o644))
	second := srv.notePathCache(t.Context())
	require.Same(t, first, second)
	_, ok = second.ResolveNote("second")
	require.False(t, ok)

	srv.invalidateNoteCaches()
	refreshed := srv.notePathCache(t.Context())
	require.NotSame(t, first, refreshed)
	resolved, ok = refreshed.ResolveNote("second")
	require.True(t, ok)
	require.Equal(t, "notes/second.md", resolved)
}

func TestNotePathCacheBuildsFromIndexedMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{
			NotesHash:    "ready",
			RawNotesHash: "ready",
			LoadedAt:     1,
			Ready:        true,
		},
		Notes: []semdb.NoteMetadataRow{{
			Path:      "docs/spec.md",
			Title:     "Spec",
			IndexedAt: 1,
		}},
		PropertyValues: []semdb.NotePropertyValueRow{{
			NotePath:     "docs/spec.md",
			PropertyName: "aliases",
			Source:       semdb.NotePropertySourceFrontmatter,
			ValueText:    "SPEC-0001",
			ValueNorm:    "spec-0001",
			ValueKind:    semdb.NotePropertyValueString,
			IsList:       true,
			ListOrdinal:  0,
		}},
	}))

	srv := &Server{
		cfg: Config{
			VaultPath:    root,
			VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
			NoteMetadata: testNoteMetadataIndexer(t),
		},
		runtime: &Runtime{IntelStore: store},
	}

	cache := srv.notePathCache(t.Context())
	resolved, ok := cache.ResolveNote("SPEC-0001")
	require.True(t, ok)
	require.Equal(t, "docs/spec.md", resolved)
	require.Equal(t, []string{"docs/spec.md"}, cache.Aliases["SPEC-0001"])
	resolved, ok = cache.ResolveNote("spec")
	require.True(t, ok)
	require.Equal(t, "docs/spec.md", resolved)
}

func TestNotePathCacheUpdatesIncrementallyFromWatchEvents(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "first.md"), []byte(`---
aliases: [FIRST]
---
# First
`), 0o644))

	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root}
	catalog, err := NewFileCatalog(vaultDef, nil, testNoteMetadataIndexer(t))
	require.NoError(t, err)
	srv := &Server{
		cfg: Config{
			VaultPath:    root,
			VaultDef:     vaultDef,
			NoteMetadata: testNoteMetadataIndexer(t),
		},
		catalog: catalog,
	}

	cache := srv.notePathCache(t.Context())
	resolved, ok := cache.ResolveNote("first")
	require.True(t, ok)
	require.Equal(t, "notes/first.md", resolved)

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "second.md"), []byte(`---
aliases: [SECOND]
---
# Second
`), 0o644))
	srv.updateNotePathCacheForWatchEvents(t.Context(), []watchhub.WatchEvent{{
		RelPath: "notes/second.md",
		Op:      watchhub.OpCreate,
	}})

	updated := srv.notePathCache(t.Context())
	require.Same(t, cache, updated)
	resolved, ok = updated.ResolveNote("SECOND")
	require.True(t, ok)
	require.Equal(t, "notes/second.md", resolved)
	require.Equal(t, []string{"notes/second.md"}, updated.Aliases["SECOND"])

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "first.md"), []byte(`---
aliases: [FIRST-UPDATED]
---
# First
`), 0o644))
	srv.updateNotePathCacheForWatchEvents(t.Context(), []watchhub.WatchEvent{{
		RelPath: "notes/first.md",
		Op:      watchhub.OpWrite,
	}})
	_, ok = updated.ResolveNote("FIRST")
	require.False(t, ok)
	resolved, ok = updated.ResolveNote("FIRST-UPDATED")
	require.True(t, ok)
	require.Equal(t, "notes/first.md", resolved)
	require.Equal(t, []string{"notes/first.md"}, updated.Aliases["FIRST-UPDATED"])

	srv.updateNotePathCacheForWatchEvents(t.Context(), []watchhub.WatchEvent{{
		RelPath: "notes/second.md",
		Op:      watchhub.OpRemove,
	}})
	_, ok = updated.ResolveNote("SECOND")
	require.False(t, ok)
}
