package markdownsyntax

import (
	"strings"
	"unicode"
)

// NormalizeTag trims spaces and lower-cases the tag for canonical comparison.
// It does not strip a leading # – callers should do that if needed.
func NormalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// IsValidTag returns true when the tag obeys basic Obsidian rules:
//   - not empty
//   - contains no spaces
//   - is not purely numeric
//   - contains at least one letter
func IsValidTag(tag string) bool {
	if tag == "" {
		return false
	}
	nonDigit := false
	for _, r := range tag {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '/' {
			return false
		}
		if !unicode.IsDigit(r) {
			nonDigit = true
		}
	}
	return nonDigit
}
