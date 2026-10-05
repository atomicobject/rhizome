package identifierreconcile

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func canonicalizeRepairRewrite(rewrite *reference.IdentifierRewrite) error {
	for _, item := range []struct {
		name string
		ref  *ontology.NodeRef
	}{{"old", &rewrite.OldRef}, {"new", &rewrite.NewRef}} {
		if err := canonicalizeRepairRef(item.ref); err != nil {
			return fmt.Errorf("%s rewrite ref: %w", item.name, err)
		}
	}
	if !rewrite.DerivedFrom.IsZero() {
		if err := canonicalizeRepairRef(&rewrite.DerivedFrom); err != nil {
			return fmt.Errorf("derived parent ref: %w", err)
		}
	}
	rewrite.OldIdentifier = strings.TrimSpace(rewrite.OldIdentifier)
	rewrite.NewIdentifier = strings.TrimSpace(rewrite.NewIdentifier)
	rewrite.PreferredField = strings.TrimSpace(rewrite.PreferredField)
	return rewrite.NormalizeAliasFields()
}

func canonicalizeRepairRef(ref *ontology.NodeRef) error {
	path, err := canonicalRepairPath(ref.NotePath)
	if err != nil {
		return err
	}
	ref.NotePath = path
	ref.Fragment = strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	ref.TypeName = strings.TrimSpace(ref.TypeName)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	return nil
}

func sameMoveConflict(left, right *obsidian.MoveConflict) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func canonicalRepairPath(value string) (string, error) {
	rel, err := paths.CleanRelPath(strings.TrimSpace(value))
	if err != nil || rel == "" {
		return "", fmt.Errorf("path must be vault-relative: %w", err)
	}
	return rel.String(), nil
}

func validateRepairRefTransition(rewrite reference.IdentifierRewrite) error {
	if rewrite.OldRef.TypeName != rewrite.NewRef.TypeName || rewrite.OldRef.Kind != rewrite.NewRef.Kind {
		return fmt.Errorf("rewrite must preserve canonical ref type and kind")
	}
	if !validRefComponentTransition(rewrite.OldRef.Fragment, rewrite.NewRef.Fragment, rewrite) {
		return fmt.Errorf("rewrite has invalid top-level fragment identity transition")
	}
	if !validRefComponentTransition(rewrite.OldRef.NodeID, rewrite.NewRef.NodeID, rewrite) {
		return fmt.Errorf("rewrite has invalid canonical node id transition")
	}
	return nil
}

func rewriteMatchesNode(rewrite reference.IdentifierRewrite, node CanonicalNodeKey) bool {
	notePath, err := canonicalRepairPath(rewrite.OldRef.NotePath)
	return err == nil && notePath == node.NotePath &&
		strings.TrimPrefix(strings.TrimSpace(rewrite.OldRef.Fragment), "#") == node.Fragment &&
		strings.TrimSpace(rewrite.OldRef.TypeName) == node.TypeName &&
		strings.TrimSpace(rewrite.PreferredField) == node.IdentifierField
}

func validDerivedRepair(parent, child reference.IdentifierRewrite) bool {
	oldSuffix, ok := derivedSuffix(child.OldIdentifier, parent.OldIdentifier)
	if !ok {
		return false
	}
	newSuffix, ok := derivedSuffix(child.NewIdentifier, parent.NewIdentifier)
	return ok && oldSuffix == newSuffix
}

