package codeanchor

import "context"

// IntelStore exposes read access to the structural intel tables populated during
// code/note ingest. Implementations are optional and may return
// ErrUnsupported if intel queries are not available.
//
// The intent is to decouple semantic indexing from the hot ingest path: the
// semantic syncer can read stable intel rows and generate embeddings
// incrementally.
type IntelStore interface {
	// IntelAnchors returns all code anchors currently stored.
	IntelAnchors(ctx context.Context) ([]IntelAnchor, error)

	// IntelDocSections returns all doc sections discovered across notes.
	IntelDocSections(ctx context.Context) ([]IntelDocSection, error)

	// IntelEdges returns all edges between intel items.
	IntelEdges(ctx context.Context) ([]IntelEdge, error)
}
