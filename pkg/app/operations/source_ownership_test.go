package operations_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/stretchr/testify/require"
)

func TestGraphMoveRefusesOwnedBacklinkOrCoderefWithoutChangingBytes(t *testing.T) {
	for _, owned := range []string{"backlink.md", "main.go", "target.md", "renamed.md"} {
		t.Run(owned, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{"target.md": "# Target\n", "backlink.md": "See [[target]].\n", "main.go": "package sample\n// See [[target]]\n"}
			for name, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0644))
			}
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: external\n    reason: Managed externally.\n    paths: ["+owned+"]\n"), 0644))
			_, err := operations.MoveNotes(namespaceVault{root}, nil, operations.MoveParams{Context: t.Context(), Moves: []operations.MoveRequest{{Source: "target.md", Target: "renamed.md"}}, UpdateBacklinks: true, NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), CodeRefConfig: &coderefs.Config{Enabled: true, Includes: []string{"**/*.go"}}})
			require.ErrorContains(t, err, "Managed externally.")
			for name, content := range files {
				data, err := os.ReadFile(filepath.Join(root, name))
				require.NoError(t, err)
				require.Equal(t, content, string(data))
			}
			require.NoFileExists(t, filepath.Join(root, "renamed.md"))
		})
	}
}
