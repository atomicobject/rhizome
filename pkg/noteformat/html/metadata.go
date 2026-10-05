package html

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

func projectFacts(notePath string, source []byte, document parsedDocument) (noteformat.ProjectionFacts, []noteformat.Diagnostic) {
	facts := noteformat.ProjectionFacts{}
	diagnostics := append([]noteformat.Diagnostic(nil), document.diagnostics...)
	metadataFacts, metadataTitle, metadataDiagnostics := rootMetadataFacts(source, document)
	facts.RootMetadata = metadataFacts
	diagnostics = append(diagnostics, metadataDiagnostics...)
	if metadataTitle != "" {
		facts.Title = &noteformat.TitleFact{Value: metadataTitle}
	} else if title := documentTitle(document.root, source); title != "" {
		facts.Title = &noteformat.TitleFact{Value: title}
	} else if heading := firstVisibleHeading(document.root, source); heading != "" {
		facts.Title = &noteformat.TitleFact{Value: heading}
	} else {
		base := path.Base(notePath)
		facts.Title = &noteformat.TitleFact{Value: strings.TrimSuffix(base, path.Ext(base))}
	}

	facts.DocumentBase, diagnostics = documentBase(document.root, source, diagnostics)
	facts.Links, diagnostics = authoredLinks(document.root, source, diagnostics)
	facts.FragmentTargets = fragmentTargets(document.root, source)
	for _, region := range visibleRegions(document.root, source) {
		regionRange := noteformat.OptionalSourceRange{}
		if region.hasRange {
			regionRange = exactRange(region.start, region.end)
		}
		facts.SearchRegions = append(facts.SearchRegions, noteformat.SearchRegionFact{
			Origin: noteformat.SearchRegionDerived, Kind: noteformat.SearchRegionVisible,
			Text: region.text, MediaType: "text/html", Range: regionRange,
		})
	}
	facts.SearchRegions = append(facts.SearchRegions, supplementalRegions(document.root, source, &diagnostics)...)
	sort.SliceStable(facts.SearchRegions, func(i, j int) bool {
		return facts.SearchRegions[i].Range.Range.StartByte < facts.SearchRegions[j].Range.Range.StartByte
	})
	return facts, diagnostics
}

func rootMetadataFacts(source []byte, document parsedDocument) ([]noteformat.RootMetadataFact, string, []noteformat.Diagnostic) {
	if len(document.metadata) == 0 {
		return nil, "", nil
	}
	diagnostics := make([]noteformat.Diagnostic, 0)
	if len(document.metadata) > 1 {
		for _, candidate := range document.metadata[1:] {
			diagnostics = append(diagnostics, diagnostic("html_duplicate_metadata", "multiple canonical metadata blocks were found", candidate.start, candidate.end, noteformat.DiagnosticCategoryMetadata))
		}
		return nil, "", diagnostics
	}
	candidate := document.metadata[0]
	var body *node
	for _, child := range candidate.children {
		if child.kind == textNode {
			body = child
			break
		}
	}
	if body == nil {
		diagnostics = append(diagnostics, diagnostic("html_metadata_invalid_json", "canonical metadata block has no JSON body", candidate.start, candidate.end, noteformat.DiagnosticCategoryMetadata))
		return nil, "", diagnostics
	}
	parsed, err := parseJSONMetadata(source[body.start:body.end])
	if err != nil {
		diagnostics = append(diagnostics, diagnostic("html_metadata_invalid_json", err.Error(), body.start, body.end, noteformat.DiagnosticCategoryMetadata))
		return nil, "", diagnostics
	}
	facts := make([]noteformat.RootMetadataFact, 0, len(parsed.members))
	title := ""
	for _, member := range parsed.members {
		decodedValue := normalizeJSONValue(member.value)
		value, err := noteformat.NewMetadataValue(decodedValue)
		if err != nil {
			diagnostics = append(diagnostics, diagnostic("html_metadata_unsupported_value", fmt.Sprintf("metadata field %q: %v", member.key, err), body.start+member.valueStart, body.start+member.valueEnd, noteformat.DiagnosticCategoryMetadata))
			continue
		}
		fact := noteformat.RootMetadataFact{
			Key: member.key, Value: value,
			Range:      exactRange(body.start, body.end),
			KeyRange:   exactRange(body.start+member.keyStart, body.start+member.keyEnd),
			ValueRange: exactRange(body.start+member.valueStart, body.start+member.valueEnd),
		}
		facts = append(facts, fact)
		if member.key == "title" {
			if text, ok := decodedValue.(string); ok {
				title = strings.TrimSpace(text)
			} else {
				diagnostics = append(diagnostics, diagnostic("html_metadata_title_type", "metadata title must be a string", body.start+member.valueStart, body.start+member.valueEnd, noteformat.DiagnosticCategoryMetadata))
			}
		}
	}
	return facts, title, diagnostics
}

