package notemeta

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestMetadataStateCurrentReadsEachNoteOnce(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	const noteCount = 50
	notes := make(map[string]string, noteCount)
	for n := range noteCount {
		notes[fmt.Sprintf("n%03d.md", n)] = fmt.Sprintf("# Note %d\n", n)
	}
	reader := &countingNoteReader{fakeNoteReader: fakeNoteReader{notes: notes}}
	vault := obsidian.VaultDefinition{Path: root}
	indexer := testIndexer(t)
	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)

	reader.reset()
	current, err := indexer.MetadataStateCurrent(ctx, vault, reader, store)
	require.NoError(t, err)
	require.True(t, current)
	require.Equal(t, int64(1), reader.lists.Load(), "one inventory per verification")
	require.Equal(t, int64(noteCount), reader.reads.Load(), "each note is read exactly once")
	require.Equal(t, int64(0), reader.stats.Load(), "content identity needs no mtime stat")
}

func TestDiscoverDirtyPathsReadsEveryNoteExactlyOnce(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	const noteCount = 20
	notes := make(map[string]string, noteCount)
	for n := range noteCount {
		notes[fmt.Sprintf("n%03d.md", n)] = fmt.Sprintf("# Note %d\n", n)
	}
	reader := &countingNoteReader{fakeNoteReader: fakeNoteReader{notes: notes}}
	vault := obsidian.VaultDefinition{Path: root}
	indexer := testIndexer(t)
	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)

	reader.reset()
	dirty, err := indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, dirty.Changed)
	require.Empty(t, dirty.Deleted)
	require.Equal(t, int64(noteCount), reader.reads.Load(), "content freshness still requires reading every note")
}

func TestNotesHashFromSourcesMatchesEntryDigest(t *testing.T) {
	descriptor := noteformat.Descriptor{
		ID:                "markdown",
		Extensions:        []string{".md"},
		ProviderVersion:   "provider-v1",
		ProjectionVersion: "projection-v1",
		OwnershipPolicy:   noteformat.OwnershipDefault,
	}
	fixtures := []struct {
		path    string
		content string
		mtime   int64
	}{
		{path: "a.md", content: "# A\n", mtime: 11},
		{path: "nested/b.md", content: "body b", mtime: 22},
		{path: "c.md", content: "", mtime: 33},
	}

	sources := make([]noteformat.AuthoredSource, 0, len(fixtures))
	entries := make([]noteEntry, 0, len(fixtures))
	for _, fixture := range fixtures {
		notePath, err := paths.CleanNotePath(fixture.path)
		require.NoError(t, err)
		source, err := noteformat.NewAuthoredSource(notePath, descriptor, []byte(fixture.content), fixture.mtime)
		require.NoError(t, err)
		sources = append(sources, source)
		entries = append(entries, noteEntry{Path: fixture.path, Content: fixture.content, Mtime: fixture.mtime})
	}

	require.Equal(t, notesHashFromNoteEntries(entries), notesHashFromSources(sources))
}

