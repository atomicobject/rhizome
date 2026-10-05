package ontology

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func testNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func descriptorOnlyHTMLNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func buildIndexFromCanonicalSourcesForTest(t testing.TB, ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, schema *Schema, notesHash string) (*BuildResult, error) {
	sources, err := testNoteMetadataIndexer(t).BuildNoteSourceSnapshots(ctx, vaultDef, noteMgr)
	if err != nil {
		return nil, err
	}
	return BuildIndexFromNoteSources(ctx, vaultDef, sources, schema, notesHash)
}
