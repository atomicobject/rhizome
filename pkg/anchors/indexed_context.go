package codeanchor

import (
	"context"
	"errors"
)

var ErrIndexedGraphSummaryMissing = errors.New("indexed graph summary is missing")

// IndexedCodeNoteLink is the narrow persisted code-to-note relationship used
// by bounded startup-context readers. Paths are vault-root-relative index keys.
type IndexedCodeNoteLink struct {
	CodePath  string `json:"codePath"`
	NotePath  string `json:"notePath"`
	Lang      string `json:"lang,omitempty"`
	Label     string `json:"label,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// IndexedRationale is the bounded rationale projection used by startup
// context. Fingerprints and write-time metadata are intentionally omitted.
type IndexedRationale struct {
	ID        string        `json:"id"`
	CodePath  string        `json:"codePath"`
	SymbolFQN string        `json:"symbolFqn,omitempty"`
	Kind      RationaleKind `json:"kind"`
	Content   string        `json:"content"`
	StartLine int64         `json:"startLine"`
	EndLine   int64         `json:"endLine"`
}

// IndexedCodeEdge is a file-level summary of resolved code-intelligence
// relationships. Weight is the number of symbol-level edges in the group.
type IndexedCodeEdge struct {
	SourcePath string `json:"sourcePath"`
	TargetPath string `json:"targetPath"`
	Kind       string `json:"kind"`
	Weight     int    `json:"weight"`
}

// IndexedNoteMetadata is the bounded raw-note identity available to startup
// without reopening Markdown. Content remains filesystem-owned and omitted.
type IndexedNoteMetadata struct {
	Path      string `json:"path"`
	Title     string `json:"title,omitempty"`
	Mtime     int64  `json:"mtime,omitempty"`
	Size      int64  `json:"size,omitempty"`
	IndexedAt int64  `json:"indexedAt,omitempty"`
}

type IndexedGraphCommunity struct {
	ID            string `json:"id"`
	DocumentCount int    `json:"documentCount"`
	NoteCount     int    `json:"noteCount"`
	TopPath       string `json:"topPath,omitempty"`
}

type IndexedGraphSummary struct {
	DocumentCount int                     `json:"documentCount"`
	NoteCount     int                     `json:"noteCount"`
	OrphanCount   int                     `json:"orphanCount"`
	Communities   []IndexedGraphCommunity `json:"communities,omitempty"`
}

type IndexedOntologyTypeCount struct {
	TypeName string   `json:"typeName"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

// IndexedOntologySummary is a bounded summary of public, schema-backed note
// types. The ontology index publishes authored resolved note types to
// ontology_note_types; internal fallback catalog nodes are not part of it.
type IndexedOntologySummary struct {
	Available              bool                       `json:"available"`
	Ready                  bool                       `json:"ready"`
	SchemaHash             string                     `json:"schemaHash,omitempty"`
	MaterializationVersion int                        `json:"materializationVersion,omitempty"`
	LoadedAt               int64                      `json:"loadedAt,omitempty"`
	TotalNotes             int                        `json:"totalNotes"`
	TypedNotes             int                        `json:"typedNotes"`
	UntypedNotes           int                        `json:"untypedNotes"`
	TypeCounts             []IndexedOntologyTypeCount `json:"typeCounts,omitempty"`
}

// IndexedContextStore exposes target-scoped reads only. Implementations must
// scope and limit in the durable store rather than loading the complete graph.
type IndexedContextStore interface {
	IndexedCodeNoteLinksForFile(ctx context.Context, codePath string, limit int) ([]IndexedCodeNoteLink, error)
	IndexedCodeNoteLinksForSubtree(ctx context.Context, subtree string, limit int) ([]IndexedCodeNoteLink, error)
	IndexedRationaleForFile(ctx context.Context, codePath string, limit int) ([]IndexedRationale, error)
	IndexedRationaleForSubtree(ctx context.Context, subtree string, limit int) ([]IndexedRationale, error)
	IndexedCodeEdgesForFile(ctx context.Context, codePath string, limit int) ([]IndexedCodeEdge, error)
	IndexedCodeEdgesForSubtree(ctx context.Context, subtree string, limit int) ([]IndexedCodeEdge, error)
	IndexedNoteMetadataForPath(ctx context.Context, notePath string) (*IndexedNoteMetadata, error)
	IndexedGraphSummary(ctx context.Context, communityLimit int) (IndexedGraphSummary, error)
	IndexedOntologySummary(ctx context.Context, maxTypes, maxExamples int) (IndexedOntologySummary, error)
}
