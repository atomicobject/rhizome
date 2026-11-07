package views

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// ontologyEditCapabilities assigns roles from the view source's profile, so
// an interface view reads the interface's lifecycle and summary fields.
func (r defaultSourceResolver) ontologyEditCapabilities(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition) []FieldCapability {
	var profile *ontoschema.TypeProfile
	if derived, ok := sourceProfile(r.opts.Schema, view); ok {
		profile = &derived
	}
	return r.capabilitiesForTypes(ctx, scope, r.sourceTypeNames(view), profile)
}

// ontologyEditCapabilitiesForTypes assigns roles from each type's own profile.
func (r defaultSourceResolver) ontologyEditCapabilitiesForTypes(ctx context.Context, scope *noderead.Scope, typeNames []string) []FieldCapability {
	return r.capabilitiesForTypes(ctx, scope, typeNames, nil)
}
func (r defaultSourceResolver) capabilitiesForTypes(ctx context.Context, scope *noderead.Scope, typeNames []string, sourceProfile *ontoschema.TypeProfile) []FieldCapability {
	if r.opts.Schema == nil {
		return nil
	}
	if len(typeNames) == 0 {
		return nil
	}
	byKey := map[string]FieldCapability{}
	for _, typeName := range typeNames {
		noteType := r.opts.Schema.Types[typeName]
		if noteType == nil {
			continue
		}
		profile := sourceProfile
		if profile == nil {
			if derived, ok := subjectProfile(r.opts.Schema, typeName); ok {
				profile = &derived
			}
		}
		for fieldIndex, field := range noteType.Fields {
			if field == nil || field.Name == "" {
				continue
			}
			if cap, ok := relationCountCapability(field); ok {
				cap.schemaOrder = fieldIndex
				cap.schemaOrderKnown = true
				byKey[cap.Key] = mergeOntologyEditCapability(byKey[cap.Key], cap)
				continue
			}
			for _, cap := range r.capabilitiesForField(ctx, scope, field) {
				if cap.Key == "" {
					continue
				}
				cap.SemanticRole = schemaFieldRole(profile, field)
				cap.schemaOrder = fieldIndex
				cap.schemaOrderKnown = true
				byKey[cap.Key] = mergeOntologyEditCapability(byKey[cap.Key], cap)
			}
		}
		if cap := titleEditCapabilityForType(noteType); cap.Key != "" {
			byKey[cap.Key] = mergeOntologyEditCapability(byKey[cap.Key], cap)
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]FieldCapability, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}
func titleEditCapabilityForType(noteType *ontoschema.NoteType) FieldCapability {
	if noteType == nil {
		return FieldCapability{}
	}
	return FieldCapability{Key: "title", Label: "Title", ValueKind: "string", CanonicalField: "title", SourceKeys: []string{"title"}, SourceKind: "builtin", SemanticRole: "title", Importance: ontoschema.FieldImportanceNormal, Sortable: true, ResidualSortable: true, Groupable: false, FilterOps: []string{"eq", "neq", "contains", "in", "exists"}, ResidualFilterOps: []string{"eq", "neq", "contains", "in", "exists"}, Source: "builtin", Filter: &FilterCapability{Ops: []string{"eq", "neq", "contains", "in", "exists"}}, Edit: &EditCapability{Kind: "text", Operation: "setField", Field: "title", InputMode: "doubleClick", ValueKind: "string"}, cardinalityKnown: true}
}
func mergeOntologyEditCapability(existing FieldCapability, next FieldCapability) FieldCapability {
	if existing.Key == "" {
		return next
	}
	merged := existing
	merged.Values = mergeAnyValues(existing.Values, next.Values)
	merged.FilterOps = mergeStringValues(existing.FilterOps, next.FilterOps)
	merged.IndexedFilterOps = mergeStringValues(existing.IndexedFilterOps, next.IndexedFilterOps)
	merged.ResidualFilterOps = mergeStringValues(existing.ResidualFilterOps, next.ResidualFilterOps)
	merged.Sortable = existing.Sortable && next.Sortable
	merged.Groupable = existing.Groupable && next.Groupable
	merged.Required = existing.Required && next.Required
	merged.Importance = mergeFieldImportance(existing.Importance, next.Importance)
	merged.PolicyReason = cmp.Or(existing.PolicyReason, next.PolicyReason)
	merged.List = existing.List || next.List
	merged.cardinalityKnown = existing.cardinalityKnown || next.cardinalityKnown
	if next.schemaOrderKnown && (!existing.schemaOrderKnown || next.schemaOrder < existing.schemaOrder) {
		merged.schemaOrder = next.schemaOrder
	}
	merged.schemaOrderKnown = existing.schemaOrderKnown || next.schemaOrderKnown
	if !editCapabilitiesCompatible(existing.Edit, next.Edit) {
		merged.Edit = nil
		return merged
	}
	if merged.Edit == nil {
		merged.Edit = next.Edit
	}
	return merged
}
func mergeFieldImportance(left, right ontoschema.FieldDisplayImportance) ontoschema.FieldDisplayImportance {
	left = effectiveFieldImportance(left)
	right = effectiveFieldImportance(right)
	if left == right {
		return left
	}
	return ontoschema.FieldImportanceNormal
}
func editCapabilitiesCompatible(a *EditCapability, b *EditCapability) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind || a.Operation != b.Operation || a.Field != b.Field || a.List != b.List || a.TargetType != b.TargetType {
		return false
	}
	if !sameStringSet(a.Options, b.Options) {
		return false
	}
	if len(a.Candidates) == 0 || len(b.Candidates) == 0 {
		return len(a.Candidates) == len(b.Candidates)
	}
	return sameStringSet(editCandidateValues(a.Candidates), editCandidateValues(b.Candidates))
}
func mergeStringValues(a []string, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, value := range append(append([]string(nil), a...), b...) {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
func mergeAnyValues(a []any, b []any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0, len(a)+len(b))
	for _, value := range append(append([]any(nil), a...), b...) {
		key := fmt.Sprint(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
func sameStringSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, value := range a {
		seen[value]++
	}
	for _, value := range b {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}
func editCandidateValues(candidates []EditCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Value)
	}
	return out
}
func (r defaultSourceResolver) capabilitiesForField(ctx context.Context, scope *noderead.Scope, field *ontoschema.Field) []FieldCapability {
	if ontoschema.IsSectionSummary(field) {
		return []FieldCapability{{Key: field.Name, Label: labelForField(field.Name), CanonicalField: field.Name, ValueKind: "string", SemanticRole: "summary", Importance: field.Display.EffectiveImportance(), cardinalityKnown: true}}
	}
	edit := r.editCapabilityForField(ctx, scope, field)
	plan, ok := ontoschema.FieldQueryCapabilityForField(r.opts.Schema, field)
	if !ok {
		return nil
	}
	keys := semanticCapabilityKeys(field)
	out := make([]FieldCapability, 0, len(keys))
	enumValues := enumValuesForCapability(r.opts.Schema, field.TypeName)
	// missing (no value) is always residual; SPEC-0112 header gap facets use it.
	filterOps := append(append([]string(nil), plan.FilterOps...), "missing")
	residualOps := append(append([]string(nil), plan.ResidualFilterOps...), "missing")
	policyReason := ""
	if field.Policy != nil {
		policyReason = strings.TrimSpace(field.Policy.Reason)
	}
	for _, key := range keys {
		sourceKind := fieldCapabilitySource(key)
		cap := FieldCapability{Key: key, Label: labelForField(key), ValueKind: plan.ValueKind, CanonicalField: field.Name, SourceKeys: append([]string(nil), keys...), SourceKind: sourceKind, SemanticRole: schemaFieldRole(nil, field), Required: field.Required, Importance: field.Display.EffectiveImportance(), Sortable: plan.Sortable, IndexedSortable: plan.Sortable, ResidualSortable: plan.Sortable, Groupable: plan.Groupable, FilterOps: append([]string(nil), filterOps...), IndexedFilterOps: append([]string(nil), plan.IndexedFilterOps...), ResidualFilterOps: append([]string(nil), residualOps...), Source: sourceKind, Filter: &FilterCapability{Ops: append([]string(nil), filterOps...)}, Edit: edit, PolicyReason: policyReason, List: field.List, cardinalityKnown: true}
		if len(enumValues) > 0 {
			cap.EnumName = field.TypeName
			cap.EnumValues = append([]FieldEnumValue(nil), enumValues...)
			cap.Filter.Options = enumValuesAsOptions(enumValues)
			cap.Group = &GroupCapability{Values: append([]FieldEnumValue(nil), enumValues...)}
		}
		out = append(out, cap)
	}
	return out
}
func enumValuesAsOptions(values []FieldEnumValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value.Value)
	}
	return out
}
func enumValuesForCapability(schema *ontoschema.Schema, typeName string) []FieldEnumValue {
	if schema == nil || schema.EnumTypes == nil {
		return nil
	}
	enumType := schema.EnumTypes[typeName]
	if enumType == nil {
		return nil
	}
	out := make([]FieldEnumValue, 0, len(enumType.Values))
	tones := enumType.Tones()
	collapsed := enumType.CollapsedDefaults()
	stages := enumType.Stages()
	for index, value := range enumType.Values {
		if value == nil || value.Name == "" {
			continue
		}
		item := FieldEnumValue{Value: value.Name, Label: firstNonEmpty(value.View.Label, value.Name), Description: value.Description, Rank: value.View.Order, Tone: tones[index], CollapsedByDefault: index < len(collapsed) && collapsed[index]}
		if index < len(stages) {
			item.Stage = stages[index].Stage
			item.StageDeclared = stages[index].Declared
		}
		out = append(out, item)
	}
	return out
}
func (r defaultSourceResolver) editCapabilityForField(ctx context.Context, scope *noderead.Scope, field *ontoschema.Field) *EditCapability {
	switch {
	case field.Kind == ontoschema.FieldKindEnum:
		return &EditCapability{Kind: "enum", Operation: "setField", Field: field.Name, List: field.List, InputMode: "direct", ValueKind: "enum", Options: enumOptions(r.opts.Schema, field.TypeName)}
	case field.SourceKind == ontoschema.FieldSourceCheckbox:
		return &EditCapability{Kind: "boolean", Operation: "setField", Field: field.Name, InputMode: "direct", ValueKind: "bool", Options: []string{"false", "true"}}
	case safeAuthoredScalarEditField(field):
		return &EditCapability{Kind: scalarEditKind(field.TypeName), Operation: "setField", Field: field.Name, List: field.List, InputMode: "doubleClick", ValueKind: scalarEditValueKind(field.TypeName)}
	case field.Kind == ontoschema.FieldKindLink && field.TypeName != "" && field.TypeName != "Note":
		return &EditCapability{Kind: "node", Operation: "setLinkField", Field: field.Name, List: field.List, InputMode: "direct", ValueKind: "relation", TargetType: field.TypeName, Candidates: r.editCandidatesForType(ctx, scope, field.TypeName)}
	default:
		return nil
	}
}
func scalarEditKind(typeName string) string {
	switch scalarEditValueKind(typeName) {
	case "date":
		return "date"
	case "datetime":
		return "datetime"
	case "int", "real":
		return "number"
	default:
		return "text"
	}
}
func scalarEditValueKind(typeName string) string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "date":
		return "date"
	case "datetime":
		return "datetime"
	case "int", "integer":
		return "int"
	case "float", "real", "number":
		return "real"
	case "url":
		return "url"
	case "id":
		return "id"
	default:
		return "string"
	}
}
func safeAuthoredScalarEditField(field *ontoschema.Field) bool {
	if field == nil || field.Kind != ontoschema.FieldKindScalar || field.List {
		return false
	}
	if field.SourceKind != ontoschema.FieldSourceFrontmatter && field.SourceKind != ontoschema.FieldSourceInline {
		return false
	}
	if field.IsIdentifier || field.IsPreferredIdentifier || field.IsDerivableIdentifier {
		return false
	}
	if field.Policy != nil && field.Policy.RequiresUserConfirmation {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(field.TypeName)) {
	case "", "string", "date", "datetime", "int", "integer", "float", "real", "number", "url":
		return true
	default:
		return false
	}
}
func semanticCapabilityKeys(field *ontoschema.Field) []string {
	keys := []string{field.Name}
	source := firstNonEmpty(field.Source, field.Name)
	switch field.SourceKind {
	case ontoschema.FieldSourceFrontmatter:
		keys = append(keys, "frontmatter."+source)
	case ontoschema.FieldSourceInline:
		keys = append(keys, "inline."+source)
	}
	return keys
}
func enumOptions(schema *ontoschema.Schema, typeName string) []string {
	if schema == nil || schema.EnumTypes == nil {
		return nil
	}
	enumType := schema.EnumTypes[typeName]
	if enumType == nil {
		return nil
	}
	out := make([]string, 0, len(enumType.Values))
	for _, value := range enumType.Values {
		if value != nil && value.Name != "" {
			out = append(out, value.Name)
		}
	}
	return out
}
func (r defaultSourceResolver) editCandidatesForType(ctx context.Context, scope *noderead.Scope, typeName string) []EditCandidate {
	if scope == nil || strings.TrimSpace(typeName) == "" {
		return nil
	}
	result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName, Limit: 200})
	if err != nil {
		return nil
	}
	out := make([]EditCandidate, 0, len(result.Items))
	refs := make([]ontology.NodeRef, 0, len(result.Items))
	for _, item := range result.Items {
		if item.Ref.NotePath == "" {
			continue
		}
		refs = append(refs, item.Ref)
	}
	records, _ := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	recordsByPath := make(map[string]noderead.NodeRecord, len(records))
	for _, record := range records {
		recordsByPath[record.Path] = record
	}
	noteType := r.opts.Schema.Types[typeName]
	for _, item := range result.Items {
		if item.Ref.NotePath == "" {
			continue
		}
		label := editCandidateLabel(recordsByPath[item.Ref.NotePath], noteType, item.Title, item.NotePath)
		out = append(out, EditCandidate{Ref: item.Ref, Value: editCandidateAuthoringValue(label, item.NotePath), Label: label, Path: item.NotePath})
	}
	return out
}
func (r defaultSourceResolver) FieldCandidates(ctx context.Context, view viewconfig.ViewDefinition, req FieldCandidatesRequest) (FieldCandidatesResponse, error) {
	fieldName := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(req.Field), "frontmatter."), "inline.")
	if fieldName == "" {
		return FieldCandidatesResponse{}, fmt.Errorf("%w: missing field", ErrInvalidRequest)
	}
	var targetType string
	for _, typeName := range r.sourceTypeNames(view) {
		noteType := r.opts.Schema.Types[typeName]
		if noteType == nil {
			continue
		}
		field := noteType.ByName[fieldName]
		if field == nil || field.Kind != ontoschema.FieldKindLink || field.List || field.TypeName == "" || field.TypeName == "Note" {
			continue
		}
		if targetType == "" {
			targetType = field.TypeName
			continue
		}
		if targetType != field.TypeName {
			return FieldCandidatesResponse{Field: req.Field}, nil
		}
	}
	if targetType == "" {
		return FieldCandidatesResponse{Field: req.Field}, nil
	}
	scope := r.nodeReadScope()
	candidates := r.editCandidatesForType(ctx, scope, targetType)
	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query != "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if strings.Contains(strings.ToLower(candidate.Label), query) || strings.Contains(strings.ToLower(candidate.Value), query) || strings.Contains(strings.ToLower(candidate.Path), query) {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	hasMore := len(candidates) > limit
	if hasMore {
		candidates = candidates[:limit]
	}
	return FieldCandidatesResponse{Field: req.Field, TargetType: targetType, Candidates: candidates, HasMore: hasMore}, nil
}
func editCandidateLabel(record noderead.NodeRecord, noteType *ontoschema.NoteType, fallbackTitle, notePath string) string {
	for _, preferred := range []string{"displayName", "display-name", "name", "title"} {
		if value := frontmatterStringForField(record.Frontmatter, noteType, preferred); value != "" {
			return value
		}
	}
	return firstNonEmpty(record.Title, fallbackTitle, notePath)
}
func frontmatterStringForField(frontmatter map[string]any, noteType *ontoschema.NoteType, fieldName string) string {
	if len(frontmatter) == 0 || noteType == nil {
		return ""
	}
	field := noteType.ByName[fieldName]
	if field == nil || field.SourceKind != ontoschema.FieldSourceFrontmatter {
		return ""
	}
	for _, source := range ontoschema.FieldSourceNames(field) {
		if value := strings.TrimSpace(fmt.Sprint(frontmatter[source])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}
func editCandidateAuthoringValue(label, notePath string) string {
	target := strings.TrimSuffix(strings.TrimSpace(notePath), ".md")
	label = strings.TrimSpace(label)
	if target == "" {
		return label
	}
	if label == "" || strings.EqualFold(label, filepath.Base(target)) {
		return "[[" + target + "]]"
	}
	return "[[" + target + "|" + label + "]]"
}
func fieldCapabilitySource(key string) string {
	if idx := strings.Index(key, "."); idx > 0 {
		return key[:idx]
	}
	return "schema"
}
