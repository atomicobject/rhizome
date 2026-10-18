package ontology

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"
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

func ParseMarkdownListSourceSpans(notePath, content string) []*MarkdownSourceSpan {
	lines := strings.Split(content, "\n")
	lineOffsets := make([]int, len(lines)+1)
	for i := 0; i < len(lines); i++ {
		lineOffsets[i+1] = lineOffsets[i] + len(lines[i]) + 1
	}

	frontmatter := frontmatterRange(content)
	raw := make([]rawMarkdownItem, 0)
	fence := ""
	for i, line := range lines {
		start := lineOffsets[i]
		if frontmatter.Valid(len(content)) && start < frontmatter.End {
			continue
		}
		trim := strings.TrimSpace(line)
		if nextFence, toggled := markdownsyntax.FenceDelimiter(trim, fence); toggled {
			fence = nextFence
			continue
		}
		if fence != "" {
			continue
		}
		item, ok := parseListItemStart(line, start)
		if !ok {
			continue
		}
		item.lineIndex = i
		raw = append(raw, item)
	}
	assignListParents(raw)

	out := make([]*MarkdownSourceSpan, 0, len(raw))
	byIndex := make(map[int]*MarkdownSourceSpan, len(raw))
	for idx := range raw {
		raw[idx].sourceEndByte = listItemEndByte(raw, idx, lines, lineOffsets, len(content))
		span := markdownSourceSpanFromRaw(notePath, content, raw[idx])
		out = append(out, span)
		byIndex[idx] = span
	}
	for idx := range raw {
		parentIdx := raw[idx].parentIndex
		if parentIdx < 0 {
			continue
		}
		child := byIndex[idx]
		parent := byIndex[parentIdx]
		if child == nil || parent == nil {
			continue
		}
		child.ParentID = parent.ID
		parent.Children = append(parent.Children, child)
	}
	for idx := range raw {
		span := byIndex[idx]
		if span == nil {
			continue
		}
		childRanges := make([]ByteRange, 0, len(span.Children))
		for _, child := range span.Children {
			if child == nil {
				continue
			}
			childRanges = append(childRanges, child.Range)
		}
		span.OwnContentRanges = subtractByteRanges(span.ContentRange, childRanges)
	}
	return out
}

func sectionMarkdownSourceSpans(nodes []*SectionNode) []*MarkdownSourceSpan {
	out := make([]*MarkdownSourceSpan, 0)
	byID := make(map[string]*MarkdownSourceSpan)
	var walk func([]*SectionNode, string)
	walk = func(current []*SectionNode, parentID string) {
		for _, section := range current {
			if section == nil {
				continue
			}
			span := &MarkdownSourceSpan{
				ID:               section.ID,
				NotePath:         section.NotePath,
				Shape:            ontoschema.EmbeddedSourceShapeSection,
				Title:            section.Title,
				ParentID:         parentID,
				BlockID:          section.BlockID,
				MalformedBlockID: section.MalformedBlockID,
				Range:            ByteRange{Start: section.StartByte, End: section.EndByte},
				ContentRange:     ByteRange{Start: section.StartByte, End: section.EndByte},
				Content:          section.Content,
			}
			out = append(out, span)
			byID[span.ID] = span
			if parent := byID[parentID]; parent != nil {
				parent.Children = append(parent.Children, span)
			}
			walk(section.Children, section.ID)
			childRanges := make([]ByteRange, 0, len(span.Children))
			for _, child := range span.Children {
				if child == nil {
					continue
				}
				childRanges = append(childRanges, child.Range)
			}
			span.OwnContentRanges = subtractByteRanges(span.ContentRange, childRanges)
		}
	}
	walk(nodes, "")
	return out
}