func TestDiscoverDirtyPathsParityWithFullRebuild(t *testing.T) {
	baseNotes := func() map[string]string {
		notes := make(map[string]string, 6)
		for n := range 6 {
			notes[fmt.Sprintf("n%04d.md", n)] = fmt.Sprintf("---\naliases: [alias-%d]\n---\n# Note %d\nSee [[n0000]]. keep\n", n, n)
		}
		return notes
	}

	scenarios := []struct {
		name    string
		mutate  func(map[string]string)
		changed []string
		deleted []string
		scanned int
	}{
		{
			name:    "unchanged",
			mutate:  func(map[string]string) {},
			scanned: 6,
		},
		{
			name:    "body edit",
			mutate:  func(notes map[string]string) { notes["n0002.md"] += "\nAdded body line.\n" },
			changed: []string{"n0002.md"},
			scanned: 6,
		},
		{
			name: "added note",
			mutate: func(notes map[string]string) {
				notes["n0006.md"] = "---\naliases: [alias-6]\n---\n# Note 6\nSee [[n0000]]. keep\n"
			},
			changed: []string{"n0006.md"},
			scanned: 7,
		},
		{
			name:    "deleted note",
			mutate:  func(notes map[string]string) { delete(notes, "n0005.md") },
			deleted: []string{"n0005.md"},
			scanned: 5,
		},
		{
			name: "alias rename",
			mutate: func(notes map[string]string) {
				notes["n0001.md"] = "---\naliases: [renamed-1]\n---\n# Note 1\nSee [[n0000]]. keep\n"
			},
			changed: []string{"n0001.md"},
			scanned: 6,
		},
		{
			name: "same mtime and size with different bytes",
			mutate: func(notes map[string]string) {
				notes["n0003.md"] = "---\naliases: [alias-3]\n---\n# Note 3\nSee [[n0000]]. kept\n"
			},
			changed: []string{"n0003.md"},
			scanned: 6,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			vault := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
			indexer := testIndexer(t)

			initial := fakeNoteReader{notes: baseNotes()}
			_, err = indexer.EnsureIndexed(ctx, vault, initial, store)
			require.NoError(t, err)

			mutatedNotes := baseNotes()
			scenario.mutate(mutatedNotes)
			mutated := fakeNoteReader{notes: mutatedNotes}
			if scenario.name == "same mtime and size with different bytes" {
				require.Len(t, mutatedNotes["n0003.md"], len(baseNotes()["n0003.md"]),
					"the fixture must keep byte length identical to exercise the content-read rule")
			}

			dirty, err := indexer.DiscoverDirtyPaths(ctx, vault, mutated, store)
			require.NoError(t, err)
			require.Equal(t, scenario.changed, dirty.Changed)
			require.Equal(t, scenario.deleted, dirty.Deleted)
			require.Equal(t, scenario.scanned, dirty.Scanned)

			if len(scenario.changed) == 0 && len(scenario.deleted) == 0 {
				state, err := store.GetNoteMetadataState(ctx)
				require.NoError(t, err)
				require.Equal(t, state.NotesHash, dirty.NotesHash)
			}

			require.NoError(t, indexer.SyncPaths(ctx, vault, mutated, store, dirty.Changed, dirty.Deleted))

			clean, err := indexer.DiscoverDirtyPaths(ctx, vault, mutated, store)
			require.NoError(t, err)
			require.Empty(t, clean.Changed)
			require.Empty(t, clean.Deleted)
			current, err := indexer.MetadataStateCurrent(ctx, vault, mutated, store)
			require.NoError(t, err)
			require.True(t, current)

			freshStore, err := sqlitefixture.Open(filepath.Join(t.TempDir(), ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, freshStore.Close()) })
			_, err = indexer.EnsureIndexed(ctx, vault, mutated, freshStore)
			require.NoError(t, err)

			require.Equal(t, comparableMetadataRows(t, ctx, freshStore), comparableMetadataRows(t, ctx, store))

			wantAliases, err := freshStore.CurrentNoteAliases(ctx)
			require.NoError(t, err)
			gotAliases, err := store.CurrentNoteAliases(ctx)
			require.NoError(t, err)
			require.Equal(t, wantAliases, gotAliases)

			wantEdges, err := freshStore.GraphDocNoteLinkEdges(ctx)
			require.NoError(t, err)
			gotEdges, err := store.GraphDocNoteLinkEdges(ctx)
			require.NoError(t, err)
			require.Equal(t, sortedEdgeRows(wantEdges), sortedEdgeRows(gotEdges))
		})
	}
}

// comparableMetadataRows drops the store-local identity and timestamps that
// legitimately differ between an incremental store and a fresh rebuild.
func comparableMetadataRows(t *testing.T, ctx context.Context, store *semdb.Store) []semdb.NoteMetadataRow {
	t.Helper()
	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	out := make([]semdb.NoteMetadataRow, 0, len(rows))
	for _, row := range rows {
		row.NoteID = 0
		row.IndexedAt = 0
		row.Projection.UpdatedAt = 0
		out = append(out, row)
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Path < out[right].Path })
	return out
}

func sortedEdgeRows(rows []semdb.GraphDocEdgeRow) []semdb.GraphDocEdgeRow {
	out := append([]semdb.GraphDocEdgeRow(nil), rows...)
	sortGraphEdges(out)
	return out
}
