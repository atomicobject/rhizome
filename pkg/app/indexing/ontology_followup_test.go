package indexing

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestOntologyFollowupNeededSkipsCleanNoOp(t *testing.T) {
	result := &ontology.SyncResult{Dirty: false, Rebuilt: false}
	if ontologyFollowupNeeded(result, nil, nil) {
		t.Fatal("clean ontology sync with no changed notes should not need follow-up work")
	}
}

func TestOntologyFollowupEmbeddingRequestUsesUpdatedProjection(t *testing.T) {
	schema := &ontology.Schema{}
	result := &ontology.SyncResult{Schema: schema, Rebuilt: true}

	request, ok := ontologyFollowupEmbeddingRequest(result, []string{"notes/changed.md"}, []string{"notes/deleted.md"})

	require.True(t, ok)
	require.Same(t, schema, request.schema)
	require.Equal(t, []string{"notes/changed.md"}, request.changedPaths)
	require.Equal(t, []string{"notes/deleted.md"}, request.deletedPaths)
	require.True(t, request.rebuilt)
}

func TestOntologyFollowupEmbeddingRequestSkipsMissingProjection(t *testing.T) {
	_, ok := ontologyFollowupEmbeddingRequest(nil, nil, nil)
	require.False(t, ok)

	_, ok = ontologyFollowupEmbeddingRequest(&ontology.SyncResult{}, nil, nil)
	require.False(t, ok)
}

func TestOntologyFollowupNeededRunsForDirtyOrTouchedNotes(t *testing.T) {
	cases := []struct {
		name    string
		result  *ontology.SyncResult
		changed []string
		deleted []string
	}{
		{name: "dirty", result: &ontology.SyncResult{Dirty: true}},
		{name: "rebuilt", result: &ontology.SyncResult{Rebuilt: true}},
		{name: "changed note", result: &ontology.SyncResult{}, changed: []string{"notes/a.md"}},
		{name: "deleted note", result: &ontology.SyncResult{}, deleted: []string{"notes/a.md"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !ontologyFollowupNeeded(tc.result, tc.changed, tc.deleted) {
				t.Fatal("expected ontology follow-up work")
			}
		})
	}
}
