package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fakeMetadataPathProvider struct {
	rows       map[string]semdb.NoteMetadataRow
	rowsCalled []string
}

func (f *fakeMetadataPathProvider) CurrentNoteMetadataRowsByPaths(_ context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	f.rowsCalled = append([]string(nil), paths...)
	if f.rows == nil {
		return map[string]semdb.NoteMetadataRow{}, nil
	}
	out := make(map[string]semdb.NoteMetadataRow, len(paths))
	for _, path := range paths {
		if row, ok := f.rows[path]; ok {
			out[path] = row
		}
	}
	return out, nil
}

func TestEnsureIndexed_PopulatesMetadataAndWikilinks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "project.md"), []byte(`---
type: Project
status: active
tags: [topic/project]
---

See [[decision]].
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "decision.md"), []byte(`---
type: Decision
---

#topic/project/decision
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	result, err := testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	projects, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "active", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/project.md"}, projects)

	tagged, err := store.CurrentNotePathsByTag(context.Background(), "topic/project")
	require.NoError(t, err)
	require.Equal(t, []string{"notes/decision.md", "notes/project.md"}, tagged)

	edges, err := store.GraphDocNoteEdges(context.Background())
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "notes/project.md", edges[0].SrcPath)
	require.Equal(t, "notes/decision.md", edges[0].DstPath)

	detailedEdges, err := store.GraphDocNoteLinkEdges(context.Background())
	require.NoError(t, err)
	require.Len(t, detailedEdges, 1)
	require.Equal(t, semdb.NoteLinkKind("wikilink", "basic"), detailedEdges[0].Kind)
}

