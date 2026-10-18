package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"maps"
	"testing"
)

func TestScopeReadOverlayInterfaceTypeInstancesMatchesCommitted(t *testing.T) {
	const schemaSDL = `
interface WorkItem {
  status: String!
  priority: Int!
}

type ProductSpec implements WorkItem @node(paths: ["specs/product.md", "specs/product-b.md"]) {
  status: String! @field
  priority: Int! @field
}
type TechnicalSpec implements WorkItem @node(paths: ["technical-a.md", "technical-b.md"]) {
  status: String! @field
  priority: Int! @field
}
`
	before := map[string]string{
		"specs/product.md":   "---\nstatus: open\npriority: 10\n---\n# Product A\n",
		"specs/product-b.md": "---\nstatus: open\npriority: 20\n---\n# Product B\n",
		"technical-a.md":     "---\nstatus: open\npriority: 30\n---\n# Technical A\n",
		"technical-b.md":     "---\nstatus: open\npriority: 40\n---\n# Technical B\n",
	}
	after := maps.Clone(before)
	after["specs/product.md"] = "---\nstatus: closed\npriority: 10\n---\n# Product A\n"
	staged := overlaySortFixtureService(t, schemaSDL, before)
	committed := overlaySortFixtureService(t, schemaSDL, after)
	overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"specs/product.md": after["specs/product.md"]}}
	for _, test := range []struct {
		name   string
		offset int
		paths  []string
	}{
		{name: "fills page after touched row leaves", paths: []string{"specs/product-b.md", "technical-a.md"}},
		{name: "offset applies once after merge", offset: 1, paths: []string{"technical-a.md", "technical-b.md"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertOverlayMatchesCommitted(t, committed, staged, overlay, TypeInstancesRequest{
				TypeName: "WorkItem", Limit: 2, Offset: test.offset,
				Sort: []readmodel.FieldSort{{FieldName: "priority", ValueKind: "int"}},
				Predicates: []readmodel.FieldPredicate{{FieldName: "status", Op: readmodel.FieldOpEq,
					Values: []readmodel.FieldValueRow{{ValueText: "open", ValueNorm: "open"}}}},
			}, test.paths)
		})
	}
}

type overlayPlanLimitStore struct{ ReadStore }

func (s *overlayPlanLimitStore) OntologyNodesByTypePlan(_ context.Context, plan readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error) {
	rows := make([]readmodel.NodeRow, overlayTypeInstancesPlanLimit+1)
	for i := range rows {
		rows[i] = readmodel.NodeRow{NotePath: "specs/product.md", NodeKind: string(ontology.NodeKindNote), TypeName: "ProductSpec"}
	}
	if plan.Limit > 0 && len(rows) > plan.Limit {
		rows = rows[:plan.Limit]
	}
	return rows, nil
}

func TestScopeOverlayTypeInstancesReportsCandidateLimit(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
interface WorkItem {
  priority: Int!
}
type ProductSpec implements WorkItem @node(paths: ["specs/product.md"]) {
  priority: Int! @field
}
`, "---\npriority: 1\n---\n# Product\n")
	for _, typeName := range []string{"ProductSpec", "WorkItem"} {
		t.Run(typeName, func(t *testing.T) {
			scope := NewService(vaultDef, &obsidian.Note{}, &overlayPlanLimitStore{ReadStore: store}, schema).NewScope(ScopeOptions{
				ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
					"specs/product.md": "---\npriority: 2\n---\n# Product\n",
				}},
			})
			_, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: typeName, Limit: 2,
				Sort: []readmodel.FieldSort{{FieldName: "priority", ValueKind: "int"}}})
			require.ErrorContains(t, err, "overlay type listing exceeds 10000 committed candidates")
		})
	}
}
