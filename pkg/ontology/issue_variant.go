package ontology

import (
	"slices"
	"strings"
)

// A variant names the specific case of a finding within its issue code, such
// that one decision (a type choice, a schema edit) resolves every finding in
// it. Keys are machine-stable within a code; labels are display text. Codes
// whose findings are per-instance carry no variant.

// TypeFieldVariant is the variant of findings about one field of one type.
func TypeFieldVariant(typeName, fieldName string) (key, label string) {
	return typeName + "." + fieldName, typeName + " · " + fieldName
}

// withTypeFieldVariant stamps field- and section-level findings.
func withTypeFieldVariant(issue ValidationIssue) ValidationIssue {
	if issue.TypeName != "" && issue.FieldName != "" {
		issue.VariantKey, issue.VariantLabel = TypeFieldVariant(issue.TypeName, issue.FieldName)
	}
	return issue
}

// withTargetTypeVariant stamps wrong_target_type with the field's expected
// target type, since fixing the schema or the links depends on it.
func withTargetTypeVariant(issue ValidationIssue, expectedType string) ValidationIssue {
	issue = withTypeFieldVariant(issue)
	if issue.VariantKey != "" {
		issue.VariantKey += "->" + expectedType
		issue.VariantLabel += " → " + expectedType
	}
	return issue
}

// withNameVariant stamps findings whose case is a single name, such as an
// unknown declared type or the type a title constraint belongs to.
func withNameVariant(issue ValidationIssue, name string) ValidationIssue {
	issue.VariantKey, issue.VariantLabel = name, name
	return issue
}

// candidateTypesVariant keys a candidate type set by its sorted members.
func candidateTypesVariant(candidates []string) (key, label string) {
	sorted := slices.Sorted(slices.Values(candidates))
	return strings.Join(sorted, "+"), strings.Join(sorted, ", ")
}

// declaredTypeVariant keys a declared type together with the types its
// selectors actually match.
func declaredTypeVariant(declared string, candidates []string) (key, label string) {
	key, label = candidateTypesVariant(candidates)
	if label == "" {
		label = "no matching type"
	}
	return declared + "->" + key, declared + " → " + label
}