func TestEnsureIndexed_PopulatesMarkdownAndTypedNoteLinkEdges(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "target.md"), []byte(`---
tags: [keep]
---
Target
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "source.md"), []byte(`---
tags: [no-prompt]
---
Wiki [[target|alias]] and md [target](target.md#heading) and ![](target.md)
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	delta, err := testIndexer(t).BuildPathDelta(context.Background(), vaultDef, &obsidian.Note{}, store, []string{"notes/source.md", "notes/target.md"}, nil)
	require.NoError(t, err)
	require.Len(t, delta.WikilinkEdges, 5)

	byKind := make(map[string]semdb.GraphDocEdgeRow, len(delta.WikilinkEdges))
	for _, edge := range delta.WikilinkEdges {
		byKind[edge.Kind] = edge
	}

	require.Equal(t, semdb.EdgeConfidenceExtracted, byKind[semdb.GraphDocEdgeKindWikilink].Confidence)
	require.InDelta(t, 1.0, byKind[semdb.GraphDocEdgeKindWikilink].ConfidenceScore, 0.0001)
	require.Equal(t, semdb.EdgeConfidenceExtracted, byKind[semdb.GraphDocEdgeKindMarkdownLink].Confidence)
	require.InDelta(t, 0.9, byKind[semdb.GraphDocEdgeKindMarkdownLink].ConfidenceScore, 0.0001)
	require.Equal(t, semdb.EdgeConfidenceExtracted, byKind[semdb.NoteLinkKind("wikilink", "alias")].Confidence)
	require.InDelta(t, 1.0, byKind[semdb.NoteLinkKind("wikilink", "alias")].ConfidenceScore, 0.0001)
	require.Equal(t, semdb.EdgeConfidenceExtracted, byKind[semdb.NoteLinkKind("mdlink", "heading")].Confidence)
	require.InDelta(t, 0.9, byKind[semdb.NoteLinkKind("mdlink", "heading")].ConfidenceScore, 0.0001)
	require.Equal(t, semdb.EdgeConfidenceExtracted, byKind[semdb.NoteLinkKind("mdlink", "embed")].Confidence)
	require.InDelta(t, 0.9, byKind[semdb.NoteLinkKind("mdlink", "embed")].ConfidenceScore, 0.0001)

	require.NoError(t, store.ApplyNoteMetadataDelta(context.Background(), *delta))
	detailedEdges, err := store.GraphDocNoteLinkEdges(context.Background())
	require.NoError(t, err)
	require.Len(t, detailedEdges, 3)
	kinds := []string{detailedEdges[0].Kind, detailedEdges[1].Kind, detailedEdges[2].Kind}
	require.Contains(t, kinds, semdb.NoteLinkKind("wikilink", "alias"))
	require.Contains(t, kinds, semdb.NoteLinkKind("mdlink", "heading"))
	require.Contains(t, kinds, semdb.NoteLinkKind("mdlink", "embed"))
}

func TestEnsureIndexed_DedupesNormalizedTags(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "example.md"), []byte(`---
tags: [Foo, foo]
---
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	tags, err := store.CurrentNoteTags(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, "foo", tags[0].TagNorm)
}

func TestEnsureIndexed_ReadFailureDoesNotReplaceExistingMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, fakeNoteReader{
		notes: map[string]string{"good.md": "---\nstatus: ok\n---\n"},
	}, store)
	require.NoError(t, err)

	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, fakeNoteReader{
		notes: map[string]string{"good.md": "---\nstatus: ok\n---\n"},
		errs:  map[string]error{"bad.md": errors.New("boom")},
		list:  []string{"bad.md", "good.md"},
	}, store)
	require.Error(t, err)

	paths, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "ok", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"good.md"}, paths)
}

func TestSyncPaths_UpdatesTouchedPathsWithoutFullRebuild(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	initial := fakeNoteReader{
		notes: map[string]string{
			"good.md": "---\nstatus: old\n---\n",
			"keep.md": "---\nstatus: keep\n---\n",
		},
	}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, initial, store)
	require.NoError(t, err)

	incremental := fakeNoteReader{
		notes: map[string]string{
			"good.md": "---\nstatus: new\n---\n",
		},
		list: []string{"good.md", "keep.md"},
		errs: map[string]error{"keep.md": errors.New("unexpected keep read")},
	}
	err = testIndexer(t).SyncPaths(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		incremental,
		store,
		[]string{"good.md"},
		nil,
	)
	require.NoError(t, err)

	updated, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "new", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"good.md"}, updated)

	kept, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "keep", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"keep.md"}, kept)

	state, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, state.NotesHash)

	result, err := testIndexer(t).EnsureIndexed(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		fakeNoteReader{
			notes: map[string]string{
				"good.md": "---\nstatus: new\n---\n",
				"keep.md": "---\nstatus: keep\n---\n",
			},
			list: []string{"good.md", "keep.md"},
		},
		store,
	)
	require.NoError(t, err)
	require.False(t, result.Dirty)
}

func TestFragmentTargets_FullAndIncrementalProjectionConverge(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fullStore, err := sqlitefixture.Open(filepath.Join(root, "full.sqlite"))
	require.NoError(t, err)
	defer func() { _ = fullStore.Close() }()
	incrementalStore, err := sqlitefixture.Open(filepath.Join(root, "incremental.sqlite"))
	require.NoError(t, err)
	defer func() { _ = incrementalStore.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"keep.md": "## Keep\n",
		"old.md":  "# Old\nParagraph ^OldBlock\n",
	}}
	final := fakeNoteReader{notes: map[string]string{
		"keep.md":    "## Keep\n",
		"renamed.md": "# New\n# New\nParagraph ^CaseID\n",
	}}

	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, final, fullStore)
	require.NoError(t, err)
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, initial, incrementalStore)
	require.NoError(t, err)
	err = testIndexer(t).SyncPaths(ctx, vaultDef, final, incrementalStore, []string{"renamed.md"}, []string{"old.md"})
	require.NoError(t, err)

	fullTargets, err := fullStore.CurrentNoteFragmentTargets(ctx, nil, "", "")
	require.NoError(t, err)
	incrementalTargets, err := incrementalStore.CurrentNoteFragmentTargets(ctx, nil, "", "")
	require.NoError(t, err)
	withoutIDs := func(rows []semdb.NoteFragmentTargetRow) []semdb.NoteFragmentTargetRow {
		for i := range rows {
			rows[i].NoteID = 0
		}
		return rows
	}
	require.Equal(t, withoutIDs(fullTargets), withoutIDs(incrementalTargets))
	require.Equal(t, []semdb.NoteFragmentTargetRow{
		{NotePath: "keep.md", Kind: semdb.NoteFragmentTargetHeading, Target: "Keep", TargetNorm: "keep", Ordinal: 1},
		{NotePath: "renamed.md", Kind: semdb.NoteFragmentTargetBlock, Target: "CaseID", TargetNorm: "CaseID", Ordinal: 1},
		{NotePath: "renamed.md", Kind: semdb.NoteFragmentTargetHeading, Target: "New", TargetNorm: "new", Ordinal: 1},
		{NotePath: "renamed.md", Kind: semdb.NoteFragmentTargetHeading, Target: "New", TargetNorm: "new", Ordinal: 2},
	}, withoutIDs(fullTargets))
}

func TestSyncPaths_URIExtensionlessAmbiguityMatchesFreshSnapshot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	initial := fakeNoteReader{notes: map[string]string{
		"a.html":      `<a href="report">ambiguous</a>`,
		"c.html":      `<a href="REPORT.HTML">platform casing</a>`,
		"b.html":      `<a href="report.html">explicit</a>`,
		"report.md":   `<h1>Markdown report</h1>`,
		"report.html": `<h1>HTML report</h1>`,
	}}
	final := fakeNoteReader{notes: map[string]string{
		"a.html":      `<p>changed</p><a href="report">ambiguous</a>`,
		"c.html":      `<p>changed</p><a href="REPORT.HTML">platform casing</a>`,
		"b.html":      `<p>changed</p><a href="report.html">explicit</a>`,
		"report.md":   `<h1>Markdown report</h1>`,
		"report.html": `<h1>HTML report</h1>`,
	}}

	incrementalStore, err := sqlitefixture.Open(filepath.Join(root, "incremental.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, incrementalStore.Close()) })
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, initial, incrementalStore)
	require.NoError(t, err)
	require.NoError(t, testIndexer(t).SyncPaths(ctx, vaultDef, final, incrementalStore, []string{"a.html", "b.html", "c.html"}, nil))

	freshStore, err := sqlitefixture.Open(filepath.Join(root, "fresh.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, freshStore.Close()) })
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, final, freshStore)
	require.NoError(t, err)

	got, err := incrementalStore.GraphDocNoteLinkEdges(ctx)
	require.NoError(t, err)
	want, err := freshStore.GraphDocNoteLinkEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)
	expected := []semdb.GraphDocEdgeRow{{
		SrcPath: "b.html", DstPath: "report.html", Kind: semdb.NoteLinkKind("uri", "anchor"),
		Confidence: semdb.EdgeConfidenceExtracted, ConfidenceScore: 1,
	}}
	if obsidian.IsCaseInsensitiveFS() {
		expected = append(expected, semdb.GraphDocEdgeRow{
			SrcPath: "c.html", DstPath: "report.html", Kind: semdb.NoteLinkKind("uri", "anchor"),
			Confidence: semdb.EdgeConfidenceExtracted, ConfidenceScore: 1,
		})
	}
	require.ElementsMatch(t, expected, got)
}

func TestSyncPaths_UsesIndexedPathsInsteadOfListingVault(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	initial := fakeNoteReader{
		notes: map[string]string{
			"source.md": "---\nstatus: old\n---\nSee [[target]].\n",
			"target.md": "---\nstatus: keep\n---\n",
		},
	}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, initial, store)
	require.NoError(t, err)

	incremental := fakeNoteReader{
		notes: map[string]string{
			"source.md": "---\nstatus: new\n---\nSee [[target]].\n",
		},
		errs: map[string]error{
			"target.md": errors.New("should not list or read unchanged target"),
		},
	}
	err = testIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, incremental, store, []string{"source.md"}, nil)
	require.NoError(t, err)

	updated, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "new", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"source.md"}, updated)

	edges, err := store.GraphDocNoteEdges(context.Background())
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "source.md", edges[0].SrcPath)
	require.Equal(t, "target.md", edges[0].DstPath)
}

func TestSyncPaths_NewTargetRebuildsPreviouslyUnresolvedInboundLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"source.md": "See [[target]].\n",
	}}
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, initial, store)
	require.NoError(t, err)
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Empty(t, edges)

	final := fakeNoteReader{notes: map[string]string{
		"source.md": "See [[target]].\n",
		"target.md": "# Target\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(ctx, vaultDef, final, store, []string{"target.md"}, nil))

	edges, err = store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "source.md", edges[0].SrcPath)
	require.Equal(t, "target.md", edges[0].DstPath)
}

func TestSyncPaths_RenamedTargetRebuildsUnchangedInboundLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"source.md": "See [[target]].\n",
		"target.md": "# Target\n",
	}}
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, initial, store)
	require.NoError(t, err)

	final := fakeNoteReader{notes: map[string]string{
		"source.md":        "See [[target]].\n",
		"folder/target.md": "# Target\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(ctx, vaultDef, final, store, []string{"folder/target.md"}, []string{"target.md"}))

	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "source.md", edges[0].SrcPath)
	require.Equal(t, "folder/target.md", edges[0].DstPath)
}

func TestSyncPaths_RemovedAliasRebuildsUnchangedInboundLinks(t *testing.T) {
	for _, tc := range []struct{ name, target string }{
		{"removed key", "# Target\n"},
		{"empty list", "---\naliases: []\n---\n# Target\n"},
		{"blank list", "---\naliases: ['', '   ']\n---\n# Target\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			defer func() { _ = store.Close() }()

			vaultDef := obsidian.VaultDefinition{Path: root}
			initial := fakeNoteReader{notes: map[string]string{
				"source.md": "See [[SPECIAL]] and [[KEEP]].\n",
				"target.md": "---\naliases: [SPECIAL]\n---\n# Target\n",
				"keep.md":   "---\naliases: [KEEP]\n---\n# Keep\n",
			}}
			_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, initial, store)
			require.NoError(t, err)
			edges, err := store.GraphDocNoteEdges(ctx)
			require.NoError(t, err)
			require.Len(t, edges, 2)

			final := fakeNoteReader{notes: map[string]string{
				"source.md": initial.notes["source.md"],
				"target.md": tc.target,
				"keep.md":   initial.notes["keep.md"],
			}}
			require.NoError(t, testIndexer(t).SyncPaths(ctx, vaultDef, final, store, []string{"target.md"}, nil))
			edges, err = store.GraphDocNoteEdges(ctx)
			require.NoError(t, err)
			require.Len(t, edges, 1)
			require.Equal(t, "keep.md", edges[0].DstPath)
		})
	}
}

func TestSyncPaths_DeletedBasenameClaimantRebuildsUnchangedInboundLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"source.md":   "See [[a/target]].\n",
		"a/target.md": "# A\n",
		"b/target.md": "# B\n",
	}}
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, initial, store)
	require.NoError(t, err)
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "a/target.md", edges[0].DstPath)

	final := fakeNoteReader{notes: map[string]string{
		"source.md":   "See [[a/target]].\n",
		"b/target.md": "# B\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(ctx, vaultDef, final, store, nil, []string{"a/target.md"}))

	edges, err = store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "b/target.md", edges[0].DstPath)
}

func TestBuildPathDeltaBootstrapTombstonesRowsWhileStateIsUnready(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: false},
		Notes: []semdb.NoteMetadataRow{
			{Path: "current.md", ContentHash: contentHash("# Current\n"), IndexedAt: 1},
			{Path: "stale.md", ContentHash: contentHash("# Stale\n"), IndexedAt: 1},
		},
	}))

	vaultDef := obsidian.VaultDefinition{Path: root}
	reader := fakeNoteReader{notes: map[string]string{"current.md": "# Current\n"}}
	delta, err := testIndexer(t).BuildPathDelta(ctx, vaultDef, reader, store, []string{"current.md"}, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))

	paths, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"current.md"}, paths)
}

func TestSyncPaths_AliasCollisionRefreshesUntouchedInboundLinks(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"source.md": "See [[SHARED]].\n",
		"a.md":      "---\naliases: [SHARED]\n---\n",
		"b.md":      "# B\n",
	}}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vaultDef, initial, store)
	require.NoError(t, err)

	edges := requireGraphEdgesMatchFreshSnapshot(t, store, vaultDef, initial)
	require.Equal(t, []semdb.GraphDocEdge{{SrcPath: "source.md", DstPath: "a.md", Kind: semdb.GraphDocEdgeKindWikilink, Weight: 1}}, edges)

	duplicate := fakeNoteReader{notes: map[string]string{
		"source.md": "See [[SHARED]].\n",
		"a.md":      "---\naliases: [SHARED]\n---\n",
		"b.md":      "---\naliases: [SHARED]\n---\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(context.Background(), vaultDef, duplicate, store, []string{"b.md"}, nil))
	require.Empty(t, requireGraphEdgesMatchFreshSnapshot(t, store, vaultDef, duplicate), "ambiguous alias must not retain the old first-wins edge")

	restored := fakeNoteReader{notes: map[string]string{
		"source.md": "See [[SHARED]].\n",
		"a.md":      "---\naliases: [SHARED]\n---\n",
		"b.md":      "# B restored\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(context.Background(), vaultDef, restored, store, []string{"b.md"}, nil))
	edges = requireGraphEdgesMatchFreshSnapshot(t, store, vaultDef, restored)
	require.Equal(t, []semdb.GraphDocEdge{{SrcPath: "source.md", DstPath: "a.md", Kind: semdb.GraphDocEdgeKindWikilink, Weight: 1}}, edges)
}

func TestSyncPaths_BasenameCollisionRefreshesUntouchedInboundLinks(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vaultDef := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"source.md":    "See [[item]].\n",
		"deep/item.md": "# Deep item\n",
	}}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vaultDef, initial, store)
	require.NoError(t, err)
	edges := requireGraphEdgesMatchFreshSnapshot(t, store, vaultDef, initial)
	require.Equal(t, []semdb.GraphDocEdge{{SrcPath: "source.md", DstPath: "deep/item.md", Kind: semdb.GraphDocEdgeKindWikilink, Weight: 1}}, edges)

	duplicate := fakeNoteReader{notes: map[string]string{
		"source.md":       "See [[item]].\n",
		"deep/item.md":    "# Deep item\n",
		"shallow/item.md": "# Shallow item\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(context.Background(), vaultDef, duplicate, store, []string{"shallow/item.md"}, nil))
	require.Empty(t, requireGraphEdgesMatchFreshSnapshot(t, store, vaultDef, duplicate), "ambiguous basename must not retain the old first-wins edge")

	restored := fakeNoteReader{notes: map[string]string{
		"source.md":    "See [[item]].\n",
		"deep/item.md": "# Deep item\n",
	}}
	require.NoError(t, testIndexer(t).SyncPaths(context.Background(), vaultDef, restored, store, nil, []string{"shallow/item.md"}))
	edges = requireGraphEdgesMatchFreshSnapshot(t, store, vaultDef, restored)
	require.Equal(t, []semdb.GraphDocEdge{{SrcPath: "source.md", DstPath: "deep/item.md", Kind: semdb.GraphDocEdgeKindWikilink, Weight: 1}}, edges)
}

func requireGraphEdgesMatchFreshSnapshot(t *testing.T, store *semdb.Store, vaultDef obsidian.VaultDefinition, reader fakeNoteReader) []semdb.GraphDocEdge {
	t.Helper()
	got, err := store.GraphDocNoteEdges(context.Background())
	require.NoError(t, err)

	freshStore, err := sqlitefixture.Open(filepath.Join(t.TempDir(), ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = freshStore.Close() }()
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vaultDef, reader, freshStore)
	require.NoError(t, err)
	want, err := freshStore.GraphDocNoteEdges(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, got)
	return got
}

func TestMetadataStateReady_DoesNotDependOnCurrentFileContents(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	initial := fakeNoteReader{
		notes: map[string]string{
			"note.md": "---\nstatus: old\n---\n",
		},
	}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, initial, store)
	require.NoError(t, err)

	ready, err := MetadataStateReady(context.Background(), store)
	require.NoError(t, err)
	require.True(t, ready)

	current, err := testIndexer(t).MetadataStateCurrent(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		fakeNoteReader{
			notes: map[string]string{
				"note.md": "---\nstatus: new\n---\n",
			},
		},
		store,
	)
	require.NoError(t, err)
	require.False(t, current)

	ready, err = MetadataStateReady(context.Background(), store)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestEnsureIndexed_ProjectsCacheBytesInsteadOfCachedParsedFacts(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	reader := fakeCachedMetadataReader{
		entries: []cache.Entry{
			{
				Path:        "cached.md",
				ModTime:     time.Unix(1700, 0),
				Size:        int64(len("---\nstatus: source\n---\n# Source\n#source-tag\n")),
				Content:     "---\nstatus: source\n---\n# Source\n#source-tag\n",
				Frontmatter: map[string]interface{}{"status": "cached"},
				Tags:        []string{"cached-tag"},
			},
		},
	}

	_, err = testIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, reader, store)
	require.NoError(t, err)

	paths, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "source", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"cached.md"}, paths)
	cachedPaths, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "cached", 0)
	require.NoError(t, err)
	require.Empty(t, cachedPaths)
}

func TestEnsureIndexed_RebuildsWhenFreshnessStateIsMaterializedIncompletely(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# A"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "playground"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "playground", "pizza-party-2026.md"), []byte("# Pizza"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"a.md"})
	require.NoError(t, err)
	require.Contains(t, rows, "a.md")

	state.LoadedAt++
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: state,
		Notes: []semdb.NoteMetadataRow{
			rows["a.md"],
		},
	}))

	current, err := testIndexer(t).MetadataStateCurrent(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.False(t, current)

	result, err := testIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	paths, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Contains(t, paths, "docs/playground/pizza-party-2026.md")
}

func TestEnsureIndexed_RebuildsWhenMetadataIndexerVersionChanges(t *testing.T) {
	root := t.TempDir()
	content := `---
name: Real Effort Title
---

# Effort
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "effort.md"), []byte(content), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	notesHash, pathsList, err := computeNotesHash(ctx, vaultDef, &obsidian.Note{})
	require.NoError(t, err)
	require.Equal(t, []string{"effort.md"}, pathsList)
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{
			NotesHash:    legacyMetadataStateHash(vaultDef, notesHash),
			RawNotesHash: notesHash,
			LoadedAt:     1,
			Ready:        true,
		},
		Notes: []semdb.NoteMetadataRow{{
			Path:        "effort.md",
			Title:       "Effort",
			ContentHash: contentHash(content),
			Mtime:       1,
			Size:        int64(len(content)),
			IndexedAt:   1,
		}},
	}))

	result, err := testIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"effort.md"})
	require.NoError(t, err)
	require.Equal(t, "Real Effort Title", rows["effort.md"].Title)
}

