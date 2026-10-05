package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

// newNoteMetadataIndexer composes the closed built-in format runtime for a
// one-shot command. Long-lived commands receive their indexer from
// bootstrap.LiveRuntime instead so all asynchronous work shares one runtime.
func newNoteMetadataIndexer() (notemeta.Indexer, error) {
	runtime, err := newNoteFormatRuntime()
	if err != nil {
		return notemeta.Indexer{}, fmt.Errorf("build note format runtime: %w", err)
	}
	indexer, err := notemeta.NewIndexer(runtime)
	if err != nil {
		return notemeta.Indexer{}, fmt.Errorf("build note metadata indexer: %w", err)
	}
	return indexer, nil
}

func newNoteFormatRuntime() (noteformat.Runtime, error) {
	runtime, err := builtin.NewRuntime()
	if err != nil {
		return noteformat.Runtime{}, fmt.Errorf("build note format runtime: %w", err)
	}
	return runtime, nil
}
