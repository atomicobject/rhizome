package markdownsyntax

import (
	"path/filepath"
)

// MdLinkType represents the type of standard markdown link.
type MdLinkType string

const (
	MdLinkTypeBasic   MdLinkType = "basic"
	MdLinkTypeHeading MdLinkType = "heading"
	MdLinkTypeEmbed   MdLinkType = "embed"
)

// MdLink represents a parsed standard markdown link.
type MdLink struct {
	Target   string     // The link target (path, possibly with anchor)
	Text     string     // The link text or alt text for images
	LinkType MdLinkType // Type of link (basic, heading, embed)
}

// MdLinkOptions defines options for extracting markdown links.
type MdLinkOptions struct {
	SkipAnchors bool // Skip links containing anchors (# symbol)
	SkipEmbeds  bool // Skip embedded links (images: ![alt](path))
}

// DefaultMdLinkOptions provides standard options for markdown link extraction.
var DefaultMdLinkOptions = MdLinkOptions{
	SkipAnchors: false,
	SkipEmbeds:  false,
}

// ExtractMdLinks extracts standard markdown links from content.
// This handles [text](path) and ![alt](path) syntax.
func ExtractMdLinks(content string, options MdLinkOptions) []string {
	details := scanMdLinks(content, options)
	var links []string
	for _, d := range details {
		links = append(links, d.Target)
	}
	return links
}

// ScanMdLinks parses markdown links and image embeds from content.
func scanMdLinks(content string, options MdLinkOptions) []MdLink {
	spans := scanStructuredMarkdownLinks(content, options, ProtectedSpans(content))
	links := make([]MdLink, 0, len(spans))
	for _, detail := range spans {
		target := detail.Target
		if detail.markdownBody != "" {
			target = detail.markdownBody
		}
		link := MdLink{Target: filepath.ToSlash(target), Text: detail.Display}
		switch {
		case detail.Embed:
			link.LinkType = MdLinkTypeEmbed
		case detail.FragmentSpan.Valid():
			link.LinkType = MdLinkTypeHeading
		default:
			link.LinkType = MdLinkTypeBasic
		}
		links = append(links, link)
	}
	return links
}

// LinkDetail represents a unified link from any source (wikilink or markdown).
type LinkDetail struct {
	Target   string
	LinkType string // "wikilink" or "mdlink"
	Subtype  string // "basic", "alias", "heading", "block", "embed"
}

// ScanAllLinks extracts both wikilinks and markdown links with type information.
func ScanAllLinks(content string, wikiOpts WikilinkOptions, mdOpts MdLinkOptions) []LinkDetail {
	var details []LinkDetail

	// Scan wikilinks
	for _, wl := range ScanWikilinks(content, wikiOpts) {
		if wl.InsideCodeBlock {
			continue
		}
		details = append(details, LinkDetail{
			Target:   wl.Target,
			LinkType: "wikilink",
			Subtype:  string(wl.LinkType),
		})
	}

	// Scan markdown links
	for _, ml := range scanMdLinks(content, mdOpts) {
		details = append(details, LinkDetail{
			Target:   ml.Target,
			LinkType: "mdlink",
			Subtype:  string(ml.LinkType),
		})
	}

	return details
}
