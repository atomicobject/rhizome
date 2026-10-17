package noteownership_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// The shared ownership walker retains the included-subtree contract previously
// exercised through the retired indexingpipe walker.
func TestDiscoverIncludedSubtreeHonorsNestedGitignore(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write(".gitignore", "app/\n")
	write("app/.gitignore", "generated/\n")
	write("app/src/main.go", "package main\n")
	write("app/generated/gen.go", "package gen\n")
	write("app/node_modules/dep/main.go", "package dependency\n")
	write("top.go", "package top\n")
	write(".rhizome/ignore", "# rhizome: included subtrees\n!/app/\n")

	snapshot, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(root)},
		CodeLanguage:    ownershipTestLanguage,
	})
	require.NoError(t, err)
	var actual []string
	for _, candidate := range snapshot.CodeCandidates() {
		actual = append(actual, candidate.Path.String())
	}
	require.Equal(t, []string{"app/src/main.go", "top.go"}, actual)
}
