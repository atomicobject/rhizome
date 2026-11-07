package retrieval

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

// Lexical and vector hits on one record merge into one candidate, records in
// one host keep separate owners, and other embedded nodes keep the host owner.
func TestRecordCandidatesShareIdentityAcrossLanes(t *testing.T) {
	const host = "model/services.yaml"
	vector := func(nodeID string, record bool) search.Candidate {
		t.Helper()
		candidate, err := candidateFromSemanticResult(semantic.Result{
			SourceRef: search.SourceRef{Path: host, NodeID: nodeID},
			Handle:    knowledge.NodeChunkHandle(nodeID, host, embeddings.GranularityOntologyNodeBody, 0).String(),
			Type:      "note", Record: record,
		})
		require.NoError(t, err)
		return candidate
	}
	refJSON, err := json.Marshal(ontology.NodeRef{NotePath: host, NodeID: "record-v1-a", Kind: ontology.NodeKindEmbedded, Fragment: "record:v1:a"})
	require.NoError(t, err)
	lexical, ok := recordLexicalCandidate(semdb.IntelSearchRow{Type: "ontology_record", ID: "record-v1-a", Path: host, Title: "Alpha", BodyHit: true, NodeRefJSON: string(refJSON)}, 0.5, []string{"alpha"})
	require.True(t, ok)

	a, b := vector("record-v1-a", true), vector("record-v1-b", true)
	require.Equal(t, a.Handle, lexical.Handle)
	require.Equal(t, a.Owner, lexical.Owner)
	require.NotEqual(t, a.Owner, b.Owner)
	require.Equal(t, knowledge.NoteHandle(host), vector("embedded-x", false).Owner)

	_, ok = recordLexicalCandidate(semdb.IntelSearchRow{Type: "ontology_record", ID: "record-v1-a", Path: host, NodeRefJSON: string(refJSON)}, 0.5, nil)
	require.False(t, ok, "a path-only hit does not match every record in the host")
	_, ok = recordLexicalCandidate(semdb.IntelSearchRow{Type: "ontology_record", ID: "record-v1-gone", Path: host, BodyHit: true}, 0.5, nil)
	require.False(t, ok, "a row without catalog identity is stale")
}