func TestIncrementalNotesHash_UsesOnlyTouchedRows(t *testing.T) {
	provider := &fakeMetadataPathProvider{
		rows: map[string]semdb.NoteMetadataRow{
			"changed.md": {Path: "changed.md", Mtime: 1, ContentHash: contentHash("old")},
			"deleted.md": {Path: "deleted.md", Mtime: 2, ContentHash: contentHash("gone")},
		},
	}
	currentNotesHash := notesHashFromRows([]semdb.NoteMetadataRow{
		{Path: "keep.md", Mtime: 3, ContentHash: contentHash("keep")},
		{Path: "changed.md", Mtime: 1, ContentHash: contentHash("old")},
		{Path: "deleted.md", Mtime: 2, ContentHash: contentHash("gone")},
	})
	updated, err := incrementalNotesHash(
		context.Background(),
		provider,
		currentNotesHash,
		[]noteEntry{{Path: "changed.md", Content: "new", Mtime: 4}},
		[]string{"deleted.md"},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"changed.md", "deleted.md"}, provider.rowsCalled)
	expectedNotesHash := notesHashFromRows([]semdb.NoteMetadataRow{
		{Path: "keep.md", Mtime: 3, ContentHash: contentHash("keep")},
		{Path: "changed.md", Mtime: 4, ContentHash: contentHash("new")},
	})
	require.Equal(t, expectedNotesHash, updated)
}

