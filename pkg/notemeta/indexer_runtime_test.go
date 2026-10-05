package notemeta

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/stretchr/testify/require"
)

func TestIndexerFormatRuntimeValidatesLikeIndexerOperations(t *testing.T) {
	var indexer Indexer

	_, err := indexer.FormatRuntime()
	require.Error(t, err)
	require.EqualError(t, err, "note format runtime is required")
}

func TestIndexerFormatRuntimeReturnsConfiguredImmutableRuntime(t *testing.T) {
	configured, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(configured)
	require.NoError(t, err)

	runtime, err := indexer.FormatRuntime()
	require.NoError(t, err)
	require.Equal(t, configured.Registry().IDs(), runtime.Registry().IDs())
	for _, formatID := range configured.Registry().IDs() {
		require.Equal(t, configured.CanProject(formatID), runtime.CanProject(formatID))
	}

	htmlProvider, ok := runtime.Provider(noteformat.FormatID("html"))
	require.True(t, ok)
	mutated := htmlProvider.Descriptor()
	mutated.Extensions[0] = ".mutated"

	freshProvider, ok := runtime.Provider(noteformat.FormatID("html"))
	require.True(t, ok)
	require.Equal(t, ".html", freshProvider.Descriptor().Extensions[0])
}
