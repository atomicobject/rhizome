package indexing

import (
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

func testNoteMetadataIndexerForHelper() notemeta.Indexer {
	runtime, err := builtin.NewRuntime()
	if err != nil {
		panic(err)
	}
	indexer, err := notemeta.NewIndexer(runtime)
	if err != nil {
		panic(err)
	}
	return indexer
}
