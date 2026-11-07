package sourceownership

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnershipIsIndependentOfSourceAndRefresh(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	declaration := filepath.Join(root, ".rhizome", "source-ownership.yml")
	require.NoError(t, Check(root, []string{"knowledge/missing.json"}))
	require.NoError(t, os.WriteFile(declaration, []byte("sources:\n  - owner: AO KB\n    reason: Managed by AO KB. Change it with the AO KB skills.\n    paths: [knowledge/**, products/*/model/**]\n"), 0644))
	for _, path := range []string{"knowledge/malformed.md", "knowledge/untyped.md", "knowledge/missing.json", "products/unselected/model/a.yml"} {
		err := Check(root, []string{path})
		require.ErrorContains(t, err, "Managed by AO KB. Change it with the AO KB skills.")
	}
	require.NoError(t, Check(root, []string{"notes/mine.md"}))
	require.Error(t, Check(root, []string{".rhizome/source-ownership.yml"}))
	require.NoError(t, os.WriteFile(declaration, []byte("sources: ["), 0644))
	require.Error(t, Check(root, []string{"knowledge/untyped.md"}))
	require.NoError(t, os.Remove(declaration))
	require.NoError(t, Check(root, []string{"knowledge/untyped.md"}))
}

func TestRejectInvalidOwnershipDeclarations(t *testing.T) {
	for _, input := range []string{"sources: []\n---\nsources: []", "sources: [{owner: x, reason: x, paths: [null]}]", "sources: [{owner: x, paths: [a/**]}]", "sources: [{owner: x, reason: x, paths: [../a]}]", "sources: [{owner: x, reason: x, paths: ['[bad']}]", "source: []", "sources: [{owner: x, reason: x, paths: ['/a']}]"} {
		t.Run(input, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "source-ownership.yml"), []byte(input), 0644))
			require.Error(t, Check(root, []string{"note.md"}))
		})
	}
}

func TestPolicyCapturesOneDeclarationAndRetainsLoadFailures(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	path := filepath.Join(root, ".rhizome/source-ownership.yml")
	require.NoError(t, os.WriteFile(path, []byte("sources:\n  - owner: external\n    reason: Use external tools.\n    paths: [owned/**]\n"), 0644))
	scope := Load(root)
	require.NoError(t, os.WriteFile(path, []byte("sources: ["), 0644))
	for _, host := range []string{"owned/a.md", "owned/b.json"} {
		require.Equal(t, "Use external tools.", scope.ReadOnlyReason(host))
	}
	failed := Load(root)
	require.NoError(t, os.Remove(path))
	require.ErrorContains(t, failed.Check([]string{"any.md"}), "invalid source ownership")
	require.Contains(t, failed.ReadOnlyReason("any.md"), "invalid source ownership")
	require.NoError(t, Check(root, []string{"owned/a.md"}))
}
