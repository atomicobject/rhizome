package noderead

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const expansionCatalogSchema = `
type ProductSpec @node(paths: ["specs/product.md"]) {
  items: ItemsSection @contains(level: H2, heading: "Items")
}
type ItemsSection implements Section {
  items: [TaskItem!] @contains(shape: LIST_ITEM)
}
type TaskItem implements Section @node(locator: EMBEDDED) {
  summary: String @field(sourceKind: ITEM_SUMMARY)
  owner: Person @link
  target: Destination @link
}
type Person @node(paths: ["people/*.md"]) { name: String }
type Destination @node(paths: ["destinations/*.md"]) { name: String }
`

func TestScopeExpandPreservesCatalogRequestedSources(t *testing.T) {
	for _, anchored := range []bool{false, true} {
		name := "generated fragments"
		if anchored {
			name = "block anchors"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			body := "# Product\n\n## Items\n\n- First item\n  owner:: [[people/alice]]\n  target:: [[destinations/one]]\n- Second item\n  owner:: [[people/alice]]\n  target:: [[destinations/two]]\n"
			if anchored {
				body = strings.Replace(body, "First item\n", "First item ^first-item\n", 1)
				body = strings.Replace(body, "Second item\n", "Second item ^second-item\n", 1)
			}
			vault, store, _ := buildFixture(t, expansionCatalogSchema, body)
			writeFixtureNote(t, vault.Path, "people/alice.md", "# Alice\n")
			writeFixtureNote(t, vault.Path, "destinations/one.md", "# One\n")
			writeFixtureNote(t, vault.Path, "destinations/two.md", "# Two\n")
			runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
			require.NoError(t, err)
			service := NewService(vault, &obsidian.Note{}, store, runtime.Schema)
			listed, err := service.NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "TaskItem"})
			require.NoError(t, err)
			require.Len(t, listed.Items, 2)
			var full, partial []ontology.NodeRef
			for _, item := range listed.Items {
				require.NotEmpty(t, item.Ref.NodeID)
				require.NotEmpty(t, item.Ref.Fragment)
				full = append(full, item.Ref)
				partial = append(partial, ontology.NodeRef{NotePath: item.Ref.NotePath, NodeID: item.Ref.NodeID, Kind: item.Ref.Kind})
			}
			for _, tc := range []struct {
				name      string
				sources   []ontology.NodeRef
				targets   []string
				withSteps bool
			}{
				{"canonical", full, []string{"destinations/one.md", "destinations/two.md"}, true},
				{"NodeID only", partial, []string{"destinations/one.md", "destinations/two.md"}, true},
				{"single NodeID", partial[:1], []string{"destinations/one.md"}, true},
				{"same canonical frontier", []ontology.NodeRef{full[0], partial[0]}, []string{"destinations/one.md", "destinations/one.md"}, true},
				{"empty steps", partial, nil, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					scope := service.NewScope(ctx, ScopeOptions{})
					plan := ExpansionPlan{Sources: tc.sources, Hydrate: HydrateOptions{Profile: HydrateSummary}, Limits: TraverseLimits{MaxDepth: 1}}
					if tc.withSteps {
						plan.Steps = []ExpansionStep{{Direction: TraversalDirectionOutbound, RelationNames: []string{"target"}, IncludeStructural: true}}
					}
					result, err := scope.Expand(ctx, plan)
					require.NoError(t, err)
					require.False(t, result.Truncated)
					require.Len(t, result.Edges, len(tc.targets))
					require.Len(t, result.Sources, len(tc.sources))
					locatorCount := map[string]int{}
					for _, source := range tc.sources {
						locatorCount[source.String()]++
					}
					uniqueTargets := map[string]struct{}{}
					for index, source := range tc.sources {
						group := result.Sources[index]
						require.Equal(t, source, group.Source)
						require.Equal(t, group, result.BySource[RefIdentityKey(source)])
						alias, exists := result.BySource[source.String()]
						require.Equal(t, locatorCount[source.String()] == 1, exists)
						if exists {
							require.Equal(t, group, alias)
						}
						if tc.withSteps {
							require.Len(t, group.Edges, 1)
							require.Equal(t, tc.targets[index], group.Edges[0].Target.NotePath)
							require.Equal(t, 1, group.Edges[0].Depth)
							uniqueTargets[tc.targets[index]] = struct{}{}
						}
					}
					require.Len(t, result.Nodes, len(uniqueTargets))
					for _, node := range result.Nodes {
						require.Empty(t, node.Content)
					}
					if tc.withSteps {
						require.Equal(t, 1, scope.Diagnostics().EdgeLoads)
					} else {
						require.Zero(t, scope.Diagnostics().EdgeLoads)
					}
				})
			}
			walk, err := service.NewScope(ctx, ScopeOptions{}).Walk(ctx, &obsidian.Note{}, vault, "people/alice.md", ontology.WalkOptions{MaxDepth: 2})
			require.NoError(t, err)
			var paths []string
			for _, node := range walk.Nodes {
				paths = append(paths, node.Path)
			}
			require.Contains(t, paths, "destinations/one.md")
			require.Contains(t, paths, "destinations/two.md")
			require.Len(t, walk.Edges, 4)
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			_, err = service.NewScope(ctx, ScopeOptions{}).Expand(canceled, ExpansionPlan{Sources: partial})
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestScopeExpandBoundsEachSharedFrontierEmission(t *testing.T) {
	ctx := context.Background()
	vault, store, _ := buildFixture(t, `type LinkedNote @node(paths: ["notes/*.md"]) { next: LinkedNote @link }`, "# Host\n")
	writeFixtureNote(t, vault.Path, "notes/a.md", "---\nnext: '[[notes/shared]]'\n---\n")
	writeFixtureNote(t, vault.Path, "notes/b.md", "---\nnext: '[[notes/shared]]'\n---\n")
	writeFixtureNote(t, vault.Path, "notes/shared.md", "---\nnext: '[[notes/end]]'\n---\n")
	writeFixtureNote(t, vault.Path, "notes/end.md", "# End\n")
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	service := NewService(vault, &obsidian.Note{}, store, runtime.Schema)
	for _, tc := range []struct {
		name   string
		limits TraverseLimits
		budget TraverseBudget
	}{
		{"FirstTotal", TraverseLimits{FirstTotal: 3}, TraverseBudget{}},
		{"FirstTotal narrower", TraverseLimits{FirstTotal: 3, MaxEdges: 5}, TraverseBudget{MaxEdges: 4}},
		{"MaxEdges narrower", TraverseLimits{FirstTotal: 5, MaxEdges: 3}, TraverseBudget{MaxEdges: 4}},
		{"budget narrower", TraverseLimits{FirstTotal: 5, MaxEdges: 4}, TraverseBudget{MaxEdges: 3}},
		{"unlimited", TraverseLimits{}, TraverseBudget{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.limits.MaxDepth = 2
			scope := service.NewScope(ctx, ScopeOptions{})
			result, err := scope.Expand(ctx, ExpansionPlan{
				Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
				Steps:   []ExpansionStep{{Direction: TraversalDirectionOutbound, RelationNames: []string{"next"}, IncludeStructural: true}},
				Limits:  tc.limits, Budget: tc.budget,
			})
			require.NoError(t, err)
			require.Len(t, result.Sources, 2)
			require.Len(t, result.BySource["notes/a.md"].Edges, 2)
			require.False(t, result.BySource["notes/a.md"].Truncated)
			if tc.name == "unlimited" {
				require.Len(t, result.Edges, 4)
				require.Len(t, result.BySource["notes/b.md"].Edges, 2)
				require.False(t, result.Truncated)
			} else {
				require.Len(t, result.Edges, 3)
				require.Len(t, result.BySource["notes/b.md"].Edges, 1)
				require.True(t, result.BySource["notes/b.md"].Truncated)
				require.True(t, result.Truncated)
			}
			require.Equal(t, 2, scope.Diagnostics().EdgeLoads)
		})
	}
	writeFixtureNote(t, vault.Path, "notes/end.md", "---\nnext: '[[notes/shared]]'\n---\n")
	_, err = ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	cycle, err := service.NewScope(ctx, ScopeOptions{}).Expand(ctx, ExpansionPlan{
		Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Steps:   []ExpansionStep{{Direction: TraversalDirectionOutbound, RelationNames: []string{"next"}, IncludeStructural: true}},
		Limits:  TraverseLimits{MaxDepth: 5},
	})
	require.NoError(t, err)
	require.Len(t, cycle.Edges, 6)
	require.False(t, cycle.Truncated)
}
