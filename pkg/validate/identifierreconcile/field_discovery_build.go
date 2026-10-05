package identifierreconcile

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// IdentifierFieldDiscoveryRequest supplies only the live vault and semantic
// contract. Paths, source snapshots, projections, and authored ranges are
// deliberately discovered inside DiscoverIdentifierFields.
type IdentifierFieldDiscoveryRequest struct {
	VaultDef     obsidian.VaultDefinition
	RootRewrites []reference.IdentifierRewrite
}

// synthesizedDerivedField is constructor-owned evidence that a derived
// preferred identity exists only in the semantic projection. It can satisfy a
// descendant obligation without inventing a source edit.
type synthesizedDerivedField struct {
	OwnerRef  ontology.NodeRef `json:"ownerRef"`
	FieldName string           `json:"fieldName"`
	Value     string           `json:"value"`
}

// DiscoverIdentifierFields derives and seals the complete structured-field
// inventory for one rewrite union from the current Markdown vault.
func DiscoverIdentifierFields(ctx context.Context, req IdentifierFieldDiscoveryRequest) (*IdentifierFieldDiscovery, error) {
	source, canonicalRewrites, err := discoverIdentifierRepairSource(ctx, req.VaultDef, req.RootRewrites)
	if err != nil {
		return nil, err
	}
	rewriteFingerprint, err := repairRewriteSetFingerprint(canonicalRewrites)
	if err != nil {
		return nil, err
	}
	preconditions := make([]SourcePrecondition, 0, len(source.sources))
	for _, noteSource := range source.sources {
		notePath, err := canonicalRepairPath(noteSource.Path.String())
		if err != nil || notePath != noteSource.Path.String() || !validSHA256Fingerprint(noteSource.ContentHash) {
			return nil, fmt.Errorf("note source inventory contains a non-canonical path or hash for %s", noteSource.Path)
		}
		preconditions = append(preconditions, SourcePrecondition{NotePath: notePath, SourceHash: noteSource.ContentHash})
	}
	if err := validateSourcePreconditions(preconditions); err != nil {
		return nil, fmt.Errorf("complete note source inventory: %w", err)
	}

	occurrences, err := discoverStructuredFieldOccurrences(source.schema, source.projections, canonicalRewrites)
	if err != nil {
		return nil, err
	}
	synthesizedDerived := discoverSynthesizedDerivedFields(source.projections, canonicalRewrites)
	plan := reference.PlanIdentifierFieldRewrites(reference.IdentifierFieldRewriteInput{
		Rewrites: canonicalRewrites, Occurrences: occurrences,
	})
	planSnapshot, err := plan.ValidatedSnapshot()
	if err != nil {
		return nil, fmt.Errorf("validate identifier field rewrite plan: %w", err)
	}
	snapshot := identifierFieldDiscoverySnapshot{
		Rewrites:            canonicalRewrites,
		SynthesizedDerived:  synthesizedDerived,
		SourcePreconditions: preconditions,
		SchemaHash:          source.schema.Hash,
		RewriteFingerprint:  rewriteFingerprint,
		Occurrences:         cloneStructuredFieldOccurrences(occurrences),
		Edits:               append([]reference.StructuredFieldEdit(nil), planSnapshot.Edits...),
		Diagnostics:         append([]reference.IdentifierRewriteDiagnostic(nil), planSnapshot.Diagnostics...),
	}
	sealed, err := identifierFieldDiscoveryFingerprint(snapshot)
	if err != nil {
		return nil, err
	}
	return &IdentifierFieldDiscovery{
		source: source, rewrites: cloneIdentifierRewrites(canonicalRewrites),
		synthesizedDerived:  append([]synthesizedDerivedField(nil), synthesizedDerived...),
		sourcePreconditions: append([]SourcePrecondition(nil), snapshot.SourcePreconditions...),
		schemaHash:          snapshot.SchemaHash,
		rewriteFingerprint:  snapshot.RewriteFingerprint,
		occurrences:         cloneStructuredFieldOccurrences(snapshot.Occurrences),
		edits:               append([]reference.StructuredFieldEdit(nil), snapshot.Edits...),
		diagnostics:         append([]reference.IdentifierRewriteDiagnostic(nil), snapshot.Diagnostics...),
		sealed:              sealed,
	}, nil
}

