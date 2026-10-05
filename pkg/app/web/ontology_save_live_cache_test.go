package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// `rzm serve` builds the server before the runtime publishes its note cache, so
// reads go through the live cache while Config.Cache stays nil. A save must
// still bring that cache current before it returns; otherwise the next read
// serves the pre-save source until the file watcher catches up, and an edit
// started from it conflicts with the saved file.
func TestOntologySaveRefreshesLateNoteCacheBeforeReturning(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	cacheSvc, err := cache.NewService(fixture.root, cache.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cacheSvc.Close() })
	require.NoError(t, cacheSvc.EnsureReady(t.Context()))
	require.Nil(t, srv.cfg.Cache)
	srv.runtime.Live = &webRuntimeView{snapshot: runtimeview.Snapshot{
		NoteReader: cache.NewNoteAdapter(cacheSvc, &obsidian.Note{}),
		IntelStore: srv.runtime.Intel(),
	}}

	const notePath = "specs/100-demo/plan.md"
	original, err := os.ReadFile(filepath.Join(fixture.root, notePath))
	require.NoError(t, err)
	base, err := buildMarkdownDocumentSnapshotCompat(notePath, string(original), time.Time{})
	require.NoError(t, err)
	saved := string(original) + "\nSaved through the live cache.\n"

	ctx := context.Background()
	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{{
		Kind: "setSource", Path: notePath, Markdown: saved,
		Expected: &OntologyEditExpected{SourceHash: base.ContentFingerprint, SourceContent: string(original)},
	}}})
	require.NoError(t, err)
	committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionCommitRequest{
		RequestID: "late-cache-save", ExpectedRevision: created.Revision,
	})
	require.NoError(t, err)
	require.Equal(t, ontology.CommitOutcomeCommitted, committed.Outcome)
	require.Empty(t, committed.Warnings)

	read, err := srv.defaultOntologyNoteReader().GetContents(fixture.vaultDef, notePath)
	require.NoError(t, err)
	require.Equal(t, saved, read, "a read right after the save served the pre-save source")
}
