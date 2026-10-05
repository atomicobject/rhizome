package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

type canonicalIdentifierFieldRewritePlan struct {
	Edits       []StructuredFieldEdit         `json:"edits"`
	Diagnostics []IdentifierRewriteDiagnostic `json:"diagnostics,omitempty"`
}

// ValidatedSnapshot returns a canonical defensive copy only when the plan is
// the sealed output of PlanIdentifierFieldRewrites and its semantic members
// have not changed. Repair mapping must consume this seam, not exported slices.
func (p *IdentifierFieldRewritePlan) ValidatedSnapshot() (*IdentifierFieldRewritePlan, error) {
	if p == nil {
		return nil, fmt.Errorf("identifier field rewrite plan is required")
	}
	if p.sealed == "" {
		return nil, fmt.Errorf("identifier field rewrite plan is not sealed")
	}
	if p.Fingerprint == "" || p.Fingerprint != p.sealed {
		return nil, fmt.Errorf("identifier field rewrite plan fingerprint does not match its seal")
	}
	snapshot := cloneIdentifierFieldRewritePlan(*p)
	canonicalizeIdentifierFieldRewritePlan(&snapshot)
	if err := validateIdentifierFieldPlanEdits(snapshot.Edits); err != nil {
		return nil, err
	}
	expected, err := identifierFieldPlanFingerprint(snapshot)
	if err != nil {
		return nil, err
	}
	if expected != p.sealed {
		return nil, fmt.Errorf("identifier field rewrite plan fingerprint does not match its members")
	}
	snapshot.Fingerprint = expected
	snapshot.sealed = expected
	return &snapshot, nil
}

func sealIdentifierFieldRewritePlan(plan IdentifierFieldRewritePlan) IdentifierFieldRewritePlan {
	plan = cloneIdentifierFieldRewritePlan(plan)
	canonicalizeIdentifierFieldRewritePlan(&plan)
	fingerprint, err := identifierFieldPlanFingerprint(plan)
	if err != nil {
		panic(fmt.Sprintf("fingerprint identifier field rewrite plan: %v", err))
	}
	plan.Fingerprint = fingerprint
	plan.sealed = fingerprint
	return plan
}

func cloneIdentifierFieldRewritePlan(plan IdentifierFieldRewritePlan) IdentifierFieldRewritePlan {
	plan.Edits = append([]StructuredFieldEdit(nil), plan.Edits...)
	plan.Diagnostics = append([]IdentifierRewriteDiagnostic(nil), plan.Diagnostics...)
	for index := range plan.Diagnostics {
		plan.Diagnostics[index].Candidates = append([]ontology.NodeRef(nil), plan.Diagnostics[index].Candidates...)
	}
	return plan
}

func canonicalizeIdentifierFieldRewritePlan(plan *IdentifierFieldRewritePlan) {
	sort.Slice(plan.Edits, func(i, j int) bool { return editTotalSortKey(plan.Edits[i]) < editTotalSortKey(plan.Edits[j]) })
	sortDiagnostics(plan.Diagnostics)
}

func identifierFieldPlanFingerprint(plan IdentifierFieldRewritePlan) (string, error) {
	encoded, err := json.Marshal(canonicalIdentifierFieldRewritePlan{Edits: plan.Edits, Diagnostics: plan.Diagnostics})
	if err != nil {
		return "", fmt.Errorf("encode identifier field rewrite plan: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validateIdentifierFieldPlanEdits(edits []StructuredFieldEdit) error {
	for _, edit := range edits {
		if err := validateIdentifierFieldPlanEdit(edit); err != nil {
			return err
		}
	}
	for left := 0; left < len(edits); left++ {
		if edits[left].Operation == StructuredFieldEditAppend {
			continue
		}
		for right := left + 1; right < len(edits); right++ {
			if edits[right].Operation == StructuredFieldEditAppend || fieldEditPhysicalPath(edits[left]) != fieldEditPhysicalPath(edits[right]) {
				continue
			}
			if byteRangesOverlap(edits[left].Range, edits[right].Range) {
				return fmt.Errorf("identifier field edits overlap in %s", fieldEditPhysicalPath(edits[left]))
			}
		}
	}
	return nil
}

func validateIdentifierFieldPlanEdit(edit StructuredFieldEdit) error {
	if canonicalRefKey(edit.OwnerRef) == "" || canonicalRefKey(edit.TargetOldRef) == "" || canonicalRefKey(edit.TargetNewRef) == "" {
		return fmt.Errorf("identifier field edit refs must use canonical vault-relative paths")
	}
	if strings.TrimSpace(edit.FieldName) == "" {
		return fmt.Errorf("identifier field edit field name is required")
	}
	switch edit.Operation {
	case StructuredFieldEditReplace:
		if edit.Kind == StructuredFieldAliasIdentifier || edit.Range.Start < 0 || edit.Range.End <= edit.Range.Start || edit.Expected == "" || edit.Replacement == "" {
			return fmt.Errorf("identifier field replace edit has invalid kind, range, or exact bytes")
		}
	case StructuredFieldEditRemove:
		if edit.Kind != StructuredFieldAliasIdentifier || edit.Range.Start < 0 || edit.Range.End <= edit.Range.Start || edit.Expected == "" || edit.Replacement != "" {
			return fmt.Errorf("identifier field remove edit has invalid kind, range, or exact bytes")
		}
	case StructuredFieldEditAppend:
		if edit.Kind != StructuredFieldAliasIdentifier || edit.Range != (ontology.ByteRange{}) || edit.Expected != "" || edit.Replacement == "" {
			return fmt.Errorf("identifier field append edit has invalid kind or bytes")
		}
	default:
		return fmt.Errorf("identifier field edit operation %q is unsupported", edit.Operation)
	}
	return nil
}

func fieldEditPhysicalPath(edit StructuredFieldEdit) string {
	return canonicalNodeNotePath(edit.OwnerRef)
}

func canonicalNodeNotePath(ref ontology.NodeRef) string {
	notePath, err := paths.CleanRelPath(ref.NotePath)
	if err != nil {
		return ""
	}
	return notePath.String()
}

func byteRangesOverlap(left, right ontology.ByteRange) bool {
	return left.Start < right.End && right.Start < left.End
}
