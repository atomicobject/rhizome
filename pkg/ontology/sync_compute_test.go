package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"runtime"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyComputeReturnsWorkerFailureBeforeCancellation(t *testing.T) {
	// More failing sources than workers must return the projection error rather
	// than leave the producer blocked until the caller's deadline expires.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snapshot, err := BuildDocumentSnapshot("notes/current.md", "# Current\n", time.Time{})
	require.NoError(t, err)
	schema := &ontoschema.Schema{}
	resolver := &projectionResolver{snapshot: snapshot, schema: schema}
	root := &NodeProjection{Ref: NodeRef{Kind: NodeKindNote, NotePath: snapshot.NotePath}, Fields: map[string]FieldBinding{
		"children": {SectionNodes: []NodeRef{{Kind: NodeKindSection, NotePath: "notes/foreign.md", Fragment: "Missing"}}},
	}}
	jobs := make([]noteSyncJob, max(runtime.GOMAXPROCS(0), 2)+1)
	for i := range jobs {
		jobs[i] = noteSyncJob{doc: &noteDoc{Path: snapshot.NotePath}, projected: &projectedNote{resolver: resolver, root: root}}
	}
	_, err = computeOntologyJobs(ctx, jobs, schema, obsidian.BuildNotePathCache([]string{snapshot.NotePath}), nil, nil)
	require.ErrorContains(t, err, "does not belong to snapshot")
	require.NoError(t, ctx.Err(), "the failure must be returned without needing cancellation")
}
