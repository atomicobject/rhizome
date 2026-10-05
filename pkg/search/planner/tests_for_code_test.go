package planner

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestPlanner_AddsTestsForCodeWithoutIntelStore(t *testing.T) {
	ctx := context.Background()
	p := Planner{
		Deps: Deps{
			VaultPath: t.TempDir(),
		},
		Options: Options{
			EnableVector: false,
			EnableIntel:  false,
			EnableGraph:  false,
			EnableRefs:   false,
			MaxPerOwner:  2,
		},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Intent: search.IntentTestsForCode,
		Seeds:  []knowledge.Handle{knowledge.FileHandle("pkg/service.go")},
		Limits: search.Limits{Total: 5},
	})
	require.NoError(t, err)

	var names []string
	for _, r := range plan.Retrievers {
		names = append(names, r.Name())
	}
	require.Contains(t, names, "tests_for_code")
}

func TestPlanner_AddsTestsForSubsystemOverviewWithExplicitSeed(t *testing.T) {
	ctx := context.Background()
	p := Planner{
		Deps: Deps{
			VaultPath: t.TempDir(),
		},
		Options: Options{
			EnableVector: false,
			EnableIntel:  false,
			EnableGraph:  false,
			EnableRefs:   false,
			MaxPerOwner:  2,
		},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Intent:            search.IntentSubsystemOverview,
		Seeds:             []knowledge.Handle{knowledge.FileHandle("pkg/ontology/query/execute.go")},
		ExplicitSeedPaths: []string{"pkg/ontology/query/execute.go"},
		HasExplicitSeeds:  true,
		Limits:            search.Limits{Total: 10},
	})
	require.NoError(t, err)

	var names []string
	for _, r := range plan.Retrievers {
		names = append(names, r.Name())
	}
	require.Contains(t, names, "tests_for_code")
}
