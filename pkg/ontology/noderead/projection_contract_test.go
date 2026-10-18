package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestScopeProjectionCacheSeparatesNoteAndNodeIDRefs(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  metrics: MetricsSection @contains(level: H2, heading: "Metrics")
}

type MetricsSection implements Section {
  metrics: [SpecMetric!] @contains(level: H3)
}

type SpecMetric implements Section @node(locator: EMBEDDED) {
  status: String @field
}
`, `# Product

## Metrics

### Throughput metric
status:: Planned
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	root, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.NotNil(t, root)
	rootAgain, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.Same(t, root, rootAgain)

	instances, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)

	nodeIDOnly := ontology.NodeRef{
		NotePath: "specs/product.md",
		NodeID:   instances.Items[0].Ref.NodeID,
		Kind:     ontology.NodeKindEmbedded,
	}
	embedded, err := scope.Projection(context.Background(), nodeIDOnly)
	require.NoError(t, err)
	require.NotNil(t, embedded)
	require.Equal(t, ontology.NodeKindEmbedded, embedded.Ref.Kind)
	require.NotSame(t, root, embedded)
	listed, err := scope.Projection(context.Background(), instances.Items[0].Ref)
	require.NoError(t, err)
	require.NotNil(t, listed)
	listedAgain, err := scope.Projection(context.Background(), instances.Items[0].Ref)
	require.NoError(t, err)
	require.Same(t, listed, listedAgain)

	again, err := scope.Projection(context.Background(), nodeIDOnly)
	require.NoError(t, err)
	require.Same(t, embedded, again)
}

func TestScopeProjectionReturnsFallbackForUntypedNoteRoot(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type ReferenceDoc @node(paths: ["docs/*.md"]) {
  title: String @field
}
`, `# Product

Untyped root prose.

## Findings

Fallback section prose.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	projection, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.NotNil(t, projection)
	require.Equal(t, ontoschema.FallbackNoteTypeName, projection.ResolvedType)
	require.Equal(t, ontoschema.FallbackNoteTypeName, projection.Ref.TypeName)
	require.Equal(t, ontology.NodeKindNote, projection.Ref.Kind)
	require.Nil(t, projection.Assessment)
	require.Contains(t, strings.Join(ontology.FallbackNoteBodyParts(projection, 0), "\n"), "Untyped root prose.")
	require.NotContains(t, strings.Join(ontology.FallbackNoteBodyParts(projection, 0), "\n"), "Fallback section prose.")
}

func TestScopeProjectionDoesNotFallbackInvalidDeclaredType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, schema, body string }{
		{"unknown type", `type ProductSpec @node(paths: ["docs/*.md"]) { status: String @field }`, "---\ntype: MissingType\n---\n# Product\nInvalid type body.\n"},
		{"type outside path", `type ProductSpec @node(paths: ["docs/*.md"]) { status: String @field }`, "---\ntype: ProductSpec\n---\n# Product\nMismatched path body.\n"},
		{"ambiguous path types", "type ProductSpec @node(paths: [\"specs/*.md\"]) { status: String @field }\ntype ResearchNote @node(paths: [\"specs/*.md\"]) { status: String @field }", "# Product\nAmbiguous type body.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vaultDef, store, schema := buildFixture(t, tc.schema, tc.body)
			projection, err := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{}).Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
			require.NoError(t, err)
			require.NotNil(t, projection)
			require.Empty(t, projection.ResolvedType)
			require.NotEqual(t, ontoschema.FallbackNoteTypeName, projection.Ref.TypeName)
			require.NotNil(t, projection.Assessment)
			require.Empty(t, ontology.FallbackNoteBodyParts(projection, 0))
		})
	}
}
