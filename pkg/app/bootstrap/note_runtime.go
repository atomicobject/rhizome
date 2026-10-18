package bootstrap

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

// NewBuiltinNoteRuntime composes one explicit provider runtime and its metadata
// indexer before application work starts. It opens no stores or background work.
func NewBuiltinNoteRuntime() (noteformat.Runtime, notemeta.Indexer, error) {
	formats, err := builtin.NewRuntime()
	if err != nil {
		return noteformat.Runtime{}, notemeta.Indexer{}, fmt.Errorf("compose note formats: %w", err)
	}
	metadata, err := notemeta.NewIndexer(formats)
	if err != nil {
		return noteformat.Runtime{}, notemeta.Indexer{}, fmt.Errorf("compose note metadata indexer: %w", err)
	}
	return formats, metadata, nil
}