func derivedSuffix(child, parent string) (string, bool) {
	child = ontology.NormalizeIdentifierSemanticValue(child)
	parent = ontology.NormalizeIdentifierSemanticValue(parent)
	if !strings.HasPrefix(child, parent) || len(child) == len(parent) {
		return "", false
	}
	suffix := child[len(parent):]
	r, _ := utf8.DecodeRuneInString(suffix)
	return suffix, !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func sameRepairRef(left, right ontology.NodeRef) bool {
	return repairRefKey(left) == repairRefKey(right)
}

func repairRefKey(ref ontology.NodeRef) string {
	notePath, err := canonicalRepairPath(ref.NotePath)
	if err != nil {
		notePath = "<invalid-path>" + strings.TrimSpace(ref.NotePath)
	}
	return strings.Join([]string{notePath, strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#"), strings.TrimSpace(ref.TypeName), string(ref.Kind)}, "\x00")
}

func repairRewriteKey(rewrite reference.IdentifierRewrite) string {
	return strings.Join([]string{repairRefKey(rewrite.OldRef), string(rewrite.Mode), IdentifierComparisonKey(rewrite.OldIdentifier), repairRefKey(rewrite.NewRef), rewrite.NewIdentifier, rewrite.AliasesField, strings.Join(rewrite.AdditionalAliasesFields, "\x01")}, "\x00")
}

func validDerivedRefTransition(parent, child reference.IdentifierRewrite) bool {
	if child.OldRef.TypeName != child.NewRef.TypeName || child.OldRef.Kind != child.NewRef.Kind {
		return false
	}
	if child.OldRef.NotePath != parent.OldRef.NotePath || child.NewRef.NotePath != parent.NewRef.NotePath {
		return false
	}
	return validRefComponentTransition(child.OldRef.Fragment, child.NewRef.Fragment, child) &&
		validRefComponentTransition(child.OldRef.NodeID, child.NewRef.NodeID, child)
}

func validRefComponentTransition(oldValue, newValue string, rewrite reference.IdentifierRewrite) bool {
	oldValue = strings.TrimSpace(oldValue)
	newValue = strings.TrimSpace(newValue)
	if oldValue == newValue {
		return true
	}
	if oldValue == "" || newValue == "" {
		return false
	}
	return semanticRepairTransition(oldValue, newValue, rewrite)
}

func fieldEditRewrite(edit reference.StructuredFieldEdit, rewrites []reference.IdentifierRewrite) (reference.IdentifierRewrite, bool) {
	for _, rewrite := range rewrites {
		if !repairRefsValid(edit.TargetOldRef, edit.TargetNewRef) || !sameRepairRef(edit.TargetOldRef, rewrite.OldRef) || !sameRepairRef(edit.TargetNewRef, rewrite.NewRef) {
			continue
		}
		if edit.Kind == reference.StructuredFieldAliasIdentifier {
			if edit.Operation == reference.StructuredFieldEditRemove && rewrite.HasAliasField(edit.FieldName) && IdentifierComparisonKey(edit.Expected) == IdentifierComparisonKey(rewrite.OldIdentifier) ||
				edit.Operation == reference.StructuredFieldEditAppend && edit.FieldName == rewrite.AliasesField && IdentifierComparisonKey(edit.Replacement) == IdentifierComparisonKey(rewrite.NewIdentifier) {
				return rewrite, true
			}
			continue
		}
		if semanticRepairTransition(edit.Expected, edit.Replacement, rewrite) {
			return rewrite, true
		}
	}
	return reference.IdentifierRewrite{}, false
}

func linkEditRewrite(edit reference.StructuredLinkEdit, rewrites []reference.IdentifierRewrite) (reference.IdentifierRewrite, bool) {
	for _, rewrite := range rewrites {
		if repairRefsValid(edit.TargetOldRef, edit.TargetNewRef) && sameRepairRef(edit.TargetOldRef, rewrite.OldRef) && sameRepairRef(edit.TargetNewRef, rewrite.NewRef) &&
			(semanticRepairTransition(edit.Expected, edit.Replacement, rewrite) || linkPathRepairTransition(edit, rewrite)) {
			return rewrite, true
		}
	}
	return reference.IdentifierRewrite{}, false
}

func linkPathRepairTransition(edit reference.StructuredLinkEdit, rewrite reference.IdentifierRewrite) bool {
	if edit.Component != reference.LinkComponentPath || sameRepairRef(rewrite.OldRef, rewrite.NewRef) {
		return false
	}
	authoredBase := path.Base(strings.ReplaceAll(edit.Expected, "\\", "/"))
	oldBase := path.Base(rewrite.OldRef.NotePath)
	newBase := path.Base(rewrite.NewRef.NotePath)
	authoredStem, authoredHasExtension := stripRepairMarkdownExtension(authoredBase)
	oldStem, _ := stripRepairMarkdownExtension(oldBase)
	newStem, _ := stripRepairMarkdownExtension(newBase)
	if authoredBase == "" || oldBase == "" || newBase == "" || !strings.EqualFold(authoredStem, oldStem) {
		return false
	}
	writtenNewBase := newBase
	if !authoredHasExtension {
		writtenNewBase = newStem
	}
	return edit.Replacement == strings.TrimSuffix(edit.Expected, authoredBase)+writtenNewBase
}

func stripRepairMarkdownExtension(value string) (string, bool) {
	if len(value) >= 3 && strings.EqualFold(value[len(value)-3:], ".md") {
		return value[:len(value)-3], true
	}
	return value, false
}

func semanticRepairTransition(expected, replacement string, rewrite reference.IdentifierRewrite) bool {
	if expected == replacement || strings.TrimSpace(expected) == "" || strings.TrimSpace(replacement) == "" {
		return false
	}
	oldComponent := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(expected), "#"), "^")
	newComponent := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(replacement), "#"), "^")
	if IdentifierComparisonKey(oldComponent) == IdentifierComparisonKey(rewrite.OldIdentifier) && IdentifierComparisonKey(newComponent) == IdentifierComparisonKey(rewrite.NewIdentifier) {
		return true
	}
	if normalizedRepairLocator(expected) == normalizedRepairLocator(rewrite.OldRef.String()) && normalizedRepairLocator(replacement) == normalizedRepairLocator(rewrite.NewRef.String()) {
		return true
	}
	want, changed := replaceBoundedRepairIdentifier(expected, rewrite.OldIdentifier, rewrite.NewIdentifier)
	if changed && want == replacement {
		return true
	}
	oldDisplay := reference.DottedIdentifierDisplay(rewrite.OldIdentifier)
	newDisplay := reference.DottedIdentifierDisplay(rewrite.NewIdentifier)
	want, changed = replaceBoundedRepairIdentifier(expected, oldDisplay, newDisplay)
	return changed && want == replacement
}

