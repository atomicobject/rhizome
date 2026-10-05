package html

import (
	"bytes"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	xhtml "golang.org/x/net/html"
)

const (
	maxSupplementalRegionBytes = 64 * 1024
	maxSupplementalBytes       = 256 * 1024
)

// node is a source-aware view of one node in the recovered HTML5 tree. The
// tree itself comes from x/net/html. The source fields come only from the
// tokenizer ledger, so a recovered node never gets a guessed source offset.
type node struct {
	tag      string
	text     string
	start    int
	end      int
	parent   *node
	children []*node
	attrs    []attribute
	domAttrs []xhtml.Attribute
	// duplicateAttrs keeps provider mutation ownership fail-closed even though
	// ordinary extraction uses the first HTML attribute occurrence.
	duplicateAttrs bool
	kind           nodeKind
	authored       bool
	opening        *htmlToken
	closing        *htmlToken
	rawTextStart   int
	rawTextEnd     int
	rawTextExact   bool
}

type nodeKind uint8

const (
	elementNode nodeKind = iota
	textNode
	commentNode
)

type attribute struct {
	name      string
	value     string
	rawValue  string
	valueFrom int
	valueTo   int
}

type parsedDocument struct {
	root        *node
	diagnostics []noteformat.Diagnostic
	metadata    []*node
	tokens      []htmlToken
}

// parse uses the HTML5 tree-construction algorithm for semantics and a
// separate tokenizer pass for original-byte evidence. x/net/html deliberately
// does not expose source positions, so the two passes are correlated by token
// identity and decoded token data. No DOM serialization or source substring
// search is used to recover positions.
func parse(source []byte) parsedDocument {
	result := parsedDocument{}
	parseSource := source
	if bytes.HasPrefix(source, []byte{0xef, 0xbb, 0xbf}) {
		// x/net/html's tree builder does not consume a UTF-8 BOM before the
		// insertion-mode decision. Strip it for semantic recovery only; the
		// ledger still runs against the complete authored bytes.
		parseSource = source[3:]
	}
	dom, err := xhtml.Parse(bytes.NewReader(parseSource))
	if err != nil {
		result.diagnostics = append(result.diagnostics, diagnostic("html_parse_failed", err.Error(), 0, len(source), noteformat.DiagnosticCategoryContent))
	}
	tokens, tokenErr := tokenizeSource(source)
	result.tokens = tokens
	if tokenErr != nil {
		result.diagnostics = append(result.diagnostics, diagnostic("html_tokenize_failed", tokenErr.Error(), 0, len(source), noteformat.DiagnosticCategoryContent))
	}
	if dom == nil {
		dom = &xhtml.Node{Type: xhtml.DocumentNode}
	}
	result.root = wrapDOM(dom, nil)
	result.root.tag = "#document"
	result.root.start = 0
	result.root.end = len(source)
	correlateSource(result.root, tokens, source)
	collectMetadataCandidates(result.root, &result)
	return result
}

func wrapDOM(dom *xhtml.Node, parent *node) *node {
	result := &node{parent: parent, start: -1, end: -1}
	switch dom.Type {
	case xhtml.ElementNode, xhtml.DocumentNode:
		result.kind = elementNode
		if dom.Type == xhtml.DocumentNode {
			result.tag = "#document"
		} else {
			result.tag = strings.ToLower(dom.Data)
			result.domAttrs = append([]xhtml.Attribute(nil), dom.Attr...)
			result.attrs = domAttributes(dom.Attr)
		}
	case xhtml.TextNode:
		result.kind = textNode
		result.text = dom.Data
	case xhtml.CommentNode, xhtml.DoctypeNode:
		// Comments and doctypes are both non-content leaves for this provider.
		result.kind = commentNode
		result.text = dom.Data
	default:
		result.kind = commentNode
		result.text = dom.Data
	}
	for child := dom.FirstChild; child != nil; child = child.NextSibling {
		wrapped := wrapDOM(child, result)
		result.children = append(result.children, wrapped)
	}
	return result
}

func domAttributes(attrs []xhtml.Attribute) []attribute {
	if len(attrs) == 0 {
		return nil
	}
	result := make([]attribute, 0, len(attrs))
	for _, attr := range attrs {
		name := strings.ToLower(attr.Key)
		if attr.Namespace != "" {
			name = strings.ToLower(attr.Namespace + ":" + attr.Key)
		}
		result = append(result, attribute{name: name, value: attr.Val, rawValue: attr.Val, valueFrom: -1, valueTo: -1})
	}
	return result
}

