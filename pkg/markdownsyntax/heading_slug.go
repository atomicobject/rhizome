package markdownsyntax

import (
	"strings"
	"unicode"
)

// HeadingSlug is the heading component of a section identity. Authored block
// IDs take precedence at the parser; empty headings have no slug.
func HeadingSlug(heading string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
