package html

import (
	stdhtml "html"
	"mime"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

type visibleRegion struct {
	text       string
	start, end int
	hasRange   bool
}

func visibleRegions(root *node, source []byte) []visibleRegion {
	regions := make([]visibleRegion, 0)
	var visit func(*node, bool, bool)
	visit = func(n *node, inheritedHidden, preformatted bool) {
		if n == nil || n.kind != elementNode {
			return
		}
		hidden := inheritedHidden || isHidden(n) || isExcluded(n.tag)
		if hidden {
			return
		}
		pre := preformatted || n.tag == "pre" || n.tag == "code"
		if isContentBlock(n.tag) {
			if region := collectBlock(n, source, pre); region.text != "" {
				regions = append(regions, region)
			}
			return
		}
		if n.tag == "img" {
			if alt, ok := firstAttr(n, "alt"); ok {
				text := normalizeDecodedText(alt.value, false)
				if text != "" {
					regions = append(regions, visibleRegion{text: text, start: alt.valueFrom, end: alt.valueTo, hasRange: exactSpan(alt.valueFrom, alt.valueTo)})
				}
			}
			return
		}
		if n.tag == "input" {
			if value, ok := firstAttr(n, "value"); ok {
				text := normalizeDecodedText(value.value, false)
				if text != "" {
					regions = append(regions, visibleRegion{text: text, start: value.valueFrom, end: value.valueTo, hasRange: exactSpan(value.valueFrom, value.valueTo)})
				}
			}
			return
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child, hidden, pre)
			} else if child.kind == textNode {
				text := normalizeNodeText(child, source, pre)
				if text != "" {
					regions = append(regions, visibleRegion{text: text, start: child.start, end: child.end, hasRange: exactSpan(child.start, child.end)})
				}
			}
		}
	}
	for _, child := range root.children {
		if child.kind == elementNode {
			visit(child, false, false)
		} else if child.kind == textNode {
			// x/net/html recovers a plain fragment into html/body, but retain
			// this path for parser modes that leave document text at the root.
			text := normalizeNodeText(child, source, false)
			if text != "" && text != "\ufeff" {
				regions = append(regions, visibleRegion{text: text, start: child.start, end: child.end, hasRange: exactSpan(child.start, child.end)})
			}
		}
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].hasRange != regions[j].hasRange {
			return regions[i].hasRange
		}
		return regions[i].start < regions[j].start
	})
	return regions
}

func collectBlock(n *node, source []byte, preformatted bool) visibleRegion {
	var builder strings.Builder
	start, end := -1, -1
	hasRange := true
	appendBoundary := func() {
		if builder.Len() > 0 && !strings.HasSuffix(builder.String(), " ") {
			builder.WriteByte(' ')
		}
	}
	appendText := func(text string, from, to int, preserve bool) {
		// Callers pass semantic text: text nodes have already been decoded from
		// their source span, and attributes are decoded by the token ledger.
		// Normalize whitespace here without decoding entities a second time.
		text = normalizeDecodedText(text, preserve)
		if text == "" {
			return
		}
		if builder.Len() > 0 && !preserve {
			appendBoundary()
		}
		builder.WriteString(text)
		if !exactSpan(from, to) {
			hasRange = false
		} else {
			if start < 0 || from < start {
				start = from
			}
			if to > end {
				end = to
			}
		}
	}
	var walk func(*node, bool)
	walk = func(current *node, preserve bool) {
		for _, child := range current.children {
			if child.kind == textNode {
				appendText(normalizeNodeText(child, source, preserve), child.start, child.end, preserve)
				continue
			}
			if child.kind != elementNode || isExcluded(child.tag) || isHidden(child) {
				continue
			}
			if child.tag == "img" {
				if alt, ok := firstAttr(child, "alt"); ok {
					appendText(alt.value, alt.valueFrom, alt.valueTo, false)
				}
				continue
			}
			if isContentBlock(child.tag) && child.tag != "code" && child.tag != "pre" {
				// Nested blockquote/list/table content remains part of the
				// containing visible region. The previous extractor inserted a
				// boundary and then skipped the entire subtree.
				appendBoundary()
			}
			walk(child, preserve || child.tag == "pre" || child.tag == "code")
		}
	}
	walk(n, preformatted || n.tag == "pre" || n.tag == "code")
	if !hasRange {
		start, end = 0, 0
	}
	return visibleRegion{text: strings.TrimSpace(builder.String()), start: start, end: end, hasRange: hasRange && exactSpan(start, end)}
}

