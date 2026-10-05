package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func touchValidationProjectionNote(t *testing.T, root, notePath string) {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(notePath))
	info, err := os.Stat(absPath)
	require.NoError(t, err)
	next := info.ModTime().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(absPath, next, next))
}

func TestRefreshValidationProjectionTouchOnlyIsMetadataOnly(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	fingerprint, err := initial.Runtime.Store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	noteState, err := initial.Runtime.Store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	ontologyState, err := initial.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	touchValidationProjectionNote(t, root, "notes/one.md")
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()

	require.Equal(t, []paths.NotePath{"notes/one.md"}, refreshed.ChangedPaths, "mtime differences must still be read")
	require.Equal(t, 1, refreshed.Counters.MetadataBatches)
	require.Zero(t, refreshed.Counters.OntologyNotesConsidered, "content-identical notes need no ontology work")
	updatedFingerprint, err := refreshed.Runtime.Store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, fingerprint, updatedFingerprint)
	updatedNoteState, err := refreshed.Runtime.Store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, noteState, updatedNoteState)
	updatedOntologyState, err := refreshed.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, ontologyState, updatedOntologyState)
}

// Exact paths are an explicit instruction that survives however the metadata
// delta narrowed: the ontology sync must still receive the requested set.
func TestRefreshValidationProjectionExactPathsKeepOntologySet(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	touchValidationProjectionNote(t, root, "notes/one.md")
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()

	require.Equal(t, 1, refreshed.Counters.MetadataBatches)
	require.Positive(t, refreshed.Counters.OntologyNotesConsidered,
		"an exact-path request must reach ontology even when the metadata delta is touch-only")
}
