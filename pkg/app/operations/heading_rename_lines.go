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
	// ownLine marks an anchor on the line after the heading, the form this
	// command writes, so a later rename reuses it instead of adding another.
	ownLine bool
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
		ownLine := false
		if next, ok := nextLine(content, loc[1]); ok && blockID == "" {
			if id, caret, _, found := markdownsyntax.TrailingBlockID(next); found && strings.TrimSpace(next[:caret]) == "" {
				blockID, ownLine = id, true
			}
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
			ownLine: ownLine,
		})
	}
	return matches
}

// replaceHeadingLine keeps an authored anchor in place. A new anchor goes on
// its own line after the heading, followed by a blank line so Obsidian reads
// it as a separate block: heading-line anchors read as malformed to the
// ontology and leak into derived titles (SPEC-0023).
func replaceHeadingLine(content string, match headingLineMatch, newHeading, newBlockID string) string {
	line := strings.Repeat("#", match.level) + " " + strings.TrimSpace(newHeading)
	if match.blockID != "" && !match.ownLine {
		line += " ^" + match.blockID
	}
	rest := content[match.end:]
	if newBlockID != "" {
		newline := strings.IndexByte(rest, '\n')
		eol := "\n"
		if newline >= 0 && strings.HasSuffix(rest[:newline], "\r") || newline < 0 && strings.Contains(content, "\r\n") {
			eol = "\r\n"
		}
		if newline < 0 {
			rest += eol + "^" + newBlockID
		} else {
			anchor, after := "^"+newBlockID+eol, rest[newline+1:]
			if next, _ := nextLine(rest, newline); after != "" && strings.TrimSpace(next) != "" {
				anchor += eol
			}
			rest = rest[:newline+1] + anchor + after
		}
	}
	return content[:match.start] + line + rest
}

// nextLine returns the line after the newline at or after offset, without its
// line ending.
func nextLine(content string, offset int) (string, bool) {
	newline := strings.IndexByte(content[offset:], '\n')
	if newline < 0 {
		return "", false
	}
	next := content[offset+newline+1:]
	if end := strings.IndexByte(next, '\n'); end >= 0 {
		next = next[:end]
	}
	return strings.TrimRight(next, "\r"), true
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
