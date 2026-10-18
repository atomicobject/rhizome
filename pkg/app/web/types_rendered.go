package web

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

type RenderedFileResponse struct {
	Path         string                 `json:"path"`
	Title        string                 `json:"title"`
	ResolvedType string                 `json:"resolvedType,omitempty"`
	Frontmatter  map[string]interface{} `json:"frontmatter,omitempty"`
	Content      string                 `json:"content,omitempty"`
	Rendered     string                 `json:"rendered,omitempty"`
	Links        []ResolvedLink         `json:"links,omitempty"`
	Embeds       []RenderedEmbed        `json:"embeds,omitempty"`
	Sections     []RenderedSection      `json:"sections,omitempty"`
}

type RenderedEmbed struct {
	Target   string `json:"target"`
	Title    string `json:"title,omitempty"`
	Kind     string `json:"kind"`
	Preview  string `json:"preview,omitempty"`
	Resolved bool   `json:"resolved"`
}

type RenderedSection struct {
	ID       string                  `json:"id"`
	Title    string                  `json:"title"`
	Level    ontoschema.SectionLevel `json:"level"`
	Content  string                  `json:"content,omitempty"`
	Children []RenderedSection       `json:"children,omitempty"`
	NotePath string                  `json:"notePath,omitempty"`
	ParentID string                  `json:"parentId,omitempty"`
	Locator  string                  `json:"locator,omitempty"`
	BlockID  string                  `json:"blockId,omitempty"`

	// TypeName is the ontology type this section is bound to, if any.
	// Populated via the web annotation pass when the enclosing note has a
	// resolved type whose schema declares a matching @contains field.
	TypeName string `json:"typeName,omitempty"`

	// FieldName is the immediate schema field that bound this section.
	// FieldPath is the dotted path from the note root (for example
	// `userStories.stories`).
	FieldName      string                    `json:"fieldName,omitempty"`
	FieldPath      string                    `json:"fieldPath,omitempty"`
	FieldList      bool                      `json:"fieldList,omitempty"`
	SectionDisplay ontoschema.SectionDisplay `json:"sectionDisplay,omitempty"`

	// Properties carries the inline `Key:: Value` pairs scoped to this
	// section's own content (descendant ranges subtracted). Keys are the
	// declared field names (not the raw inline keys); list fields are
	// joined with ", ". Populated only when the section has a typed shape.
	Properties      map[string]string `json:"properties,omitempty"`
	IdentifierField string            `json:"identifierField,omitempty"`

	// PreviewTemplate carries the *interpolated* template string — the
	// frontend renders it as markdown directly without re-resolving
	// property values. Empty unless the section type declares @preview.
	PreviewTemplate string `json:"previewTemplate,omitempty"`

	// Collapsed mirrors the @preview directive's `collapsed` argument;
	// the frontend uses it to decide whether the typed card starts open.
	Collapsed bool `json:"collapsed,omitempty"`
}

type StructuralViewResponse struct {
	Root        StructuralNodeResponse  `json:"root"`
	Tabs        []StructuralTabResponse `json:"tabs,omitempty"`
	DefaultView string                  `json:"defaultView,omitempty"`
}

type StructuralTabResponse struct {
	Key       string                   `json:"key"`
	Label     string                   `json:"label"`
	FieldName string                   `json:"fieldName,omitempty"`
	Count     int                      `json:"count"`
	Nodes     []StructuralNodeResponse `json:"nodes,omitempty"`
}

type StructuralNodeResponse struct {
	NodeID          string                    `json:"nodeId"`
	Fragment        string                    `json:"fragment,omitempty"`
	Title           string                    `json:"title"`
	TypeName        string                    `json:"typeName,omitempty"`
	Locator         string                    `json:"locator"`
	NotePath        string                    `json:"notePath"`
	ParentNodeID    string                    `json:"parentNodeId,omitempty"`
	FieldName       string                    `json:"fieldName,omitempty"`
	FieldPath       string                    `json:"fieldPath,omitempty"`
	FieldList       bool                      `json:"fieldList,omitempty"`
	SectionDisplay  ontoschema.SectionDisplay `json:"sectionDisplay,omitempty"`
	Level           ontoschema.SectionLevel   `json:"level,omitempty"`
	Content         string                    `json:"content,omitempty"`
	Properties      map[string]string         `json:"properties,omitempty"`
	IdentifierField string                    `json:"identifierField,omitempty"`
	Preview         string                    `json:"preview,omitempty"`
	PreviewTemplate string                    `json:"previewTemplate,omitempty"`
	Collapsed       bool                      `json:"collapsed,omitempty"`
	Children        []StructuralNodeResponse  `json:"children,omitempty"`
}

type NoteSearchResponse struct {
	Query   string            `json:"query"`
	Offset  int               `json:"offset"`
	Limit   int               `json:"limit"`
	Count   int               `json:"count"`
	Total   int               `json:"total"`
	Matches []NoteSearchMatch `json:"matches,omitempty"`
}

type NoteSearchMatch struct {
	Path         string   `json:"path"`
	Title        string   `json:"title"`
	ResolvedType string   `json:"resolvedType,omitempty"`
	Heading      string   `json:"heading,omitempty"`
	Snippet      string   `json:"snippet,omitempty"`
	Score        float64  `json:"score,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// ResolvedLink represents a parsed markdown or wikilink.
type ResolvedLink struct {
	Target string `json:"target"`
	Text   string `json:"text,omitempty"`
	Kind   string `json:"kind"` // wikilink|markdown
	Anchor string `json:"anchor,omitempty"`
}

// CodeRefLink represents a coderef discovered in code comments.
type CodeRefLink struct {
	Target  string `json:"target"`
	Kind    string `json:"kind"` // wikilink|mention
	Line    int    `json:"line"`
	Snippet string `json:"snippet,omitempty"`
}

// RelatedNote represents a note linked to a code file (coderefs or code anchors).
type RelatedNote struct {
	Path        string `json:"path"`
	Title       string `json:"title,omitempty"`
	Reason      string `json:"reason,omitempty"`      // coderef|codeanchor
	AnchorLabel string `json:"anchorLabel,omitempty"` // for codeanchor matches
}
