package ontology

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"strings"
	"unicode/utf8"
)

type NativeSourceValue struct {
	Kind    noteformat.MetadataValueKind `json:"kind"`
	Scalar  *noteformat.MetadataValue    `json:"scalar,omitempty"`
	Items   []NativeSourceValue          `json:"items,omitempty"`
	Entries []NativeSourceEntry          `json:"entries,omitempty"`
}
type NativeSourceEntry struct {
	Key   noteformat.MetadataValue `json:"key"`
	Value NativeSourceValue        `json:"value"`
}
type NodeSourceContent struct {
	Text           string             `json:"text"`
	MediaType      string             `json:"mediaType"`
	Representation string             `json:"representation"`
	Address        string             `json:"address,omitempty"`
	Value          *NativeSourceValue `json:"value,omitempty"`
}

func (p *PreparedDocument) nativeValue(id noteformat.ValueID) NativeSourceValue {
	value, _ := p.value(id)
	out := NativeSourceValue{Kind: value.Kind}
	if value.Scalar != nil {
		scalar := *value.Scalar
		out.Scalar = &scalar
	}
	for _, item := range value.Items {
		out.Items = append(out.Items, p.nativeValue(item))
	}
	for _, entry := range value.Entries {
		out.Entries = append(out.Entries, NativeSourceEntry{Key: entry.Key, Value: p.nativeValue(entry.Value)})
	}
	return out
}

// SourceContent keeps native values and their text representation distinct from
// Markdown prose. Content reads never require an edit engine.
func (projection *NodeProjection) SourceContent() NodeSourceContent {
	if projection == nil {
		return NodeSourceContent{}
	}
	if projection.Record != nil && projection.Prepared != nil {
		p := projection.Prepared
		owner := p.owners[projection.OwnerKey]
		if owner != nil {
			value := p.nativeValue(owner.Value)
			return NodeSourceContent{Text: string(p.sourceValueJSON(owner.Value)), MediaType: "application/json", Representation: "source_value", Address: owner.Address, Value: &value}
		}
	}
	if projection.Snapshot != nil {
		return NodeSourceContent{Text: nodeProjectionMarkdown(projection), MediaType: "text/markdown", Representation: "markdown"}
	}
	if projection.RootSnapshot != nil {
		return NodeSourceContent{Text: string(projection.RootSnapshot.RawSource), MediaType: "text/plain", Representation: "source_utf8"}
	}
	return NodeSourceContent{}
}
func (projection *NodeProjection) DisplayTitle() string { return nodeProjectionTitle(projection) }

func recordOwnText(projection *NodeProjection, maxBytes int) []string {
	p := projection.Prepared
	if p == nil {
		return nil
	}
	owner := p.owners[projection.OwnerKey]
	if owner == nil {
		return nil
	}
	excluded := map[noteformat.ValueID]bool{}

	for _, field := range projection.Fields {
		for _, value := range field.SourceValues {
			excluded[value.Value] = true
		}
	}
	var leaves []string
	var walk func(noteformat.ValueID)
	walk = func(id noteformat.ValueID) {
		if excluded[id] || (id != owner.Value && p.selectedRecordValues[id]) {
			return
		}
		value, _ := p.value(id)
		if value.Scalar != nil {
			if value.Kind != noteformat.MetadataNull {
				leaves = append(leaves, sourceScalarText(*value.Scalar))
			}
			return
		}
		for _, entry := range value.Entries {
			walk(entry.Value)
		}
		for _, item := range value.Items {
			walk(item)
		}
	}
	walk(owner.Value)
	text := strings.Join(leaves, "\n")
	var out []string
	for len(text) > 0 {
		end := min(maxBytes, len(text))
		for end > 0 && end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		if end == 0 {
			_, end = utf8.DecodeRuneInString(text)
		}
		out = append(out, text[:end])
		text = text[end:]
	}
	return out
}
