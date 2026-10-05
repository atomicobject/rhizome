package reference

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// PlanIdentifierFieldRewrites builds deterministic semantic edits without
// reading or mutating files. Plain prose/code can only enter as REVIEW_CANDIDATE
// and is never promoted to an edit.
func PlanIdentifierFieldRewrites(input IdentifierFieldRewriteInput) IdentifierFieldRewritePlan {
	rewrites := append([]IdentifierRewrite(nil), input.Rewrites...)
	var fieldDiagnostics []IdentifierRewriteDiagnostic
	for index := range rewrites {
		if err := rewrites[index].NormalizeAliasFields(); err != nil {
			fieldDiagnostics = append(fieldDiagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrites[index], err.Error()))
		}
	}
	occurrences := cloneOccurrences(input.Occurrences)
	sort.Slice(rewrites, func(i, j int) bool { return rewriteSortKey(rewrites[i]) < rewriteSortKey(rewrites[j]) })
	sort.Slice(occurrences, func(i, j int) bool { return occurrenceSortKey(occurrences[i]) < occurrenceSortKey(occurrences[j]) })

	valid, byOldRef, diagnostics := validateIdentifierRewrites(rewrites)
	diagnostics = append(diagnostics, fieldDiagnostics...)
	occurrences, occurrenceDiagnostics := validateStructuredFieldOccurrences(occurrences)
	diagnostics = append(diagnostics, occurrenceDiagnostics...)
	edits := make([]StructuredFieldEdit, 0, len(occurrences)+len(valid))

	for _, rewrite := range valid {
		owned := ownedFieldOccurrences(occurrences, rewrite.OldRef)
		preferredFound := false
		aliasFound := false
		aliasMirrorFound := false
		preferredValue := rewrite.OldIdentifier
		if effectiveRewriteMode(rewrite) == IdentifierRewriteAliasRemoval {
			preferredValue = rewrite.NewIdentifier
			if preferredRekey, ok := onePreferredRewrite(byOldRef[canonicalRefKey(rewrite.OldRef)]); ok && identifierEqual(preferredRekey.NewIdentifier, rewrite.NewIdentifier) {
				preferredValue = preferredRekey.OldIdentifier
			}
		}
		for _, occurrence := range owned {
			switch {
			case occurrence.Kind == StructuredFieldPreferredIdentifier && occurrence.FieldName == rewrite.PreferredField:
				if identifierEqual(occurrence.Value, preferredValue) {
					preferredFound = true
					if effectiveRewriteMode(rewrite) == IdentifierRewritePreferredRekey {
						replacement := rewrite.NewIdentifier
						if authored, ok := replaceBoundedIdentifier(occurrence.Value, rewrite.OldIdentifier, rewrite.NewIdentifier); ok {
							replacement = authored
						}
						edits = append(edits, replacementEdit(occurrence, rewrite, replacement))
					}
				}
			case occurrence.Kind == StructuredFieldAliasIdentifier && rewrite.HasAliasField(occurrence.FieldName):
				if identifierEqual(occurrence.Value, rewrite.OldIdentifier) {
					aliasFound = true
					edits = append(edits, removalEdit(occurrence, rewrite))
				}
				if occurrence.FieldName == rewrite.AliasesField && identifierEqual(occurrence.Value, rewrite.NewIdentifier) {
					aliasMirrorFound = true
				}
			}
		}
		if !preferredFound && rewrite.DerivedFrom.IsZero() {
			diagnostics = append(diagnostics, IdentifierRewriteDiagnostic{
				Kind: IdentifierRewriteDiagnosticMissingPreferred, OwnerRef: rewrite.OldRef,
				FieldName: rewrite.PreferredField, Value: preferredValue,
				Message: "the declared preferred identifier has no matching typed field occurrence",
			})
			continue
		}
		if effectiveRewriteMode(rewrite) == IdentifierRewriteAliasRemoval && !aliasFound {
			diagnostics = append(diagnostics, IdentifierRewriteDiagnostic{
				Kind: IdentifierRewriteDiagnosticMissingAlias, OwnerRef: rewrite.OldRef,
				FieldName: rewrite.AliasesField, Value: rewrite.OldIdentifier,
				Message: "the collided alias has no matching typed field occurrence",
			})
			continue
		}
		if effectiveRewriteMode(rewrite) == IdentifierRewritePreferredRekey && rewrite.DerivedFrom.IsZero() && !aliasMirrorFound {
			edits = append(edits, StructuredFieldEdit{
				OwnerRef: rewrite.OldRef, FieldName: rewrite.AliasesField,
				Kind: StructuredFieldAliasIdentifier, Operation: StructuredFieldEditAppend,
				Replacement: rewrite.NewIdentifier, TargetOldRef: rewrite.OldRef, TargetNewRef: rewrite.NewRef,
			})
		}
	}

	for _, occurrence := range occurrences {
		if occurrence.Kind == StructuredFieldPreferredIdentifier || occurrence.Kind == StructuredFieldAliasIdentifier {
			continue
		}
		if occurrence.Kind == StructuredFieldReviewCandidate {
			if occurrenceTouchesRewrite(occurrence, byOldRef, valid) {
				diagnostics = append(diagnostics, occurrenceDiagnostic(IdentifierRewriteDiagnosticReviewOnly, occurrence, "plain text and source-code occurrences require review and are never auto-edited"))
			}
			continue
		}
		if !isAutomaticInboundKind(occurrence.Kind) {
			diagnostics = append(diagnostics, occurrenceDiagnostic(IdentifierRewriteDiagnosticInvalidInput, occurrence, "unsupported structured field kind"))
			continue
		}

		candidates := sortedUniqueRefs(occurrence.Candidates)
		mapped := mappedCandidates(occurrence, candidates, byOldRef)
		if len(candidates) > 1 && len(mapped) > 0 {
			diagnostic := occurrenceDiagnostic(IdentifierRewriteDiagnosticAmbiguousTarget, occurrence, "structured reference resolves to multiple canonical targets")
			diagnostic.Candidates = candidates
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if len(candidates) == 0 {
			if occurrenceTouchesRewrite(occurrence, byOldRef, valid) {
				diagnostics = append(diagnostics, occurrenceDiagnostic(IdentifierRewriteDiagnosticUnresolvedTarget, occurrence, "structured reference matches a changing identifier but has no canonical target"))
			}
			continue
		}
		if len(candidates) == 1 && len(mapped) == 0 {
			if _, hasPreferred := onePreferredRewrite(byOldRef[canonicalRefKey(candidates[0])]); hasPreferred {
				diagnostics = append(diagnostics, occurrenceDiagnostic(IdentifierRewriteDiagnosticInvalidInput, occurrence, "structured value does not match the resolved identity's old identifier or locator"))
			}
			continue
		}
		if len(candidates) == 1 && len(mapped) > 1 {
			diagnostic := occurrenceDiagnostic(IdentifierRewriteDiagnosticConflictingEdit, occurrence, "structured reference matches multiple identifier rewrite memberships")
			diagnostic.Candidates = candidates
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if len(candidates) != 1 || len(mapped) != 1 {
			continue
		}
		rewrite := mapped[0]
		replacement, ok := inboundReplacement(occurrence, rewrite)
		if !ok {
			diagnostics = append(diagnostics, occurrenceDiagnostic(IdentifierRewriteDiagnosticInvalidInput, occurrence, "structured value does not match its resolved old identity"))
			continue
		}
		if replacement != occurrence.Value {
			edits = append(edits, replacementEdit(occurrence, rewrite, replacement))
		}
	}

	edits, conflictDiagnostics := normalizeEdits(edits)
	diagnostics = append(diagnostics, conflictDiagnostics...)
	sortDiagnostics(diagnostics)
	return sealIdentifierFieldRewritePlan(IdentifierFieldRewritePlan{Edits: edits, Diagnostics: diagnostics})
}

func validateIdentifierRewrites(rewrites []IdentifierRewrite) ([]IdentifierRewrite, map[string][]IdentifierRewrite, []IdentifierRewriteDiagnostic) {
	byOldRef := make(map[string][]IdentifierRewrite, len(rewrites))
	invalid := make(map[string]bool)
	seen := make(map[string]struct{}, len(rewrites))
	preferredByRef := make(map[string]string, len(rewrites))
	var diagnostics []IdentifierRewriteDiagnostic
	for _, rewrite := range rewrites {
		refKey := canonicalRefKey(rewrite.OldRef)
		key := rewriteIdentityKey(rewrite)
		if refKey == "" || canonicalRefKey(rewrite.NewRef) == "" || strings.TrimSpace(rewrite.OldIdentifier) == "" || strings.TrimSpace(rewrite.NewIdentifier) == "" || strings.TrimSpace(rewrite.PreferredField) == "" || strings.TrimSpace(rewrite.AliasesField) == "" {
			invalid[key] = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "rewrite requires old/new canonical refs, identifiers, and preferred/aliases fields"))
			continue
		}
		if effectiveRewriteMode(rewrite) == "" {
			invalid[key] = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "rewrite mode is unsupported"))
			continue
		}
		if identifierEqual(rewrite.OldIdentifier, rewrite.NewIdentifier) {
			invalid[key] = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "old and new identifier must differ"))
			continue
		}
		if effectiveRewriteMode(rewrite) == IdentifierRewriteAliasRemoval && !rewrite.DerivedFrom.IsZero() {
			invalid[key] = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "alias removal cannot declare derived ownership"))
			continue
		}
		if effectiveRewriteMode(rewrite) == IdentifierRewriteAliasRemoval && canonicalRefKey(rewrite.OldRef) != canonicalRefKey(rewrite.NewRef) {
			invalid[key] = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "alias removal must preserve its canonical ref"))
			continue
		}
		if _, exists := seen[key]; exists {
			invalid[key] = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "duplicate semantic identifier rewrite"))
			continue
		}
		seen[key] = struct{}{}
		if effectiveRewriteMode(rewrite) == IdentifierRewritePreferredRekey {
			if previous, exists := preferredByRef[refKey]; exists {
				invalid[key] = true
				invalid[previous] = true
				diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidInput, rewrite, "canonical ref has conflicting preferred rekeys"))
				continue
			}
			preferredByRef[refKey] = key
		}
		byOldRef[refKey] = append(byOldRef[refKey], rewrite)
	}

	for {
		changed := false
		for _, rewrite := range rewrites {
			key := rewriteIdentityKey(rewrite)
			if rewrite.DerivedFrom.IsZero() || invalid[key] {
				continue
			}
			parent, ok := oneValidPreferredRewrite(byOldRef[canonicalRefKey(rewrite.DerivedFrom)], invalid)
			if ok && validDerivedCascade(parent, rewrite) {
				continue
			}
			invalid[key] = true
			changed = true
			diagnostics = append(diagnostics, rewriteDiagnostic(IdentifierRewriteDiagnosticInvalidDerivedCascade, rewrite, "derived rewrite must preserve structural descent and the same bounded suffix under a valid parent rewrite"))
		}
		if !changed {
			break
		}
	}

	valid := make([]IdentifierRewrite, 0, len(rewrites))
	validByOldRef := make(map[string][]IdentifierRewrite, len(rewrites))
	for _, rewrite := range rewrites {
		key := rewriteIdentityKey(rewrite)
		if invalid[key] {
			continue
		}
		valid = append(valid, rewrite)
		refKey := canonicalRefKey(rewrite.OldRef)
		validByOldRef[refKey] = append(validByOldRef[refKey], rewrite)
	}
	return valid, validByOldRef, diagnostics
}

