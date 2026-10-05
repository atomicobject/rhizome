//go:build cgo

package codeanchor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTSIndexer_DefaultExportAliasKeepsDeclarationMetadata(t *testing.T) {
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
		for _, declaration := range []string{"function Thing() {}", "class Thing {}"} {
			t.Run(ext+"/"+declaration, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"export-fixture"}`), 0o644))
				path := filepath.Join(root, "Thing"+ext)
				content := []byte("/** declaration documentation */\nexport default " + declaration)
				require.NoError(t, os.WriteFile(path, content, 0o644))
				summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
				require.NoError(t, err)
				var named []Symbol
				for _, symbol := range summary.Symbols {
					if symbol.Name == "Thing" {
						named = append(named, symbol)
					}
				}
				require.Len(t, named, 2, "the default alias precedes the authored declaration, even when their names match")
				require.Equal(t, named[0].NormalizeFQN(), named[1].NormalizeFQN())
				require.Zero(t, named[0].StartByte)
				require.Zero(t, named[0].EndByte)
				require.Positive(t, named[1].StartByte)
				require.Equal(t, int64(len(content)), named[1].EndByte)
			})
		}
	}
}