func correlateSource(root *node, tokens []htmlToken, source []byte) {
	elements := make([]*node, 0)
	texts := make([]*node, 0)
	collectNodes(root, &elements, &texts)
	correlateTextNodes(texts, tokens)

	byTag := make(map[string][]*htmlToken)
	for index := range tokens {
		token := &tokens[index]
		if token.typ != xhtml.StartTagToken && token.typ != xhtml.SelfClosingTagToken {
			continue
		}
		byTag[token.tag] = append(byTag[token.tag], token)
	}
	usedOpening := make(map[*htmlToken]bool)
	openingNodes := make(map[*htmlToken]*node)
	for _, element := range elements {
		if element.tag == "#document" {
			continue
		}
		candidates := byTag[element.tag]
		best := chooseOpeningCandidate(element, candidates, usedOpening)
		if best == nil {
			continue
		}
		candidate := best
		usedOpening[candidate] = true
		element.authored = true
		element.opening = candidate
		element.start = candidate.start
		element.end = candidate.end
		element.attrs = candidate.attrs
		element.duplicateAttrs = candidate.duplicateAttrs
		openingNodes[candidate] = element
	}

	correlateClosingTokens(root, tokens, openingNodes, source)
	for _, element := range elements {
		correlateRawText(element, tokens)
	}
	boundUnclosedNodes(root)
}

func chooseOpeningCandidate(element *node, candidates []*htmlToken, used map[*htmlToken]bool) *htmlToken {
	matching := make([]*htmlToken, 0, len(candidates))
	for _, candidate := range candidates {
		if !used[candidate] && attrsMatch(element.domAttrs, candidate.attrs) {
			matching = append(matching, candidate)
		}
	}
	if len(matching) == 0 {
		// Synthetic html/head/body nodes have no authored attributes. If a
		// malformed authored node could not be matched semantically, do not
		// borrow another tag's token and fabricate mutation ownership.
		return nil
	}
	childStart := -1
	for _, child := range element.children {
		if child.start >= 0 && (childStart < 0 || child.start < childStart) {
			childStart = child.start
		}
	}
	if childStart >= 0 {
		var best *htmlToken
		for _, candidate := range matching {
			if candidate.start <= childStart && (best == nil || candidate.start > best.start) {
				best = candidate
			}
		}
		if best != nil {
			return best
		}
	}
	return matching[0]
}

func attrsMatch(domAttrs []xhtml.Attribute, rawAttrs []attribute) bool {
	unique := make([]xhtml.Attribute, 0, len(domAttrs))
	seen := make(map[string]bool)
	for _, domAttr := range domAttrs {
		name := domAttr.Key
		if domAttr.Namespace != "" {
			name = domAttr.Namespace + ":" + domAttr.Key
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, domAttr)
	}
	if len(unique) != len(rawAttrs) {
		return false
	}
	for index, domAttr := range unique {
		rawAttr := rawAttrs[index]
		name := domAttr.Key
		if domAttr.Namespace != "" {
			name = domAttr.Namespace + ":" + domAttr.Key
		}
		if !strings.EqualFold(name, rawAttr.name) || domAttr.Val != rawAttr.value {
			return false
		}
	}
	return true
}

func collectNodes(current *node, elements, texts *[]*node) {
	if current == nil {
		return
	}
	if current.kind == elementNode {
		*elements = append(*elements, current)
	} else if current.kind == textNode {
		*texts = append(*texts, current)
	}
	for _, child := range current.children {
		collectNodes(child, elements, texts)
	}
}

// correlateClosingTokens follows authored token order only to recover explicit
// closing tags. It intentionally does not try to recreate HTML5 tree
// construction: the recovered DOM already owns that semantic decision. An
// unclosed or implicitly closed element therefore has no exact closing span.
func correlateClosingTokens(root *node, tokens []htmlToken, openingNodes map[*htmlToken]*node, source []byte) {
	stack := make([]*node, 0)
	for index := range tokens {
		token := &tokens[index]
		switch token.typ {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			element := openingNodes[token]
			if element == nil || voidElement(token.tag) {
				continue
			}
			stack = append(stack, element)
		case xhtml.EndTagToken:
			found := -1
			for stackIndex := len(stack) - 1; stackIndex >= 0; stackIndex-- {
				if stack[stackIndex].tag == token.tag {
					found = stackIndex
					break
				}
			}
			if found < 0 {
				continue
			}
			for stackIndex := len(stack) - 1; stackIndex >= found; stackIndex-- {
				current := stack[stackIndex]
				if stackIndex == found {
					current.closing = token
					current.end = token.end
				} else if current.end < 0 || current.end > token.start {
					current.end = token.start
				}
			}
			stack = stack[:found]
		}
	}
	for _, element := range stack {
		if element.end < 0 {
			element.end = len(source)
		}
	}
}