func onePreferredRewrite(rewrites []IdentifierRewrite) (IdentifierRewrite, bool) {
	var found IdentifierRewrite
	count := 0
	for _, rewrite := range rewrites {
		if effectiveRewriteMode(rewrite) == IdentifierRewritePreferredRekey {
			found = rewrite
			count++
		}
	}
	return found, count == 1
}

func oneValidPreferredRewrite(rewrites []IdentifierRewrite, invalid map[string]bool) (IdentifierRewrite, bool) {
	valid := make([]IdentifierRewrite, 0, len(rewrites))
	for _, rewrite := range rewrites {
		if !invalid[rewriteIdentityKey(rewrite)] {
			valid = append(valid, rewrite)
		}
	}
	return onePreferredRewrite(valid)
}

func replacementEdit(occurrence StructuredFieldOccurrence, rewrite IdentifierRewrite, replacement string) StructuredFieldEdit {
	return StructuredFieldEdit{
		OwnerRef: occurrence.OwnerRef, FieldName: occurrence.FieldName, Kind: occurrence.Kind,
		Operation: StructuredFieldEditReplace, Range: occurrence.Range, Expected: occurrence.Value,
		Replacement: replacement, TargetOldRef: rewrite.OldRef, TargetNewRef: rewrite.NewRef,
	}
}

