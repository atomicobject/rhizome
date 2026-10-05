package codeanchor

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"
)

// extractMarkdownIntelDocSections splits an already-authorized Markdown source
// into sections and produces intel doc sections + FTS rows. Other providers
// must supply their own section projection; this seam never treats their bytes
// as Markdown.
func extractMarkdownIntelDocSections(intelPath string, content string) ([]IntelDocSection, []IntelEdge, []IntelFTSRow) {
	sections := flattenParsedSections(content, sectionparse.Parse(intelPath, content))

	// Fallback: whole-file single section when no headings.
	if len(sections) == 0 {
		lines := strings.Split(content, "\n")
		sections = append(sections, sec{
			title:     filepath.Base(intelPath),
			level:     1,
			startLine: 1,
			endLine:   len(lines),
			startByte: 0,
			endByte:   len(content),
		})
	}

	var outSections []IntelDocSection
	var ftsRows []IntelFTSRow
	for _, s := range sections {
		if s.endByte < s.startByte {
			s.endByte = s.startByte
		}
		if s.endLine < s.startLine {
			s.endLine = s.startLine
		}
		slug := slugifyHeading(s.title)
		sectionID := intelDocSectionID(intelPath, slug, s.startByte)
		body := substringBytes(content, s.startByte, s.endByte)
		outSections = append(outSections, IntelDocSection{
			SectionID:   sectionID,
			Path:        intelPath,
			Title:       s.title,
			Level:       int64(s.level),
			StartByte:   int64(s.startByte),
			EndByte:     int64(s.endByte),
			Content:     body,
			Fingerprint: sha256Hex(body),
			// UpdatedAt set by store if zero
		})
		ftsRows = append(ftsRows, IntelFTSRow{
			ItemType: "doc_section",
			ItemID:   sectionID,
			Path:     intelPath,
			Title:    s.title,
			Body:     body,
		})
	}

	return outSections, nil, ftsRows
}

type sec struct {
	title     string
	level     int
	startLine int
	endLine   int
	startByte int
	endByte   int
}

func flattenParsedSections(content string, nodes []*sectionparse.Node) []sec {
	var out []sec
	var walk func([]*sectionparse.Node)
	walk = func(nodes []*sectionparse.Node) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			out = append(out, sec{
				title:     node.Title,
				level:     node.Level,
				startLine: lineNumberAtByte(content, node.StartByte),
				endLine:   lineNumberAtByte(content, node.EndByte),
				startByte: node.StartByte,
				endByte:   node.EndByte,
			})
			walk(node.Children)
		}
	}
	walk(nodes)
	return out
}

func lineNumberAtByte(content string, offset int) int {
	if offset <= 0 {
		return 1
	}
	if offset > len(content) {
		offset = len(content)
	}
	return strings.Count(content[:offset], "\n") + 1
}

func substringBytes(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if start >= end {
		return ""
	}
	return s[start:end]
}
