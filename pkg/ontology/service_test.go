package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

// A failed assessment read must not poison the service cache: only an absent
// row is a known miss.
func TestServiceAssessmentDoesNotCacheStoreErrors(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.UpsertOntologyAssessments(ctx, []semdb.OntologyNoteAssessmentRow{{
		NotePath: "a.md", ResolvedType: "Person", AssessmentJSON: `{"notePath":"a.md","resolvedType":"Person"}`, SchemaHash: "h", UpdatedAt: 1,
	}}))
	svc := NewService(store, &ontoschema.Schema{})

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = svc.Assessment(cancelled, "a.md")
	require.ErrorIs(t, err, context.Canceled)

	assessment, ok, err := svc.Assessment(ctx, "a.md")
	require.NoError(t, err)
	require.True(t, ok, "healthy retry on the same service returned a miss")
	require.NotNil(t, assessment)
}

// Reusing a service must observe rows published after an earlier miss.
func TestServiceAssessmentObservesRowsPublishedAfterMiss(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	svc := NewService(store, &ontoschema.Schema{})

	assessment, ok, err := svc.Assessment(ctx, "missing.md")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, assessment)

	require.NoError(t, store.UpsertOntologyAssessments(ctx, []semdb.OntologyNoteAssessmentRow{{
		NotePath: "missing.md", ResolvedType: "Person", AssessmentJSON: `{"notePath":"missing.md","resolvedType":"Person"}`, SchemaHash: "h", UpdatedAt: 1,
	}}))
	assessment, ok, err = svc.Assessment(ctx, "missing.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, assessment)

}
