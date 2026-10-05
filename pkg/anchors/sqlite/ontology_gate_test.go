package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestOntologyNodeCountTracksMaterializedReadModel(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-gate.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	count, err := store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"docs/spec.md"},
		Nodes: []codeanchor.IntelOntologyNode{{
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

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		FullReplace: true,
	}))
	count, err = store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
