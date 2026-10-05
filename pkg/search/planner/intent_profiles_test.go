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
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIntentProfiles_P3Overrides(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	p := Planner{Deps: Deps{
		IntelStore: store, Semantic: &semantic.Searcher{CodeProvider: provider, NoteProvider: provider, IntelStore: store},
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}, Options: Options{EnableVector: true, EnableIntel: true, EnableGraph: true, EnableRefs: true, MaxPerOwner: 3}}
	ordinary, err := p.Plan(ctx, search.QuerySpec{Text: "search", Intent: search.IntentSearch, Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	require.True(t, planContainsLane(ordinary, "vector"), "vector backend is available")
	seeded, err := p.Plan(ctx, search.QuerySpec{Text: "Service", Intent: search.IntentSearch, Seeds: []knowledge.Handle{knowledge.AnchorHandle("service")}, Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	require.True(t, planContainsLane(seeded, "graph"), "graph backend is available")
	for _, tc := range []struct {
		intent      search.Intent
		ownerCapOne bool
	}{
		{search.IntentGoToDef, true},
		{search.IntentFindUsages, false},
		{search.IntentTestsForCode, false},
		{search.IntentRefactorImpact, false},
	} {
		t.Run(string(tc.intent), func(t *testing.T) {
			spec := search.QuerySpec{Text: "Service", Intent: tc.intent, Seeds: []knowledge.Handle{knowledge.AnchorHandle("service")}, Limits: search.Limits{Total: 10}}
			plan, err := p.Plan(ctx, spec)
			require.NoError(t, err)
			require.False(t, planContainsLane(plan, "vector"))
			require.False(t, planContainsLane(plan, "graph"))
			if tc.ownerCapOne {
				owner := knowledge.FileHandle("pkg/search/service.go")
				candidates := []search.Candidate{
					{Handle: knowledge.AnchorHandle("a"), Owner: owner, Type: "anchor", Path: "pkg/search/service.go", Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: 1}}},
					{Handle: knowledge.AnchorHandle("b"), Owner: owner, Type: "anchor", Path: "pkg/search/service.go", Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: .8}}},
				}
				ranked, err := plan.Ranker.Rank(ctx, spec, candidates)
				require.NoError(t, err)
				require.Len(t, ranked, 1, "go-to-definition plan must apply its owner cap")
			}
		})
	}
}
