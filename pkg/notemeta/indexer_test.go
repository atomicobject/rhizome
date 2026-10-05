package notemeta

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/stretchr/testify/require"
)

func TestNewIndexerRequiresRuntime(t *testing.T) {
	_, err := NewIndexer(noteformat.Runtime{})
	require.Error(t, err)
}

func TestIndexerValidateRejectsZeroValue(t *testing.T) {
	var indexer Indexer
	require.Error(t, indexer.Validate())
}

func TestNewIndexerAcceptsBuiltInRuntime(t *testing.T) {
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)

	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	require.NoError(t, indexer.Validate())
}

func testIndexer(t testing.TB) Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}
