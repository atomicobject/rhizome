package noderead

import (
	"context"
	"fmt"
	"maps"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
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
					assertOverlaySortMatchesCommitted(t, committed, staged, overlay, TypeInstancesRequest{
						TypeName: "Task",
						Limit:    5,
						Sort: []codeanchor.OntologyFieldSort{{
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
						req := TypeInstancesRequest{TypeName: "Task", Limit: 120, Sort: []codeanchor.OntologyFieldSort{{
							FieldName: "rank", ValueKind: "int", Desc: desc, NullsLast: nullsLast,
						}}}
						assertOverlaySortMatchesCommitted(t, committed, staged, overlay, req, paths)
						for _, offset := range []int{0, 20, 90, 120} {
							t.Run(fmt.Sprintf("offset=%d", offset), func(t *testing.T) {
								req.Limit, req.Offset = 20, offset
								end := min(offset+req.Limit, len(paths))
								assertOverlaySortMatchesCommitted(t, committed, staged, overlay, req, paths[offset:end])
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
	_, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	return NewService(vaultDef, &obsidian.Note{}, store, schema)
}

func assertOverlaySortMatchesCommitted(t *testing.T, committed, staged *Service, overlay *ReadOverlay, req TypeInstancesRequest, paths []string) {
	t.Helper()
	ctx := context.Background()
	want, err := committed.NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, req)
	require.NoError(t, err)
	got, err := staged.NewScope(ctx, ScopeOptions{ReadOverlay: overlay}).TypeInstances(ctx, req)
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
	require.Equal(t, paths, wantPaths, "committed sort order")
	require.Equal(t, wantPaths, gotPaths, "staged and committed note-root identities must have the same order")
	require.Equal(t, want.Count, got.Count)
}
