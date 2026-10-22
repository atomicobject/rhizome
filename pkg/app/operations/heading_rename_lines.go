package operations

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
)

type headingLineMatch struct {
	start   int
	end     int
	level   int
	text    string
	blockID string
}

// Horizontal whitespace only: \s would cross line breaks and swallow the newline after the heading.
var headingLineWithHashesRE = regexp.MustCompile(`(?m)^(#{1,6})[ \t]+([^\r\n]+?)[ \t\r]*$`)
var trailingHeadingBlockIDRE = regexp.MustCompile(`\s+\^([A-Za-z0-9_-]+)\s*$`)

func matchingHeadingLines(content, oldHeading string) []headingLineMatch {
	var matches []headingLineMatch
	oldNorm := markdownsyntax.NormalizeMarkdownHeadingFragment(oldHeading)
	for _, loc := range headingLineWithHashesRE.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < 6 {
			continue
		}
		if headingRenameInFencedCodeBlockAt(content, loc[0]) {
			continue
		}
		text := strings.TrimSpace(content[loc[4]:loc[5]])
		blockID := ""
		if m := trailingHeadingBlockIDRE.FindStringSubmatch(text); len(m) == 2 {
			blockID = m[1]
			text = strings.TrimSpace(trailingHeadingBlockIDRE.ReplaceAllString(text, ""))
		}
		if markdownsyntax.NormalizeMarkdownHeadingFragment(text) != oldNorm {
			continue
		}
		matches = append(matches, headingLineMatch{
			start:   loc[0],
			end:     loc[5], // keep trailing spaces and CR so only the heading text changes
			level:   loc[3] - loc[2],
			text:    text,
			blockID: blockID,
		})
	}
	return matches
}

// replaceHeadingLine changes only the heading text. Obsidian links headings by
// text and does not support block IDs on them, so rename never adds one; an
// authored heading-line anchor is left as written for validation to report.
func replaceHeadingLine(content string, match headingLineMatch, newHeading string) string {
	line := strings.Repeat("#", match.level) + " " + strings.TrimSpace(newHeading)
	if match.blockID != "" {
		line += " ^" + match.blockID
	}
	return content[:match.start] + line + content[match.end:]
}

// checkNewHeading rejects a new heading that incoming links could not name
// uniquely: link syntax characters would split the rewritten wikilink, and a
// name shared with another heading in the note makes the fragment ambiguous.
func checkNewHeading(content string, match headingLineMatch, newHeading string) error {
	heading := strings.TrimSpace(newHeading)
	if strings.ContainsAny(heading, "|#[]\r\n") || strings.HasPrefix(heading, "^") {
		return fmt.Errorf("new heading %q contains characters a heading link cannot use: | # [ ] or a leading ^", heading)
	}
	for _, other := range matchingHeadingLines(content, heading) {
		if other.start != match.start {
			return fmt.Errorf("new heading %q already exists in the note", heading)
		}
	}
	return nil
}
