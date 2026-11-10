package operations_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"github.com/stretchr/testify/require"
)

// SPEC-0133 R5: a move that touches a YAML host a repository's own declaration
// owns reports the owner's reason, not the absence of a YAML mutation adapter,
// and changes no bytes. The AO KB starter declares no ownership of its own.
func TestMoveRefusesOwnedStructuredHostsWithOwnerReason(t *testing.T) {
	const declaration = "sources:\n  - owner: Model tool\n    reason: Managed by the model tool. Change it there.\n    paths:\n      - artifacts/**\n      - products/*/artifacts/**\n"
	model, err := os.ReadFile("../../../testdata/ao-kb-product/account/artifacts/_assets/_model/features.yaml")
	require.NoError(t, err)
	for _, move := range []operations.MoveRequest{
		{Source: "artifacts/_assets/_model/features.yaml", Target: "notes/features.yaml"},
		{Source: "notes/draft.yaml", Target: "products/alpha/artifacts/_assets/_model/features.yaml"},
	} {
		t.Run(move.Source, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{sourceownership.DeclarationPath: declaration, "artifacts/_assets/_model/features.yaml": string(model), "notes/draft.yaml": string(model)}
			for name, content := range files {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
			}
			_, err := operations.MoveNotes(namespaceVault{root}, nil, operations.MoveParams{Context: t.Context(), Moves: []operations.MoveRequest{move}, UpdateBacklinks: true, NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root)})
			require.ErrorContains(t, err, "Managed by the model tool. Change it there.")
			for name, content := range files {
				data, err := os.ReadFile(filepath.Join(root, name))
				require.NoError(t, err)
				require.Equal(t, content, string(data))
			}
			require.NoFileExists(t, filepath.Join(root, move.Target))
		})
	}
}
