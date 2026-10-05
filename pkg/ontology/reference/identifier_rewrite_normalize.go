package reference

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func normalizeEdits(edits []StructuredFieldEdit) ([]StructuredFieldEdit, []IdentifierRewriteDiagnostic) {
	sort.Slice(edits, func(i, j int) bool { return editTotalSortKey(edits[i]) < editTotalSortKey(edits[j]) })
	out := make([]StructuredFieldEdit, 0, len(edits))
	var diagnostics []IdentifierRewriteDiagnostic
	for i := 0; i < len(edits); {
		edit := edits[i]
		if err := validateIdentifierFieldPlanEdit(edit); err != nil {
			diagnostics = append(diagnostics, IdentifierRewriteDiagnostic{
				Kind: IdentifierRewriteDiagnosticInvalidInput, OwnerRef: edit.OwnerRef, FieldName: edit.FieldName,
				Range: edit.Range, Value: edit.Expected, Message: err.Error(),
			})
			i++
			continue
		}
		j := i + 1
		for j < len(edits) && editIdentityKey(edits[j]) == editIdentityKey(edit) {
			j++
		}
		conflict := false
		for k := i + 1; k < j; k++ {
			if edits[k] != edit {
				conflict = true
				break
			}
		}
		if conflict {
			diagnostics = append(diagnostics, IdentifierRewriteDiagnostic{
				Kind: IdentifierRewriteDiagnosticConflictingEdit, OwnerRef: edit.OwnerRef, FieldName: edit.FieldName,
				Range: edit.Range, Value: edit.Expected, Message: "multiple identifier rewrites claim the same structured field location",
			})
		} else {
			out = append(out, edit)
		}
		i = j
	}

	blocked := make([]bool, len(out))
	for left := 0; left < len(out); left++ {
		if out[left].Operation == StructuredFieldEditAppend {
			continue
		}
		for right := left + 1; right < len(out); right++ {
			if out[right].Operation == StructuredFieldEditAppend || fieldEditPhysicalPath(out[left]) != fieldEditPhysicalPath(out[right]) {
				continue
			}
			if !byteRangesOverlap(out[left].Range, out[right].Range) {
				continue
			}
			blocked[left], blocked[right] = true, true
			diagnostics = append(diagnostics, IdentifierRewriteDiagnostic{
				Kind: IdentifierRewriteDiagnosticConflictingEdit, OwnerRef: out[left].OwnerRef,
				Range: ontology.ByteRange{Start: min(out[left].Range.Start, out[right].Range.Start), End: max(out[left].Range.End, out[right].Range.End)},
				Value: out[left].Expected, Message: "structured field edits overlap in the same physical note",
			})
		}
	}
	filtered := out[:0]
	for index, edit := range out {
		if !blocked[index] {
			filtered = append(filtered, edit)
		}
	}
	return filtered, diagnostics
}

func rewriteSortKey(rewrite IdentifierRewrite) string {
	return strings.Join([]string{
		rewriteIdentityKey(rewrite), nodeRefTotalSortKey(rewrite.OldRef), nodeRefTotalSortKey(rewrite.NewRef),
		rewrite.OldIdentifier, rewrite.NewIdentifier, rewrite.PreferredField, rewrite.AliasesField, strings.Join(rewrite.AdditionalAliasesFields, "\x01"), nodeRefTotalSortKey(rewrite.DerivedFrom),
	}, "\x00")
}

func rewriteIdentityKey(rewrite IdentifierRewrite) string {
	return canonicalRefKey(rewrite.OldRef) + "\x00" + ontology.NormalizeIdentifierSemanticValue(rewrite.OldIdentifier) + "\x00" + string(effectiveRewriteMode(rewrite))
}

func occurrenceSortKey(occurrence StructuredFieldOccurrence) string {
	candidates := sortedUniqueRefs(occurrence.Candidates)
	candidateKeys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateKeys = append(candidateKeys, nodeRefTotalSortKey(candidate))
	}
	return fmt.Sprintf("%s\x00%012d\x00%012d\x00%s\x00%s\x00%s\x00%s", nodeRefTotalSortKey(occurrence.OwnerRef), occurrence.Range.Start, occurrence.Range.End, occurrence.FieldName, occurrence.Kind, occurrence.Value, strings.Join(candidateKeys, "\x01"))
}

