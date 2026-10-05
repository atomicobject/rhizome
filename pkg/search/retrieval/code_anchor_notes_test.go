package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestCodeAnchorNotesRetriever_ExpandsFileToAnchoredNotes(t *testing.T) {
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
	)

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, codePath, code))

	noteRel := filepath.ToSlash(filepath.Join("notes", "foo.MD"))
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

	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	relCode, err := vaultPaths.RelCodeStrict(codePath)
	require.NoError(t, err)

	r := &CodeAnchorNotesRetriever{Store: store, VaultPath: root}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Seeds: []knowledge.Handle{knowledge.FileHandle(relCode.String())}})
	require.NoError(t, err)

	found := false
	for _, c := range cands {
		if c.Type != "note" || c.NoteID != noteRel {
			continue
		}
		found = true
		require.NotEmpty(t, c.Evidence)
		require.Equal(t, "code_anchor", c.Evidence[0].Type)
		require.Equal(t, "hashTextAnchor", c.Evidence[0].Details["anchor"])
	}
	require.True(t, found, "expected file seed to expand to anchored note")
}
