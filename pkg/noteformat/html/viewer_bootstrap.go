package html

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

// ViewerBootstrapOffset returns a proven byte boundary at which an ephemeral
// viewer bootstrap runs before the first authored executable script. The
// returned offset addresses the original bytes; callers must never persist the
// injected document.
func ViewerBootstrapOffset(source []byte) (int, error) {
	if !utf8.Valid(source) {
		return 0, errors.New("HTML viewer source is not valid UTF-8")
	}
	document := parse(source)
	if err := validateViewerStructure(document); err != nil {
		return 0, err
	}
	for _, token := range document.tokens {
		if token.typ != xhtml.StartTagToken && token.typ != xhtml.SelfClosingTagToken {
			continue
		}
		if token.tag != "script" || !executableScript(token.attrs) {
			continue
		}
		element := authoredElementForToken(document.root, token.index)
		if element == nil {
			return 0, errors.New("HTML viewer bootstrap cannot correlate the first authored script")
		}
		if excludedViewerScript(element) {
			continue
		}
		return token.start, nil
	}
	return viewerDocumentPrefixEnd(source, document.tokens), nil
}

func validateViewerStructure(document parsedDocument) error {
	if authoredElementCount(document.root, "head") > 1 || authoredElementCount(document.root, "body") > 1 {
		return errors.New("HTML viewer bootstrap placement is ambiguous")
	}
	if authoredHeadBodyTokenIsInsideTemplate(document.tokens) {
		return errors.New("HTML viewer bootstrap placement is ambiguous")
	}
	for _, tag := range []string{"head", "body"} {
		var invalid *node
		walkElements(document.root, func(element *node) {
			if invalid != nil || !element.authored || element.tag != tag {
				return
			}
			if element.parent == nil || (element.parent.tag != "#document" && element.parent.tag != "html") {
				invalid = element
			}
		})
		if invalid != nil {
			return errors.New("HTML viewer bootstrap placement is ambiguous")
		}
	}
	return nil
}

func authoredHeadBodyTokenIsInsideTemplate(tokens []htmlToken) bool {
	stack := make([]string, 0)
	for _, token := range tokens {
		switch token.typ {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			if (token.tag == "head" || token.tag == "body") && containsTag(stack, "template") {
				return true
			}
			if token.typ == xhtml.StartTagToken && !voidElement(token.tag) {
				stack = append(stack, token.tag)
			}
		case xhtml.EndTagToken:
			for index := len(stack) - 1; index >= 0; index-- {
				if stack[index] == token.tag {
					stack = stack[:index]
					break
				}
			}
		}
	}
	return false
}

func containsTag(stack []string, tag string) bool {
	for _, current := range stack {
		if current == tag {
			return true
		}
	}
	return false
}

func executableScript(attrs []attribute) bool {
	typeAttr, ok := firstAttribute(attrs, "type")
	if !ok {
		return true
	}
	typeValue := strings.ToLower(strings.TrimSpace(typeAttr.value))
	if typeValue == "" {
		return true
	}
	switch typeValue {
	case "module", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript", "text/jscript", "text/livescript", "application/x-javascript":
		return true
	default:
		return false
	}
}

func firstAttribute(attrs []attribute, name string) (attribute, bool) {
	for _, attr := range attrs {
		if attr.name == name {
			return attr, true
		}
	}
	return attribute{}, false
}

func authoredElementForToken(root *node, tokenIndex int) *node {
	var result *node
	walkElements(root, func(element *node) {
		if result == nil && element.authored && element.opening != nil && element.opening.index == tokenIndex {
			result = element
		}
	})
	return result
}

func excludedViewerScript(element *node) bool {
	for current := element.parent; current != nil && current.tag != "#document"; current = current.parent {
		if current.tag == "template" || current.tag == "noscript" {
			return true
		}
	}
	return false
}

func walkElements(root *node, visit func(*node)) {
	if root == nil {
		return
	}
	if root.kind == elementNode {
		visit(root)
	}
	for _, child := range root.children {
		if child.kind == elementNode {
			walkElements(child, visit)
		}
	}
}

func authoredElementCount(root *node, tag string) int {
	count := 0
	walkElements(root, func(element *node) {
		if element.authored && element.tag == tag {
			count++
		}
	})
	return count
}

func viewerDocumentPrefixEnd(source []byte, tokens []htmlToken) int {
	offset := 0
	if bytes.HasPrefix(source, []byte{0xef, 0xbb, 0xbf}) {
		offset = 3
	}
	for offset < len(source) && isSpace(source[offset]) {
		offset++
	}
	for _, token := range tokens {
		if token.start != offset {
			continue
		}
		if token.typ != xhtml.DoctypeToken {
			break
		}
		offset = token.end
		for offset < len(source) && isSpace(source[offset]) {
			offset++
		}
		break
	}
	return offset
}