func editSortKey(edit StructuredFieldEdit) string {
	rank := map[StructuredFieldEditOperation]int{StructuredFieldEditReplace: 0, StructuredFieldEditRemove: 1, StructuredFieldEditAppend: 2}[edit.Operation]
	return fmt.Sprintf("%s\x00%d\x00%012d\x00%012d\x00%s\x00%s", canonicalRefKey(edit.OwnerRef), rank, edit.Range.Start, edit.Range.End, edit.FieldName, edit.Expected)
}

func editIdentityKey(edit StructuredFieldEdit) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", canonicalRefKey(edit.OwnerRef), edit.FieldName, edit.Range.Start, edit.Range.End, edit.Operation)
}

func editTotalSortKey(edit StructuredFieldEdit) string {
	return strings.Join([]string{
		editSortKey(edit), string(edit.Kind), string(edit.Operation), edit.Expected, edit.Replacement,
		nodeRefTotalSortKey(edit.OwnerRef), nodeRefTotalSortKey(edit.TargetOldRef), nodeRefTotalSortKey(edit.TargetNewRef),
	}, "\x00")
}

func nodeRefTotalSortKey(ref ontology.NodeRef) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%012d\x00%012d\x00%s\x00%s", canonicalRefKey(ref), ref.NotePath, ref.Fragment, ref.NodeID, ref.TypeName, ref.Kind, ref.StartByte, ref.EndByte, ref.ParentID, ref.Structural)
}

func occurrenceDiagnostic(kind IdentifierRewriteDiagnosticKind, occurrence StructuredFieldOccurrence, message string) IdentifierRewriteDiagnostic {
	return IdentifierRewriteDiagnostic{Kind: kind, OwnerRef: occurrence.OwnerRef, FieldName: occurrence.FieldName, Range: occurrence.Range, Value: occurrence.Value, Message: message}
}

func rewriteDiagnostic(kind IdentifierRewriteDiagnosticKind, rewrite IdentifierRewrite, message string) IdentifierRewriteDiagnostic {
	return IdentifierRewriteDiagnostic{Kind: kind, OwnerRef: rewrite.OldRef, FieldName: rewrite.PreferredField, Value: rewrite.OldIdentifier, Message: message}
}

func diagnosticRank(kind IdentifierRewriteDiagnosticKind) int {
	switch kind {
	case IdentifierRewriteDiagnosticAmbiguousTarget:
		return 0
	case IdentifierRewriteDiagnosticUnresolvedTarget:
		return 1
	case IdentifierRewriteDiagnosticReviewOnly:
		return 2
	case IdentifierRewriteDiagnosticInvalidDerivedCascade:
		return 3
	case IdentifierRewriteDiagnosticInvalidInput:
		return 4
	case IdentifierRewriteDiagnosticConflictingEdit:
		return 5
	case IdentifierRewriteDiagnosticMissingPreferred, IdentifierRewriteDiagnosticMissingAlias:
		return 6
	default:
		return 7
	}
}

func sortDiagnostics(diagnostics []IdentifierRewriteDiagnostic) {
	for i := range diagnostics {
		diagnostics[i].Candidates = sortedUniqueRefs(diagnostics[i].Candidates)
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		return diagnosticTotalSortKey(diagnostics[i]) < diagnosticTotalSortKey(diagnostics[j])
	})
}

func diagnosticTotalSortKey(diagnostic IdentifierRewriteDiagnostic) string {
	candidates := sortedUniqueRefs(diagnostic.Candidates)
	candidateKeys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateKeys = append(candidateKeys, nodeRefTotalSortKey(candidate))
	}
	return fmt.Sprintf("%02d\x00%s\x00%s\x00%012d\x00%012d\x00%s\x00%s\x00%s\x00%s", diagnosticRank(diagnostic.Kind), diagnostic.Kind, nodeRefTotalSortKey(diagnostic.OwnerRef), diagnostic.Range.Start, diagnostic.Range.End, diagnostic.FieldName, diagnostic.Value, diagnostic.Message, strings.Join(candidateKeys, "\x01"))
}
