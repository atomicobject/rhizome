package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)

import "github.com/atomicobject/rhizome/pkg/paths"

// IndexerVersion is a hash/version string that changes when indexer logic changes.
// Bump this when indexer output format or behavior changes significantly.
//
// v1.16.0 refreshes code artifacts and external evidence left stale by
// older live publication paths that advanced freshness before later writes.
const IndexerVersion = "v1.16.0"

// NoteIndexerVersion is a hash/version string that changes when note indexing logic changes.
// Bump this when note parsing or section extraction behavior changes.
const NoteIndexerVersion = "v1.0.0"

// ReverseIndexVersion is a hash/version string that changes when reverse-index
// persistence or lookup behavior changes.
const ReverseIndexVersion = "v1.3.0"

// LanguageIndexer extracts FileSummary data for a single language.
type LanguageIndexer interface {
	IndexFile(content []byte, path paths.CodePathRef) (FileSummary, error)
	Lang() Lang
}

// ModuleMetadataInvalidator lets lifecycle owners invalidate language-specific
// module-resolution metadata after filesystem changes.
type ModuleMetadataInvalidator interface {
	InvalidateModuleMetadata(path string)
}

// ReverseIndexFallbackProvider supplies language-specific reverse-index fallbacks.
// Implementations should return ref-only fallbacks for def-delta rebuilds.
type ReverseIndexFallbackProvider interface {
	ReverseIndexFallbacks(deltas DefDeltas) []ReverseIndexFallback
}

// ReverseIndexFallback provides a fallback ref and optional path filter.
type ReverseIndexFallback struct {
	Ref        SymbolRef
	PathFilter func(path string) bool
}
