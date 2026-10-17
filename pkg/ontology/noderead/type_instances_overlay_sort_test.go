package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"maps"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeReadOverlayTypeInstancesSortNullPlacement(t *testing.T) {
	t.Parallel()

	const schemaSDL = `type Task @node(paths: ["tasks/*.md"]) {
  rank: Int @field
  score: Float @field
  ready: Boolean @field
  label: String @field
  ranks: [Int!] @field
}`
	before := map[string]string{
		"tasks/a.md": "---\nrank: 2\nscore: 2.5\nready: false\nlabel: '2'\nranks: ['invalid', '02', '30']\n---\n# Task\n",
		"tasks/b.md": "---\nrank: 10\nscore: 10.5\nready: true\nlabel: '10'\nranks: [10, 20]\n---\n# Task\n",
		"tasks/c.md": "---\nrank: 10\nscore: 10.5\nready: true\nlabel: '10'\nranks: [10, 20]\n---\n# Task\n",
		"tasks/d.md": "---\nrank: invalid\nscore: invalid\nready: invalid\nranks: [invalid]\n---\n# Task\n",
		"tasks/e.md": "# Task\n",
	}
	after := maps.Clone(before)
	after["tasks/a.md"] = "---\nrank: 3\nscore: 3.5\nready: false\nlabel: '3'\nranks: [3, 30]\n---\n# Task\n"
	committed := overlaySortFixtureService(t, schemaSDL, after)
	staged := overlaySortFixtureService(t, schemaSDL, before)
	overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
		"tasks/a.md": after["tasks/a.md"],
		"tasks/d.md": after["tasks/d.md"],
	}}

	for _, field := range []struct {
		name, kind string
		asc, desc  []string
	}{
		{"rank", "int", []string{"a", "b", "c"}, []string{"b", "c", "a"}},
		{"score", "float", []string{"a", "b", "c"}, []string{"b", "c", "a"}},
		{"ready", "bool", []string{"a", "b", "c"}, []string{"b", "c", "a"}},
		{"label", "string", []string{"b", "c", "a"}, []string{"a", "b", "c"}},
		{"ranks", "int", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
	} {
		for _, desc := range []bool{false, true} {
			for _, nullsLast := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/desc=%t/nullsLast=%t", field.name, desc, nullsLast), func(t *testing.T) {
					present := field.asc
					if desc {
						present = field.desc
					}
					ordered := append([]string(nil), present...)
					// Without NullsLast, SQLite puts nulls first only in ASC.
					if !nullsLast && !desc {
						ordered = append([]string{"d", "e"}, ordered...)
					} else {
						ordered = append(ordered, "d", "e")
					}
					paths := make([]string, len(ordered))
					for i, name := range ordered {
						paths[i] = "tasks/" + name + ".md"
					}
					assertOverlayMatchesCommitted(t, committed, staged, overlay, TypeInstancesRequest{
						TypeName: "Task",
						Limit:    5,
						Sort: []readmodel.FieldSort{{
							FieldName: field.name, ValueKind: field.kind, Desc: desc, NullsLast: nullsLast,
						}},
					}, paths)
				})
			}
		}
	}
}

func TestScopeReadOverlayTypeInstancesSortNullPlacementPages(t *testing.T) {
	t.Parallel()

	const schemaSDL = `type Task @node(paths: ["tasks/*.md"]) { rank: Int @field }`
	before := make(map[string]string, 120)
	for i := 0; i < 120; i++ {
		before[fmt.Sprintf("tasks/%03d.md", i)] = overlaySortRankNote(i, false)
	}
	staged := overlaySortFixtureService(t, schemaSDL, before)
	for _, touched := range []int{1, 100} {
		t.Run(fmt.Sprintf("touched=%d", touched), func(t *testing.T) {
			after := maps.Clone(before)
			overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: make(map[string]string, touched)}
			groups := make(map[int][]string)
			for i := 0; i < 120; i++ {
				path := fmt.Sprintf("tasks/%03d.md", i)
				if i < touched {
					after[path] = overlaySortRankNote(i, true)
					overlay.UpdatedContentByPath[path] = after[path]
				}
				rank := 0 // Missing rank.
				switch i % 3 {
				case 0:
					rank = 2
					if i < touched {
						rank = 3
					}
				case 1:
					rank = 10
				}
				groups[rank] = append(groups[rank], path)
			}
			committed := overlaySortFixtureService(t, schemaSDL, after)
			for _, desc := range []bool{false, true} {
				for _, nullsLast := range []bool{false, true} {
					t.Run(fmt.Sprintf("desc=%t/nullsLast=%t", desc, nullsLast), func(t *testing.T) {
						order := []int{2, 3, 10}
						if desc {
							order = []int{10, 3, 2}
						}
						if !nullsLast && !desc {
							order = append([]int{0}, order...)
						} else {
							order = append(order, 0)
						}
						paths := make([]string, 0, len(before))
						for _, rank := range order {
							paths = append(paths, groups[rank]...)
						}
						req := TypeInstancesRequest{TypeName: "Task", Limit: 120, Sort: []readmodel.FieldSort{{
							FieldName: "rank", ValueKind: "int", Desc: desc, NullsLast: nullsLast,
						}}}
						assertOverlayMatchesCommitted(t, committed, staged, overlay, req, paths)
						for _, offset := range []int{0, 20, 90, 120} {
							t.Run(fmt.Sprintf("offset=%d", offset), func(t *testing.T) {
								req.Limit, req.Offset = 20, offset
								end := min(offset+req.Limit, len(paths))
								assertOverlayMatchesCommitted(t, committed, staged, overlay, req, paths[offset:end])
							})
						}
					})
				}
			}
		})
	}
}

