// Package builtin assembles the closed set of note format providers shipped by Rhizome.
package builtin

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
)

// NewRegistry returns the immutable built-in provider registry.
func NewRegistry() (noteformat.Registry, error) {
	return noteformat.NewRegistry(markdown.New(), html.New())
}

// NewRuntime assembles the immutable built-in provider runtime.
func NewRuntime() (noteformat.Runtime, error) {
	registry, err := NewRegistry()
	if err != nil {
		return noteformat.Runtime{}, err
	}
	return noteformat.NewRuntime(registry, markdown.New(), html.New())
}
