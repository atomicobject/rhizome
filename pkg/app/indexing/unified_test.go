package indexing

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestOntologyNodeEmbeddingsRequireRebuildPreservesEarlierSync(t *testing.T) {
	t.Parallel()

	require.True(t, ontologyNodeEmbeddingsRequireRebuild(
		&ontology.SyncResult{Rebuilt: true},
		&ontology.SyncResult{Rebuilt: false},
	))
	require.True(t, ontologyNodeEmbeddingsRequireRebuild(
		&ontology.SyncResult{Rebuilt: false},
		&ontology.SyncResult{Rebuilt: true},
	))
	require.False(t, ontologyNodeEmbeddingsRequireRebuild(
		nil,
		&ontology.SyncResult{Rebuilt: false},
	))
}
