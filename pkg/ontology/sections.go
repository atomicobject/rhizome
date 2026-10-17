package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"
)

type SectionNode struct {
	ID               string
	NotePath         string
	Title            string
	Level            ontoschema.SectionLevel
	BlockID          string
	MalformedBlockID string
	StartByte        int
	EndByte          int
	Content          string
	Children         []*SectionNode
}

// CanonicalSectionID returns the source identity for an unanchored Markdown
// heading. The byte offset is part of the identity so duplicate headings stay
// addressable independently.
func CanonicalSectionID(notePath, title string, startByte int) string {
	return fmt.Sprintf("%s#%s-%d", notePath, markdownsyntax.HeadingSlug(title), startByte)
}

func ParseSections(notePath, content string) []*SectionNode {
	return convertParsedSections(sectionparse.Parse(notePath, content))
}

func convertParsedSections(nodes []*sectionparse.Node) []*SectionNode {
	out := make([]*SectionNode, 0, len(nodes))
	for _, parsed := range nodes {
		if parsed == nil {
			continue
		}
		node := &SectionNode{
			ID:               parsed.ID,
			NotePath:         parsed.NotePath,
			Title:            parsed.Title,
			Level:            headingLevel(parsed.Level),
			BlockID:          parsed.BlockID,
			MalformedBlockID: parsed.MalformedBlockID,
			StartByte:        parsed.StartByte,
			EndByte:          parsed.EndByte,
			Content:          parsed.Content,
			Children:         convertParsedSections(parsed.Children),
		}
		out = append(out, node)
	}
	return out
}

func FindMatchingSections(nodes []*SectionNode, level ontoschema.SectionLevel, heading string) []*SectionNode {
	heading = strings.TrimSpace(heading)
	if len(nodes) == 0 || heading == "" || level == "" {
		return nil
	}
	out := make([]*SectionNode, 0)
	var walk func([]*SectionNode)
	walk = func(current []*SectionNode) {
		for _, node := range current {
			if node == nil {
				continue
			}
			if node.Level == level && strings.TrimSpace(node.Title) == heading {
				out = append(out, node)
			}
			walk(node.Children)
		}
	}
	walk(nodes)
	return out
}

// UnwrapSingleH1SectionRoot returns the children of a lone top-level H1 when
// present. Notes commonly use `# Title` as a wrapper around the real structure,
// and note-scoped section matching should treat those child headings as the
// direct roots so `[Section!] @contains(level: H2)` still binds as authors
// expect. Nested matching should continue to pass explicit child slices.
func UnwrapSingleH1SectionRoot(nodes []*SectionNode) []*SectionNode {
	if len(nodes) != 1 {
		return nodes
	}
	only := nodes[0]
	if only == nil || only.Level != ontoschema.SectionLevelH1 || len(only.Children) == 0 {
		return nodes
	}
	return only.Children
}

func headingLevel(n int) ontoschema.SectionLevel {
	switch n {
	case 1:
		return ontoschema.SectionLevelH1
	case 2:
		return ontoschema.SectionLevelH2
	case 3:
		return ontoschema.SectionLevelH3
	case 4:
		return ontoschema.SectionLevelH4
	case 5:
		return ontoschema.SectionLevelH5
	case 6:
		return ontoschema.SectionLevelH6
	default:
		return ""
	}
}

func sectionLevelInt(level ontoschema.SectionLevel) int {
	switch level {
	case ontoschema.SectionLevelH1:
		return 1
	case ontoschema.SectionLevelH2:
		return 2
	case ontoschema.SectionLevelH3:
		return 3
	case ontoschema.SectionLevelH4:
		return 4
	case ontoschema.SectionLevelH5:
		return 5
	case ontoschema.SectionLevelH6:
		return 6
	default:
		return 0
	}
}

func SectionBody(node *SectionNode) string {
	if node == nil {
		return ""
	}
	if idx := strings.IndexByte(node.Content, '\n'); idx >= 0 {
		return strings.TrimSpace(node.Content[idx+1:])
	}
	return ""
}

// SectionOwnContent returns the section's body with descendant section ranges
// removed. It keeps the text before the first child, between consecutive
// children, and after the last child — i.e. everything that belongs to this
// section but not to any of its subsections. The leading heading line is
// stripped, matching SectionBody's contract.
//
// This is the right input for scoped inline-property extraction
// (Dataview-style `Key:: Value`): authors can sprinkle inline props anywhere
// inside a section and they stay attached to that section regardless of
// whether subsections trail after them.
func SectionOwnContent(node *SectionNode) string {
	return sectionOwnContent(node, true)
}

func sectionOwnContent(node *SectionNode, stripBlockIDs bool) string {
	if node == nil {
		return ""
	}
	content := node.Content
	// Drop the heading line so callers receive only the body text.
	bodyStart := 0
	if idx := strings.IndexByte(content, '\n'); idx >= 0 {
		bodyStart = idx + 1
	} else {
		return ""
	}
	if len(node.Children) == 0 {
		out := strings.TrimSpace(content[bodyStart:])
		if stripBlockIDs {
			out = stripStandaloneBlockIDLines(out)
		}
		return strings.TrimSpace(out)
	}
	// Direct children's [StartByte, EndByte] ranges already cover their entire
	// subtrees, so subtracting them is sufficient — no recursive walk needed.
	var b strings.Builder
	cursor := bodyStart
	for _, child := range node.Children {
		if child == nil {
			continue
		}
		childStart := child.StartByte - node.StartByte
		childEnd := child.EndByte - node.StartByte
		if childStart < cursor {
			childStart = cursor
		}
		if childStart > len(content) {
			childStart = len(content)
		}
		if childEnd < childStart {
			childEnd = childStart
		}
		if childEnd > len(content) {
			childEnd = len(content)
		}
		if childStart > cursor {
			b.WriteString(content[cursor:childStart])
		}
		cursor = childEnd
	}
	if cursor < len(content) {
		b.WriteString(content[cursor:])
	}
	out := strings.TrimSpace(b.String())
	if stripBlockIDs {
		out = stripStandaloneBlockIDLines(out)
	}
	return strings.TrimSpace(out)
}

func stripStandaloneBlockIDLines(content string) string {
	if strings.TrimSpace(content) == "" {
		return strings.TrimSpace(content)
	}
	lines := strings.Split(content, "\n")
	filtered := make([]string, 0, len(lines))
	fence := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if nextFence, toggled := markdownsyntax.FenceDelimiter(line, fence); toggled {
			fence = nextFence
			filtered = append(filtered, raw)
			continue
		}
		if fence != "" {
			filtered = append(filtered, raw)
			continue
		}
		if len(line) >= 2 && strings.HasPrefix(line, "^") {
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "^"))
			if candidate != "" && markdownsyntax.ValidBlockID(candidate) {
				continue
			}
		}
		filtered = append(filtered, raw)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}