func normalizeNodeText(n *node, source []byte, preserve bool) string {
	if n == nil {
		return ""
	}
	if exactSpan(n.start, n.end) && n.end <= len(source) {
		text := string(source[n.start:n.end])
		if n.start == 0 && strings.HasPrefix(text, "\ufeff") {
			text = strings.TrimPrefix(text, "\ufeff")
		}
		return normalizeText(text, preserve)
	}
	// x/net/html has already decoded a text node that could not be correlated
	// to an authored span. Keep that semantic value intact and only normalize
	// its whitespace.
	return normalizeDecodedText(strings.TrimPrefix(n.text, "\ufeff"), preserve)
}

func exactSpan(start, end int) bool {
	return start >= 0 && end >= start
}

func normalizeText(text string, preserve bool) string {
	text = stdhtml.UnescapeString(text)
	return normalizeDecodedText(text, preserve)
}

func normalizeDecodedText(text string, preserve bool) string {
	if preserve {
		return text
	}
	var builder strings.Builder
	space := false
	for _, value := range text {
		if unicode.IsSpace(value) {
			space = true
			continue
		}
		if space && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		space = false
		builder.WriteRune(value)
	}
	return builder.String()
}

func supplementalRegions(root *node, source []byte, diagnostics *[]noteformat.Diagnostic) []noteformat.SearchRegionFact {
	regions := make([]noteformat.SearchRegionFact, 0)
	indexed := 0
	var visit func(*node)
	visit = func(n *node) {
		if n.kind != elementNode {
			return
		}
		if n.tag == "script" {
			id, hasID := firstAttr(n, "id")
			typeAttr, hasType := firstAttr(n, "type")
			canonical := hasID && id.value == "rhizome-metadata" && hasType && strings.EqualFold(strings.TrimSpace(typeAttr.value), "application/json")
			_, external := firstAttr(n, "src")
			if !canonical && !external {
				for _, child := range n.children {
					if child.kind != textNode || child.end <= child.start {
						continue
					}
					text := string(source[child.start:child.end])
					available := len(text)
					limit := maxSupplementalRegionBytes
					if remaining := maxSupplementalBytes - indexed; remaining < limit {
						limit = remaining
					}
					if limit < 0 {
						limit = 0
					}
					text = validUTF8Prefix(text, limit)
					if len(text) > 0 {
						mediaType := "text/javascript"
						if hasType && strings.TrimSpace(typeAttr.value) != "" {
							mediaType = strings.TrimSpace(typeAttr.value)
						}
						if _, _, err := mime.ParseMediaType(mediaType); err != nil {
							mediaType = "text/plain"
						}
						region := noteformat.SearchRegionFact{Origin: noteformat.SearchRegionDerived, Kind: noteformat.SearchRegionSupplemental, Text: text, MediaType: mediaType}
						if exactSpan(child.start, child.end) {
							region.Range = exactRange(child.start, child.start+len(text))
						}
						regions = append(regions, region)
						indexed += len(text)
					}
					if len(text) < available {
						*diagnostics = append(*diagnostics, diagnostic("html_supplemental_truncated", "supplemental script content was truncated", child.start, child.end, noteformat.DiagnosticCategoryLimit))
					}
				}
			}
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child)
			}
		}
	}
	for _, child := range root.children {
		if child.kind == elementNode {
			visit(child)
		}
	}
	return regions
}

func validUTF8Prefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for len(text) > 0 && !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

