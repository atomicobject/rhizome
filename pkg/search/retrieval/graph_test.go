package retrieval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type errNoteReader struct{}

func (e errNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", errors.New("unexpected call")
}
func (e errNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, errors.New("unexpected call")
}
func (e errNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("unexpected call")
}
func (e errNoteReader) Title(string) (string, bool) { return "", false }

func TestGraphRetriever_CodeOnlySeeds_NoWork(t *testing.T) {
	r := &GraphRetriever{
		VaultDef:   obsidian.VaultDefinition{Path: "/tmp"},
		NoteReader: errNoteReader{},
	}
	out, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{
			knowledge.FileHandle("pkg/app/mcp"),
			knowledge.AnchorHandle("anchor-1"),
		},
	})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestGraphRetriever_UsesPersistedGraphStore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Project.MD"), []byte("# Project\n[[decision]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "decision.html"), []byte("<h1>Decision</h1>"), 0o644))
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: int64(2 * time.Second), Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: "notes/Project.MD", Title: "Project", IndexedAt: int64(2 * time.Second)},
			{Path: "notes/decision.html", Title: "Decision", IndexedAt: int64(2 * time.Second)},
		},
	}))
	require.NoError(t, store.ReplaceGraphDocScores(context.Background(), []semdb.GraphDocScore{
		{DocPath: "notes/Project.MD", DocType: "note", Community: "c1", Outbound: 1, UpdatedAt: 2},
		{DocPath: "notes/decision.html", DocType: "note", Community: "c1", Inbound: 1, UpdatedAt: 2},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(context.Background(), "notes/Project.MD", semdb.GraphDocEdgeKindWikilink, []string{"notes/decision.html"}))

	r := &GraphRetriever{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: errNoteReader{},
		Store:      store,
	}
	out, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteChunkHandle("notes/Project.MD", 0)},
		Intent: search.IntentRelatedToSeed,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Equal(t, "notes/decision.html", out[0].Path)
	require.Equal(t, knowledge.NoteHandle("notes/decision.html"), out[0].Handle)
}

func TestGraphRetriever_IndexedOnlyUsesReadySnapshotWithoutFilesystemReads(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "alpha.md"), []byte("# Alpha\n[[beta]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "beta.md"), []byte("# Beta\n[[alpha]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "gamma.md"), []byte("# Gamma\n[[beta]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "orphan.md"), []byte("# Orphan\n"), 0o644))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	noteMetadata, err := notemeta.NewIndexer(formats)
	require.NoError(t, err)
	_, err = noteMetadata.EnsureIndexed(context.Background(), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceGraphDocScores(context.Background(), nil))

	spec := search.QuerySpec{
		Seeds: []knowledge.Handle{
			knowledge.NoteHandle("notes/alpha.md"),
			knowledge.NoteHandle("notes/missing.md"),
			knowledge.NoteHandle("notes/gamma.md"),
		},
		Intent: search.IntentRelatedToSeed,
		Limits: search.Limits{Total: 10},
	}
	live := &GraphRetriever{VaultDef: vaultDef, NoteReader: &obsidian.Note{}}
	want, err := live.Retrieve(context.Background(), spec)
	require.NoError(t, err)
	require.NotEmpty(t, want)

	timings := &search.Timings{}
	ctx := search.WithTimings(context.Background(), timings)
	collector := indexingperf.NewSemanticQueryCollector()
	ctx = indexingperf.WithCollector(ctx, collector)
	indexed := &GraphRetriever{
		VaultDef:     vaultDef,
		Store:        store,
		SourcePolicy: GraphSourceIndexedOnly,
	}
	got, err := indexed.Retrieve(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, want, got)

	names := make(map[string]bool)
	for _, timing := range timings.Snapshot() {
		names[timing.Name] = true
	}
	require.True(t, names["graph.store.freshness"])
	require.True(t, names["graph.snapshot.load"])
	require.True(t, names["graph.snapshot.analyze"])
	require.False(t, names["graph.filesystem"])
	sawRepoWalks := false
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		if operation.Label == indexingperf.SemanticQueryOpRepoWalks {
			sawRepoWalks = true
			require.Zero(t, operation.Count)
		}
	}
	require.True(t, sawRepoWalks)
}

func TestGraphRetriever_IndexedOnlyHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: int64(time.Second), Ready: true},
		Notes: []semdb.NoteMetadataRow{{Path: "notes/alpha.md", Title: "Alpha", IndexedAt: int64(time.Second)}},
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (&GraphRetriever{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		Store:        store,
		SourcePolicy: GraphSourceIndexedOnly,
	}).Retrieve(ctx, search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle("notes/alpha.md")}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestGraphRetriever_IndexedOnlyDoesNotFallbackWhenMetadataIsUnavailable(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	r := &GraphRetriever{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteReader:   errNoteReader{},
		Store:        store,
		SourcePolicy: GraphSourceIndexedOnly,
	}
	out, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("notes/alpha.md")},
	})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestGraphRetriever_FallsBackWhenPersistedGraphScoresAreStale(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "project.md"), []byte("# Project\n[[decision]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "decision.md"), []byte("# Decision\n"), 0o644))

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: int64(3*time.Second) + 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: "notes/project.md", Title: "Project", IndexedAt: int64(3*time.Second) + 1},
			{Path: "notes/decision.md", Title: "Decision", IndexedAt: int64(3*time.Second) + 1},
			{Path: "notes/stale.md", Title: "Stale", IndexedAt: int64(3*time.Second) + 1},
		},
	}))
	require.NoError(t, store.ReplaceGraphDocScores(context.Background(), []semdb.GraphDocScore{
		{DocPath: "notes/project.md", DocType: "note", Community: "c1", Outbound: 1, UpdatedAt: 3},
		{DocPath: "notes/stale.md", DocType: "note", Community: "c1", Inbound: 1, UpdatedAt: 3},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(context.Background(), "notes/project.md", semdb.GraphDocEdgeKindWikilink, []string{"notes/stale.md"}))

	r := &GraphRetriever{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Store:      store,
	}
	out, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("notes/project.md")},
		Intent: search.IntentRelatedToSeed,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	paths := make([]string, 0, len(out))
	for _, candidate := range out {
		paths = append(paths, candidate.Path)
	}
	require.Contains(t, paths, "notes/decision.md")
	require.NotContains(t, paths, "notes/stale.md")
}
