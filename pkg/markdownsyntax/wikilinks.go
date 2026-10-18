package markdownsyntax

import "strings"

type BacklinkType string

const (
	BacklinkTypeBasic   BacklinkType = "basic"
	BacklinkTypeAlias   BacklinkType = "alias"
	BacklinkTypeHeading BacklinkType = "heading"
	BacklinkTypeBlock   BacklinkType = "block"
	BacklinkTypeEmbed   BacklinkType = "embed"
)

type WikilinkOptions struct {
	SkipAnchors bool // Skip links containing anchors (# symbol)
	SkipEmbeds  bool // Skip embedded links (![[...]])
}

// DefaultWikilinkOptions provides standard options for wikilink extraction
var DefaultWikilinkOptions = WikilinkOptions{
	SkipAnchors: false,
	SkipEmbeds:  false,
}

// ExtractWikilinks extracts wikilinks from markdown content with configurable options
func ExtractWikilinks(content string, options WikilinkOptions) []string {
	details := ScanWikilinks(content, options)
	var links []string
	for _, d := range details {
		if d.InsideCodeBlock {
			continue
		}
		links = append(links, d.Target)
	}
	return links
}

type WikilinkDetail struct {
	Target   string
	LinkType BacklinkType
	Start    int
	End      int
	Line     int
	Raw      string
	// InsideCodeBlock reports a link inside fenced, indented, or inline code,
	// where Obsidian does not create a link.
	InsideCodeBlock bool
}

// ScanWikilinks parses wikilinks and embeds from markdown content in a single pass.
// Returns WikilinkDetail structs with target path and link type classification.
//
// Link types:
//   - BacklinkTypeBasic: [[Note]]
//   - BacklinkTypeAlias: [[Note|Display]]
//   - BacklinkTypeHeading: [[Note#Heading]]
//   - BacklinkTypeBlock: [[Note#^blockid]]
//   - BacklinkTypeEmbed: ![[Note]]
//
// Options control whether anchors (#) or embeds (![[) are skipped.
func ScanWikilinks(content string, options WikilinkOptions) []WikilinkDetail {
	var details []WikilinkDetail
	appendSegment := func(start, end int, protected bool) {
		for _, link := range scanStructuredWikilinkSegment(content, start, end) {
			if options.SkipEmbeds && link.Embed || options.SkipAnchors && link.FragmentSpan.Valid() {
				continue
			}
			subtype := BacklinkTypeBasic
			switch {
			case link.Embed:
				subtype = BacklinkTypeEmbed
			case link.DisplaySpan.Valid():
				subtype = BacklinkTypeAlias
			case strings.HasPrefix(link.Fragment, "^"):
				subtype = BacklinkTypeBlock
			case link.FragmentSpan.Valid():
				subtype = BacklinkTypeHeading
			}
			details = append(details, WikilinkDetail{Target: link.Target, LinkType: subtype, Start: link.RawSpan.Start, End: link.RawSpan.End, Line: 1 + strings.Count(content[:link.RawSpan.Start], "\n"), Raw: link.RawSpan.Text(content), InsideCodeBlock: protected})
		}
	}
	start := 0
	for _, span := range ProtectedSpans(content) {
		appendSegment(start, span.Start, false)
		appendSegment(span.Start, span.End, true)
		start = span.End
	}
	appendSegment(start, len(content), false)
	return details
}
