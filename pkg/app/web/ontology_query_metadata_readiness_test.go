package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestTypedQueriesRetainPublishedSchemaDuringMetadataUpdate(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "unrelated.md"), []byte("# Unrelated edit\n"), 0o644))
	indexer := testkit.NoteMetadataIndexer(t)
	_, err := indexer.EnsureIndexed(t.Context(), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	published, err := ontology.PublishedRuntimeWithStore(t.Context(), indexer, fixture.vaultDef, fixture.intelStore)
	require.NoError(t, err)
	require.False(t, published.Ready, "metadata has advanced without an ontology publication")
	for _, query := range []string{
		`{ plan(first: 2) { path } }`,
		`{ node(ref: "specs/100-demo/plan.md") { __typename path } }`,
	} {
		reply := postGraphQL(t, srv, map[string]any{"query": query})
		require.Equal(t, http.StatusOK, reply.status, string(reply.body))
		require.NotContains(t, reply.decoded, "errors")
		require.Contains(t, string(reply.body), "specs/100-demo/plan.md")
	}
}
