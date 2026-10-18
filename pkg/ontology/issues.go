package ontology

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

type ValidationIssue struct {
	Code       string `json:"code"`
	NotePath   string `json:"notePath,omitempty"`
	TypeName   string `json:"typeName,omitempty"`
	FieldName  string `json:"fieldName,omitempty"`
	NodeRef    string `json:"nodeRef,omitempty"`
	NodeID     string `json:"nodeId,omitempty"`
	Structural string `json:"structuralFingerprint,omitempty"`
	Line       int    `json:"line,omitempty"`
	LinkKind   string `json:"linkKind,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
	Message    string `json:"message"`
	// FixTarget is the note path that should be edited to resolve this issue.
	// For inverse_mismatch, this is the target note missing the inverse link.
	FixTarget string `json:"fixTarget,omitempty"`
	// FixFieldName is the field on the fix target that should receive the fix.
	// For inverse_mismatch, this is the inverse relation field name.
	FixFieldName string `json:"fixFieldName,omitempty"`
	// VariantKey and VariantLabel name the finding's case within its code
	// (issue_variant.go). Both are empty for codes without cases.
	VariantKey   string `json:"variantKey,omitempty"`
	VariantLabel string `json:"variantLabel,omitempty"`
	// CandidateTypes lists the sorted types a type_ambiguous note matches.
	CandidateTypes []string `json:"candidateTypes,omitempty"`
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func decodeIssues(raw string) []ValidationIssue {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var issues []ValidationIssue
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		return []ValidationIssue{{Code: "schema_state_decode_error", Message: err.Error()}}
	}
	return issues
}

func ValidationIssuesFromJSON(raw string) []ValidationIssue {
	return decodeIssues(raw)
}

func equalValidationIssues(left, right []ValidationIssue) bool {
	left = append([]ValidationIssue(nil), left...)
	right = append([]ValidationIssue(nil), right...)
	sortValidationIssues(left)
	sortValidationIssues(right)
	return reflect.DeepEqual(left, right)
}

func sortValidationIssues(issues []ValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		switch {
		case a.NotePath != b.NotePath:
			return a.NotePath < b.NotePath
		case a.Code != b.Code:
			return a.Code < b.Code
		case a.TypeName != b.TypeName:
			return a.TypeName < b.TypeName
		case a.FieldName != b.FieldName:
			return a.FieldName < b.FieldName
		case a.NodeRef != b.NodeRef:
			return a.NodeRef < b.NodeRef
		case a.NodeID != b.NodeID:
			return a.NodeID < b.NodeID
		case a.Structural != b.Structural:
			return a.Structural < b.Structural
		case a.Line != b.Line:
			return a.Line < b.Line
		case a.LinkKind != b.LinkKind:
			return a.LinkKind < b.LinkKind
		case a.LinkTarget != b.LinkTarget:
			return a.LinkTarget < b.LinkTarget
		case a.Message != b.Message:
			return a.Message < b.Message
		case a.FixTarget != b.FixTarget:
			return a.FixTarget < b.FixTarget
		default:
			return a.FixFieldName < b.FixFieldName
		}
	})
}
