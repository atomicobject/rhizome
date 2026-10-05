package views

import (
	"context"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// hydrateRelationValueTitles titles the untitled link values of rows.
func hydrateRelationValueTitles(ctx context.Context, scope *noderead.Scope, rows []TableRow) ([]TableRow, error) {
	refs := relationTargetRefs(nil, map[string]struct{}{}, rows, nil)
	if len(refs) == 0 {
		return rows, nil
	}
	targets, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return rows, err
	}
	return applyRelationTargetTitles(rows, targets), nil
}

// hydrateGroupAndFacetTitles titles, in one read before paging, the link
// values that group headers and filter options name.
func hydrateGroupAndFacetTitles(ctx context.Context, scope *noderead.Scope, rows, filtered []TableRow, capabilities []FieldCapability, group *viewconfig.GroupSpec) ([]TableRow, []TableRow, error) {
	refs := groupAndFacetRelationRefs(rows, filtered, capabilities, group)
	if len(refs) == 0 {
		return rows, filtered, nil
	}
	targets, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return rows, filtered, err
	}
	if facetFields, _ := relationTitleFields(capabilities, group); len(facetFields) > 0 {
		rows = applyRelationTargetTitles(rows, targets)
	}
	return rows, applyRelationTargetTitles(filtered, targets), nil
}

// groupAndFacetRelationRefs lists untitled link targets of faceted fields over
// every source row, since filter options include values the filters hide, and
// of group-only fields over just the filtered rows.
func groupAndFacetRelationRefs(rows, filtered []TableRow, capabilities []FieldCapability, group *viewconfig.GroupSpec) []ontology.NodeRef {
	facetFields, groupFields := relationTitleFields(capabilities, group)
	seen := map[string]struct{}{}
	refs := relationTargetRefs(nil, seen, rows, facetFields)
	return relationTargetRefs(refs, seen, filtered, groupFields)
}

// relationTargetRefs appends the untitled link targets of rows, limited to
// fields when it is non-nil.
func relationTargetRefs(refs []ontology.NodeRef, seen map[string]struct{}, rows []TableRow, fields map[string]bool) []ontology.NodeRef {
	for _, row := range rows {
		for field, values := range row.RelationValues {
			if fields != nil && !fields[field] {
				continue
			}
			for _, value := range values {
				if value.Ref == nil || value.Title != "" {
					continue
				}
				key := noderead.RefIdentityKey(*value.Ref)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				refs = append(refs, *value.Ref)
			}
		}
	}
	return refs
}

func applyRelationTargetTitles(rows []TableRow, targets []noderead.NodeRecord) []TableRow {
	byKey := make(map[string]noderead.NodeRecord, len(targets))
	for _, target := range targets {
		byKey[noderead.RefIdentityKey(target.Ref)] = target
	}
	out := append([]TableRow(nil), rows...)
	for rowIndex := range out {
		if len(out[rowIndex].RelationValues) == 0 {
			continue
		}
		nextFields := make(map[string][]TableRelationValue, len(out[rowIndex].RelationValues))
		for field, values := range out[rowIndex].RelationValues {
			nextValues := append([]TableRelationValue(nil), values...)
			for valueIndex := range nextValues {
				if nextValues[valueIndex].Ref == nil {
					continue
				}
				target, ok := byKey[noderead.RefIdentityKey(*nextValues[valueIndex].Ref)]
				if !ok {
					continue
				}
				canonical := target.Ref
				nextValues[valueIndex].Ref = &canonical
				nextValues[valueIndex].Title = target.Title
			}
			nextFields[field] = nextValues
		}
		out[rowIndex].RelationValues = nextFields
	}
	return out
}

