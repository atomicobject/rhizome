package notemeta

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLoadReadyPersistedGraphSnapshotAllowsNilStore(t *testing.T) {
	snapshot, err := LoadReadyPersistedGraphSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, snapshot)
}

func TestPersistedAndFilesystemGraphSnapshotsHaveAlgorithmParity(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "alpha.md"), []byte(`---
tags: [planning]
---
Alpha links [[beta]].
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "beta.md"), []byte(`---
tags: [reference]
---
Beta links [[alpha]].
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "orphan.md"), []byte("No links."), 0o644))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	note := &obsidian.Note{}
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vaultDef, note, store)
	require.NoError(t, err)

	options := obsidian.GraphAnalysisOptions{
		WikilinkOptions:   obsidian.DefaultWikilinkOptions,
		IncludeTags:       true,
		RecencyCascade:    false,
		RecencyCascadeSet: true,
	}
	filesystem, err := obsidian.BuildGraphSnapshot(vaultDef, note, options)
	require.NoError(t, err)
	persisted, err := testIndexer(t).LoadPersistedGraphSnapshot(context.Background(), vaultDef, note, store)
	require.NoError(t, err)
	require.NotNil(t, persisted)

	require.Equal(t, graphShape(filesystem), graphShape(persisted))
	require.Equal(t, obsidian.ComputeGraphStatsFromSnapshot(filesystem), obsidian.ComputeGraphStatsFromSnapshot(persisted))
	require.Equal(t,
		map[string][]obsidian.Backlink{
			"notes/alpha.md":  {{Referrer: "notes/beta.md", LinkType: obsidian.BacklinkTypeBasic}},
			"notes/beta.md":   {{Referrer: "notes/alpha.md", LinkType: obsidian.BacklinkTypeBasic}},
			"notes/orphan.md": {},
		},
		obsidian.CollectBacklinksFromGraph(persisted, []string{"notes/alpha.md", "notes/beta.md", "notes/orphan.md"}, nil),
	)

	filesystemAnalysis := obsidian.ComputeGraphAnalysisFromSnapshot(filesystem, options)
	persistedAnalysis := obsidian.ComputeGraphAnalysisFromSnapshot(persisted, options)
	filesystemAnalysis.Timings = obsidian.GraphTimings{}
	persistedAnalysis.Timings = obsidian.GraphTimings{}
	filesystemAnalysis.EffectiveTimes = nil
	persistedAnalysis.EffectiveTimes = nil
	for i := range filesystemAnalysis.Communities {
		filesystemAnalysis.Communities[i].Recency = nil
	}
	for i := range persistedAnalysis.Communities {
		persistedAnalysis.Communities[i].Recency = nil
	}
	sort.Slice(filesystemAnalysis.Communities, func(i, j int) bool {
		return filesystemAnalysis.Communities[i].ID < filesystemAnalysis.Communities[j].ID
	})
	sort.Slice(persistedAnalysis.Communities, func(i, j int) bool {
		return persistedAnalysis.Communities[i].ID < persistedAnalysis.Communities[j].ID
	})
	require.Equal(t, filesystemAnalysis, persistedAnalysis)
}

func TestPersistedGraphPathPreservesAuthoredExtensionCase(t *testing.T) {
	require.Equal(t, "notes/Decision.MD", persistedGraphPath("notes/Decision.MD"))
	require.Equal(t, "", persistedGraphPath("../outside.md"))
}

func TestIndexerLoadPersistedGraphSnapshotRejectsStaleProjection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "source.md"), []byte("# Source\n"), 0o644))
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	_, err = indexer.EnsureIndexed(context.Background(), vaultDef, note, store)
	require.NoError(t, err)

	snapshot, err := indexer.LoadPersistedGraphSnapshot(context.Background(), vaultDef, note, store)
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	rows, err := store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	rows[0].Projection.Status = semdb.NoteProjectionStatusStale
	state, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{
		State: state,
		Notes: []semdb.NoteMetadataRow{rows[0]},
	}))

	snapshot, err = indexer.LoadPersistedGraphSnapshot(context.Background(), vaultDef, note, store)
	require.NoError(t, err)
	require.Nil(t, snapshot)
	rows, err = store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1, "stale projection retains raw source identity")
}

type graphSnapshotShape struct {
	Paths []string
	Tags  map[string][]string
	Sizes map[string]int64
	Edges []obsidian.GraphSnapshotEdge
}

func graphShape(snapshot *obsidian.GraphSnapshot) graphSnapshotShape {
	shape := graphSnapshotShape{Tags: make(map[string][]string), Sizes: make(map[string]int64), Edges: snapshot.Edges}
	for path, node := range snapshot.Nodes {
		shape.Paths = append(shape.Paths, path)
		shape.Tags[path] = node.Tags
		shape.Sizes[path] = node.Size
	}
	sort.Strings(shape.Paths)
	return shape
}
