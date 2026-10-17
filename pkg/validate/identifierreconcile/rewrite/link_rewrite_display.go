package rewrite

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"
)

func replacementLinkDisplay(authored string, rewrite IdentifierRewrite) string {
	if identifierComponentEqual(authored, rewrite.OldIdentifier) {
		return rewrite.NewIdentifier
	}
	if replacement, ok := replaceBoundedIdentifier(authored, rewrite.OldIdentifier, rewrite.NewIdentifier); ok {
		return replacement
	}
	// Human-facing descendant labels conventionally use dots even though the
	// canonical identifier uses hyphens (SPEC-0001.US1). The resolved target
	// makes this substitution semantic rather than a prose-wide text rewrite.
	oldDisplay := DottedIdentifierDisplay(rewrite.OldIdentifier)
	newDisplay := DottedIdentifierDisplay(rewrite.NewIdentifier)
	if replacement, ok := replaceBoundedIdentifier(authored, oldDisplay, newDisplay); ok {
		return replacement
	}
	return authored
}

// DottedIdentifierDisplay renders every suffix-and-ordinal descendant segment
// with the author-facing dot convention, without assuming a fixed schema suffix
// vocabulary. For example, SPEC-0001-US1-TC2 becomes SPEC-0001.US1.TC2.
func DottedIdentifierDisplay(identifier string) string {
	parts := strings.Split(identifier, "-")
	if len(parts) < 2 {
		return identifier
	}
	var display strings.Builder
	display.Grow(len(identifier))
	display.WriteString(parts[0])
	for _, part := range parts[1:] {
		separator := '-'
		if isDerivedDisplaySegment(part) {
			separator = '.'
		}
		display.WriteRune(separator)
		display.WriteString(part)
	}
	return display.String()
}

func isDerivedDisplaySegment(segment string) bool {
	ordinalStart := -1
	for i := 0; i < len(segment); i++ {
		if segment[i] >= '0' && segment[i] <= '9' {
			ordinalStart = i
			break
		}
		if (segment[i] < 'A' || segment[i] > 'Z') && (segment[i] < 'a' || segment[i] > 'z') {
			return false
		}
	}
	if ordinalStart <= 0 {
		return false
	}
	for i := ordinalStart; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return false
		}
	}
	return true
}

func identifierComponentEqual(left, right string) bool {
	return ontoschema.NormalizeIdentifierSemanticValue(left) == ontoschema.NormalizeIdentifierSemanticValue(right)
}
