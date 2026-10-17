package unifiedsearch

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNormalizeQueryInputsDedupesAndKeepsMode(t *testing.T) {
	got := NormalizeQueryInputs("foo bar", []string{" extra semantic query ", "", "foo bar"}, "overview")
	require.Equal(t, []QueryInput{
		{Text: "foo bar", Mode: "overview"},
		{Text: "extra semantic query", Mode: "overview"},
	}, got)
	require.Equal(t, "foo bar\n\nextra semantic query", JoinQueryInputs(got))
}

func TestNormalizeExplicitQueryInputsUsesEffectiveModeBeforeDedupe(t *testing.T) {
	got := NormalizeExplicitQueryInputs([]QueryInput{
		{Text: " policy ", Mode: ""},
		{Text: "policy", Mode: string(search.IntentSearch)},
		{Text: "other", Mode: " callers "},
	}, string(search.IntentSearch))
	require.Equal(t, []QueryInput{
		{Text: "policy", Mode: string(search.IntentSearch)},
		{Text: "other", Mode: "callers"},
	}, got)
}

func TestResolveSeedHandlesWithPathKinds_PreservesResolvedKindAndExactPath(t *testing.T) {
	handles, err := ResolveSeedHandlesWithPathKinds(context.Background(), []string{
		"note:notes/Decision.MD",
		"file:pkg/Decision.mD",
	}, nil, nil, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{
		knowledge.NoteHandle("notes/Decision.MD"),
		knowledge.FileHandle("pkg/Decision.mD"),
	}, handles)
}

func TestResolveSeedHandles_CanonicalizesDurableCodeFilesAndDirectories(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := semdb.Open(filepath.Join(vaultPath, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "pkg/search/seed.go",
		Lang: codeanchor.LangGo,
		Hash: "seed",
	}))

	handles, err := ResolveSeedHandles(ctx, []string{
		"pkg/search/seed.go",
		"pkg/search",
		filepath.Join(vaultPath, "pkg/search"),
	}, vaultPath, store, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{
		knowledge.FileHandle("pkg/search/seed.go"),
		knowledge.FileHandle("pkg/search"),
	}, handles)
}

func TestResolveSeedHandles_ExpandsCurrentNoteOnlyDirectorySeeds(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := semdb.Open(filepath.Join(vaultPath, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{
				Path:        "notes/first.html",
				ContentHash: "first",
				IndexedAt:   1,
				Projection: semdb.NoteProjectionState{
					ProviderVersion:   "html-v1",
					ProjectionVersion: "root-v1",
					SourceContentHash: "first",
					Status:            semdb.NoteProjectionStatusCurrent,
				},
			},
			{
				Path:        "notes/nested/second.md",
				ContentHash: "second",
				IndexedAt:   1,
				Projection: semdb.NoteProjectionState{
					ProviderVersion:   "markdown-v1",
					ProjectionVersion: "root-v1",
					SourceContentHash: "second",
					Status:            semdb.NoteProjectionStatusCurrent,
				},
			},
			{
				Path:      "notes/stale.html",
				IndexedAt: 1,
				Projection: semdb.NoteProjectionState{
					Status: semdb.NoteProjectionStatusStale,
				},
			},
		},
	}))

	handles, err := ResolveSeedHandles(ctx, []string{
		"notes",
		filepath.Join(vaultPath, "notes"),
	}, vaultPath, store, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{
		knowledge.NoteHandle("notes/first.html"),
		knowledge.NoteHandle("notes/nested/second.md"),
	}, handles)
}

func TestResolveSeedHandles_CapsDirectoryNoteSamplesDeterministically(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := semdb.Open(filepath.Join(vaultPath, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const seedLimit = 3
	notes := make([]semdb.NoteMetadataRow, 0, seedLimit+3)
	for i := seedLimit + 2; i >= 0; i-- {
		path := fmt.Sprintf("notes/sample-%02d.html", i)
		notes = append(notes, semdb.NoteMetadataRow{
			Path:        path,
			ContentHash: path,
			IndexedAt:   1,
			Projection: semdb.NoteProjectionState{
				ProviderVersion:   "html-v1",
				ProjectionVersion: "root-v1",
				SourceContentHash: path,
				Status:            semdb.NoteProjectionStatusCurrent,
			},
		})
	}
	notes = append(notes, semdb.NoteMetadataRow{
		Path:      "notes/stale.html",
		IndexedAt: 1,
		Projection: semdb.NoteProjectionState{
			Status: semdb.NoteProjectionStatusStale,
		},
	})
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: notes,
	}))
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "notes/descriptor.html",
		Lang: codeanchor.LangGo,
		Hash: "descriptor",
	}))

	for _, test := range []struct {
		name  string
		raw   []string
		limit int
	}{
		{name: "relative path", raw: []string{"notes"}, limit: 1},
		{name: "absolute path", raw: []string{filepath.Join(vaultPath, "notes")}, limit: 2},
		{name: "duplicate paths", raw: []string{"notes", filepath.Join(vaultPath, "notes")}, limit: seedLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := []knowledge.Handle{knowledge.FileHandle("notes")}
			for i := 0; i < test.limit; i++ {
				want = append(want, knowledge.NoteHandle(fmt.Sprintf("notes/sample-%02d.html", i)))
			}
			handles, err := ResolveSeedHandles(ctx, test.raw, vaultPath, store, test.limit)
			require.NoError(t, err)
			require.Equal(t, want, handles)
		})
	}
}

