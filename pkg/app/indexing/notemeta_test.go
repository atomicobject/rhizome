package indexing

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

func testNoteMetadataIndexer(t *testing.T) notemeta.Indexer {
	t.Helper()
	return testNoteMetadataIndexerForHelper()
}

func descriptorOnlyHTMLNoteMetadataIndexer(t *testing.T) notemeta.Indexer {
	t.Helper()
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	if err != nil {
		t.Fatal(err)
	}
	indexer, err := notemeta.NewIndexer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	return indexer
}

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
