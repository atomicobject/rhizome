//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/stretchr/testify/require"
)

func testNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	indexer, err := newNoteMetadataIndexer()
	require.NoError(t, err)
	return indexer
}

func newNoteMetadataIndexer() (notemeta.Indexer, error) {
	runtime, err := builtin.NewRuntime()
	if err != nil {
		return notemeta.Indexer{}, err
	}
	return notemeta.NewIndexer(runtime)
}
