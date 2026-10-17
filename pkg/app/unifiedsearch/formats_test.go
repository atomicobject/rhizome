package unifiedsearch

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/stretchr/testify/require"
	"testing"
)

func testNoteFormats(t testing.TB) noteformat.Runtime {
	t.Helper()
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	return formats
}
