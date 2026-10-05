package reference

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func validDerivedCascade(parent, child IdentifierRewrite) bool {
	if effectiveRewriteMode(parent) != IdentifierRewritePreferredRekey || effectiveRewriteMode(child) != IdentifierRewritePreferredRekey {
		return false
	}
	if canonicalRefKey(child.DerivedFrom) != canonicalRefKey(parent.OldRef) ||
		canonicalNodeNotePath(child.OldRef) != canonicalNodeNotePath(parent.OldRef) ||
		canonicalNodeNotePath(child.NewRef) != canonicalNodeNotePath(parent.NewRef) {
		return false
	}
	if child.OldRef.Kind != child.NewRef.Kind ||
		(child.OldRef.Kind != ontology.NodeKindEmbedded && child.OldRef.Kind != ontology.NodeKindSection) ||
		strings.TrimSpace(child.OldRef.TypeName) == "" || strings.TrimSpace(child.OldRef.TypeName) != strings.TrimSpace(child.NewRef.TypeName) {
		return false
	}
	if (strings.TrimSpace(child.OldRef.Fragment) == "" && strings.TrimSpace(child.OldRef.NodeID) == "") ||
		(strings.TrimSpace(child.NewRef.Fragment) == "" && strings.TrimSpace(child.NewRef.NodeID) == "") {
		return false
	}
	oldSuffix, ok := boundedIdentifierSuffix(child.OldIdentifier, parent.OldIdentifier)
	if !ok {
		return false
	}
	newSuffix, ok := boundedIdentifierSuffix(child.NewIdentifier, parent.NewIdentifier)
	return ok && oldSuffix == newSuffix
}

func effectiveRewriteMode(rewrite IdentifierRewrite) IdentifierRewriteMode {
	switch rewrite.Mode {
	case "", IdentifierRewritePreferredRekey:
		return IdentifierRewritePreferredRekey
	case IdentifierRewriteAliasRemoval:
		return IdentifierRewriteAliasRemoval
	default:
		return ""
	}
}

func boundedIdentifierSuffix(child, parent string) (string, bool) {
	child = ontology.NormalizeIdentifierSemanticValue(child)
	parent = ontology.NormalizeIdentifierSemanticValue(parent)
	if !strings.HasPrefix(child, parent) || len(child) == len(parent) {
		return "", false
	}
	suffix := child[len(parent):]
	r, _ := utf8.DecodeRuneInString(suffix)
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return "", false
	}
	return suffix, true
}

func inboundReplacement(occurrence StructuredFieldOccurrence, rewrite IdentifierRewrite) (string, bool) {
	switch occurrence.Kind {
	case StructuredFieldTypedIdentifierReference:
		if !identifierEqual(occurrence.Value, rewrite.OldIdentifier) {
			return "", false
		}
		return rewrite.NewIdentifier, true
	case StructuredFieldCanonicalNodeRef:
		if effectiveRewriteMode(rewrite) == IdentifierRewriteAliasRemoval || normalizeLocator(occurrence.Value) != normalizeLocator(rewrite.OldRef.String()) {
			return "", false
		}
		return rewrite.NewRef.String(), true
	case StructuredFieldIdentifierBackedLocator:
		return rewriteIdentifierBackedLocator(occurrence.Value, rewrite)
	default:
		return "", false
	}
}

func rewriteIdentifierBackedLocator(value string, rewrite IdentifierRewrite) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if effectiveRewriteMode(rewrite) == IdentifierRewriteAliasRemoval {
		return replaceBoundedIdentifier(trimmed, rewrite.OldIdentifier, rewrite.NewIdentifier)
	}
	oldFragment := strings.TrimPrefix(strings.TrimSpace(rewrite.OldRef.Fragment), "#")
	newFragment := strings.TrimPrefix(strings.TrimSpace(rewrite.NewRef.Fragment), "#")
	switch trimmed {
	case strings.TrimSpace(rewrite.OldRef.String()):
		return rewrite.NewRef.String(), true
	case oldFragment:
		return newFragment, newFragment != ""
	case "#" + oldFragment:
		return "#" + newFragment, newFragment != ""
	case strings.TrimSpace(rewrite.OldRef.NodeID):
		return strings.TrimSpace(rewrite.NewRef.NodeID), strings.TrimSpace(rewrite.NewRef.NodeID) != ""
	}
	return replaceBoundedIdentifier(trimmed, rewrite.OldIdentifier, rewrite.NewIdentifier)
}

func replaceBoundedIdentifier(value, oldIdentifier, newIdentifier string) (string, bool) {
	foldedOld := ontology.NormalizeIdentifierSemanticValue(oldIdentifier)
	if foldedOld == "" {
		return "", false
	}
	wantRunes := utf8.RuneCountInString(foldedOld)
	for start := 0; start < len(value); {
		end := start
		for range wantRunes {
			if end >= len(value) {
				end = -1
				break
			}
			_, size := utf8.DecodeRuneInString(value[end:])
			end += size
		}
		if end >= 0 && strings.EqualFold(value[start:end], foldedOld) && boundedIdentifierAt(value, start, end) {
			return value[:start] + newIdentifier + value[end:], true
		}
		_, size := utf8.DecodeRuneInString(value[start:])
		start += size
	}
	return "", false
}

func boundedIdentifierAt(value string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(value[:start])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(value) {
		r, _ := utf8.DecodeRuneInString(value[end:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
