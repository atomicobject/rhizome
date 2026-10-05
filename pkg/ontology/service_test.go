package ontology

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
	svc := NewService(obsidian.VaultDefinition{Path: t.TempDir()}, &obsidian.Note{}, store, &Schema{})

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = svc.Assessment(cancelled, "a.md")
	require.ErrorIs(t, err, context.Canceled)

	assessment, ok, err := svc.Assessment(ctx, "a.md")
	require.NoError(t, err)
	require.True(t, ok, "healthy retry on the same service returned a miss")
	require.NotNil(t, assessment)
}

// An absent row stays cached as a known miss.
func TestServiceAssessmentCachesAbsentRowAsMiss(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	svc := NewService(obsidian.VaultDefinition{Path: t.TempDir()}, &obsidian.Note{}, store, &Schema{})

	assessment, ok, err := svc.Assessment(ctx, "missing.md")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, assessment)

	require.NoError(t, store.UpsertOntologyAssessments(ctx, []semdb.OntologyNoteAssessmentRow{{
		NotePath: "missing.md", ResolvedType: "Person", AssessmentJSON: `{"notePath":"missing.md","resolvedType":"Person"}`, SchemaHash: "h", UpdatedAt: 1,
	}}))
	assessment, ok, err = svc.Assessment(ctx, "missing.md")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, assessment)

	fresh := NewService(obsidian.VaultDefinition{Path: t.TempDir()}, &obsidian.Note{}, store, &Schema{})
	assessment, ok, err = fresh.Assessment(ctx, "missing.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, assessment)
}