type fakeNoteReader struct {
	notes map[string]string
	list  []string
	errs  map[string]error
}

func (f fakeNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	if err := f.errs[path]; err != nil {
		return "", err
	}
	if body, ok := f.notes[path]; ok {
		return body, nil
	}
	return "", os.ErrNotExist
}

func (f fakeNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	if len(f.list) > 0 {
		return append([]string(nil), f.list...), nil
	}
	out := make([]string, 0, len(f.notes))
	for path := range f.notes {
		out = append(out, path)
	}
	return out, nil
}

func (f fakeNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Unix(1, 0), nil
}

func (f fakeNoteReader) Title(path string) (string, bool) { return path, true }

type fakeCachedMetadataReader struct {
	entries []cache.Entry
}

func (f fakeCachedMetadataReader) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	return append([]cache.Entry(nil), f.entries...), nil
}

func (f fakeCachedMetadataReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", errors.New("should not read note contents")
}

func (f fakeCachedMetadataReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, errors.New("should not list notes")
}

func (f fakeCachedMetadataReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("should not stat notes")
}

func (f fakeCachedMetadataReader) Title(path string) (string, bool) { return path, true }

type blockingNoteReader struct {
	notes   map[string]string
	release <-chan struct{}
	started chan struct{}

	mu        sync.Mutex
	active    int
	maxActive int
}