func removalEdit(occurrence StructuredFieldOccurrence, rewrite IdentifierRewrite) StructuredFieldEdit {
	return StructuredFieldEdit{
		OwnerRef: occurrence.OwnerRef, FieldName: occurrence.FieldName, Kind: occurrence.Kind,
		Operation: StructuredFieldEditRemove, Range: occurrence.Range, Expected: occurrence.Value,
		TargetOldRef: rewrite.OldRef, TargetNewRef: rewrite.NewRef,
	}
}

func ownedFieldOccurrences(occurrences []StructuredFieldOccurrence, owner ontology.NodeRef) []StructuredFieldOccurrence {
	key := canonicalRefKey(owner)
	out := make([]StructuredFieldOccurrence, 0)
	for _, occurrence := range occurrences {
		if canonicalRefKey(occurrence.OwnerRef) == key {
			out = append(out, occurrence)
		}
	}
	return out
}

func occurrenceTouchesRewrite(occurrence StructuredFieldOccurrence, byOldRef map[string][]IdentifierRewrite, rewrites []IdentifierRewrite) bool {
	for _, candidate := range occurrence.Candidates {
		for _, rewrite := range byOldRef[canonicalRefKey(candidate)] {
			if occurrenceMatchesRewrite(occurrence, rewrite) {
				return true
			}
		}
	}
	for _, rewrite := range rewrites {
		if identifierEqual(occurrence.Value, rewrite.OldIdentifier) || normalizeLocator(occurrence.Value) == normalizeLocator(rewrite.OldRef.String()) {
			return true
		}
		if _, ok := replaceBoundedIdentifier(strings.TrimSpace(occurrence.Value), rewrite.OldIdentifier, rewrite.NewIdentifier); ok {
			return true
		}
	}
	return false
}

