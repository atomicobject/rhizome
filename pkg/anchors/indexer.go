package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
)

var defaultLangExts = map[Lang][]string{
	LangPy:  {".py"},
	LangGo:  {".go"},
	LangTS:  codefile.TypeScriptJavaScriptExtensions(),
	LangCs:  {".cs"},
	LangPhp: {".php"},
}

// LangForPath returns the language a code file is indexed as, from its extension.
func LangForPath(path string) (Lang, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for lang, exts := range defaultLangExts {
		for _, candidate := range exts {
			if candidate == ext {
				return lang, true
			}
		}
	}
	return "", false
}

// IndexerVersion is a hash/version string that changes when indexer logic changes.
// Bump this when indexer output format or behavior changes significantly.
//
// v1.16.0 refreshes code artifacts and external evidence left stale by
// older live publication paths that advanced freshness before later writes.
// v1.17.0 renames Python modules under named source roots, which were never
// stripped (src.pkg.mod became pkg.mod), and stops stripping a root matched
// inside another folder name (tools/mysrc/pkg/mod stays tools.mysrc.pkg.mod).
const IndexerVersion = "v1.17.0"

// NoteIndexerVersion is a hash/version string that changes when note indexing logic changes.
// Bump this when note parsing or section extraction behavior changes.
// v1.1.0 rebuilds note anchors with the shared exact frontmatter boundary.
const NoteIndexerVersion = "v1.1.0"

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
