package obsidian

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
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
	spans := scanStructuredMarkdownLinks(content, options, markdownProtectedSpans(content))
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

// ResolveMdLink resolves a markdown link target to an actual note path.
// It handles relative paths and anchors.
func (c *NotePathCache) ResolveMdLink(link string, fromNote string) (string, bool) {
	resolved, ok := c.ResolveMdLinkTarget(link, fromNote)
	if !ok {
		return "", false
	}
	return resolved.Path, true
}

// ResolveMdLinkTarget returns the resolved note path plus any trailing fragment.
func (c *NotePathCache) ResolveMdLinkTarget(link string, fromNote string) (ResolvedNoteTarget, bool) {
	target, fragment := SplitMarkdownTarget(link)

	// If empty after removing anchor, it's a same-file anchor link
	if target == "" {
		return ResolvedNoteTarget{Path: fromNote, Fragment: fragment}, true
	}

	// Handle relative paths
	if !strings.HasPrefix(target, "/") && fromNote != "" {
		dir := filepath.Dir(fromNote)
		if dir != "." {
			target = filepath.Join(dir, target)
		}
	}

	// Normalize the path
	target = string(paths.Normalize(target))

	// An authored Markdown path is stronger evidence than a basename. Keep
	// root-relative targets such as README.md resolvable even when many nested
	// notes share that basename.
	if canonical, ok := c.ResolveExplicitPath(target); ok {
		return ResolvedNoteTarget{Path: canonical, Fragment: fragment}, true
	}
	candidates := c.resolveNotePathCandidates(target, fragment)
	if len(candidates) != 1 {
		return ResolvedNoteTarget{}, false
	}
	return candidates[0], true
}

// ExtractAllLinks extracts both wikilinks and markdown links from content.
// Returns a unified list of link targets.
func ExtractAllLinks(content string, wikiOpts WikilinkOptions, mdOpts MdLinkOptions) []string {
	seen := make(map[string]bool)
	var links []string

	// Extract wikilinks
	for _, link := range ExtractWikilinks(content, wikiOpts) {
		if !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
	}

	// Extract markdown links
	for _, link := range ExtractMdLinks(content, mdOpts) {
		if !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
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
	for _, wl := range scanWikilinks(content, wikiOpts) {
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
