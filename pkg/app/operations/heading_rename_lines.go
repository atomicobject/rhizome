package operations

import (
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology"
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

// replaceHeadingLine keeps an authored heading-line anchor in place. A new
// anchor goes on its own line after the heading: heading-line anchors read as
// malformed to the ontology and leak into derived titles (SPEC-0023).
func replaceHeadingLine(content string, match headingLineMatch, newHeading, newBlockID string) string {
	line := strings.Repeat("#", match.level) + " " + strings.TrimSpace(newHeading)
	if match.blockID != "" {
		line += " ^" + match.blockID
	}
	rest := content[match.end:]
	if newBlockID != "" {
		eol := "\n"
		if strings.HasPrefix(rest, "\r") {
			eol = "\r\n"
		}
		if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
			rest = rest[:newline+1] + "^" + newBlockID + eol + rest[newline+1:]
		} else {
			rest += eol + "^" + newBlockID
		}
	}
	return content[:match.start] + line + rest
}

// generatedHeadingBlockID derives an anchor from the heading text and returns
// "" when the Markdown target parser would not read it back as that block ID
// (for example non-ASCII text) or the note already uses it.
func generatedHeadingBlockID(content, heading string) string {
	blockID := ontology.BlockSafeIdentifier(heading)
	if parsed, _, _, ok := markdownsyntax.TrailingBlockID("^" + blockID); !ok || parsed != blockID {
		return ""
	}
	for _, target := range markdownsyntax.EnumerateMarkdownTargets(content) {
		if target.Kind == markdownsyntax.MarkdownTargetBlock && target.Text == blockID {
			return ""
		}
	}
	return blockID
}
