package notemeta

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/stretchr/testify/require"
)

func TestIndexerRequiresExplicitRuntime(t *testing.T) {
	configured, err := builtin.NewRuntime()
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		runtime noteformat.Runtime
		valid   bool
	}{{"missing", noteformat.Runtime{}, false}, {"configured", configured, true}} {
		t.Run(tc.name, func(t *testing.T) {
			indexer, createErr := NewIndexer(tc.runtime)
			require.Equal(t, tc.valid, createErr == nil)
			require.Equal(t, tc.valid, indexer.Validate() == nil)
			_, runtimeErr := indexer.FormatRuntime()
			require.Equal(t, tc.valid, runtimeErr == nil)
		})
	}
}

func testIndexer(t testing.TB) Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}
