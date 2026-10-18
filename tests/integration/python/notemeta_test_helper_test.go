//go:build integration
// +build integration

package integration

import (
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

func newNoteMetadataIndexer() (notemeta.Indexer, error) {
	runtime, err := builtin.NewRuntime()
	if err != nil {
		return notemeta.Indexer{}, err
	}
	return notemeta.NewIndexer(runtime)
}