func discoverSynthesizedDerivedFields(projections []*ontology.NodeProjection, rewrites []reference.IdentifierRewrite) []synthesizedDerivedField {
	byRef := make(map[string]reference.IdentifierRewrite, len(rewrites))
	for _, rewrite := range rewrites {
		if !rewrite.DerivedFrom.IsZero() {
			byRef[repairRefKey(rewrite.OldRef)] = rewrite
		}
	}
	out := make([]synthesizedDerivedField, 0)
	for _, projection := range projections {
		field, binding, value, ok := projectedPreferredIdentifier(projection)
		if !ok || !binding.Derived {
			continue
		}
		owner := semanticRepairRef(projection.Ref)
		rewrite, found := byRef[repairRefKey(owner)]
		if !found || field.Name != rewrite.PreferredField || IdentifierComparisonKey(value) != IdentifierComparisonKey(rewrite.OldIdentifier) {
			continue
		}
		out = append(out, synthesizedDerivedField{OwnerRef: owner, FieldName: field.Name, Value: value})
	}
	sort.Slice(out, func(i, j int) bool {
		if repairRefKey(out[i].OwnerRef) != repairRefKey(out[j].OwnerRef) {
			return repairRefKey(out[i].OwnerRef) < repairRefKey(out[j].OwnerRef)
		}
		return out[i].FieldName < out[j].FieldName
	})
	return out
}

type projectedFieldValue struct {
	owner         ontology.NodeRef
	field         *ontology.Field
	binding       ontology.FieldBinding
	value         string
	range_        ontology.ByteRange
	contentLen    int
	authoredExact bool
	alias         bool
}

func discoverStructuredFieldOccurrences(schema *ontology.Schema, projections []*ontology.NodeProjection, rewrites []reference.IdentifierRewrite) ([]reference.StructuredFieldOccurrence, error) {
	aliasFieldsByType := identifierAliasFieldsByType(projections, rewrites)

	identifierCandidates := make(map[string][]ontology.NodeRef)
	locatorCandidates := make(map[string][]ontology.NodeRef)
	var values []projectedFieldValue
	for _, projection := range projections {
		if projection == nil || projection.Type == nil {
			continue
		}
		aliasFields := aliasFieldsByType[projection.Type.Name]
		indexProjectionLocators(locatorCandidates, projection.Ref)
		if rewrite, ok := rewriteByRepairRef(rewrites, semanticRepairRef(projection.Ref)); ok && rewrite.PreferredField == reference.StructuredFieldBlockLocator {
			if occurrence, found := blockLocatorOccurrence(projection, rewrite); found {
				values = append(values, occurrence)
			}
		}
		for _, field := range projection.Type.Fields {
			if field == nil {
				continue
			}
			binding, found := projection.Fields[field.Name]
			if !found || (!binding.Present && !binding.Derived) {
				continue
			}
			for index, value := range binding.Values {
				entry := projectedFieldValue{owner: projection.Ref, field: field, binding: binding, value: value, contentLen: len(projection.Snapshot.Content), alias: slices.Contains(aliasFields, field.Name)}
				if index < len(binding.ValueRanges) {
					entry.range_ = binding.ValueRanges[index]
				}
				entry.value, entry.authoredExact = exactProjectedFieldValue(projection.Snapshot.Content, field, binding, index, entry.value, entry.range_)
				values = append(values, entry)
				if field.IsIdentifier {
					appendFieldCandidate(identifierCandidates, ontology.NormalizeIdentifierSemanticValue(value), projection.Ref)
				}
				if entry.alias {
					appendFieldCandidate(identifierCandidates, ontology.NormalizeIdentifierSemanticValue(value), projection.Ref)
				}
			}
		}
		if projection.Ref.Kind == ontology.NodeKindNote {
			for _, aliasField := range aliasFields {
				if projection.Type.ByName[aliasField] != nil {
					continue
				}
				binding := ontology.ProjectRawFrontmatterFieldBinding(projection.Snapshot, aliasField)
				if !binding.Present {
					continue
				}
				field := &ontology.Field{Name: aliasField, Source: aliasField, SourceKind: ontology.FieldSourceFrontmatter, Kind: ontology.FieldKindScalar, TypeName: "String", List: true}
				for index, value := range binding.Values {
					entry := projectedFieldValue{owner: projection.Ref, field: field, binding: binding, value: value, contentLen: len(projection.Snapshot.Content), alias: slices.Contains(aliasFields, field.Name)}
					if index < len(binding.ValueRanges) {
						entry.range_ = binding.ValueRanges[index]
					}
					entry.value, entry.authoredExact = exactProjectedFieldValue(projection.Snapshot.Content, field, binding, index, entry.value, entry.range_)
					values = append(values, entry)
					appendFieldCandidate(identifierCandidates, ontology.NormalizeIdentifierSemanticValue(value), projection.Ref)
				}
			}
		}
	}
	normalizeFieldCandidateIndex(identifierCandidates)
	normalizeFieldCandidateIndex(locatorCandidates)

	occurrences := make([]reference.StructuredFieldOccurrence, 0, len(values))
	for _, value := range values {
		kind, candidates, include := classifyProjectedFieldValue(schema, value, identifierCandidates, locatorCandidates)
		if !include {
			continue
		}
		if value.binding.Derived && kind == reference.StructuredFieldPreferredIdentifier {
			continue
		}
		if !value.authoredExact || len(value.binding.ValueRanges) != len(value.binding.Values) || !value.range_.Valid(value.contentLen) || value.range_.Len() == 0 {
			if projectedFieldValueTouchesRewrites(value, kind, candidates, rewrites) {
				return nil, fmt.Errorf("structured identifier field %s.%s in %s is relevant but its authored value range is not exact", value.owner.TypeName, value.field.Name, value.owner.NotePath)
			}
			continue
		}
		occurrences = append(occurrences, reference.StructuredFieldOccurrence{
			OwnerRef: value.owner, FieldName: value.field.Name, Kind: kind, Value: value.value,
			Range: value.range_, Candidates: append([]ontology.NodeRef(nil), candidates...),
		})
	}
	sort.Slice(occurrences, func(i, j int) bool {
		left, right := occurrences[i], occurrences[j]
		if left.OwnerRef.NotePath != right.OwnerRef.NotePath {
			return left.OwnerRef.NotePath < right.OwnerRef.NotePath
		}
		if left.Range.Start != right.Range.Start {
			return left.Range.Start < right.Range.Start
		}
		if left.Range.End != right.Range.End {
			return left.Range.End < right.Range.End
		}
		return left.FieldName < right.FieldName
	})
	return occurrences, nil
}