func relationValuesForRecord(schema *ontology.Schema, record noderead.NodeRecord) map[string][]TableRelationValue {
	if schema == nil {
		return nil
	}
	noteType := schema.Types[record.TypeName]
	if noteType == nil {
		return nil
	}
	result := map[string][]TableRelationValue{}
	for _, field := range noteType.Fields {
		if field == nil || field.Kind != ontology.FieldKindLink {
			continue
		}
		rows := indexedRelationFieldRows(record.FieldValues, field.Name)
		if len(rows) == 0 {
			continue
		}
		values := make([]TableRelationValue, 0, len(rows))
		for _, row := range rows {
			value := TableRelationValue{Value: strings.TrimSpace(row.ValueText)}
			if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
				canonical := ref
				value.Ref = &canonical
			}
			values = append(values, value)
		}
		result[field.Name] = values
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

type pendingRelationValue struct {
	recordKey string
	field     string
	index     int
	value     string
}

// resolveRawRelationValues reads the link values of records that carry no
// indexed field rows, such as note roots. Values the index still holds, by
// field name and text, take its target in one batched read; staged, unindexed,
// or changed values resolve, sharing one Resolve unless their meaning depends
// on the note that authors them.
func resolveRawRelationValues(ctx context.Context, scope *noderead.Scope, schema *ontology.Schema, records []noderead.NodeRecord) (map[string]map[string][]TableRelationValue, error) {
	result := map[string]map[string][]TableRelationValue{}
	var pending []pendingRelationValue
	var raw []noderead.NodeRecord
	for _, record := range records {
		if len(record.FieldValues) > 0 || schema == nil {
			continue
		}
		noteType := schema.Types[record.TypeName]
		if noteType == nil {
			continue
		}
		recordKey := noderead.RefIdentityKey(record.Ref)
		for _, field := range noteType.Fields {
			if field == nil || field.Kind != ontology.FieldKindLink {
				continue
			}
			rawValues := rawRelationFieldValues(record, field)
			if len(rawValues) == 0 {
				continue
			}
			if result[recordKey] == nil {
				result[recordKey] = map[string][]TableRelationValue{}
				raw = append(raw, record)
			}
			for _, value := range rawValues {
				pending = append(pending, pendingRelationValue{recordKey: recordKey, field: field.Name, index: len(result[recordKey][field.Name]), value: value})
				result[recordKey][field.Name] = append(result[recordKey][field.Name], TableRelationValue{Value: value})
			}
		}
	}
	if len(pending) == 0 {
		return result, nil
	}
	indexed, err := scope.IndexedRecordFieldValues(ctx, raw)
	if err != nil {
		return result, err
	}
	notePaths := make(map[string]string, len(raw))
	for _, record := range raw {
		notePaths[noderead.RefIdentityKey(record.Ref)] = record.Ref.NotePath
	}
	unresolvedByFromPath := map[string][]pendingRelationValue{}
	for _, value := range pending {
		if row, ok := noderead.IndexedLinkRow(indexed[value.recordKey], value.field, value.value); ok {
			if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
				result[value.recordKey][value.field][value.index].Ref = &ref
			}
			continue
		}
		fromPath := ""
		if linkDependsOnSource(value.value) {
			fromPath = notePaths[value.recordKey]
		}
		unresolvedByFromPath[fromPath] = append(unresolvedByFromPath[fromPath], value)
	}

	for fromPath, values := range unresolvedByFromPath {
		seen := map[string]struct{}{}
		targets := make([]noderead.NodeTarget, 0, len(values))
		for _, value := range values {
			if _, ok := seen[value.value]; ok {
				continue
			}
			seen[value.value] = struct{}{}
			targets = append(targets, noderead.NodeTarget{Input: value.value})
		}
		resolved, err := scope.Resolve(ctx, noderead.ResolveRequest{
			OmitLocators: true,
			IndexOnly:    true,
			Targets:      targets,
			FromPath:     fromPath,
			Hydrate:      noderead.HydrateOptions{Profile: noderead.HydrateIdentity},
		})
		if err != nil {
			return result, err
		}
		byInput := existingRelationRefsByInput(resolved.Resolved)
		for _, value := range values {
			if ref, ok := byInput[value.value]; ok {
				result[value.recordKey][value.field][value.index].Ref = &ref
			}
		}
	}
	return result, nil
}

