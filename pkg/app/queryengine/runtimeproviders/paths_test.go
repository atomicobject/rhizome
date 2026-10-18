package runtimeproviders

import (
	"os"
	"path/filepath"
	"testing"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyQueryRuntimeCodePathsRejectEscapes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing.go"), []byte("package pkg\n"), 0o644))
	provider := New(nil, nil, ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Path: root}}, nil)

	rel, abs, err := provider.runtimeCodePath("pkg/thing.go")
	require.NoError(t, err)
	require.Equal(t, "pkg/thing.go", rel)
	expectedAbs, err := filepath.EvalSymlinks(filepath.Join(root, "pkg", "thing.go"))
	require.NoError(t, err)
	require.Equal(t, expectedAbs, filepath.FromSlash(abs))

	_, _, err = provider.runtimeCodePath("../outside.go")
	require.Error(t, err)
	require.Contains(t, err.Error(), "inside the vault")
}

func TestOntologyQueryRuntimeNotePathsPreserveMixedCaseAuthoredExtensions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := New(nil, nil, ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Path: root}}, nil)

	for _, rel := range []string{"notes/Decision.MD", "notes/Reference.HTML"} {
		t.Run(rel, func(t *testing.T) {
			absPath := filepath.Join(root, filepath.FromSlash(rel))
			require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
			require.NoError(t, os.WriteFile(absPath, []byte("note"), 0o644))

			gotRel, gotAbs, err := provider.runtimeNotePath(rel)
			require.NoError(t, err)
			require.Equal(t, rel, gotRel)
			expectedAbs, err := filepath.EvalSymlinks(absPath)
			require.NoError(t, err)
			require.Equal(t, expectedAbs, filepath.FromSlash(gotAbs))
		})
	}
}