func rewriteByRepairRef(rewrites []reference.IdentifierRewrite, ref ontology.NodeRef) (reference.IdentifierRewrite, bool) {
	for _, rewrite := range rewrites {
		if sameRepairRef(rewrite.OldRef, ref) {
			return rewrite, true
		}
	}
	return reference.IdentifierRewrite{}, false
}

func blockLocatorOccurrence(projection *ontology.NodeProjection, rewrite reference.IdentifierRewrite) (projectedFieldValue, bool) {
	identifier, rng, ok := parserOwnedBlockLocator(projection)
	if !ok || IdentifierComparisonKey(identifier) != IdentifierComparisonKey(rewrite.OldIdentifier) {
		return projectedFieldValue{}, false
	}
	expected := "^" + identifier
	field := &ontology.Field{Name: reference.StructuredFieldBlockLocator, Kind: ontology.FieldKindLink, TypeName: projection.Ref.TypeName}
	binding := ontology.FieldBinding{Present: true, Values: []string{expected}, ValueRanges: []ontology.ByteRange{rng}, ValueRangesExact: true}
	return projectedFieldValue{owner: projection.Ref, field: field, binding: binding, value: expected, range_: rng, contentLen: len(projection.Snapshot.Content), authoredExact: true}, true
}

func exactProjectedFieldValue(content string, field *ontology.Field, binding ontology.FieldBinding, index int, value string, valueRange ontology.ByteRange) (string, bool) {
	if binding.ValueRangesExact {
		return value, true
	}
	if field == nil || !field.IsIdentifier || index >= len(binding.Values) || !valueRange.Valid(len(content)) || valueRange.Len() == 0 {
		return value, false
	}
	authored := content[valueRange.Start:valueRange.End]
	if !strings.HasPrefix(strings.TrimSpace(authored), "^") || ontology.NormalizeIdentifierSemanticValue(authored) != ontology.NormalizeIdentifierSemanticValue(value) {
		return value, false
	}
	return authored, true
}

func classifyProjectedFieldValue(schema *ontology.Schema, value projectedFieldValue, identifiers, locators map[string][]ontology.NodeRef) (reference.StructuredFieldKind, []ontology.NodeRef, bool) {
	if value.field.IsPreferredIdentifier {
		return reference.StructuredFieldPreferredIdentifier, nil, true
	}
	if value.alias {
		return reference.StructuredFieldAliasIdentifier, nil, true
	}
	if value.field.Kind != ontology.FieldKindLink {
		return "", nil, false
	}
	raw := strings.TrimSpace(value.value)
	target := unwrapStructuredFieldTarget(raw)
	identifierKey := ontology.NormalizeIdentifierSemanticValue(target)
	candidates := append([]ontology.NodeRef(nil), identifiers[identifierKey]...)
	locatorKey := normalizeFieldLocator(target)
	if strings.HasPrefix(strings.TrimSpace(target), "#") || strings.HasPrefix(strings.TrimSpace(target), "^") {
		locatorKey = scopedFieldLocatorKey(value.owner.NotePath, target)
	}
	for _, candidate := range locators[locatorKey] {
		candidates = appendUniqueFieldCandidate(candidates, candidate)
	}
	candidates = filterFieldCandidatesByType(schema, candidates, value.field.TypeName)
	if raw == target && len(identifiers[identifierKey]) > 0 {
		return reference.StructuredFieldTypedIdentifierReference, candidates, true
	}
	for _, candidate := range candidates {
		if normalizeFieldLocator(target) == normalizeFieldLocator(candidate.String()) && raw == target {
			return reference.StructuredFieldCanonicalNodeRef, candidates, true
		}
	}
	return reference.StructuredFieldIdentifierBackedLocator, candidates, true
}