func replaceBoundedRepairIdentifier(value, oldIdentifier, newIdentifier string) (string, bool) {
	if oldIdentifier == "" || oldIdentifier == newIdentifier {
		return value, false
	}
	var out strings.Builder
	cursor, searchFrom := 0, 0
	changed := false
	for searchFrom < len(value) {
		start, end, found := nextFoldedRepairIdentifier(value, oldIdentifier, searchFrom)
		if !found {
			break
		}
		if repairIdentifierBoundary(value, start, end) {
			out.WriteString(value[cursor:start])
			out.WriteString(newIdentifier)
			cursor = end
			changed = true
			searchFrom = end
			continue
		}
		_, size := utf8.DecodeRuneInString(value[start:])
		searchFrom = start + size
	}
	if !changed {
		return value, false
	}
	out.WriteString(value[cursor:])
	return out.String(), true
}

func nextFoldedRepairIdentifier(value, oldIdentifier string, from int) (int, int, bool) {
	wantRunes := utf8.RuneCountInString(oldIdentifier)
	for start := from; start < len(value); {
		end := start
		for range wantRunes {
			if end >= len(value) {
				break
			}
			_, size := utf8.DecodeRuneInString(value[end:])
			end += size
		}
		if strings.EqualFold(value[start:end], oldIdentifier) {
			return start, end, true
		}
		_, size := utf8.DecodeRuneInString(value[start:])
		start += size
	}
	return 0, 0, false
}

