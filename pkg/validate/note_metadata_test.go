package validate

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

func testNoteMetadata(t *testing.T) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	if err != nil {
		t.Fatalf("build test note-format runtime: %v", err)
	}
	indexer, err := notemeta.NewIndexer(runtime)
	if err != nil {
		t.Fatalf("build test note metadata indexer: %v", err)
	}
	return indexer
}