func projectedFieldValueTouchesRewrites(value projectedFieldValue, kind reference.StructuredFieldKind, candidates []ontology.NodeRef, rewrites []reference.IdentifierRewrite) bool {
	for _, rewrite := range rewrites {
		owned := sameRepairRef(value.owner, rewrite.OldRef)
		if owned && ((kind == reference.StructuredFieldPreferredIdentifier && value.field.Name == rewrite.PreferredField) ||
			(kind == reference.StructuredFieldAliasIdentifier && rewrite.HasAliasField(value.field.Name))) {
			if IdentifierComparisonKey(value.value) == IdentifierComparisonKey(rewrite.OldIdentifier) || IdentifierComparisonKey(value.value) == IdentifierComparisonKey(rewrite.NewIdentifier) {
				return true
			}
		}
		for _, candidate := range candidates {
			if sameRepairRef(candidate, rewrite.OldRef) || sameRepairRef(candidate, rewrite.NewRef) {
				return true
			}
		}
		if diagnosticMentionsIdentifier(value.value, rewrite.OldIdentifier) || normalizeFieldLocator(value.value) == normalizeFieldLocator(rewrite.OldRef.String()) {
			return true
		}
	}
	return false
}

func indexProjectionLocators(index map[string][]ontology.NodeRef, ref ontology.NodeRef) {
	for _, value := range []string{ref.String(), ref.NotePath} {
		appendFieldCandidate(index, normalizeFieldLocator(value), ref)
	}
	fragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	if fragment != "" {
		for _, value := range []string{fragment, "#" + fragment} {
			appendFieldCandidate(index, scopedFieldLocatorKey(ref.NotePath, value), ref)
		}
	}
	if nodeID := strings.TrimPrefix(strings.TrimSpace(ref.NodeID), "^"); nodeID != "" {
		for _, value := range []string{"^" + nodeID, "#^" + nodeID} {
			appendFieldCandidate(index, scopedFieldLocatorKey(ref.NotePath, value), ref)
		}
	}
	trimmedPath := strings.TrimSuffix(ref.NotePath, ".md")
	appendFieldCandidate(index, normalizeFieldLocator(trimmedPath), ref)
	if slash := strings.LastIndex(trimmedPath, "/"); slash >= 0 {
		appendFieldCandidate(index, normalizeFieldLocator(trimmedPath[slash+1:]), ref)
	}
}

func scopedFieldLocatorKey(notePath, value string) string {
	return strings.ToLower(strings.TrimSpace(notePath)) + "\x00" + normalizeFieldLocator(value)
}

func appendFieldCandidate(index map[string][]ontology.NodeRef, key string, ref ontology.NodeRef) {
	if key == "" {
		return
	}
	index[key] = appendUniqueFieldCandidate(index[key], ref)
}

func appendUniqueFieldCandidate(input []ontology.NodeRef, candidate ontology.NodeRef) []ontology.NodeRef {
	for _, existing := range input {
		if jsonKey(existing) == jsonKey(candidate) {
			return input
		}
	}
	return append(input, candidate)
}

func normalizeFieldCandidateIndex(index map[string][]ontology.NodeRef) {
	for key := range index {
		sort.Slice(index[key], func(i, j int) bool { return jsonKey(index[key][i]) < jsonKey(index[key][j]) })
	}
}

func unwrapStructuredFieldTarget(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[[") && strings.HasSuffix(value, "]]") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "[["), "]]"))
		if pipe := strings.Index(value, "|"); pipe >= 0 {
			value = strings.TrimSpace(value[:pipe])
		}
	}
	return value
}

func normalizeFieldLocator(value string) string {
	return strings.ToLower(strings.TrimSpace(unwrapStructuredFieldTarget(value)))
}

func filterFieldCandidatesByType(schema *ontology.Schema, candidates []ontology.NodeRef, expected string) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(candidates))
	for _, candidate := range candidates {
		if ontology.TypeMatchesOrImplements(schema, candidate.TypeName, expected) {
			out = append(out, candidate)
		}
	}
	return out
}
