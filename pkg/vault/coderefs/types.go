package coderefs

// Docs:
// - [Coderefs (Hub)](docs/hubs/Coderefs (Hub).md)

// RefKind distinguishes between different types of code references.
type RefKind string

const (
	RefKindWikilink RefKind = "wikilink"
	RefKindMention  RefKind = "mention"
	RefKindMdLink   RefKind = "mdlink"
)

// CodeRef represents a reference to a vault note found in source code.
type CodeRef struct {
	SourceFile string  // Vault-relative path to the source file
	Language   string  // Language identifier (go, ts, py, etc.)
	Target     string  // Normalized note path (with .md suffix)
	Fragment   string  // Optional note fragment or block id target without '#'
	RawTarget  string  // Raw author-facing target as written in the link
	Kind       RefKind // Authored syntax: wikilink, Markdown link, or mention
	Line       int     // 1-based source line from the extracted comment offset
	Snippet    string  // Contextual snippet (e.g. the comment line, truncated to ~100 chars)
}
