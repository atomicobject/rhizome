package schema

// Subject contains provider-current facts used by ontology selectors.
// Tags are authoritative, including an empty slice for formats without tags.
type Subject struct {
	Path        string
	Title       string
	Tags        []string
	Frontmatter map[string]any
	Inline      map[string][]string
}
