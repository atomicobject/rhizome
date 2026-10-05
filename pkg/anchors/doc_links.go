package codeanchor

import "context"

// Docs: [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

// DocLink represents a link between code/notes and code anchors/notes, stored in doc_links.
type DocLink struct {
	SrcType   string // code|note
	SrcPath   string
	SrcID     string // optional anchor_id|section_id
	DstKind   string // note|anchor|section
	DstID     string // optional
	DstPath   string
	Lang      string
	Label     string
	Snippet   string
	MetaJSON  string
	UpdatedAt int64
}

// DocLinkStore persists doc_links.
type DocLinkStore interface {
	ReplaceDocLinksForPath(ctx context.Context, srcPath string, links []DocLink) error
	DocLinksForAnchor(ctx context.Context, anchorID string, limit int) ([]DocLink, error)
	DocLinksForNote(ctx context.Context, notePath string, limit int) ([]DocLink, error)
	DocLinksFromCodePath(ctx context.Context, srcPath string, limit int) ([]DocLink, error)
	DeleteDocLinksByPath(ctx context.Context, srcPath string) error
}

// CodeDocLinker produces doc links during ingest.
type CodeDocLinker interface {
	LinksForCode(path string, content []byte, fc FileContext) []DocLink
	LinksForNote(note Note) []DocLink
}