// linkDependsOnSource reports whether a link resolves relative to the note
// that authors it: Markdown links and relative wikilinks do; other wikilinks
// resolve the same from any note.
func linkDependsOnSource(value string) bool {
	inner, ok := strings.CutPrefix(value, "[[")
	if !ok || !strings.HasSuffix(inner, "]]") || strings.Contains(inner, "](") {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(inner), ".")
}

func existingRelationRefsByInput(resolved []noderead.ResolvedNode) map[string]ontology.NodeRef {
	byInput := make(map[string]ontology.NodeRef, len(resolved))
	for _, target := range resolved {
		if strings.TrimSpace(target.Record.Path) == "" {
			continue
		}
		ref := target.Record.Ref
		if ref.IsZero() {
			ref = target.Ref
		}
		byInput[target.Input] = ref
	}
	return byInput
}

func rawRelationFieldValues(record noderead.NodeRecord, field *ontology.Field) []string {
	sources := ontology.FieldSourceNames(field)
	if len(sources) == 0 {
		// Embedded-node link fields have no authored source; their inline key
		// is the field name, which is also how projections key the binding.
		sources = []string{field.Name}
	}
	for _, source := range sources {
		if field.SourceKind == ontology.FieldSourceInline {
			if values, ok := lookupInlineProp(record.InlineProps, source); ok {
				return nonEmptyRelationValues(values)
			}
			continue
		}
		if value, ok := caseInsensitiveMapValue(record.Frontmatter, source); ok {
			return nonEmptyRelationValues(value)
		}
	}
	return nil
}

