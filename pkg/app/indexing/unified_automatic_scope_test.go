package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// Code added after setup, in folders init never saw, indexes on the next run
// when config turns code on without naming folders.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US9-AC1]]
func TestRunUnifiedCoreIndexesCodeAddedAfterSetupWithoutFolderLimits(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	write("docs/a.md", "# A\n")
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Code:           obsidian.LocalCodeConfig{Enabled: true},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	run := func() {
		t.Helper()
		require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{
			VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root}, NoteMetadata: testNoteMetadataIndexer(t),
		}))
	}
	run()

	write("go.mod", "module example.com/app\n\ngo 1.24\n")
	write("tools/hello/hello.go", "package hello\n\nfunc Hello() {}\n")
	write("web/app.ts", "export function greet(): string { return \"hi\" }\n")
	run()

	store := openTouchParityStore(t, root)
	files, err := store.ListFilesWithLang(ctx, 0)
	require.NoError(t, err)
	langs := map[string]string{}
	for _, file := range files {
		langs[file.Path] = file.Lang
	}
	require.Equal(t, "go", langs["tools/hello/hello.go"])
	require.Equal(t, "ts", langs["web/app.ts"])
	meta, err := store.SymbolMetaForFQNs(ctx, "go", []string{"example.com/app/tools/hello.Hello"})
	require.NoError(t, err)
	require.Contains(t, meta, "example.com/app/tools/hello.Hello")
}
