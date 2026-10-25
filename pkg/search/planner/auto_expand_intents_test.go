package planner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
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