func (b *blockingNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	b.mu.Lock()
	b.active++
	if b.active > b.maxActive {
		b.maxActive = b.active
	}
	b.mu.Unlock()
	if b.started != nil {
		b.started <- struct{}{}
	}
	<-b.release
	b.mu.Lock()
	b.active--
	b.mu.Unlock()
	return b.notes[path], nil
}

func (b *blockingNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, errors.New("should not list notes")
}

func (b *blockingNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Unix(1, 0), nil
}

func (b *blockingNoteReader) Title(path string) (string, bool) { return path, true }

func (b *blockingNoteReader) maxObservedActive() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxActive
}

func TestTitleFor_PriorityChain(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		content string
		fm      map[string]any
		want    string
	}{
		{
			name:    "frontmatter title wins",
			path:    "specs/100-demo/spec.md",
			content: "# H1 Ignored When FM Title Set\n\nbody",
			fm:      map[string]any{"title": "Frontmatter Wins"},
			want:    "Frontmatter Wins",
		},
		{
			name:    "falls back to h1 when no frontmatter title",
			path:    "specs/100-demo/spec.md",
			content: "---\ntype: spec\n---\n\n# Feature Specification: Vault Health Analysis\n",
			fm:      map[string]any{"type": "spec"},
			want:    "Feature Specification: Vault Health Analysis",
		},
		{
			name: "falls back to filename when note has multiple h1 headings",
			path: "docs/decisions/Use persistent memory.md",
			content: `# Decision

First decision.

# Decision

Second decision.
`,
			fm:   nil,
			want: "Use persistent memory",
		},
		{
			name:    "falls back to filename when no h1",
			path:    "specs/100-demo/spec.md",
			content: "no heading here\n",
			fm:      nil,
			want:    "spec",
		},
		{
			name:    "ignores blank frontmatter title",
			path:    "notes/example.md",
			content: "# Real Title\n",
			fm:      map[string]any{"title": "   "},
			want:    "Real Title",
		},
		{
			name:    "uses frontmatter name before generic heading",
			path:    "notes/example.md",
			content: "# Effort\n",
			fm:      map[string]any{"name": "Real Effort Title"},
			want:    "Real Effort Title",
		},
		{
			name:    "frontmatter title wins over name",
			path:    "notes/example.md",
			content: "# Effort\n",
			fm:      map[string]any{"title": "Explicit Title", "name": "Real Effort Title"},
			want:    "Explicit Title",
		},
		{
			name:    "preserves trailing hash in language name",
			path:    "notes/csharp.md",
			content: "# C#\n",
			fm:      nil,
			want:    "C#",
		},
		{
			name:    "removes atx closing hash sequence",
			path:    "notes/feature.md",
			content: "# Feature Spec ###\n",
			fm:      nil,
			want:    "Feature Spec",
		},
		{
			name:    "preserves trailing hash without separating whitespace",
			path:    "notes/fsharp.md",
			content: "# F#\n",
			fm:      nil,
			want:    "F#",
		},
		{name: "setext heading", path: "notes/setext.md", content: "Plain setext title\n==========\n", want: "Plain setext title"},
		{name: "h2 before h1", path: "notes/heading.md", content: "## subsection\n\n# Real title\n", want: "Real title"},
		{name: "fenced heading", path: "notes/fenced.md", content: "```\n# Not a title\n```\n# Actual title\n", want: "Actual title"},
		{name: "wikilink heading", path: "notes/link.md", content: "# [[Cache Hub|Cache Hub Docs]]\n", want: "Cache Hub Docs"},
		{name: "unterminated frontmatter", path: "notes/unterminated.md", content: "---\ntype: note\n# Buried\n", want: "Buried"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, titleFor(tc.path, tc.content, tc.fm))
		})
	}
}

