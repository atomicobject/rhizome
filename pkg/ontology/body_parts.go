package ontology

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
)

const defaultNodeBodyMaxBytes = 12000

// NodeBodyParts returns the source body slices used for ontology-node embeddings.
func NodeBodyParts(projection *NodeProjection, maxBytes int) []string {
	if maxBytes <= 0 {
		maxBytes = defaultNodeBodyMaxBytes
	}
	return nodeOwnMarkdownParts(projection, maxBytes)
}

type ontologyBodyPart struct {
	Text string
}

func nodeOwnMarkdownParts(projection *NodeProjection, maxBytes int) []string {
	if projection == nil || projection.Snapshot == nil {
		return nil
	}
	if IsFallbackNoteProjection(projection) {
		return FallbackNoteBodyParts(projection, maxBytes)
	}
	if IsFallbackSectionProjection(projection) {
		return FallbackSectionBodyParts(projection, maxBytes)
	}
	parts := nodeBodyOwnMarkdownParts(projection, maxBytes)
	if len(parts) > 0 || projection.Ref.Kind == NodeKindNote || projection.Ref.Kind == NodeKindSection || projection.Ref.Kind == NodeKindEmbedded {
		return parts
	}
	return splitOntologyBody(projection.Snapshot.Content, maxBytes)
}

func nodeBodyOwnMarkdownParts(projection *NodeProjection, maxBytes int) []string {
	blocks := BuildNodeBody(projection, nil)
	var rawParts []ontologyBodyPart
	content := projection.Snapshot.Content
	for _, block := range blocks {
		switch block.Kind {
		case NodeBodyBlockKindNarrative:
			rawParts = append(rawParts, ontologyBodyPart{Text: block.Markdown})
		case NodeBodyBlockKindInlineField:
			if block.Range.Start >= 0 && block.Range.End >= block.Range.Start && block.Range.End <= len(content) {
				rawParts = append(rawParts, ontologyBodyPart{Text: content[block.Range.Start:block.Range.End]})
			}
		}
	}
	var parts []string
	for _, body := range packOntologyBodyParts(rawParts, maxBytes) {
		parts = append(parts, splitOntologyBody(body, maxBytes)...)
	}
	return parts
}

func packOntologyBodyParts(parts []ontologyBodyPart, maxBytes int) []string {
	if maxBytes <= 0 {
		maxBytes = defaultNodeBodyMaxBytes
	}
	var out, current []string
	currentLen := 0
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.TrimSpace(strings.Join(current, "\n\n")))
		}
		current = nil
		currentLen = 0
	}
	for _, part := range parts {
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if len(text) > maxBytes {
			flush()
			out = append(out, splitOntologyBody(text, maxBytes)...)
			continue
		}
		nextLen := currentLen + len(text)
		if len(current) > 0 {
			nextLen += 2
		}
		if nextLen > maxBytes {
			flush()
			nextLen = len(text)
		}
		current = append(current, text)
		currentLen = nextLen
	}
	flush()
	return out
}

func splitOntologyBody(body string, maxBytes int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = defaultNodeBodyMaxBytes
	}
	if len(body) <= maxBytes {
		return []string{body}
	}
	parts := splitMarkdownByBoundaries(body, maxBytes)
	if len(parts) == 0 {
		parts = markdownsyntax.SplitTextWithOverlap(body, maxBytes, 300)
	}
	return parts
}

func splitMarkdownByBoundaries(body string, maxBytes int) []string {
	var parts, current []string
	currentLen := 0
	inFence := false
	flush := func() {
		if len(current) > 0 {
			parts = append(parts, strings.TrimSpace(strings.Join(current, "\n\n")))
		}
		current = nil
		currentLen = 0
	}
	for _, paragraph := range markdownParagraphs(body) {
		text := strings.TrimSpace(paragraph)
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
				inFence = !inFence
			}
		}
		if len(text) > maxBytes {
			flush()
			parts = append(parts, markdownsyntax.SplitTextWithOverlap(text, maxBytes, 300)...)
			continue
		}
		nextLen := currentLen + len(text)
		if len(current) > 0 {
			nextLen += 2
		}
		if !inFence && nextLen > maxBytes {
			flush()
			nextLen = len(text)
		}
		current = append(current, text)
		currentLen = nextLen
	}
	flush()
	return parts
}

func markdownParagraphs(body string) []string {
	lines := strings.Split(body, "\n")
	var paragraphs, current []string
	inFence := false
	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, strings.Join(current, "\n"))
		}
		current = nil
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		fence := strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~")
		if trim == "" && !inFence {
			flush()
			continue
		}
		current = append(current, line)
		if fence {
			inFence = !inFence
		}
	}
	flush()
	return paragraphs
}
