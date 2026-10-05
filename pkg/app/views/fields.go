package views

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

const defaultTableColumnLimit = 6

func tableColumns(def viewconfig.ViewDefinition, capabilities []FieldCapability) []TableColumn {
	columns := []viewconfig.ViewColumn(nil)
	if def.Variants.Table != nil {
		columns = def.Variants.Table.Columns
	} else {
		card := cardSpecForTable(def)
		columns = cardTableColumns(card)
		if card.Fields == nil && usesImportanceDefaults(capabilities) {
			columns = appendDefaultCapabilityColumns(columns, capabilities, defaultTableColumnLimit)
		}
	}
	if def.Mount.Kind == viewconfig.MountKindInterface || def.SourceSpec.Kind == viewconfig.SourceKindOntologyInterface {
		columns = ensureResolvedTypeColumn(columns)
	}
	out := make([]TableColumn, 0, len(columns))
	for _, col := range columns {
		out = append(out, TableColumn{Field: col.Field, Label: firstNonEmpty(col.Label, labelForField(col.Field))})
	}
	return out
}

func appendDefaultCapabilityColumns(columns []viewconfig.ViewColumn, capabilities []FieldCapability, limit int) []viewconfig.ViewColumn {
	out := append([]viewconfig.ViewColumn(nil), columns...)
	seen := make(map[string]struct{}, len(out))
	for _, column := range out {
		seen[column.Field] = struct{}{}
	}
	// Changed always keeps its slot, as in generated defaults: title,
	// identifier, and KEY fields fill up to the reserve, and NORMAL fields only
	// take leftover room. Issues stays available but is not a default column.
	const operationalColumns = 1
	ordered := orderedDefaultCapabilities(capabilities)
	fill := func(max int, include func(FieldCapability) bool) {
		for _, capability := range ordered {
			if limit > 0 && len(out) >= max {
				return
			}
			if _, ok := seen[capability.Key]; ok || !include(capability) {
				continue
			}
			seen[capability.Key] = struct{}{}
			out = append(out, viewconfig.ViewColumn{Field: capability.Key, Label: capability.Label})
		}
	}
	fill(limit-operationalColumns, func(capability FieldCapability) bool { return defaultCapabilityRank(capability) < 3 })
	if _, ok := seen["updatedAt"]; !ok {
		seen["updatedAt"] = struct{}{}
		out = append(out, viewconfig.ViewColumn{Field: "updatedAt", Label: "Changed"})
	}
	fill(limit, func(FieldCapability) bool { return true })
	return out
}

func orderedDefaultCapabilities(capabilities []FieldCapability) []FieldCapability {
	ordered := make([]FieldCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability.Key == "" {
			continue
		}
		if capability.CanonicalField != "" && capability.Key != capability.CanonicalField {
			continue
		}
		if effectiveFieldImportance(capability.Importance) == ontology.FieldImportanceDetail &&
			capability.SemanticRole != "identifier" && capability.SemanticRole != "title" && capability.Key != "title" {
			continue
		}
		ordered = append(ordered, capability)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := defaultCapabilityRank(ordered[i]), defaultCapabilityRank(ordered[j])
		if left != right {
			return left < right
		}
		if ordered[i].schemaOrderKnown != ordered[j].schemaOrderKnown {
			return ordered[i].schemaOrderKnown
		}
		if ordered[i].schemaOrderKnown && ordered[i].schemaOrder != ordered[j].schemaOrder {
			return ordered[i].schemaOrder < ordered[j].schemaOrder
		}
		return false
	})
	return ordered
}

func defaultCapabilityRank(capability FieldCapability) int {
	switch {
	case capability.SemanticRole == "title" || capability.Key == "title":
		return 0
	case capability.SemanticRole == "identifier":
		return 1
	case effectiveFieldImportance(capability.Importance) == ontology.FieldImportanceKey:
		return 2
	default:
		return 3
	}
}

func usesImportanceDefaults(capabilities []FieldCapability) bool {
	for _, capability := range capabilities {
		if importance := effectiveFieldImportance(capability.Importance); importance != ontology.FieldImportanceNormal {
			return true
		}
	}
	return false
}

func effectiveFieldImportance(importance ontology.FieldDisplayImportance) ontology.FieldDisplayImportance {
	if importance == "" {
		return ontology.FieldImportanceNormal
	}
	return importance
}