func TestResolveSeedHandles_PreservesCodeDirectoryAndExpandsCurrentNotes(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := semdb.Open(filepath.Join(vaultPath, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "docs/code.go",
		Lang: codeanchor.LangGo,
		Hash: "code",
	}))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{{
			Path:        "docs/current.html",
			ContentHash: "current",
			IndexedAt:   1,
			Projection: semdb.NoteProjectionState{
				ProviderVersion:   "html-v1",
				ProjectionVersion: "root-v1",
				SourceContentHash: "current",
				Status:            semdb.NoteProjectionStatusCurrent,
			},
		}},
	}))

	handles, err := ResolveSeedHandles(ctx, []string{"docs"}, vaultPath, store, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{
		knowledge.FileHandle("docs"),
		knowledge.NoteHandle("docs/current.html"),
	}, handles)
}

func TestResolveSeedHandles_RejectsUnknownAndUsesDurableCodeOwnedHTML(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := semdb.Open(filepath.Join(vaultPath, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "docs/descriptor.html",
		Lang: codeanchor.LangGo,
		Hash: "descriptor",
	}))

	_, err = ResolveSeedHandles(ctx, []string{"pkg/missing.go"}, vaultPath, store, 10)
	require.ErrorContains(t, err, "requires configured or persisted path ownership")

	handles, err := ResolveSeedHandles(ctx, []string{"docs/descriptor.html"}, vaultPath, store, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{knowledge.FileHandle("docs/descriptor.html")}, handles)

	_, err = ResolveSeedHandles(ctx, []string{"docs/stale.html"}, vaultPath, store, 10)
	require.ErrorContains(t, err, "requires configured or persisted path ownership")
}

func TestHydrateSearchPathKinds_PrefersCurrentNotesAndOmitsStaleNotes(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "docs/code.html",
		Lang: codeanchor.LangGo,
		Hash: "code",
	}))
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "docs/current.html",
		Lang: codeanchor.LangGo,
		Hash: "current-code",
	}))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{
				Path:        "docs/current.html",
				ContentHash: "current-note",
				IndexedAt:   1,
				Projection: semdb.NoteProjectionState{
					ProviderVersion:   "html-v1",
					ProjectionVersion: "root-v1",
					SourceContentHash: "current-note",
					Status:            semdb.NoteProjectionStatusCurrent,
				},
			},
			{
				Path:      "docs/stale.html",
				IndexedAt: 1,
				Projection: semdb.NoteProjectionState{
					Status: semdb.NoteProjectionStatusStale,
				},
			},
		},
	}))

	for _, path := range []string{"docs/current.html", "docs/stale.html", "docs/orphan.html"} {
		require.NoError(t, store.ReplaceIntelDocSections(ctx, path, []codeanchor.IntelDocSection{{SectionID: path + "#root", Path: path, Title: path, Level: 1, Content: "retained section", Fingerprint: path}}, nil, nil))
	}

	corpus, err := search.LoadCorpusSnapshot(ctx, store, search.CorpusMetadata)
	kinds := corpus.PathKinds
	require.NoError(t, err)
	pathsOnly, err := search.LoadCorpusSnapshot(ctx, store, search.CorpusOwnership)
	require.NoError(t, err)
	require.Equal(t, kinds, pathsOnly.PathKinds, "code-only planning must retain identical current note ownership")
	require.Equal(t, []string{"docs/current.html", "docs/orphan.html", "docs/stale.html"}, pathsOnly.NotePaths)
	require.Equal(t, corpus.NotePaths, pathsOnly.NotePaths)
	_, orphanOwned := pathsOnly.PathKinds["docs/orphan.html"]
	require.False(t, orphanOwned)

	require.Empty(t, pathsOnly.Notes)
	require.Equal(t, search.PathKindCode, kinds["docs/code.html"])
	require.Equal(t, search.PathKindNote, kinds["docs/current.html"])
	require.Equal(t, search.PathKindNote, kinds["docs"])
	_, found := kinds["docs/stale.html"]
	require.False(t, found)

	handles, err := ResolveSeedHandles(ctx, []string{"docs/current.html"}, t.TempDir(), store, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{knowledge.NoteHandle("docs/current.html")}, handles)

	_, err = ResolveSeedHandles(ctx, []string{"docs/stale.html"}, t.TempDir(), store, 10)
	require.ErrorContains(t, err, "requires configured or persisted path ownership")
}

func TestAggregateMultiQueryLanesPreservesFacetAvailability(t *testing.T) {
	complete := aggregateMultiQueryLanes([]search.Response{
		{Query: search.QuerySpec{Text: "ownership", Intent: search.IntentSearch}, Lanes: []search.LaneStatus{{Lane: "note_vector", Status: search.LaneStateRan, ResultCount: 2}}},
		{Query: search.QuerySpec{Text: "outage", Intent: search.IntentSearch}, Lanes: []search.LaneStatus{{Lane: "note_vector", Status: search.LaneStateEmpty}}},
	})
	require.Len(t, complete, 2)
	require.Equal(t, AvailabilityComplete, searchapplication.AvailabilityFromLanes(complete))

	partial := aggregateMultiQueryLanes([]search.Response{
		{Query: search.QuerySpec{Text: "ownership", Intent: search.IntentSearch}, Lanes: []search.LaneStatus{{Lane: "note_vector", Status: search.LaneStateRan}}},
		{Query: search.QuerySpec{Text: "outage", Intent: search.IntentSearch}, Lanes: []search.LaneStatus{{Lane: "note_vector", Status: search.LaneStateTimedOut, Reason: "deadline"}}},
	})
	require.Len(t, partial, 2)
	require.Equal(t, AvailabilityPartial, searchapplication.AvailabilityFromLanes(partial))
	require.ElementsMatch(t, []string{"ownership\x00search", "outage\x00search"}, []string{partial[0].Facet, partial[1].Facet})
}
