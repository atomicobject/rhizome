package markdownsyntax

import (
	"regexp"
	"strings"
	"unicode"
)

// HeadingInfo describes one Markdown heading exposed by a note.
type HeadingInfo struct {
	Text           string `json:"text"`
	Level          int    `json:"level"`
	NormalizedText string `json:"normalizedText"`
	Line           int    `json:"line"`
	ParentText     string `json:"parentText,omitempty"`
}

// EnumerateHeadings returns canonical Markdown headings with
// resolver-normalized text and parent heading context.
func EnumerateHeadings(content string) []HeadingInfo {
	var headings []HeadingInfo
	parentByLevel := map[int]string{}
	for _, target := range EnumerateMarkdownTargets(content) {
		if target.Kind != MarkdownTargetHeading {
			continue
		}
		parent := ""
		for l := target.Level - 1; l >= 1; l-- {
			if candidate := strings.TrimSpace(parentByLevel[l]); candidate != "" {
				parent = candidate
				break
			}
		}
		for l := target.Level; l <= 6; l++ {
			delete(parentByLevel, l)
		}
		parentByLevel[target.Level] = target.Text
		headings = append(headings, HeadingInfo{
			Text:           target.Text,
			Level:          target.Level,
			NormalizedText: target.NormalizedText,
			Line:           target.Line,
			ParentText:     parent,
		})
	}
	return headings
}

// BlockIDsAndHeadings returns the block IDs and heading keys present
// in note content. Headings compare the way Obsidian's resolveSubpath does;
// block IDs stay case-sensitive because they back Rhizome node identifiers.
//
// WHY: fragment-broken-link detection needs to know what the target note
// actually exposes. The canonical parser lives in this syntax package so
// link health and persisted raw-note targets share syntax without depending on
// ontology projection.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func BlockIDsAndHeadings(content string) (map[string]struct{}, map[string]struct{}) {
	blocks := map[string]struct{}{}
	headings := map[string]struct{}{}
	for _, target := range EnumerateMarkdownTargets(content) {
		switch target.Kind {
		case MarkdownTargetBlock:
			blocks[target.Text] = struct{}{}
		case MarkdownTargetHeading:
			headings[ObsidianHeadingKey(target.Text)] = struct{}{}
		}
	}
	return blocks, headings
}

// MarkdownHeadingSlug is the anchor GitHub-flavored renderers give a heading:
// lowercase, punctuation removed, spaces become hyphens.
func MarkdownHeadingSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

var obsidianHeadingPunctuation = regexp.MustCompile("[!\"#$%&()*+,.:;<=>?@^`{|}~/\\[\\]\\\\\r\n]")

// ObsidianHeadingKey mirrors Obsidian's stripHeading plus lowercasing: the
// comparison its resolveSubpath uses between a link fragment and a heading.
// Emoji and typographic dashes are not stripped, matching Obsidian.
func ObsidianHeadingKey(heading string) string {
	stripped := obsidianHeadingPunctuation.ReplaceAllString(heading, " ")
	return strings.ToLower(strings.Join(strings.Fields(stripped), " "))
}