func mappedCandidates(occurrence StructuredFieldOccurrence, candidates []ontology.NodeRef, byOldRef map[string][]IdentifierRewrite) []IdentifierRewrite {
	out := make([]IdentifierRewrite, 0, len(candidates))
	for _, candidate := range candidates {
		for _, rewrite := range byOldRef[canonicalRefKey(candidate)] {
			if occurrenceMatchesRewrite(occurrence, rewrite) {
				out = append(out, rewrite)
			}
		}
	}
	return out
}

func occurrenceMatchesRewrite(occurrence StructuredFieldOccurrence, rewrite IdentifierRewrite) bool {
	_, ok := inboundReplacement(occurrence, rewrite)
	return ok
}

func isAutomaticInboundKind(kind StructuredFieldKind) bool {
	switch kind {
	case StructuredFieldTypedIdentifierReference, StructuredFieldCanonicalNodeRef, StructuredFieldIdentifierBackedLocator:
		return true
	default:
		return false
	}
}

func cloneOccurrences(in []StructuredFieldOccurrence) []StructuredFieldOccurrence {
	out := append([]StructuredFieldOccurrence(nil), in...)
	for i := range out {
		out[i].Candidates = append([]ontology.NodeRef(nil), out[i].Candidates...)
	}
	return out
}

func validateStructuredFieldOccurrences(occurrences []StructuredFieldOccurrence) ([]StructuredFieldOccurrence, []IdentifierRewriteDiagnostic) {
	valid := make([]StructuredFieldOccurrence, 0, len(occurrences))
	var diagnostics []IdentifierRewriteDiagnostic
	for _, occurrence := range occurrences {
		message := ""
		switch {
		case canonicalRefKey(occurrence.OwnerRef) == "":
			message = "structured field occurrence owner must use a canonical vault-relative path"
		case occurrence.Range.Start < 0 || occurrence.Range.End <= occurrence.Range.Start:
			message = "structured field occurrence requires a non-negative non-empty exact value range"
		default:
			for _, candidate := range occurrence.Candidates {
				if canonicalRefKey(candidate) == "" {
					message = "structured field occurrence candidate must use a canonical vault-relative path"
					break
				}
			}
		}
		if message != "" {
			diagnostics = append(diagnostics, occurrenceDiagnostic(IdentifierRewriteDiagnosticInvalidInput, occurrence, message))
			continue
		}
		valid = append(valid, occurrence)
	}
	return valid, diagnostics
}

func sortedUniqueRefs(refs []ontology.NodeRef) []ontology.NodeRef {
	refs = append([]ontology.NodeRef(nil), refs...)
	sort.Slice(refs, func(i, j int) bool {
		left, right := canonicalRefKey(refs[i]), canonicalRefKey(refs[j])
		if left != right {
			return left < right
		}
		return nodeRefTotalSortKey(refs[i]) < nodeRefTotalSortKey(refs[j])
	})
	out := refs[:0]
	for _, ref := range refs {
		if len(out) == 0 || nodeRefTotalSortKey(out[len(out)-1]) != nodeRefTotalSortKey(ref) {
			out = append(out, ref)
		}
	}
	return out
}

func identifierEqual(a, b string) bool {
	return ontology.NormalizeIdentifierSemanticValue(a) == ontology.NormalizeIdentifierSemanticValue(b)
}

func canonicalRefKey(ref ontology.NodeRef) string {
	notePath, err := paths.CleanRelPath(ref.NotePath)
	if err != nil || notePath.String() == "" {
		return ""
	}
	return strings.Join([]string{
		notePath.String(),
		strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#"),
		strings.TrimSpace(ref.TypeName),
		string(ref.Kind),
	}, "\x00")
}

func normalizeLocator(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "#")
}
