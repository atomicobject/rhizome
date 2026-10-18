package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
	"maps"
	"testing"
)

func TestOverlayLinkNodeIDPredicatesMatchCommittedTargetIdentity(t *testing.T) {
	const schema = `
type Task @node(paths: ["tasks/*.md"]) { owner: Person @link }
type Person @node(paths: ["people/*.md"]) { name: String @field }
`
	before := map[string]string{
		"people/alice.md": "---\nname: Alice\naliases: [Ally]\n---\n# Alice\n\n## Details\n",
		"people/bob.md":   "---\nname: Bob\n---\n# Bob\n",
		"tasks/a.md":      "---\nowner: '[[people/bob]]'\n---\n# A\n",
		"tasks/b.md":      "---\nowner: '[[people/bob]]'\n---\n# B\n",
	}
	staged := overlaySortFixtureService(t, schema, before)
	for _, test := range []struct{ authored, fragment string }{
		{"[[Ally]]", ""}, {"[[alice]]", ""}, {"people/alice.md", ""}, {"[[Ally#Details]]", "Details"},
	} {
		t.Run(test.authored, func(t *testing.T) {
			updates := map[string]string{"tasks/a.md": "---\nowner: '" + test.authored + "'\n---\n# A\n"}
			after := maps.Clone(before)
			after["tasks/a.md"] = updates["tasks/a.md"]
			committed := overlaySortFixtureService(t, schema, after)
			overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: updates}
			alice := readmodel.FieldValueRow{TargetNodeID: ontology.OntologyNodeID(ontology.NodeRef{NotePath: "people/alice.md", Fragment: test.fragment, TypeName: "Person", Kind: ontology.NodeKindNote})}
			bob := readmodel.FieldValueRow{TargetNodeID: ontology.OntologyNodeID(ontology.NodeRef{NotePath: "people/bob.md", TypeName: "Person", Kind: ontology.NodeKindNote})}
			for _, test := range []struct {
				op     readmodel.FieldOperator
				values []readmodel.FieldValueRow
				paths  []string
			}{
				{readmodel.FieldOpEq, []readmodel.FieldValueRow{alice}, []string{"tasks/a.md"}},
				{readmodel.FieldOpIn, []readmodel.FieldValueRow{alice, bob}, []string{"tasks/a.md", "tasks/b.md"}},
			} {
				assertOverlayMatchesCommitted(t, committed, staged, overlay, TypeInstancesRequest{
					TypeName: "Task", Limit: 10, Sort: []readmodel.FieldSort{{FieldName: "path"}},
					Predicates: []readmodel.FieldPredicate{{FieldName: "owner", Op: test.op, Values: test.values}},
				}, test.paths)
			}
		})
	}
}

func TestOverlayLinkPredicatesMatchCommittedTargetIdentity(t *testing.T) {
	const schema = `
type Task @node(paths: ["tasks/*.md"]) { owner: Person @link }
type Person @node(paths: ["people/*.md"]) { name: String @field }
`
	before := map[string]string{
		"people/alice.md": "---\nname: Alice\naliases: [Ally]\n---\n# Alice\n",
		"people/bob.md":   "---\nname: Bob\n---\n# Bob\n",
		"tasks/a.md":      "---\nowner: '[[people/bob]]'\n---\n# A\n",
		"tasks/b.md":      "---\nowner: '[[people/alice]]'\n---\n# B\n",
		"tasks/c.md":      "---\nowner: '[[people/bob]]'\n---\n# C\n",
	}
	staged := overlaySortFixtureService(t, schema, before)
	for _, authored := range []string{"[[Ally]]", "[[Ally|Alice display]]", "[[alice]]", "[[people/alice]]", "people/alice.md"} {
		t.Run(authored, func(t *testing.T) {
			updates := map[string]string{"tasks/a.md": "---\nowner: '" + authored + "'\n---\n# A\n"}
			after := maps.Clone(before)
			after["tasks/a.md"] = updates["tasks/a.md"]
			committed := overlaySortFixtureService(t, schema, after)
			overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: updates}
			alice := readmodel.FieldValueRow{TargetNotePath: "people/alice.md"}
			bob := readmodel.FieldValueRow{TargetNotePath: "people/bob.md"}
			for _, test := range []struct {
				name   string
				op     readmodel.FieldOperator
				values []readmodel.FieldValueRow
				offset int
				limit  int
				paths  []string
			}{
				{"eq includes staged canonical target", readmodel.FieldOpEq, []readmodel.FieldValueRow{alice}, 0, 10, []string{"tasks/a.md", "tasks/b.md"}},
				{"eq excludes old target", readmodel.FieldOpEq, []readmodel.FieldValueRow{bob}, 0, 10, []string{"tasks/c.md"}},
				{"in pages after canonical match", readmodel.FieldOpIn, []readmodel.FieldValueRow{alice, bob}, 1, 1, []string{"tasks/b.md"}},
			} {
				t.Run(test.name, func(t *testing.T) {
					assertOverlayMatchesCommitted(t, committed, staged, overlay, TypeInstancesRequest{
						TypeName: "Task", Limit: test.limit, Offset: test.offset,
						Sort:       []readmodel.FieldSort{{FieldName: "path"}},
						Predicates: []readmodel.FieldPredicate{{FieldName: "owner", Op: test.op, Values: test.values}},
					}, test.paths)
				})
			}
		})
	}
}

