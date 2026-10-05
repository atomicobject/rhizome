package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func TestIndexedPopulationReadsFreshSchema(t *testing.T) {
	store, err := Open(currentSchemaTestDBPath(t, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	inventory, err := store.IndexedPopulation(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, inventory.Fingerprint)
	require.Zero(t, inventory.UnresolvedChunkOwner)
	require.Empty(t, inventory.Paths)
}

func TestIndexedPopulationIncludesResolvedIntelEdgesAndPermitsExternalTargets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "source", Lang: codeanchor.LangGo, Kind: "func", Path: "src/source.go", Symbol: "Source", FQN: "sample.Source", Fingerprint: "source-fp"},
		{AnchorID: "target", Lang: codeanchor.LangGo, Kind: "func", Path: "src/target.go", Symbol: "Target", FQN: "sample.Target", Fingerprint: "target-fp"},
	}
	edges := []codeanchor.IntelEdge{
		{SrcID: "source", DstID: "target", Kind: "calls"},
		{SrcID: "source", DstID: "external/package.Function", Kind: "calls"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/source.go", anchors, edges, nil))

	inventory, err := store.IndexedPopulation(ctx)
	require.NoError(t, err)
	// The persisted graph contains only resolved internal endpoints. The supplied
	// external symbol remains valid unresolved evidence outside intel_edges.
	require.Equal(t, 1, inventory.IntelEdges)
	require.Zero(t, inventory.UnresolvedIntelOwner)
	require.Contains(t, inventory.Paths, "src/source.go")
	require.Contains(t, inventory.Paths, "src/target.go")
}

func TestIndexedPopulationFlagsDanglingInternalIntelEdgeOwner(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_edges(src_type,src_row_id,dst_type,dst_row_id,kind) VALUES('anchor',999,'anchor',998,'calls')`)
	require.NoError(t, err)

	inventory, err := store.IndexedPopulation(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, inventory.UnresolvedIntelOwner)
}

func TestIndexedPopulationRetainsCanonicalIdentitiesAndResolvedGraphPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/source.go", []codeanchor.IntelAnchor{{AnchorID: "source", Lang: codeanchor.LangGo, Kind: "func", Path: "src/source.go", Symbol: "Source", FQN: "sample.Source", Fingerprint: "fp"}}, nil, nil))
	_, err = store.db.ExecContext(ctx, `INSERT INTO notes(path,title,content_hash,indexer_version,indexed_at) VALUES('docs/known.md','Known','hash','test',1),('docs/placeholder.md','Placeholder','','',0)`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_nodes(node_id,note_path,node_ref_json,node_kind,fragment,start_byte,end_byte,structural_fingerprint,updated_at) VALUES('known-node','docs/known.md','{}','SECTION','known',0,10,'structure',1)`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_doc_edges(src_path,dst_path,kind) VALUES('docs/known.md','docs/missing.md','wikilink')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_doc_edges(src_path,dst_path,kind) VALUES('docs/known.md','docs/placeholder.md','wikilink')`)
	require.NoError(t, err)

	inventory, err := store.IndexedPopulation(ctx)
	require.NoError(t, err)
	require.Contains(t, inventory.Identities, "fqn\x00sample.Source\x00src/source.go")
	require.Contains(t, inventory.Identities, "node\x00docs/known.md\x00known-node\x00known\x00structure")
	require.Contains(t, inventory.Paths, "docs/known.md")
	require.NotContains(t, inventory.Paths, "docs/missing.md")
	require.Zero(t, inventory.UnresolvedGraphSource)
	require.Equal(t, 2, inventory.UnresolvedGraphTarget)
	require.Contains(t, inventory.UnresolvedGraphTargets, "docs/placeholder.md")
}
