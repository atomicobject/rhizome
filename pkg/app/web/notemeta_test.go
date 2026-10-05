package web

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/stretchr/testify/require"
)

func testNoteMetadataIndexer(t *testing.T) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func descriptorOnlyHTMLNoteMetadataIndexer(t *testing.T) notemeta.Indexer {
	t.Helper()
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}