func caseInsensitiveMapValue(values map[string]any, key string) (any, bool) {
	if value, ok := values[key]; ok {
		return value, true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for current, value := range values {
		if strings.ToLower(strings.TrimSpace(current)) == needle {
			return value, true
		}
	}
	return nil, false
}

func nonEmptyRelationValues(values any) []string {
	raw := scalarValues(values)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		text := strings.TrimSpace(valueString(value))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func indexedRelationFieldRows(fields map[string][]codeanchor.IntelOntologyNodeFieldValue, fieldName string) []codeanchor.IntelOntologyNodeFieldValue {
	for name, rows := range fields {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(fieldName)) {
			return rows
		}
	}
	return nil
}

// relationTitleFields names the link fields whose titles rows need before
// paging: faceted fields, then grouped fields that are not also faceted.
func relationTitleFields(capabilities []FieldCapability, group *viewconfig.GroupSpec) (facetFields, groupFields map[string]bool) {
	facetFields, groupFields = map[string]bool{}, map[string]bool{}
	for _, cap := range capabilities {
		if cap.Facets != nil && normalizedValueKind(cap.ValueKind) == "relation" {
			facetFields[cap.CanonicalField] = true
		}
	}
	if group != nil {
		caps := capabilityMap(capabilities)
		for _, field := range normalizedGroupFields(group) {
			if cap, ok := caps[field]; ok && normalizedValueKind(cap.ValueKind) == "relation" && !facetFields[cap.CanonicalField] {
				groupFields[cap.CanonicalField] = true
			}
		}
	}
	return facetFields, groupFields
}

// withRelationFacetLabels lists each link facet's targets once, as their
// canonical links, and labels each with its target's title. Unresolved
// values keep their authored text.
func withRelationFacetLabels(capabilities []FieldCapability, rows []TableRow) []FieldCapability {
	for i, cap := range capabilities {
		if cap.Facets == nil || normalizedValueKind(cap.ValueKind) != "relation" {
			continue
		}
		links := map[string]TableRelationValue{}
		for _, row := range rows {
			for value, link := range rowRelationValues(row, cap.Key, cap) {
				if link.Ref != nil && (links[value].Ref == nil || link.Title != "") {
					links[value] = link
				}
			}
		}
		values := make([]any, 0, len(cap.Facets.Values))
		labels := map[string]string{}
		seen := map[string]struct{}{}
		for _, raw := range cap.Facets.Values {
			identity := "value:" + valueString(raw)
			if link, ok := links[strings.TrimSpace(valueString(raw))]; ok {
				identity = "ref:" + noderead.RefIdentityKey(*link.Ref)
				raw = canonicalLinkValue(*link.Ref)
				if link.Title != "" {
					labels[raw.(string)] = link.Title
				}
			}
			if _, ok := seen[identity]; ok {
				continue
			}
			seen[identity] = struct{}{}
			values = append(values, raw)
		}
		sort.SliceStable(values, func(a, b int) bool { return valueString(values[a]) < valueString(values[b]) })
		facets := FacetCapability{Values: values}
		if len(labels) > 0 {
			facets.Labels = labels
		}
		capabilities[i].Facets = &facets
		capabilities[i].Values = values
	}
	return capabilities
}

// canonicalRelationGroupValues rewrites the group.values entries of link
// fields to the canonical link of the target each resolves to, so authored
// labels, order, and collapse settings match a link group however the
// configuration spells its target.
func canonicalRelationGroupValues(ctx context.Context, scope *noderead.Scope, group *viewconfig.GroupSpec, capabilities []FieldCapability) (*viewconfig.GroupSpec, error) {
	if group == nil || len(group.Values) == 0 {
		return group, nil
	}
	caps := capabilityMap(capabilities)
	fields := normalizedGroupFields(group)
	var indexes []int
	var targets []noderead.NodeTarget
	seen := map[string]struct{}{}
	for i, value := range group.Values {
		field := strings.TrimSpace(value.Field)
		if field == "" && len(fields) == 1 {
			field = fields[0]
		}
		if cap, ok := caps[field]; !ok || normalizedValueKind(cap.ValueKind) != "relation" || strings.TrimSpace(value.Value) == "" {
			continue
		}
		indexes = append(indexes, i)
		if _, ok := seen[value.Value]; !ok {
			seen[value.Value] = struct{}{}
			targets = append(targets, noderead.NodeTarget{Input: value.Value})
		}
	}
	if len(targets) == 0 {
		return group, nil
	}
	resolved, err := scope.Resolve(ctx, noderead.ResolveRequest{
		OmitLocators: true,
		IndexOnly:    true,
		Targets:      targets,
		Hydrate:      noderead.HydrateOptions{Profile: noderead.HydrateIdentity},
	})
	if err != nil {
		return group, err
	}
	byInput := existingRelationRefsByInput(resolved.Resolved)
	out := *group
	out.Values = append([]viewconfig.GroupValueSpec(nil), group.Values...)
	for _, i := range indexes {
		if ref, ok := byInput[out.Values[i].Value]; ok {
			out.Values[i].Value = canonicalLinkValue(ref)
		}
	}
	return &out, nil
}

// canonicalLinkValue is the link views write for a target: its vault path
// without the Markdown extension, which resolves from any note. Relation edit
// candidates write the same target.
func canonicalLinkValue(ref ontology.NodeRef) string {
	target := strings.TrimSuffix(strings.TrimSpace(ref.NotePath), ".md")
	if fragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#"); fragment != "" {
		target += "#" + fragment
	}
	return "[[" + target + "]]"
}

// LinkDisplayText shows a link value without a resolved title: a wikilink
// shows its alias, otherwise its target without folders; other text shows
// as written. It matches the web's relationDisplayValue.
func LinkDisplayText(value string) string {
	text := strings.TrimSpace(value)
	inner, ok := strings.CutPrefix(text, "[[")
	if !ok || !strings.HasSuffix(inner, "]]") {
		return text
	}
	target, alias, _ := strings.Cut(strings.TrimSuffix(inner, "]]"), "|")
	if alias = strings.TrimSpace(alias); alias != "" {
		return alias
	}
	target = strings.TrimSpace(target)
	return target[strings.LastIndex(target, "/")+1:]
}
