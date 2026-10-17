package ontology

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
)

type scannedMarkdownLink struct {
	Line   int
	Detail markdownsyntax.LinkDetail
}

// ScanMarkdownBodyLinks returns wikilinks and markdown links from prose body lines only.
// It skips leading YAML frontmatter, fenced code blocks, and inline code spans while
// preserving original 1-based line numbers from the source content.
func ScanMarkdownBodyLinks(content string, wikiOpts markdownsyntax.WikilinkOptions, mdOpts markdownsyntax.MdLinkOptions) []scannedMarkdownLink {
	masked := []byte(markdownsyntax.MaskProtected(content))
	_, _, bodyStart, _ := markdownsyntax.FrontmatterSpan(content)
	for i := 0; i < bodyStart; i++ {
		if masked[i] != '\n' && masked[i] != '\r' {
			masked[i] = ' '
		}
	}
	lines := strings.Split(string(masked), "\n")
	out := make([]scannedMarkdownLink, 0)
	for idx, line := range lines {
		for _, detail := range markdownsyntax.ScanAllLinks(line, wikiOpts, mdOpts) {
			out = append(out, scannedMarkdownLink{Line: idx + 1, Detail: detail})
		}
	}

	return out
}

// MarkdownProjectionStructuredLinks is the explicit Markdown-only adapter for
// an already-authorized ontology projection. Generic consumers must use
// provider facts or persisted rows; descriptor-only note formats never reach
// this boundary.
func MarkdownProjectionStructuredLinks(projection *NodeProjection) []markdownsyntax.StructuredLink {
	content := markdownProjectionContent(projection)
	if content == "" {
		return nil
	}
	return markdownsyntax.ScanStructuredLinks(content)
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