func normalizeJSONValue(value any) any {
	switch value := value.(type) {
	case jsonObject:
		result := make(map[string]any, len(value.members))
		for _, member := range value.members {
			result[member.key] = normalizeJSONValue(member.value)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			result[index] = normalizeJSONValue(item)
		}
		return result
	default:
		return value
	}
}

func documentTitle(root *node, source []byte) string {
	var found string
	var visit func(*node, bool)
	visit = func(n *node, hidden bool) {
		if found != "" || n.kind != elementNode || hidden || isHidden(n) || n.tag == "script" || n.tag == "style" || n.tag == "template" {
			return
		}
		if n.tag == "title" {
			found = strings.TrimSpace(nodeText(n, source, false))
			return
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child, hidden)
			}
		}
	}
	visit(root, false)
	return found
}

func firstVisibleHeading(root *node, source []byte) string {
	var found string
	var visit func(*node, bool)
	visit = func(n *node, hidden bool) {
		if found != "" || n.kind != elementNode || hidden || isHidden(n) || isExcluded(n.tag) {
			return
		}
		if n.tag == "h1" {
			found = strings.TrimSpace(nodeText(n, source, false))
			if found != "" {
				return
			}
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child, hidden)
			}
		}
	}
	visit(root, false)
	return found
}

func nodeText(n *node, source []byte, preserve bool) string {
	var builder strings.Builder
	var walk func(*node, bool)
	walk = func(current *node, keep bool) {
		for _, child := range current.children {
			if child.kind == textNode {
				text := normalizeText(string(source[child.start:child.end]), keep)
				if builder.Len() > 0 && text != "" && !keep {
					builder.WriteByte(' ')
				}
				builder.WriteString(text)
			} else if child.kind == elementNode && !isExcluded(child.tag) && !isHidden(child) {
				walk(child, keep || child.tag == "pre" || child.tag == "code")
			}
		}
	}
	walk(n, preserve)
	return builder.String()
}

func documentBase(root *node, source []byte, diagnostics []noteformat.Diagnostic) (*noteformat.DocumentBaseFact, []noteformat.Diagnostic) {
	var result *noteformat.DocumentBaseFact
	var visit func(*node)
	visit = func(n *node) {
		if result != nil || n.kind != elementNode {
			return
		}
		if n.tag == "base" {
			attr, ok := firstAttr(n, "href")
			if !ok || strings.TrimSpace(attr.value) == "" {
				return
			}
			uri, _, err := parseURI(attr.rawValue, attr.valueFrom, source)
			if err != nil {
				diagnostics = append(diagnostics, diagnostic("html_invalid_base_uri", "document base is not a valid URI reference", attr.valueFrom, attr.valueTo, noteformat.DiagnosticCategoryLink))
				return
			}
			remapURIRanges(&uri, attr.rawValue, attr.valueFrom)
			result = &noteformat.DocumentBaseFact{URI: uri}
			return
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child)
			}
		}
	}
	visit(root)
	return result, diagnostics
}

