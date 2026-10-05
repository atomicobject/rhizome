package planner

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestPlanner_TextAndSeeds_UsesSeedVector(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})

	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	srch := &semantic.Searcher{
		CodeProvider: prov,
		IntelStore:   intelStore,
	}

	p := Planner{
		Deps:    Deps{Semantic: srch},
		Options: Options{EnableVector: true},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Text:   "query",
		Seeds:  []knowledge.Handle{knowledge.FileHandle("pkg")},
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 25},
	})
	require.NoError(t, err)

	var names []string
	for _, r := range plan.Retrievers {
		names = append(names, r.Name())
	}
	require.Contains(t, names, "seed_vector")
}
