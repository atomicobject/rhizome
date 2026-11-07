package ontology

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func ownTestSource(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: AO KB\n    reason: Managed by AO KB. Change it with the AO KB skills.\n    paths: [owned.md]\n"), 0644))
}

func TestOwnedSourceRefusesEveryStagedEditWithoutProjection(t *testing.T) {
	root := t.TempDir()
	original := "---\nbroken: [\n---\nmalformed but owned\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "owned.md"), []byte(original), 0644))
	ownTestSource(t, root)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, nil)
	ref := NodeRef{NotePath: "owned.md", Kind: NodeKindNote}
	for _, edit := range []func() error{
		func() error { return session.SetScalarField(ref, "title", "new") },
		func() error { return session.SetLinkField(ref, "related", []string{"other"}) },
		func() error { return session.SetRootMetadataList(ref, "aliases", []string{"new"}) },
		func() error { return session.SetNarrative(ref, 0, 1, "x", "y") },
		func() error { return session.AddEmbeddedNode(ref, "items", "new", "body", "id") },
		func() error { return session.ReorderCollection(ref, "items", []string{"id"}) },
		func() error { return session.DeleteNode(ref) },
		func() error { return session.EnsureBlockID(ref, "new-id") },
		func() error { return session.RewriteFile("owned.md", "replacement") },
	} {
		require.ErrorContains(t, edit(), "Managed by AO KB")
	}
	require.Empty(t, session.ops)
	data, err := os.ReadFile(filepath.Join(root, "owned.md"))
	require.NoError(t, err)
	require.Equal(t, original, string(data))
}

func TestEditCommitRechecksOwnershipAddedAfterStaging(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"owned.md", "mine.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("original\n"), 0644))
	}
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, nil)
	require.NoError(t, session.RewriteFile("mine.md", "changed\n"))
	require.NoError(t, session.RewriteFile("owned.md", "changed\n"))
	ownTestSource(t, root)
	_, err := session.Commit(t.Context())
	require.ErrorContains(t, err, "Managed by AO KB")
	for _, path := range []string{"owned.md", "mine.md"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Equal(t, "original\n", string(data))
	}
	unrelated := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, nil)
	require.NoError(t, unrelated.RewriteFile("mine.md", "changed\n"))
	_, err = unrelated.Commit(t.Context())
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(root, "mine.md"))
	require.NoError(t, err)
	require.Equal(t, "changed\n", string(data))
}
