package unifiedsearch

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
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

func TestMergeSeedTokens(t *testing.T) {
	got := MergeSeedTokens([]string{"a.md", "file1.go"}, []string{"file2.go", "file1.go", " "})
	require.Equal(t, []string{"a.md", "file1.go", "file2.go"}, got)
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

	kinds, err := hydrateSearchPathKinds(ctx, store, nil)
	require.NoError(t, err)
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

func TestNeedsPathKindHydration_OnlySkipsKnownSuppliedPaths(t *testing.T) {
	require.False(t, needsPathKindHydration([]string{"pkg/search/service.go"}, map[string]search.PathKind{
		"pkg/search/service.go": search.PathKindCode,
	}))
	require.True(t, needsPathKindHydration([]string{"pkg/search/other.go"}, map[string]search.PathKind{
		"pkg/search/service.go": search.PathKindCode,
	}))
	require.True(t, needsPathKindHydration([]string{"pkg/search/service.go"}, nil))
	require.False(t, needsPathKindHydration([]string{"notes/decision.md"}, nil))
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

func TestBuildAnswer_SelectsTaskEvidence(t *testing.T) {
	resp := BuildAnswer(search.IntentSubsystemOverview, "search subsystem overview", search.TargetStatusExplicitPath, nil, []search.RankedResult{
		{Candidate: search.Candidate{Type: "code", Path: "pkg/search/service.go", Symbol: "Search", Kind: "module", Granularity: "module"}, FinalScore: 0.9},
		{Candidate: search.Candidate{Type: "note", Path: "docs/hubs/Search (Hub).md", Title: "Search (Hub)"}, FinalScore: 0.8},
		{Candidate: search.Candidate{Type: "code", Path: "pkg/search/service_test.go", Symbol: "TestSearch"}, FinalScore: 0.7},
	})

	require.NotEmpty(t, resp.MustRead)
	require.True(t, resp.Coverage.Code)
	require.True(t, resp.Coverage.Docs)
	require.True(t, resp.Coverage.Tests)
	require.Equal(t, "high", resp.Confidence.Level)
}

func TestBuildAnswerPreservesNodeRef(t *testing.T) {
	resp := BuildAnswer(search.IntentSearch, "story context", search.TargetStatusNone, nil, []search.RankedResult{
		{
			Candidate: search.Candidate{
				Type:    "note",
				Path:    "docs/spec.md",
				Title:   "Spec",
				NodeRef: &ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote},
			},
			FinalScore: 0.9,
		},
	})

	require.NotEmpty(t, resp.MustRead)
	require.Equal(t, "docs/spec.md", resp.MustRead[0].NodeRef.NotePath)
}

func TestBuildAnswerPromotesPrimaryOntologyNodeResult(t *testing.T) {
	resp := BuildAnswer(search.IntentSearch, "how does this story connect?", search.TargetStatusNone, nil, []search.RankedResult{
		{
			Candidate: search.Candidate{
				Type:        "note",
				Path:        "docs/specs/product/parent-spec.md",
				Title:       "Parent Spec",
				Granularity: "node_body",
				NodeRef:     &ontology.NodeRef{NotePath: "docs/specs/product/parent-spec.md", TypeName: "TechnicalSpec", Kind: ontology.NodeKindNote},
			},
			FinalScore: 0.7,
		},
		{Candidate: search.Candidate{Type: "code", Path: "pkg/story/service.go", Symbol: "Service"}, FinalScore: 0.8},
		{Candidate: search.Candidate{Type: "code", Path: "pkg/story/entry.go", Kind: "module", Granularity: "module"}, FinalScore: 0.75},
	})

	require.NotEmpty(t, resp.MustRead)
	contextIdx := -1
	for i := range resp.MustRead {
		if resp.MustRead[i].Path == "docs/specs/product/parent-spec.md" {
			contextIdx = i
		}
	}
	require.NotEqual(t, -1, contextIdx)
	contextItem := resp.MustRead[contextIdx]
	require.Equal(t, "documentation", contextItem.Role)
	require.Equal(t, "documentation tied to the task", contextItem.Why)
}

func TestBuildAnswerCarriesSpecificityForPrimaryChunk(t *testing.T) {
	resp := BuildAnswer(search.IntentSearch, "how are notes embedded?", search.TargetStatusNone, nil, []search.RankedResult{
		{
			Candidate: search.Candidate{
				Type:        "code",
				Path:        "pkg/search/semantic/note_syncer.go",
				Symbol:      "PlanNoteEmbeddings",
				Granularity: "signature_body",
				Evidence: []search.Evidence{
					{Type: "query_specificity", RawScore: 0.8},
					{Type: "code_vector_similarity", RawScore: 0.7},
				},
			},
			FinalScore: 0.9,
		},
		{Candidate: search.Candidate{Type: "note", Path: "docs/hubs/Embeddings (Hub).md"}, FinalScore: 0.8},
	})

	require.NotEmpty(t, resp.MustRead)
	foundCode := false
	for _, item := range resp.MustRead {
		if item.Path == "pkg/search/semantic/note_syncer.go" {
			foundCode = true
			require.Greater(t, item.Specificity, 0.0)
		}
	}
	require.True(t, foundCode)
}
