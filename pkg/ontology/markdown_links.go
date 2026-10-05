package ontology

import (
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var inlineCodePattern = regexp.MustCompile("`[^`]+`")

type scannedMarkdownLink struct {
	Line   int
	Detail obsidian.LinkDetail
}

// ScanMarkdownBodyLinks returns wikilinks and markdown links from prose body lines only.
// It skips leading YAML frontmatter, fenced code blocks, and inline code spans while
// preserving original 1-based line numbers from the source content.
func ScanMarkdownBodyLinks(content string, wikiOpts obsidian.WikilinkOptions, mdOpts obsidian.MdLinkOptions) []scannedMarkdownLink {
	lines := strings.Split(content, "\n")
	startLine := markdownBodyStartLine(lines)
	inFence := false
	fenceDelimiter := ""
	out := make([]scannedMarkdownLink, 0)
	for idx := startLine; idx < len(lines); idx++ {
		rawLine := strings.TrimRight(lines[idx], "\r")
		line := strings.TrimLeft(rawLine, " \t")
		switch {
		case strings.HasPrefix(line, "```"):
			if inFence && fenceDelimiter == "```" {
				inFence = false
				fenceDelimiter = ""
			} else if !inFence {
				inFence = true
				fenceDelimiter = "```"
			}
			continue
		case strings.HasPrefix(line, "~~~"):
			if inFence && fenceDelimiter == "~~~" {
				inFence = false
				fenceDelimiter = ""
			} else if !inFence {
				inFence = true
				fenceDelimiter = "~~~"
			}
			continue
		}
		if inFence {
			continue
		}
		safeLine := inlineCodePattern.ReplaceAllString(rawLine, " ")
		for _, detail := range obsidian.ScanAllLinks(safeLine, wikiOpts, mdOpts) {
			out = append(out, scannedMarkdownLink{Line: idx + 1, Detail: detail})
		}
	}
	return out
}

// MarkdownProjectionStructuredLinks is the explicit Markdown-only adapter for
// an already-authorized ontology projection. Generic consumers must use
// provider facts or persisted rows; descriptor-only note formats never reach
// this boundary.
func MarkdownProjectionStructuredLinks(projection *NodeProjection) []obsidian.StructuredLink {
	content := markdownProjectionContent(projection)
	if content == "" {
		return nil
	}
	return obsidian.ScanStructuredLinks(content)
}

func markdownProjectionContent(projection *NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if projection.Ref.Kind == NodeKindNote {
		return projection.Snapshot.Content
	}
	if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
		return node.Content
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return span.Content
	}
	return ""
}

func markdownBodyStartLine(lines []string) int {
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return 0
	}
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(strings.TrimRight(lines[idx], "\r")) == "---" {
			return idx + 1
		}
	}
	return 0
}