func legacyMetadataStateHash(vaultDef obsidian.VaultDefinition, notesHash string) string {
	sum := sha256.New()
	_, _ = sum.Write([]byte(notesHash))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(vaultDef.Links))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(vaultDef.BasePath()))
	_, _ = sum.Write([]byte{0})
	includes := append([]string(nil), vaultDef.Includes...)
	excludes := append([]string(nil), vaultDef.Excludes...)
	sort.Strings(includes)
	sort.Strings(excludes)
	for _, pattern := range includes {
		_, _ = sum.Write([]byte(pattern))
		_, _ = sum.Write([]byte{0})
	}
	_, _ = sum.Write([]byte{1})
	for _, pattern := range excludes {
		_, _ = sum.Write([]byte(pattern))
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func TestBuildSnapshot_EmitsDisplayAliasWikilinkEdges(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "specs", "product"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "specs", "product", "001-indexed-search.md"), []byte(`---
aliases: [SPEC-001]
---

# Indexed search spec
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "project.md"), []byte(`See [[001-indexed-search|SPEC-001]] for details.
`), 0o644))

	vaultDef := obsidian.VaultDefinition{Path: root}
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	delta, err := testIndexer(t).BuildPathDelta(context.Background(), vaultDef, &obsidian.Note{}, store, []string{"docs/specs/product/001-indexed-search.md", "notes/project.md"}, nil)
	require.NoError(t, err)

	require.Len(t, delta.WikilinkEdges, 2)
	byKind := make(map[string]semdb.GraphDocEdgeRow, len(delta.WikilinkEdges))
	for _, edge := range delta.WikilinkEdges {
		require.Equal(t, "notes/project.md", edge.SrcPath, "display-alias edge must originate from the referencing note")
		require.Equal(t, "docs/specs/product/001-indexed-search.md", edge.DstPath, "link target must resolve by Obsidian note title/path, not alias")
		byKind[edge.Kind] = edge
	}
	require.Contains(t, byKind, semdb.GraphDocEdgeKindWikilink)
	require.Contains(t, byKind, semdb.NoteLinkKind("wikilink", "alias"))
}

func TestBuildSnapshot_StoresLinkLabelAndLineOnCoarseEdge(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Project Larkspur.md"), []byte("# Project Larkspur\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "standup.md"), []byte(`---
project: "[[Project Larkspur]]"
---

Intro paragraph without links.
- Kickoff for the [[Project Larkspur|catalog migration]] with branch staff
- Repeat: [[Project Larkspur|catalog migration]] with branch staff
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	delta, err := testIndexer(t).BuildPathDelta(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"Project Larkspur.md", "standup.md"}, nil)
	require.NoError(t, err)

	var coarse semdb.GraphDocEdgeRow
	for _, edge := range delta.WikilinkEdges {
		if edge.Kind == semdb.GraphDocEdgeKindWikilink {
			coarse = edge
		} else {
			require.Empty(t, edge.LinkText, "only the coarse edge carries link text")
		}
	}
	require.Equal(t, []semdb.LinkTextEntry{
		{Label: "", Line: `project: "[[Project Larkspur]]"`},
		{Label: "catalog migration", Line: "- Kickoff for the [[Project Larkspur|catalog migration]] with branch staff"},
		{Label: "catalog migration", Line: "- Repeat: [[Project Larkspur|catalog migration]] with branch staff"},
	}, semdb.ParseLinkText(coarse.LinkText))
}

