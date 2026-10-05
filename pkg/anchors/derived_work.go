package codeanchor

import "time"

// DerivedKind identifies a destination independently from structural indexing.
type DerivedKind string

const (
	DerivedNotes    DerivedKind = "notes"
	DerivedOntology DerivedKind = "ontology"
	DerivedCode     DerivedKind = "code"
	DerivedGraph    DerivedKind = "graph"
)

// DerivedScope has a vault-relative path, or an empty path for global work.
type DerivedScope struct {
	Kind DerivedKind
	Path string
}

// DerivedWork is an exact durable obligation. Epoch fences global invalidations
// while Generation prevents an older completion from acknowledging a new edit.
type DerivedWork struct {
	DerivedScope
	Generation int64
	Epoch      int64
	Revision   int64
	Attempt    int
	RetryAt    time.Time
	Ready      bool
}
