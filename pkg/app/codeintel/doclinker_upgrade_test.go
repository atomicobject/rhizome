package codeintel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestDocLinkerUpgradeRefreshesUnchangedIndexedSource(t *testing.T) {
	for _, mode := range []string{"live", "batch"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := semdb.Open(filepath.Join(root, "index.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			linker, err := NewDocLinkerFromNotePaths([]paths.NotePath{"Existing.md", "Budget#USD.md"}, store)
			require.NoError(t, err)
			service := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
				codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache(), codeanchor.WithCodeDocLinker(linker))
			content := []byte("package fixture\n// [[Existing]] and `[changed](Budget%23USD.md)`\nfunc Example() {}\n")
			hash := fmt.Sprintf("%x", sha256.Sum256(content))
			const source = "refs.go"
			require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{Path: source, Lang: codeanchor.LangGo, Hash: hash, ParseStatus: codeanchor.ParseOK}))
			// This is the durable output of the old parser: the comment example is absent.
			require.NoError(t, store.ReplaceDocLinksForPath(ctx, source, []codeanchor.DocLink{{SrcType: "code", SrcPath: source, DstKind: "note", DstPath: "Existing.md"}}))

			index := func() {
				t.Helper()
				if mode == "live" {
					require.NoError(t, service.IndexCodeFile(ctx, codeanchor.LangGo, source, content))
					return
				}
				work, err := service.BuildCodeIndexWork(ctx, codeanchor.LangGo, source, content)
				require.NoError(t, err)
				if work != nil {
					require.NoError(t, service.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))
				}
			}
			links := func(note string) []codeanchor.DocLink {
				t.Helper()
				got, err := store.DocLinksForNote(ctx, note, 10)
				require.NoError(t, err)
				return got
			}
			// A current-version hash match skips doc-link production in both routes.
			index()
			require.Empty(t, links("Budget#USD.md"))
			require.Len(t, links("Existing.md"), 1)
			_, err = store.DB().ExecContext(ctx, "UPDATE files SET indexer_version = ? WHERE path = ?", "v1.14.0", source)
			require.NoError(t, err)
			index()
			require.Len(t, links("Budget#USD.md"), 1, "the old indexer version must refresh unchanged comment links")
			require.Len(t, links("Existing.md"), 1)
			gotHash, version, status, ok, err := store.FileHash(ctx, source)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, hash, gotHash)
			require.Equal(t, codeanchor.IndexerVersion, version)
			require.True(t, status.TrustedForIndexing())
			index()
			require.Len(t, links("Budget#USD.md"), 1)
			t.Logf("unchanged hash upgraded v1.14.0 -> %s; links Existing=1, Budget#USD=1", version)
		})
	}
}
