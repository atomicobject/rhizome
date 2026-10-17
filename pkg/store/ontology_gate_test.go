package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOntologyNodeCountTracksMaterializedReadModel(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-gate.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	count, err := store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{
		NotePaths: []string{"docs/spec.md"},
		Nodes: []readmodel.NodeRow{{
			NodeID:      "spec-1",
			NotePath:    "docs/spec.md",
			NodeRefJSON: `{"notePath":"docs/spec.md","nodeId":"spec-1"}`,
			NodeKind:    "NOTE",
			TypeName:    "Spec",
		}},
	}))

	count, err = store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{
		FullReplace: true,
	}))
	count, err = store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
