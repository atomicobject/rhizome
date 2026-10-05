package actions

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOpenMetadataStore_SkipsMissingVaultPath(t *testing.T) {
	store, cleanup, err := openMetadataStore(obsidian.VaultDefinition{
		Name: "vault",
		Path: "/definitely-missing-test-vault",
	})
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestOpenDocGraphStore_SkipsMissingVaultPath(t *testing.T) {
	store, cleanup, err := openDocGraphStore("/definitely-missing-test-vault")
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestLoadCodeAnchorForContext_SkipsMissingVaultPath(t *testing.T) {
	svc, cleanup, err := loadCodeAnchorForContext("/definitely-missing-test-vault", nil)
	require.NoError(t, err)
	require.Nil(t, svc)
	require.Nil(t, cleanup)
}

func TestLoadCodeAnchorForContext_UsesProvidedStore(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(root + "/db.sqlite")
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.UpsertNoteWithCleanup(context.Background(), codeanchor.Note{
		Path: "notes/only-in-supplied.md", Title: "Supplied",
		DefinedAnchors: []codeanchor.Anchor{{Kind: codeanchor.AnchorPath, Label: "supplied-proof", PathPrefix: "pkg"}},
	}, nil)
	require.NoError(t, err)

	svc, cleanup, err := loadCodeAnchorForContext(root, store)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Nil(t, cleanup)
	anchors, err := svc.AnchorsByNotePaths(context.Background(), []string{"notes/only-in-supplied.md"})
	require.NoError(t, err)
	require.Len(t, anchors["notes/only-in-supplied.md"], 1)
	require.Equal(t, "supplied-proof", anchors["notes/only-in-supplied.md"][0].Label)
}
