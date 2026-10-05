package actions

import (
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
)

func xmlEscapeAttr(s string) string {
	// Minimal, XML-ish escaping for attribute values.
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func embeddedFileAttributes(kind, path string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "file"
	}
	return fmt.Sprintf(` kind="%s" path="%s"`, xmlEscapeAttr(kind), xmlEscapeAttr(strings.TrimSpace(path)))
}

func renderEmbeddedFileXML(kind, path, markdown string, truncated bool) string {
	return renderContextXML("doc", embeddedFileAttributes(kind, path), "markdown", markdown, truncated)
}

func renderEmbeddedFileXMLWithMeta(kind, path, title, source, markdown string, truncated bool) string {
	attrs := embeddedFileAttributes(kind, path)
	if title = strings.TrimSpace(title); title != "" {
		attrs += fmt.Sprintf(` title="%s"`, xmlEscapeAttr(title))
	}
	if source = strings.TrimSpace(source); source != "" {
		attrs += fmt.Sprintf(` source="%s"`, xmlEscapeAttr(source))
	}
	return renderContextXML("doc", attrs, "markdown", markdown, truncated)
}

func renderOntologyNodeContextXML(node OntologyNodeContext, budget int) string {
	attrs := ` kind="ontology-node"`
	if node.SourceLocator != "" {
		attrs += fmt.Sprintf(` locator="%s"`, xmlEscapeAttr(node.SourceLocator))
	}
	if node.TypeName != "" {
		attrs += fmt.Sprintf(` type="%s"`, xmlEscapeAttr(node.TypeName))
	}
	if node.Title != "" {
		attrs += fmt.Sprintf(` title="%s"`, xmlEscapeAttr(node.Title))
	}
	if node.Wikilink != "" {
		attrs += fmt.Sprintf(` wikilink="%s"`, xmlEscapeAttr(node.Wikilink))
	}
	if node.Status != "" {
		attrs += fmt.Sprintf(` status="%s"`, xmlEscapeAttr(string(node.Status)))
	}
	if node.Line > 0 {
		attrs += fmt.Sprintf(` line="%d"`, node.Line)
	}
	body := strings.TrimSpace(node.Content)
	if body == "" {
		body = strings.TrimSpace(node.Snippet)
	}
	body, truncated := contextpack.TrimMarkdown(body, budget)
	return renderContextXML("doc", attrs, "markdown", body, truncated)
}

func renderEmbeddedFileXMLWithDepth(kind, path, markdown string, depth int, truncated bool) string {
	attrs := embeddedFileAttributes(kind, path) + fmt.Sprintf(` depth="%d"`, depth)
	return renderContextXML("doc", attrs, "markdown", markdown, truncated)
}

func renderRationaleXML(path string, rationale codeanchor.Rationale, content string, truncated bool) string {
	attrs := fmt.Sprintf(` kind="%s" file="%s" line="%d"`, xmlEscapeAttr(string(rationale.Kind)), xmlEscapeAttr(strings.TrimSpace(path)), rationale.StartLine)
	if rationale.SymbolFQN != "" {
		attrs += fmt.Sprintf(` symbol="%s"`, xmlEscapeAttr(rationale.SymbolFQN))
	}
	return renderContextXML("rationale", attrs, "text", content, truncated)
}

func renderContextXML(tag, attrs, language, content string, truncated bool) string {
	if truncated {
		attrs += ` truncated="true"`
	}
	body := strings.TrimSpace(content)
	if body == "" {
		return fmt.Sprintf("<%s%s />", tag, attrs)
	}
	return fmt.Sprintf("<%s%s>\n```%s\n%s\n```\n</%s>", tag, attrs, language, body, tag)
}
