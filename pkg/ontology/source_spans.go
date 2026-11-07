package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

type CheckboxState struct {
	Checked    bool
	TokenRange ByteRange
}

type MarkerSpan struct {
	Value string
	Range ByteRange
}

// MarkdownSourceSpan is a schema-free parsed markdown ownership span.
//
// WHY: source spans answer "which bytes does this markdown construct own?"
// before ontology projection answers "does this construct become a typed
// embedded node?" Inline editing and replay need this ownership layer so list
// items, checkbox tokens, nested children, marker tags, and block locators have
// one source of truth independent of schema matching.
type MarkdownSourceSpan struct {
	ID               string
	NotePath         string
	Shape            ontoschema.EmbeddedSourceShape
	Title            string
	ParentID         string
	Children         []*MarkdownSourceSpan
	BlockID          string
	MalformedBlockID string
	MarkerRange      ByteRange
	Markers          []MarkerSpan
	Checkbox         *CheckboxState
	Range            ByteRange
	ContentRange     ByteRange
	OwnContentRanges []ByteRange
	Content          string
}
