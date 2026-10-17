package web

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchNotesHidesInternalFallbackTypes(t *testing.T) {
	fixture := prepareIdentityStatusFixtureVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "plain.md"), []byte("# Plain\n\nUntyped source.\n"), 0o644))
	_, err := ontologytest.Publish(context.Background(), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	var unfiltered NoteSearchResponse
	getJSON(t, httpSrv.URL+"/api/v1/search/notes?q=plain", &unfiltered)
	require.Len(t, unfiltered.Matches, 1)
	require.Equal(t, "plain.md", unfiltered.Matches[0].Path)
	require.Empty(t, unfiltered.Matches[0].ResolvedType)
	for _, typeName := range []string{ontoschema.FallbackNoteTypeName, ontoschema.FallbackSectionTypeName} {
		var filtered NoteSearchResponse
		getJSON(t, httpSrv.URL+"/api/v1/search/notes?q=plain&type="+typeName, &filtered)
		require.Zero(t, filtered.Total)
		require.Empty(t, filtered.Matches)
	}
}

func TestOntologyEmbeddedCardsUseHostSummaryTags(t *testing.T) {
	fixture := prepareEmbeddedIdentityStatusFixtureVault(t)
	const path = "specs/product.md"
	bytes, err := os.ReadFile(filepath.Join(fixture.root, path))
	require.NoError(t, err)
	committedSource := "---\ntags: [host]\n---\n" + string(bytes) + "\nTagged #committed-inline\n"
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, path), []byte(committedSource), 0o644))
	_, err = ontologytest.Publish(context.Background(), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	var committed OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Story", &committed)
	require.Len(t, committed.Notes, 2)
	for _, note := range committed.Notes {
		require.ElementsMatch(t, []string{"host", "committed-inline"}, note.Tags)
	}
	base, err := buildMarkdownDocumentSnapshotCompat(path, committedSource, time.Time{})
	require.NoError(t, err)
	var session OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{{
		Kind: "setSource", Path: path, Markdown: "---\ntags: [preview]\n---\n" + string(bytes) + "\nTagged #preview-inline\n",
		Expected: &OntologyEditExpected{SourceHash: base.ContentFingerprint, SourceContent: committedSource},
	}}}, &session)
	var staged OntologyTypeResponse
	postJSON(t, httpSrv.URL+"/api/v1/ontology/types/Story", stagedReadRequest{EditSession: &EditSessionReadRequest{SessionID: session.SessionID}}, &staged)
	require.Len(t, staged.Notes, 2)
	for _, note := range staged.Notes {
		require.ElementsMatch(t, []string{"preview", "preview-inline"}, note.Tags)
	}
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Story", &committed)
	for _, note := range committed.Notes {
		require.ElementsMatch(t, []string{"host", "committed-inline"}, note.Tags)
	}
}