func cardTableColumns(card *viewconfig.CardSpec) []viewconfig.ViewColumn {
	if card == nil {
		return []viewconfig.ViewColumn{{Field: "title"}}
	}
	out := make([]viewconfig.ViewColumn, 0, len(card.Fields)+2)
	seen := map[string]struct{}{}
	add := func(column viewconfig.ViewColumn) {
		field := strings.TrimSpace(column.Field)
		if field == "" {
			return
		}
		if _, ok := seen[field]; ok {
			return
		}
		seen[field] = struct{}{}
		column.Field = field
		out = append(out, column)
	}
	add(viewconfig.ViewColumn{Field: card.Eyebrow})
	add(viewconfig.ViewColumn{Field: firstNonEmpty(card.Title, "title")})
	add(viewconfig.ViewColumn{Field: card.Preview})
	for _, field := range card.Fields {
		add(field)
	}
	return out
}

func ensureResolvedTypeColumn(columns []viewconfig.ViewColumn) []viewconfig.ViewColumn {
	for _, column := range columns {
		if column.Field == "resolvedType" {
			return columns
		}
	}
	out := make([]viewconfig.ViewColumn, 0, len(columns)+1)
	out = append(out, viewconfig.ViewColumn{Field: "resolvedType", Label: "Type"})
	out = append(out, columns...)
	return out
}

// mergeCapabilities adds builtin and row-observed fields to a source's
// capabilities. When the source supplied schema fields, row-observed keys get
// no name-guessed role: SPEC-0112 roles come from the type profile.
func mergeCapabilities(existing []FieldCapability, rows []TableRow, completeness SourceCompleteness) []FieldCapability {
	byKey := map[string]FieldCapability{}
	schemaBacked := false
	for _, cap := range existing {
		schemaBacked = schemaBacked || cap.schemaOrderKnown
	}
	for _, cap := range existing {
		if cap.Key != "" {
			cap.Importance = effectiveFieldImportance(cap.Importance)
			cap.FilterOps = unionStrings(cap.FilterOps, cap.IndexedFilterOps, cap.ResidualFilterOps)
			byKey[cap.Key] = cap
		}
	}
	for _, key := range []string{"title", "path", "resolvedType", "updatedAt", "hasIssues", "tags"} {
		if _, ok := byKey[key]; !ok {
			cap := capability(key, "builtin", completeness)
			cap.cardinalityKnown = true
			cap.List = key == "tags"
			if key == "updatedAt" {
				cap.Label = "Changed"
			}
			byKey[key] = cap
		}
	}
	builtin := make(map[string]struct{}, len(byKey))
	for key := range byKey {
		builtin[key] = struct{}{}
	}
	for _, row := range rows {
		collectFieldCapabilities(byKey, row.Fields, "", completeness)
	}
	if schemaBacked {
		for key, cap := range byKey {
			if _, ok := builtin[key]; !ok {
				cap.SemanticRole = builtinRole(key)
				byKey[key] = cap
			}
		}
	}
	valueSets := collectCapabilityValues(byKey, rows)
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]FieldCapability, 0, len(keys))
	for _, key := range keys {
		cap := byKey[key]
		if values := valueSets[key]; len(values) > 0 && len(values) <= 50 {
			cap.Values = values
			cap.Facets = &FacetCapability{Values: values}
			if len(values) <= 20 {
				cap.FilterOps = preferInFilter(cap.FilterOps)
				cap.ResidualFilterOps = preferInFilter(cap.ResidualFilterOps)
			}
		}
		cap.FilterOps = unionStrings(cap.FilterOps, cap.IndexedFilterOps, cap.ResidualFilterOps)
		if cap.Filter == nil && len(cap.FilterOps) > 0 {
			cap.Filter = &FilterCapability{Ops: append([]string(nil), cap.FilterOps...)}
		}
		out = append(out, cap)
	}
	return out
}

func collectFieldCapabilities(out map[string]FieldCapability, fields map[string]any, prefix string, completeness SourceCompleteness) {
	for key, val := range fields {
		if key == "" {
			continue
		}
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		switch nested := val.(type) {
		case map[string]any:
			collectFieldCapabilities(out, nested, full, completeness)
		default:
			cap, ok := out[full]
			if !ok {
				cap := capability(full, strings.Split(full, ".")[0], completeness)
				cap.List = isListValue(val)
				out[full] = cap
			} else if !cap.cardinalityKnown && !cap.List && isListValue(val) {
				cap.List = true
				out[full] = cap
			}
		}
	}
}

func isListValue(value any) bool {
	_, ok := listValues(value)
	return ok
}

func listValues(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, reflected.Len())
	for i := 0; i < reflected.Len(); i++ {
		out[i] = reflected.Index(i).Interface()
	}
	return out, true
}

