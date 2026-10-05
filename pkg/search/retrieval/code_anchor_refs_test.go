package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

func TestCodeAnchorRefsRetriever_ExpandsNoteToIntelAnchors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	codePath := filepath.Join(root, "pkg", "embeddings", "chunker.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	code := []byte(`package embeddings

func HashText(s string) string {
	return s
}
`)
	require.NoError(t, os.WriteFile(codePath, code, 0o644))

	dbPath := filepath.Join(root, "intel.db")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
		codeanchor.WithWriteAccess(),
	)

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, codePath, code))

	noteRel := "notes/foo.MD"
	noteAbs := filepath.Join(root, filepath.FromSlash(noteRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(noteAbs), 0o755))
	note := `---
code-anchors:
  go:
    - label: hashTextAnchor
      symbol: example.com/mod/pkg/embeddings.HashText
---
# Foo
`
	require.NoError(t, os.WriteFile(noteAbs, []byte(note), 0o644))
	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot(noteRel, note, 0))
	require.NoError(t, err)

	require.NoError(t, svc.RecomputeAnchorScopes(ctx))

	anchorIDs, err := store.AnchorIDsForNotePath(ctx, noteRel)
	require.NoError(t, err)
	require.NotEmpty(t, anchorIDs)
	anchors, err := store.AnchorsByIDs(ctx, anchorIDs)
	require.NoError(t, err)
	require.NotEmpty(t, anchors)
	syms, _, err := store.AnchorScope(ctx, anchors[0].ID)
	require.NoError(t, err)
	require.NotEmpty(t, syms)
	intelIDs, err := store.IntelAnchorIDsByFQNs(ctx, syms)
	require.NoError(t, err)
	require.NotEmpty(t, intelIDs)

	r := &CodeAnchorRefsRetriever{Store: store, VaultPath: root}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle(noteRel)}})
	require.NoError(t, err)

	found := false
	for _, c := range cands {
		if c.Type != "anchor" || c.AnchorID == "" {
			continue
		}
		if c.Path == "pkg/embeddings/chunker.go" && c.Symbol == "HashText" {
			found = true
			require.NotEmpty(t, c.Evidence)
		}
	}
	require.True(t, found, "expected note seed to expand to HashText intel anchor")
}

func TestCodeAnchorRefsRetriever_ExpandsGlobToModuleAnchors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	codePaths := []string{
		filepath.Join(root, "pkg", "a.go"),
		filepath.Join(root, "pkg", "b.go"),
		filepath.Join(root, "other", "c.go"),
	}
	for _, p := range codePaths {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("package main\n"), 0o644))
	}

	dbPath := filepath.Join(root, "intel.db")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
		codeanchor.WithWriteAccess(),
	)
	for _, p := range codePaths {
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, p, data))
	}

	noteRel := string(paths.NormalizeNote("notes/glob.md"))
	noteAbs := filepath.Join(root, filepath.FromSlash(noteRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(noteAbs), 0o755))
	note := `---
code-anchors:
  go:
    - label: pkgFiles
      glob: pkg/*.go
---
# Glob note
`
	require.NoError(t, os.WriteFile(noteAbs, []byte(note), 0o644))
	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot(noteRel, note, 0))
	require.NoError(t, err)
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))

	r := &CodeAnchorRefsRetriever{Store: store, VaultPath: root}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle(noteRel)}})
	require.NoError(t, err)

	var got []string
	for _, c := range cands {
		if c.Type != "anchor" || !strings.EqualFold(c.Kind, "module") {
			continue
		}
		got = append(got, c.Path)
	}
	require.Contains(t, got, "pkg/a.go")
	require.Contains(t, got, "pkg/b.go")
	require.NotContains(t, got, "other/c.go")
}
