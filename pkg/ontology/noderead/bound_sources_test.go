package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRelationCountsBoundSourcesDoNotAuthorizeExternalOrForeignRefs(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- Same\n- Same\n")
	service := NewService(vault, &obsidian.Note{}, store, schema)
	scope := service.NewScope(ScopeOptions{})
	root, err := scope.Projection(ctx, ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	nodes, err := root.Prepared.Nodes()
	require.NoError(t, err)
	var captured *ontology.NodeProjection
	for _, node := range nodes {
		if node.Ref.Kind == ontology.NodeKindEmbedded {
			captured = node
			break
		}
	}
	require.NotNil(t, captured)
	request := RelationCountsRequest{Sources: []ontology.NodeRef{captured.Ref}, SourceProjections: []*ontology.NodeProjection{captured}}
	_, err = scope.RelationCounts(ctx, request)
	require.NoError(t, err)
	_, err = service.NewScope(ScopeOptions{}).RelationCounts(ctx, request)
	require.ErrorContains(t, err, "does not belong to this read scope")
	request.SourceProjections = nil
	_, err = scope.RelationCounts(ctx, request)
	require.ErrorIs(t, err, ontology.ErrAmbiguousSourceSpanIdentity)
}