func correlateTextNodes(texts []*node, tokens []htmlToken) {
	textTokens := make([]*htmlToken, 0)
	for index := range tokens {
		if tokens[index].typ == xhtml.TextToken {
			textTokens = append(textTokens, &tokens[index])
		}
	}
	used := make(map[*htmlToken]bool)
	for _, textNode := range texts {
		if textNode.text == "" {
			continue
		}
		for index, token := range textTokens {
			if used[token] {
				continue
			}
			if normalizedTokenText(token.data) == normalizedTokenText(textNode.text) {
				used[token] = true
				textNode.start, textNode.end = token.start, token.end
				break
			}
			// x/net/html may merge adjacent text tokens while recovering table
			// content. Correlate a contiguous run without crossing markup.
			combined := ""
			for end := index; end < len(textTokens) && !used[textTokens[end]]; end++ {
				if end > index && textTokens[end-1].end != textTokens[end].start {
					break
				}
				combined += textTokens[end].data
				if normalizedTokenText(combined) == normalizedTokenText(textNode.text) {
					for run := index; run <= end; run++ {
						used[textTokens[run]] = true
					}
					textNode.start = textTokens[index].start
					textNode.end = textTokens[end].end
					break
				}
				if len(combined) >= len(textNode.text) {
					break
				}
			}
			if textNode.start >= 0 {
				break
			}
		}
	}
}

func normalizedTokenText(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func correlateRawText(element *node, tokens []htmlToken) {
	if element == nil || element.opening == nil || (element.tag != "script" && element.tag != "style" && element.tag != "title" && element.tag != "textarea") {
		return
	}
	openIndex := element.opening.index
	if openIndex < 0 || openIndex >= len(tokens) || element.opening.typ == xhtml.SelfClosingTagToken {
		return
	}
	bodyStart := element.opening.end
	if openIndex+1 < len(tokens) && tokens[openIndex+1].typ == xhtml.TextToken {
		bodyStart = tokens[openIndex+1].start
		openIndex++
	}
	if openIndex+1 >= len(tokens) || tokens[openIndex+1].typ != xhtml.EndTagToken || tokens[openIndex+1].tag != element.tag {
		return
	}
	if element.closing == nil || element.closing.index != tokens[openIndex+1].index {
		return
	}
	element.rawTextStart = bodyStart
	element.rawTextEnd = tokens[openIndex+1].start
	element.rawTextExact = element.rawTextEnd >= element.rawTextStart
}

func boundUnclosedNodes(root *node) int {
	if root == nil {
		return -1
	}
	maxEnd := root.end
	for _, child := range root.children {
		childEnd := boundUnclosedNodes(child)
		if childEnd > maxEnd {
			maxEnd = childEnd
		}
	}
	if root.kind == elementNode && root.authored && root.closing == nil && root.end > root.start && maxEnd > root.end {
		root.end = maxEnd
	}
	return root.end
}

func collectMetadataCandidates(root *node, document *parsedDocument) {
	var visit func(*node, bool)
	visit = func(n *node, inTemplate bool) {
		if n == nil || n.kind != elementNode || inTemplate {
			return
		}
		if n.tag == "head" {
			for _, candidate := range n.children {
				if candidate.kind == elementNode && candidate.tag == "script" && canonicalMarker(candidate) {
					document.metadata = append(document.metadata, candidate)
				}
			}
			return
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child, inTemplate || n.tag == "template")
			}
		}
	}
	for _, child := range root.children {
		visit(child, false)
	}
}

func canonicalMarker(n *node) bool {
	id, ok := firstAttr(n, "id")
	if !ok || id.value != "rhizome-metadata" {
		return false
	}
	typeAttr, ok := firstAttr(n, "type")
	return ok && strings.EqualFold(strings.TrimSpace(typeAttr.value), "application/json")
}

func firstAttr(n *node, name string) (attribute, bool) {
	for _, attr := range n.attrs {
		if attr.name == name {
			return attr, true
		}
	}
	return attribute{}, false
}

func voidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func isContentBlock(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", "li", "dt", "dd", "caption", "td", "th", "pre", "blockquote", "button", "label", "option", "summary", "textarea":
		return true
	default:
		return false
	}
}

func isExcluded(tag string) bool {
	switch tag {
	case "head", "script", "style", "template", "noscript":
		return true
	default:
		return false
	}
}

func isHidden(n *node) bool {
	if _, ok := firstAttr(n, "hidden"); ok {
		return true
	}
	aria, ok := firstAttr(n, "aria-hidden")
	return ok && strings.EqualFold(strings.TrimSpace(aria.value), "true")
}

func diagnostic(code, message string, start, end int, category noteformat.DiagnosticCategory) noteformat.Diagnostic {
	return noteformat.Diagnostic{Code: code, Category: category, Message: message, Range: noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}}
}

func lineNumber(source []byte, offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := 1
	for _, value := range source[:offset] {
		if value == '\n' {
			line++
		}
	}
	return line
}