func repairIdentifierBoundary(value string, start, end int) bool {
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

func repairRefsValid(refs ...ontology.NodeRef) bool {
	for _, ref := range refs {
		if _, err := canonicalRepairPath(ref.NotePath); err != nil {
			return false
		}
	}
	return true
}

func normalizedRepairLocator(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
}

func validateRequiredFieldObligations(component CollisionRepairIntent, rewrites []reference.IdentifierRewrite, roots map[int]struct{}, synthesized []synthesizedDerivedField) error {
	rootIndices := make([]int, 0, len(roots))
	for index := range roots {
		rootIndices = append(rootIndices, index)
	}
	sort.Ints(rootIndices)
	for _, index := range rootIndices {
		if index < 0 || index >= len(rewrites) {
			return fmt.Errorf("root rewrite index is outside the rewrite union")
		}
		rewrite := rewrites[index]
		if rewrite.Mode == reference.IdentifierRewriteAliasRemoval {
			if !hasAliasObligation(component.AliasEdits, rewrite, reference.StructuredFieldEditRemove) {
				return fmt.Errorf("alias removal root is missing its required alias edit")
			}
			continue
		}
		if !hasPreferredIdentifierObligation(component.FieldEdits, rewrite) {
			return fmt.Errorf("preferred rekey is missing its required preferred identifier edit")
		}
		// A preferred identifier can already be missing its old mirror; that is
		// precisely the legacy validation state reconciliation must repair. The
		// sealed discovery contributes a removal whenever the old alias exists,
		// while every rekey must still append the replacement mirror.
		if !hasAliasObligation(component.AliasEdits, rewrite, reference.StructuredFieldEditAppend) {
			return fmt.Errorf("preferred rekey is missing its required replacement alias edit")
		}
	}
	for index, rewrite := range rewrites {
		if _, root := roots[index]; root {
			continue
		}
		found := false
		for _, intent := range component.FieldEdits {
			if requiredDerivedFieldEdit(intent.Edit, rewrite) {
				found = true
				break
			}
		}
		if !found {
			found = hasSynthesizedDerivedObligation(synthesized, rewrite)
		}
		if !found {
			return fmt.Errorf("derived rewrite %s is missing its required descendant field edit", rewrite.OldRef.String())
		}
	}
	return nil
}

func hasSynthesizedDerivedObligation(fields []synthesizedDerivedField, rewrite reference.IdentifierRewrite) bool {
	for _, field := range fields {
		if sameRepairRef(field.OwnerRef, rewrite.OldRef) && field.FieldName == rewrite.PreferredField &&
			IdentifierComparisonKey(field.Value) == IdentifierComparisonKey(rewrite.OldIdentifier) {
			return true
		}
	}
	return false
}

func requiredDerivedFieldEdit(edit reference.StructuredFieldEdit, rewrite reference.IdentifierRewrite) bool {
	if edit.Operation != reference.StructuredFieldEditReplace ||
		!sameRepairRef(edit.OwnerRef, rewrite.OldRef) ||
		!sameRepairRef(edit.TargetOldRef, rewrite.OldRef) ||
		!sameRepairRef(edit.TargetNewRef, rewrite.NewRef) ||
		!semanticRepairTransition(edit.Expected, edit.Replacement, rewrite) {
		return false
	}
	switch edit.Kind {
	case reference.StructuredFieldPreferredIdentifier, reference.StructuredFieldIdentifierBackedLocator:
		return edit.FieldName == rewrite.PreferredField
	default:
		return false
	}
}

func hasPreferredIdentifierObligation(edits []FieldRepairIntent, rewrite reference.IdentifierRewrite) bool {
	for _, intent := range edits {
		edit := intent.Edit
		if edit.Kind == reference.StructuredFieldPreferredIdentifier && edit.FieldName == rewrite.PreferredField && edit.Operation == reference.StructuredFieldEditReplace && sameRepairRef(edit.OwnerRef, rewrite.OldRef) && semanticRepairTransition(edit.Expected, edit.Replacement, rewrite) {
			return true
		}
	}
	return false
}

func hasAliasObligation(edits []FieldRepairIntent, rewrite reference.IdentifierRewrite, operation reference.StructuredFieldEditOperation) bool {
	for _, intent := range edits {
		edit := intent.Edit
		if edit.Operation != operation || edit.FieldName != rewrite.AliasesField || !sameRepairRef(edit.OwnerRef, rewrite.OldRef) {
			continue
		}
		if operation == reference.StructuredFieldEditRemove && IdentifierComparisonKey(edit.Expected) == IdentifierComparisonKey(rewrite.OldIdentifier) {
			return true
		}
		if operation == reference.StructuredFieldEditAppend && IdentifierComparisonKey(edit.Replacement) == IdentifierComparisonKey(rewrite.NewIdentifier) {
			return true
		}
	}
	return false
}