func overlaySortRankNote(i int, staged bool) string {
	rank := ""
	switch i % 3 {
	case 0:
		rank = "rank: 2\n"
		if staged {
			rank = "rank: 3\n"
		}
	case 1:
		rank = "rank: 10\n"
	}
	return "---\n" + rank + "---\n# Task\n"
}

func overlaySortFixtureService(t *testing.T, schemaSDL string, notes map[string]string) *Service {
	t.Helper()
	vaultDef, store, schema := buildFixture(t, schemaSDL, "# Unrelated\n")
	for path, content := range notes {
		writeFixtureNote(t, vaultDef.Path, path, content)
	}
	_, err := ontologytest.Publish(context.Background(), testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	return NewService(vaultDef, &obsidian.Note{}, store, schema)
}

func assertOverlayMatchesCommitted(t *testing.T, committed, staged *Service, overlay *ReadOverlay, req TypeInstancesRequest, paths []string) {
	t.Helper()
	ctx := context.Background()
	want, err := committed.NewScope(ScopeOptions{}).TypeInstances(ctx, req)
	require.NoError(t, err)
	got, err := staged.NewScope(ScopeOptions{ReadOverlay: overlay}).TypeInstances(ctx, req)
	require.NoError(t, err)
	wantPaths := make([]string, 0, len(want.Items))
	gotPaths := make([]string, 0, len(got.Items))
	for _, item := range want.Items {
		require.Equal(t, ontology.NodeKindNote, item.Ref.Kind)
		require.Empty(t, item.Ref.Fragment)
		require.Equal(t, item.NotePath, item.Ref.NotePath)
		wantPaths = append(wantPaths, item.NotePath)
	}
	for _, item := range got.Items {
		require.Equal(t, ontology.NodeKindNote, item.Ref.Kind)
		require.Empty(t, item.Ref.Fragment)
		require.Equal(t, item.NotePath, item.Ref.NotePath)
		gotPaths = append(gotPaths, item.NotePath)
	}
	require.Equal(t, paths, wantPaths, "committed result order")
	require.Equal(t, wantPaths, gotPaths, "staged and committed note-root identities must have the same order")
	require.Equal(t, want.Count, got.Count)
}

func TestScopeReadOverlayTypeInstancesMatchesCommitted(t *testing.T) {
	const filterSchema = `type ProductSpec @node(paths: ["specs/*.md"]) {
  rank: Int @field
  specStatus: String @field
}`
	filteredBefore := map[string]string{
		"specs/product.md": "---\nrank: 1\nspec-status: proposed\n---\n# A\n",
		"specs/b.md":       "---\nrank: 5\nspec-status: proposed\n---\n# B\n",
	}
	filteredUpdates := map[string]string{"specs/product.md": "---\nrank: 9\nspec-status: active\n---\n# A\n"}
	three, big := int64(3), int64(9007199254740992)
	ready := true
	for _, test := range []struct {
		name, schema    string
		before, updates map[string]string
		request         TypeInstancesRequest
		paths           []string
	}{
		{
			name:   "limited page after touched row leaves filter",
			schema: `type ProductSpec @node(paths: ["specs/*.md"]) { status: String @field }`,
			before: map[string]string{
				"specs/product.md":   "---\nstatus: open\n---\n# Product A\n",
				"specs/product-b.md": "---\nstatus: open\n---\n# Product B\n",
				"specs/product-c.md": "---\nstatus: open\n---\n# Product C\n",
			},
			updates: map[string]string{"specs/product.md": "---\nstatus: closed\n---\n# Product A\n"},
			request: TypeInstancesRequest{Limit: 2, Sort: []readmodel.FieldSort{{FieldName: "title"}},
				Predicates: []readmodel.FieldPredicate{{FieldName: "status", Op: readmodel.FieldOpEq,
					Values: []readmodel.FieldValueRow{{ValueText: "open", ValueNorm: "open"}}}}},
			paths: []string{"specs/product-b.md", "specs/product-c.md"},
		},
		{
			name:   "typed boolean excludes touched false",
			schema: `type ProductSpec @node(paths: ["specs/*.md"]) { ready: Boolean @field }`,
			before: map[string]string{
				"specs/product.md":   "---\nready: true\n---\n# Product A\n",
				"specs/product-b.md": "---\nready: true\n---\n# Product B\n",
			},
			updates: map[string]string{"specs/product.md": "---\nready: false\n---\n# Product A\n"},
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "ready", Op: readmodel.FieldOpEq,
				Values: []readmodel.FieldValueRow{{ValueBool: &ready}}}}},
			paths: []string{"specs/product-b.md"},
		},
		{
			name:   "schema fields beat prose inline lookalike",
			schema: `type ProductSpec @node(paths: ["specs/*.md"]) { id: String @field specStatus: String @field }`,
			before: map[string]string{
				"specs/product.md": "---\nid: S-1\nspec-status: proposed\n---\n# A\n",
				"specs/b.md":       "---\nid: S-2\nspec-status: proposed\n---\n# B\n\nPrefer a bullet such as `- id:: ^S-2-US1`. Stories follow it.\n",
				"specs/c.md":       "---\nid: S-0\nspec-status: active\n---\n# C\n",
				"specs/d.md":       "---\nid: S-9\nspec-status: proposed\n---\n# D\n",
			},
			updates: map[string]string{"specs/product.md": "---\nid: S-1\nspec-status: active\n---\n# A\n"},
			request: TypeInstancesRequest{Limit: 10, Sort: []readmodel.FieldSort{
				{FieldName: "specStatus", ValueKind: "string", NullsLast: true},
				{FieldName: "id", ValueKind: "string", NullsLast: true}}},
			paths: []string{"specs/c.md", "specs/product.md", "specs/b.md", "specs/d.md"},
		},
		{
			name: "staged status matches", schema: filterSchema, before: filteredBefore, updates: filteredUpdates,
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "specStatus", Op: readmodel.FieldOpEq,
				Values: []readmodel.FieldValueRow{{ValueText: "active", ValueNorm: "active"}}}}},
			paths: []string{"specs/product.md"},
		},
		{
			name: "staged status excluded", schema: filterSchema, before: filteredBefore, updates: filteredUpdates,
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "specStatus", Op: readmodel.FieldOpEq,
				Values: []readmodel.FieldValueRow{{ValueText: "proposed", ValueNorm: "proposed"}}}}},
			paths: []string{"specs/b.md"},
		},
		{
			name: "staged and committed numeric range", schema: filterSchema, before: filteredBefore, updates: filteredUpdates,
			request: TypeInstancesRequest{Limit: 10, Sort: []readmodel.FieldSort{{FieldName: "path"}},
				Predicates: []readmodel.FieldPredicate{{FieldName: "rank", Op: readmodel.FieldOpGT, Values: []readmodel.FieldValueRow{{ValueInt: &three}}}}},
			paths: []string{"specs/b.md", "specs/product.md"},
		},
		{
			name: "numeric range excludes both", schema: filterSchema, before: filteredBefore, updates: filteredUpdates,
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "rank", Op: readmodel.FieldOpLT, Values: []readmodel.FieldValueRow{{ValueInt: &three}}}}},
			paths:   []string{},
		},
		{
			name: "integer precision beyond float64", schema: filterSchema, before: filteredBefore,
			updates: map[string]string{"specs/product.md": "---\nrank: 9007199254740993\nspec-status: active\n---\n# A\n"},
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "rank", Op: readmodel.FieldOpGT, Values: []readmodel.FieldValueRow{{ValueInt: &big}}}}},
			paths:   []string{"specs/product.md"},
		},
		{
			name: "malformed integer has no numeric range", schema: filterSchema, before: filteredBefore,
			updates: map[string]string{"specs/product.md": "---\nrank: abc\nspec-status: active\n---\n# A\n"},
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "rank", Op: readmodel.FieldOpGT, Values: []readmodel.FieldValueRow{{ValueInt: &three}}}}},
			paths:   []string{"specs/b.md"},
		},
		{
			name:   "exists includes and excludes staged rows",
			schema: `type ProductSpec @node(paths: ["specs/*.md"]) { ready: Boolean @field }`,
			before: map[string]string{
				"specs/product.md": "---\nready: true\n---\n# A\n",
				"specs/b.md":       "# B\n",
			},
			updates: map[string]string{"specs/product.md": "# A\n", "specs/b.md": "---\nready: false\n---\n# B\n"},
			request: TypeInstancesRequest{Limit: 10, Predicates: []readmodel.FieldPredicate{{FieldName: "ready", Op: readmodel.FieldOpExists}}},
			paths:   []string{"specs/b.md"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := maps.Clone(test.before)
			for path, content := range test.updates {
				after[path] = content
			}
			staged := overlaySortFixtureService(t, test.schema, test.before)
			committed := overlaySortFixtureService(t, test.schema, after)
			test.request.TypeName = "ProductSpec"
			assertOverlayMatchesCommitted(t, committed, staged, &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: test.updates}, test.request, test.paths)
		})
	}
}