func exactRange(start, end int) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}
}

func parseURI(raw string, rawStart int, source []byte) (noteformat.URIReferenceFact, bool, error) {
	working := stdhtml.UnescapeString(raw)
	parsed, err := url.Parse(working)
	if err != nil {
		return noteformat.URIReferenceFact{}, false, err
	}
	queryDecoded, err := url.PathUnescape(parsed.RawQuery)
	if err != nil {
		return noteformat.URIReferenceFact{}, false, err
	}
	// url.Parse already decodes the hierarchical path and fragment once. Do
	// not unescape those fields again: %2520 must remain the semantic value
	// "%20", rather than becoming a space. RawQuery is retained by net/url,
	// so decode it exactly once while preserving '+' as authored data.
	uri := noteformat.URIReferenceFact{
		Raw:              working,
		Scheme:           strings.ToLower(parsed.Scheme),
		Authority:        parsed.Host,
		Path:             parsed.Path,
		Query:            queryDecoded,
		Fragment:         parsed.Fragment,
		Encoding:         noteformat.URIEncodingPercent,
		ProtocolRelative: strings.HasPrefix(working, "//"),
		RawRange:         noteformat.SourceRange{StartByte: rawStart, EndByte: rawStart + len(raw)},
	}
	pathStart, pathEnd, queryStart, queryEnd, fragmentStart := uriSourceRanges(raw, parsed)
	if pathStart >= 0 && pathEnd > pathStart {
		uri.PathRange = exactRange(rawStart+pathStart, rawStart+pathEnd)
	}
	if queryStart >= 0 {
		uri.QueryRange = exactRange(rawStart+queryStart+1, rawStart+queryEnd)
	}
	if fragmentStart >= 0 {
		uri.FragmentRange = exactRange(rawStart+fragmentStart+1, rawStart+len(raw))
	}
	_ = source
	return uri, uri.Scheme != "" || uri.ProtocolRelative, nil
}

// uriSourceRanges returns ranges in the authored attribute value, rather than
// in the HTML-entity-decoded or percent-decoded URI. URL components provide
// semantic values; these offsets retain the exact source spelling used by a
// provider fact.
func uriSourceRanges(raw string, parsed *url.URL) (pathStart, pathEnd, queryStart, queryEnd, fragmentStart int) {
	pathStart, pathEnd, queryStart, queryEnd, fragmentStart = -1, -1, -1, -1, -1
	fragmentStart = strings.IndexByte(raw, '#')
	queryStart = strings.IndexByte(raw, '?')
	if queryStart < 0 || (fragmentStart >= 0 && queryStart > fragmentStart) {
		queryStart = -1
	}
	queryEnd = len(raw)
	if fragmentStart >= 0 && (queryStart < 0 || fragmentStart > queryStart) {
		queryEnd = fragmentStart
	}
	pathEnd = len(raw)
	if queryStart >= 0 && queryStart < pathEnd {
		pathEnd = queryStart
	}
	if fragmentStart >= 0 && fragmentStart < pathEnd {
		pathEnd = fragmentStart
	}

	// The path starts after the scheme and authority. Opaque/external
	// references are still represented as URI facts, but their opaque payload
	// is not an internal vault path and therefore has no path range to match.
	pathStart = 0
	if parsed != nil && parsed.Scheme != "" {
		colon := strings.IndexByte(raw, ':')
		if colon >= 0 {
			pathStart = colon + 1
			if strings.HasPrefix(raw[pathStart:], "//") {
				pathStart += 2
				for pathStart < pathEnd && raw[pathStart] != '/' {
					pathStart++
				}
			}
		}
	} else if strings.HasPrefix(raw, "//") {
		pathStart = 2
		for pathStart < pathEnd && raw[pathStart] != '/' {
			pathStart++
		}
	}
	if pathStart > pathEnd {
		pathStart = pathEnd
	}
	return pathStart, pathEnd, queryStart, queryEnd, fragmentStart
}