func TestOverlayLinkPredicatesResolveStagedTargetInventory(t *testing.T) {
	const schema = `
type Task @node(paths: ["tasks/*.md"]) { owner: Person @link }
type Person @node(paths: ["people/*.md"]) { name: String @field }
`
	before := map[string]string{
		"people/alice.md": "---\nname: Alice\naliases: [Ally]\n---\n# Alice\n",
		"people/bob.md":   "---\nname: Bob\n---\n# Bob\n",
		"tasks/a.md":      "---\nowner: '[[people/bob]]'\n---\n# A\n",
	}
	staged := overlaySortFixtureService(t, schema, before)
	for _, test := range []struct {
		name, link, target string
		updates            map[string]string
		deleted            string
		paths              []string
	}{
		{"changed alias", "[[New Ally]]", "people/alice.md", map[string]string{"people/alice.md": "---\nname: Alice\naliases: [New Ally]\n---\n# Alice\n"}, "", []string{"tasks/a.md"}},
		{"new target", "[[New teammate]]", "people/carol.md", map[string]string{"people/carol.md": "---\nname: Carol\naliases: [New teammate]\n---\n# Carol\n"}, "", []string{"tasks/a.md"}},
		{"deleted target", "[[Ally]]", "people/alice.md", nil, "people/alice.md", []string{}},
		{"ambiguous alias", "[[Ally]]", "people/alice.md", map[string]string{"people/other.md": "---\nname: Other\naliases: [Ally]\n---\n# Other\n"}, "", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			updates := maps.Clone(test.updates)
			if updates == nil {
				updates = map[string]string{}
			}
			updates["tasks/a.md"] = "---\nowner: '" + test.link + "'\n---\n# A\n"
			after := maps.Clone(before)
			for path, content := range updates {
				after[path] = content
			}
			overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: updates}
			if test.deleted != "" {
				delete(after, test.deleted)
				overlay.Notes = map[string]ReadOverlayNote{test.deleted: {Deleted: true}}
			}
			committed := overlaySortFixtureService(t, schema, after)
			index, err := BuildReadOverlayIndex(context.Background(), staged.Schema, overlay)
			require.NoError(t, err)
			for _, mode := range []struct {
				name    string
				overlay *ReadOverlay
			}{
				{"raw", overlay},
				{"prebuilt", &ReadOverlay{SourceFormat: "markdown", Index: index}},
			} {
				t.Run(mode.name, func(t *testing.T) {
					for _, target := range []readmodel.FieldValueRow{
						{TargetNotePath: test.target},
						{TargetNodeID: ontology.OntologyNodeID(ontology.NodeRef{NotePath: test.target, TypeName: "Person", Kind: ontology.NodeKindNote})},
					} {
						assertOverlayMatchesCommitted(t, committed, staged, mode.overlay, TypeInstancesRequest{
							TypeName: "Task", Limit: 10,
							Predicates: []readmodel.FieldPredicate{{FieldName: "owner", Op: readmodel.FieldOpEq, Values: []readmodel.FieldValueRow{target}}},
						}, test.paths)
					}
				})
			}
		})
	}
}