func authoredLinks(root *node, source []byte, diagnostics []noteformat.Diagnostic) ([]noteformat.UnresolvedAuthoredLinkFact, []noteformat.Diagnostic) {
	links := make([]noteformat.UnresolvedAuthoredLinkFact, 0)
	var visit func(*node, bool)
	visit = func(n *node, protected bool) {
		if n.kind != elementNode {
			return
		}
		protected = protected || n.tag == "script" || n.tag == "style" || n.tag == "template"
		if n.tag == "a" && !protected {
			if attr, ok := firstAttr(n, "href"); ok {
				uri, _, err := parseURI(attr.rawValue, attr.valueFrom, source)
				if err != nil {
					diagnostics = append(diagnostics, diagnostic("html_invalid_link_uri", "anchor href is not a valid URI reference", attr.valueFrom, attr.valueTo, noteformat.DiagnosticCategoryLink))
					uri = fallbackURI(attr.rawValue, attr.valueFrom)
				}
				remapURIRanges(&uri, attr.rawValue, attr.valueFrom)
				// Every HTML href is a provider URI reference. Relative and
				// fragment-only hrefs need the same path/base/ambiguity rules as
				// absolute hrefs, so do not route them through Markdown semantics.
				copy := uri
				target := uri.Raw
				links = append(links, noteformat.UnresolvedAuthoredLinkFact{
					Resolution: noteformat.LinkResolutionURI, Syntax: "html", Subtype: "anchor", ResolverInput: target,
					Target: target, Path: uri.Path, Fragment: uri.Fragment,
					URI: &copy, RawRange: noteformat.SourceRange{StartByte: n.start, EndByte: n.end},
					TargetRange: noteformat.SourceRange{StartByte: attr.valueFrom, EndByte: attr.valueTo},
					PathRange:   uri.PathRange, FragmentRange: uri.FragmentRange,
				})
			}
		}
		for _, child := range n.children {
			if child.kind == elementNode {
				visit(child, protected)
			}
		}
	}
	for _, child := range root.children {
		if child.kind == elementNode {
			visit(child, false)
		}
	}
	return links, diagnostics
}

func fragmentTargets(root *node, source []byte) []noteformat.FragmentTargetFact {
	result := make([]noteformat.FragmentTargetFact, 0)
	var visit func(*node)
	visit = func(n *node) {
		if n.kind != elementNode {
			return
		}
		if attr, ok := firstAttr(n, "id"); ok && attr.value != "" {
			result = append(result, fragmentTarget(n, attr, noteformat.FragmentTargetElementID, source))
		}
		if n.tag == "a" {
			if attr, ok := firstAttr(n, "name"); ok && attr.value != "" {
				result = append(result, fragmentTarget(n, attr, noteformat.FragmentTargetLegacyName, source))
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
	return result
}

func fragmentTarget(n *node, attr attribute, kind noteformat.FragmentTargetKind, source []byte) noteformat.FragmentTargetFact {
	return noteformat.FragmentTargetFact{Kind: kind, Text: attr.value, NormalizedText: strings.ToLower(attr.value), Ordinal: 1, Line: lineNumber(source, attr.valueFrom), Range: exactRange(attr.valueFrom, attr.valueTo)}
}

func fallbackURI(raw string, start int) noteformat.URIReferenceFact {
	return noteformat.URIReferenceFact{Raw: raw, Path: raw, Encoding: noteformat.URIEncodingPercent, RawRange: noteformat.SourceRange{StartByte: start, EndByte: start + len(raw)}, PathRange: exactRange(start, start+len(raw))}
}

func remapURIRanges(uri *noteformat.URIReferenceFact, raw string, start int) {
	uri.RawRange = noteformat.SourceRange{StartByte: start, EndByte: start + len(raw)}
	// parseURI computes component ranges against the authored value while
	// using URL components for semantic decoding. Keep those exact ranges;
	// only malformed/fallback facts need the conservative delimiter mapping.
	if uri.PathRange.Present || uri.QueryRange.Present || uri.FragmentRange.Present {
		return
	}
	query := strings.IndexByte(raw, '?')
	fragment := strings.IndexByte(raw, '#')
	pathEnd := len(raw)
	if query >= 0 && query < pathEnd {
		pathEnd = query
	}
	if fragment >= 0 && fragment < pathEnd {
		pathEnd = fragment
	}
	if pathEnd > 0 {
		uri.PathRange = exactRange(start, start+pathEnd)
	}
	if query >= 0 {
		end := len(raw)
		if fragment > query {
			end = fragment
		}
		uri.QueryRange = exactRange(start+query+1, start+end)
	}
	if fragment >= 0 {
		uri.FragmentRange = exactRange(start+fragment+1, start+len(raw))
	}
}