func capability(key, source string, completeness SourceCompleteness) FieldCapability {
	filterOps := []string{"eq", "neq", "contains", "in", "exists", "missing", "gt", "gte", "lt", "lte"}
	return FieldCapability{
		Key:               key,
		Label:             labelForField(key),
		ValueKind:         "scalar",
		CanonicalField:    canonicalCapabilityField(key),
		SourceKeys:        []string{key},
		SourceKind:        source,
		SemanticRole:      semanticRoleForKey(key),
		Importance:        ontology.FieldImportanceNormal,
		Sortable:          true,
		ResidualSortable:  true,
		Groupable:         true,
		FilterOps:         append([]string(nil), filterOps...),
		ResidualFilterOps: append([]string(nil), filterOps...),
		Completeness:      completeness,
		Source:            source,
	}
}

func unionStrings(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, group := range groups {
		for _, value := range group {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func canonicalCapabilityField(key string) string {
	return strings.TrimPrefix(strings.TrimPrefix(key, "frontmatter."), "inline.")
}

func collectCapabilityValues(caps map[string]FieldCapability, rows []TableRow) map[string][]any {
	out := map[string][]any{}
	seen := map[string]map[string]struct{}{}
	for key := range caps {
		seen[key] = map[string]struct{}{}
	}
	for _, row := range rows {
		for key := range caps {
			// A facet with more than 50 distinct values is never exposed.
			if len(out[key]) > 50 {
				continue
			}
			value, ok := fieldValue(row, key)
			if !ok {
				continue
			}
			for _, scalar := range scalarValues(value) {
				token := fmt.Sprint(scalar)
				if token == "" {
					continue
				}
				if _, ok := seen[key][token]; ok {
					continue
				}
				seen[key][token] = struct{}{}
				out[key] = append(out[key], scalar)
				if len(out[key]) > 50 {
					break
				}
			}
		}
	}
	for key := range out {
		if !shouldExposeCapabilityValues(key, len(out[key])) {
			delete(out, key)
			continue
		}
		sort.SliceStable(out[key], func(i, j int) bool {
			return fmt.Sprint(out[key][i]) < fmt.Sprint(out[key][j])
		})
	}
	return out
}

func shouldExposeCapabilityValues(key string, distinct int) bool {
	if distinct == 0 || distinct > 50 {
		return false
	}
	lower := strings.ToLower(key)
	if key == "title" || key == "path" || key == "updatedAt" || lower == "id" ||
		strings.HasSuffix(lower, ".id") || strings.Contains(lower, "slug") ||
		strings.Contains(lower, "updated") || strings.Contains(lower, "created") ||
		strings.Contains(lower, "date") || strings.Contains(lower, "time") {
		return false
	}
	if key == "hasIssues" || key == "resolvedType" || key == "tags" {
		return true
	}
	for _, token := range []string{"status", "type", "kind", "category", "priority"} {
		if strings.Contains(lower, token) {
			return distinct <= 20
		}
	}
	return false
}

func scalarValues(value any) []any {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, scalarValues(item)...)
		}
		return out
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case map[string]any:
		return nil
	default:
		return []any{v}
	}
}

func preferInFilter(ops []string) []string {
	hasIn := false
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		if op == "in" {
			hasIn = true
			continue
		}
		out = append(out, op)
	}
	if !hasIn {
		return ops
	}
	return append([]string{"in"}, out...)
}

func fieldValue(row TableRow, selector string) (any, bool) {
	switch selector {
	case "title":
		return row.Title, row.Title != ""
	case "path":
		return row.Path, row.Path != ""
	case "resolvedType":
		return row.ResolvedType, row.ResolvedType != ""
	case "updatedAt":
		return row.UpdatedAt, row.UpdatedAt != 0
	case "hasIssues":
		return row.HasIssues, true
	case "tags":
		return row.Tags, len(row.Tags) > 0
	}
	return nestedFieldValue(row.Fields, selector)
}

func capabilityFieldValue(row TableRow, selector string, capability FieldCapability) (any, bool) {
	seen := map[string]struct{}{}
	for _, selectors := range [][]string{{selector, capability.Key}, capability.SourceKeys, {capability.CanonicalField}} {
		for _, candidate := range selectors {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if value, ok := fieldValue(row, candidate); ok {
				return value, true
			}
		}
	}
	return nil, false
}

func nestedFieldValue(fields map[string]any, selector string) (any, bool) {
	if len(fields) == 0 || selector == "" {
		return nil, false
	}
	parts := strings.Split(selector, ".")
	var current any = fields
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case []string:
		return strings.Join(typed, " ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, valueString(item))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(typed)
	}
}

func labelForField(field string) string {
	field = strings.TrimPrefix(field, "frontmatter.")
	field = strings.TrimPrefix(field, "inline.")
	return ontology.HumanizeFieldName(field)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
