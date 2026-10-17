package markdownsyntax

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// StructuredLinkKind identifies the authored Markdown link syntax.
type StructuredLinkKind string

const (
	StructuredLinkWikilink StructuredLinkKind = "wikilink"
	StructuredLinkMarkdown StructuredLinkKind = "markdown"
)

// StructuredLink describes an internal wikilink or Markdown link without
// resolving it. Component spans exclude syntax delimiters such as # and |.
type StructuredLink struct {
	Kind     StructuredLinkKind
	Embed    bool
	Target   string
	Path     string
	Fragment string
	Display  string

	RawSpan      Span
	TargetSpan   Span
	PathSpan     Span
	FragmentSpan Span
	DisplaySpan  Span

	markdownBody string
	sourceSeal   string
	scanIndex    int
	scanCount    int
}

// ResolverInput returns exactly the value legacy shared link resolution used
// for this scanned link. Markdown preserves its authored parenthesized body
// for compatibility (including angle destinations and titles); wikilinks use
// their decoded target. It performs no resolution or vault access.
func (link StructuredLink) ResolverInput() string {
	input := link.Target
	if link.Kind == StructuredLinkMarkdown && link.markdownBody != "" {
		input = link.markdownBody
	}
	return filepath.ToSlash(input)
}

// ScanStructuredLinks discovers internal links in source order. It skips
// fenced and inline code, and excludes external Markdown targets. All offsets
// are byte offsets into content so callers can construct precise edits.
func ScanStructuredLinks(content string) []StructuredLink {
	snapshot := ScanStructuredLinkSnapshot(content)
	return append([]StructuredLink(nil), snapshot.Links...)
}

// ScanCommentLinks scans already extracted source comments/docstrings. Explicit
// links in their inline or fenced code examples remain coderefs; ordinary note
// scanning continues to protect those spans through ScanStructuredLinks.
func ScanCommentLinks(content string) []StructuredLink {
	return scanStructuredLinksWithProtected(content, nil)
}

func scanStructuredLinksWithProtected(content string, protected []Span) []StructuredLink {
	links := scanStructuredWikilinks(content, protected)
	excluded := append([]Span(nil), protected...)
	for _, link := range links {
		excluded = append(excluded, link.RawSpan)
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].Start < excluded[j].Start })
	links = append(links, scanStructuredMarkdownLinks(content, DefaultMdLinkOptions, excluded)...)
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].RawSpan.Start != links[j].RawSpan.Start {
			return links[i].RawSpan.Start < links[j].RawSpan.Start
		}
		return links[i].Kind < links[j].Kind
	})
	for index := range links {
		links[index].scanIndex = index
		links[index].scanCount = len(links)
		sealStructuredLink(content, &links[index])
	}
	return links
}

func scanStructuredWikilinks(content string, protected []Span) []StructuredLink {
	var links []StructuredLink
	segmentStart := 0
	for _, span := range protected {
		links = append(links, scanStructuredWikilinkSegment(content, segmentStart, span.Start)...)
		segmentStart = span.End
	}
	links = append(links, scanStructuredWikilinkSegment(content, segmentStart, len(content))...)
	return links
}

func scanStructuredWikilinkSegment(content string, start, end int) []StructuredLink {
	var links []StructuredLink
	for cursor := start; cursor < end; {
		open := indexUnescapedToken(content[:end], cursor, "[[")
		if open < 0 {
			break
		}
		closeStart := -1
		for scan := open + 2; scan+1 < end; scan++ {
			token := content[scan : scan+2]
			if token != "[[" && token != "]]" {
				continue
			}
			if isBackslashEscaped(content, scan) {
				continue
			}
			switch token {
			case "[[":
				open = scan
				scan++
			case "]]":
				closeStart = scan
			}
			if closeStart >= 0 {
				break
			}
		}
		if closeStart < 0 {
			break
		}
		if open > start && content[open-1] == '!' && isBackslashEscaped(content, open-1) {
			cursor = closeStart + 2
			continue
		}

		rawStart := open
		embed := open > start && content[open-1] == '!' && !isBackslashEscaped(content, open-1)
		if embed {
			rawStart--
		}
		innerStart, innerEnd := open+2, closeStart
		targetEnd := innerEnd
		displaySpan := Span{}
		if pipe := strings.IndexByte(content[innerStart:innerEnd], '|'); pipe >= 0 {
			targetEnd = innerStart + pipe
			displaySpan = linkSpan(targetEnd+1, innerEnd)
		}
		link := structuredLink(content, StructuredLinkWikilink, embed,
			linkSpan(rawStart, closeStart+2), linkSpan(innerStart, targetEnd), displaySpan)
		links = append(links, link)
		cursor = closeStart + 2
	}
	return links
}

func structuredLink(
	content string,
	kind StructuredLinkKind,
	embed bool,
	rawSpan Span,
	targetSpan Span,
	displaySpan Span,
) StructuredLink {
	pathSpan := targetSpan
	fragmentSpan := Span{}
	if hash := strings.IndexByte(targetSpan.Text(content), '#'); hash >= 0 {
		pathSpan.End = targetSpan.Start + hash
		fragmentSpan = linkSpan(pathSpan.End+1, targetSpan.End)
	}
	return StructuredLink{
		Kind:         kind,
		Embed:        embed,
		Target:       targetSpan.Text(content),
		Path:         pathSpan.Text(content),
		Fragment:     fragmentSpan.Text(content),
		Display:      displaySpan.Text(content),
		RawSpan:      rawSpan,
		TargetSpan:   targetSpan,
		PathSpan:     pathSpan,
		FragmentSpan: fragmentSpan,
		DisplaySpan:  displaySpan,
	}
}

// ValidateStructuredLinkSource reports whether link is an unchanged result of
// scanning the supplied content. Hand-built or caller-mutated values fail.
func ValidateStructuredLinkSource(content string, link StructuredLink) bool {
	return link.sourceSeal != "" && link.sourceSeal == structuredLinkSeal(content, link)
}

// ValidateStructuredLinkScan reports whether links is the complete, ordered
// scan snapshot for content. Dropped, duplicated, or reordered links fail.
func ValidateStructuredLinkScan(content string, links []StructuredLink) bool {
	if len(links) == 0 {
		return false
	}
	for index, link := range links {
		if link.scanIndex != index || link.scanCount != len(links) || !ValidateStructuredLinkSource(content, link) {
			return false
		}
	}
	return true
}

func sealStructuredLink(content string, link *StructuredLink) {
	link.sourceSeal = structuredLinkSeal(content, *link)
}

func structuredLinkSeal(content string, link StructuredLink) string {
	payload := struct {
		ContentHash  [sha256.Size]byte
		Kind         StructuredLinkKind
		Embed        bool
		Target       string
		Path         string
		Fragment     string
		Display      string
		RawSpan      Span
		TargetSpan   Span
		PathSpan     Span
		FragmentSpan Span
		DisplaySpan  Span
		MarkdownBody string
		ScanIndex    int
		ScanCount    int
	}{
		ContentHash: sha256.Sum256([]byte(content)), Kind: link.Kind, Embed: link.Embed,
		Target: link.Target, Path: link.Path, Fragment: link.Fragment, Display: link.Display,
		RawSpan: link.RawSpan, TargetSpan: link.TargetSpan, PathSpan: link.PathSpan,
		FragmentSpan: link.FragmentSpan, DisplaySpan: link.DisplaySpan, MarkdownBody: link.markdownBody,
		ScanIndex: link.scanIndex, ScanCount: link.scanCount,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
