package web

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkOntologyEditSessionSave measures an exact-path save after the
// 3,600-note vault and its ontology are already indexed.
func BenchmarkOntologyEditSessionSave(b *testing.B) {
	fixture := buildOntologyBenchFixture(b, 3600)
	srv := fixture.server
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{{
			Kind: "setField", Path: "decisions/n0001.md", Field: "summary", Value: fmt.Sprintf("Saved %d", i),
		}}})
		require.NoError(b, err)
		b.StartTimer()
		response, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionCommitRequest{})
		require.NoError(b, err)
		require.Empty(b, response.Warnings)
	}
}
