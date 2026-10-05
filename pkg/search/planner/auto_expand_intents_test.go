package planner

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestPlanner_AutoExpandForOverviewCodeForDocsAndDocsForCode(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})

	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	searcher := &semantic.Searcher{
		NoteProvider: prov,
		IntelStore:   intelStore,
	}

	p := Planner{
		Deps: Deps{
			Semantic:   searcher,
			IntelStore: intelStore,
		},
		Options: Options{EnableVector: true, EnableRefs: true},
	}

	for _, intent := range []search.Intent{search.IntentOverview, search.IntentCodeForDocs, search.IntentDocsForCode} {
		plan, err := p.Plan(ctx, search.QuerySpec{
			Text:   "query",
			Intent: intent,
			Limits: search.Limits{Total: 10},
		})
		require.NoError(t, err)

		var names []string
		for _, r := range plan.Retrievers {
			names = append(names, r.Name())
		}
		require.Contains(t, names, "auto_expand")
	}
}

func TestPlanner_SubsystemOverviewRepairsMissingSeedsAndSkipsAutoExpand(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})

	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	searcher := &semantic.Searcher{
		NoteProvider: prov,
		IntelStore:   intelStore,
	}

	p := Planner{
		Deps: Deps{
			Semantic:   searcher,
			IntelStore: intelStore,
			VaultPath:  t.TempDir(),
		},
		Options: Options{EnableVector: true, EnableRefs: true},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Text:   "search subsystem overview for pkg/app/mcp/semantic_query_unified.go",
		Intent: search.IntentSubsystemOverview,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)

	var names []string
	for _, r := range plan.Retrievers {
		names = append(names, r.Name())
	}
	require.NotContains(t, names, "auto_expand")
	require.Contains(t, names, "local_docs")

	plan, err = p.Plan(ctx, search.QuerySpec{
		Text:              "query",
		Intent:            search.IntentSubsystemOverview,
		ExplicitSeedPaths: []string{"pkg/app/mcp/semantic_query_unified.go"},
		HasExplicitSeeds:  true,
		Seeds:             []knowledge.Handle{knowledge.FileHandle("pkg/app/mcp/semantic_query_unified.go")},
		Limits:            search.Limits{Total: 10},
	})
	require.NoError(t, err)

	names = nil
	for _, r := range plan.Retrievers {
		names = append(names, r.Name())
	}
	require.NotContains(t, names, "auto_expand")
	require.Contains(t, names, "local_docs")
}

func TestPlanner_AutoExpandSharesNodeScopeAcrossOntologyAndDiffusion(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})

	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })
	require.NoError(t, intelStore.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{
			SchemaHash:             "schema",
			NotesHash:              "notes",
			MaterializationVersion: ontology.OntologyMaterializationVersion,
			LoadedAt:               1,
			Ready:                  true,
		},
	}))
	require.NoError(t, intelStore.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{
			NotesHash: "notes",
			LoadedAt:  1,
			Ready:     true,
		},
	}))
	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("docs/n%d.md", i)
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, path, []codeanchor.IntelDocSection{{
			SectionID:   fmt.Sprintf("section-%d", i),
			Path:        path,
			Title:       "Doc",
			Level:       1,
			Content:     "doc",
			Fingerprint: fmt.Sprintf("doc-%d", i),
		}}, nil, nil))
	}
	for i := 0; i < 8; i++ {
		require.NoError(t, intelStore.UpsertFileMeta(ctx, codeanchor.FileMeta{
			Path:        fmt.Sprintf("pkg/f%d.go", i),
			Lang:        codeanchor.LangGo,
			Hash:        fmt.Sprintf("file-%d", i),
			ParseStatus: codeanchor.ParseOK,
		}))
	}

	searcher := &semantic.Searcher{
		NoteProvider: prov,
		IntelStore:   intelStore,
	}
	p := Planner{
		Deps: Deps{
			Semantic:   searcher,
			IntelStore: intelStore,
		},
		Options: Options{EnableVector: true, EnableGraph: true, EnableRefs: true},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Text:   "query",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 50},
	})
	require.NoError(t, err)

	var auto *retrieval.AutoExpandRetriever
	for _, r := range plan.Retrievers {
		if candidate, ok := r.(*retrieval.AutoExpandRetriever); ok {
			auto = candidate
			break
		}
	}
	require.NotNil(t, auto)
	ontologyRetriever, ok := auto.Ontology.(*retrieval.OntologyRetriever)
	require.True(t, ok)
	diffusionRetriever, ok := auto.Diffusion.(*retrieval.GraphPPRRetriever)
	require.True(t, ok)
	diffusionScope, ok := diffusionRetriever.GraphFacts.(*noderead.Scope)
	require.True(t, ok)
	require.NotNil(t, ontologyRetriever.Scope)
	require.Same(t, ontologyRetriever.Scope, diffusionScope)
}
