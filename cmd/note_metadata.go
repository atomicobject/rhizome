package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

// newNoteMetadataIndexer composes the closed built-in format runtime for a
// one-shot command. Long-lived commands receive their indexer from
// bootstrap.LiveRuntime instead so all asynchronous work shares one runtime.
func newNoteMetadataIndexer() (notemeta.Indexer, error) {
	_, indexer, err := bootstrap.NewBuiltinNoteRuntime()
	return indexer, err
}

func newNoteFormatRuntime() (noteformat.Runtime, error) {
	runtime, _, err := bootstrap.NewBuiltinNoteRuntime()
	if err != nil {
		return noteformat.Runtime{}, fmt.Errorf("build note format runtime: %w", err)
	}
	return runtime, nil
}
