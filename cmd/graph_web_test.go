package cmd

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveLocalGraphTargetUsesPersistedDocKind(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceGraphDocScores(context.Background(), []semdb.GraphDocScore{
		{DocPath: "Notes/Decision.MD", DocType: "note"},
		{DocPath: "Notes/Reference.html", DocType: "note"},
	}))

	gb := &graphBuilder{store: store, vaultDef: obsidian.VaultDefinition{Path: root}}
	for _, input := range []struct {
		raw  string
		want string
	}{
		{raw: "Notes/Decision.MD", want: "Notes/Decision.MD"},
		{raw: "Notes/Reference.html", want: "Notes/Reference.html"},
		{raw: filepath.Join(root, "Notes", "Reference.html"), want: "Notes/Reference.html"},
	} {
		kind, resolved, err := gb.resolveLocalGraphTarget(context.Background(), input.raw)
		require.NoError(t, err)
		require.Equal(t, "note", kind)
		require.Equal(t, input.want, resolved)
	}
}

func TestResolveLocalGraphTargetRejectsUnknownPath(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	gb := &graphBuilder{store: store, vaultDef: obsidian.VaultDefinition{Path: root}}
	_, _, err = gb.resolveLocalGraphTarget(context.Background(), "Notes/Unknown.html")
	require.ErrorContains(t, err, "not in the persisted graph catalog")
}
