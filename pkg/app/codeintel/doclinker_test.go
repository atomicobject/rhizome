package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestDocLinkerPersistsMixedCaseAuthoredNotePath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Decision.MD"), []byte("# Decision\n"), 0o644))

	linker := NewDocLinker(root)
	require.NotNil(t, linker)
	links := linker.LinksForCode("src/main.go", []byte("package main\n// See [[Decision]]\n"), codeanchor.FileContext{})
	require.Len(t, links, 1)
	require.Equal(t, "Decision.MD", links[0].DstPath)

	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceDocLinksForPath(context.Background(), "src/main.go", links))

	persisted, err := store.DocLinksForNote(context.Background(), "Decision.MD", 10)
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	require.Equal(t, "Decision.MD", persisted[0].DstPath)
	require.NotEqual(t, "Decision.MD.md", persisted[0].DstPath)
}

func TestDocLinkerFromNotePathsResolvesSuppliedPathsAndStoredAliases(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "aliases.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes-hash", LoadedAt: 42, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: "Notes/Decision.MD", Title: "Decision", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "stale.md", Title: "Stale", ContentHash: "b", Mtime: 1, Size: 1},
		},
		PropertyValues: []semdb.NotePropertyValueRow{
			{NotePath: "Notes/Decision.MD", PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "DEC-001", ValueNorm: "dec-001", ValueKind: semdb.NotePropertyValueString, IsList: true},
			{NotePath: "stale.md", PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "STALE", ValueNorm: "stale", ValueKind: semdb.NotePropertyValueString, IsList: true},
		},
	}))

	linker, err := NewDocLinkerFromNotePaths([]paths.NotePath{"Notes/Decision.MD"}, store)
	require.NoError(t, err)
	require.NotNil(t, linker)

	links := linker.LinksForCode("src/main.go", []byte("package main\n// See [[Decision]] and [[DEC-001]] and [[STALE]]\n"), codeanchor.FileContext{})
	require.Len(t, links, 2)
	require.Equal(t, "Notes/Decision.MD", links[0].DstPath)
	require.Equal(t, "Notes/Decision.MD", links[1].DstPath)
}

func TestDocLinkerFromNotePathsRejectsNonCanonicalPath(t *testing.T) {
	linker, err := NewDocLinkerFromNotePaths([]paths.NotePath{"./notes/decision.md"}, nil)
	require.Nil(t, linker)
	require.Error(t, err)
}