func TestBuildProjectionAliasCache_PreservesCandidateUnionAndEveryAliasClaimant(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{"notes/a.md", "notes/b.md"})
	cache = buildProjectionAliasCache(cache, map[string][]string{
		"notes/a.md": {"SHARED", "A-ONLY"},
		"notes/c.md": {"SHARED", "C-ONLY"},
	})
	require.Equal(t, []string{"notes/a.md", "notes/c.md"}, cache.Aliases["SHARED"])
	require.Equal(t, []string{"notes/a.md"}, cache.Aliases["A-ONLY"])
	require.Equal(t, []string{"notes/c.md"}, cache.Aliases["C-ONLY"])
	require.Contains(t, cache.NotePaths, "notes/b.md", "existing candidates must remain available")
	require.Contains(t, cache.NotePaths, "notes/c.md", "alias owners must become candidates")
}

func TestNoteStateDigestIgnoresMtime(t *testing.T) {
	entries := []noteEntry{{Path: "note.md", Content: "# Note\n", Mtime: 1}}
	touched := []noteEntry{{Path: "note.md", Content: "# Note\n", Mtime: 900}}
	require.Equal(t, notesHashFromNoteEntries(entries), notesHashFromNoteEntries(touched),
		"filesystem mtime is freshness evidence, not content identity")

	rows := []semdb.NoteMetadataRow{{Path: "note.md", Mtime: 1, ContentHash: contentHash("# Note\n")}}
	touchedRows := []semdb.NoteMetadataRow{{Path: "note.md", Mtime: 900, ContentHash: contentHash("# Note\n")}}
	require.Equal(t, notesHashFromRows(rows), notesHashFromRows(touchedRows))
	require.NotEqual(t, notesHashFromRows(rows), notesHashFromRows([]semdb.NoteMetadataRow{
		{Path: "note.md", Mtime: 1, ContentHash: contentHash("# Other\n")},
	}))
}
