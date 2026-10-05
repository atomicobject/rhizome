package views

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

const kanbanDefaultPageSize = 500

func availableVariants(def viewconfig.ViewDefinition) []string {
	card := executableCardSpec(def.Variants.Card)
	kanban := executableKanbanVariant(def.Variants.Kanban)
	table := card || kanban
	if declared := def.Variants.Table; declared != nil {
		table = len(declared.Columns) > 0 && executableViewColumns(declared.Columns)
	}
	out := make([]string, 0, 3)
	if table {
		out = append(out, "table")
	}
	if kanban {
		out = append(out, "kanban")
	}
	if card {
		out = append(out, "card")
	}
	return out
}

func executableCardSpec(card *viewconfig.CardSpec) bool {
	return card != nil && executableViewColumns(card.Fields)
}

func executableKanbanVariant(kanban *viewconfig.KanbanVariant) bool {
	return kanban != nil && strings.TrimSpace(kanban.ColumnField) != "" && (kanban.Card == nil || executableCardSpec(kanban.Card))
}

func executableViewColumns(columns []viewconfig.ViewColumn) bool {
	for _, column := range columns {
		if strings.TrimSpace(column.Field) == "" {
			return false
		}
	}
	return true
}

func hasVariant(variants []string, want string) bool {
	for _, variant := range variants {
		if variant == want {
			return true
		}
	}
	return false
}

func cardSpecForVariant(def viewconfig.ViewDefinition, variant string) *viewconfig.CardSpec {
	if variant == "kanban" && def.Variants.Kanban != nil && def.Variants.Kanban.Card != nil {
		return def.Variants.Kanban.Card
	}
	if def.Variants.Card != nil {
		return def.Variants.Card
	}
	return &viewconfig.CardSpec{}
}

func cardSpecForTable(def viewconfig.ViewDefinition) *viewconfig.CardSpec {
	if def.Variants.Card != nil {
		return def.Variants.Card
	}
	if def.Variants.Kanban != nil && def.Variants.Kanban.Card != nil {
		return def.Variants.Kanban.Card
	}
	return &viewconfig.CardSpec{}
}

func resolveCardLayout(def viewconfig.ViewDefinition, variant string, capabilities []FieldCapability) *CardLayout {
	spec := cardSpecForVariant(def, variant)
	title, ok := resolvedCardColumn(spec.Title, "", capabilities)
	if !ok {
		title, _ = resolvedCardColumn("title", "Title", capabilities)
	}
	layout := &CardLayout{Title: title, Fields: []TableColumn{}}
	if eyebrow, ok := resolvedCardColumn(spec.Eyebrow, "", capabilities); ok {
		layout.Eyebrow = &eyebrow
	} else if field := firstCapabilityField(capabilities, "identifier"); field != "" {
		eyebrow, _ := resolvedCardColumn(field, "", capabilities)
		layout.Eyebrow = &eyebrow
	}
	if preview, ok := resolvedCardColumn(spec.Preview, "", capabilities); ok {
		layout.Preview = &preview
	} else if field := defaultRoleField(capabilities, "summary"); field != "" {
		preview, _ := resolvedCardColumn(field, "", capabilities)
		layout.Preview = &preview
	}
	if spec.Fields != nil {
		layout.Fields = resolvedCardFields(spec.Fields, capabilities)
	} else {
		layout.Fields = defaultCardFields(def, layout, capabilities)
	}
	return layout
}

func resolvedCardFields(fields []viewconfig.ViewColumn, capabilities []FieldCapability) []TableColumn {
	out := make([]TableColumn, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		column, ok := resolvedCardColumn(field.Field, field.Label, capabilities)
		if !ok {
			continue
		}
		if _, ok := seen[column.Field]; ok {
			continue
		}
		seen[column.Field] = struct{}{}
		out = append(out, column)
	}
	return out
}

func defaultCardFields(def viewconfig.ViewDefinition, layout *CardLayout, capabilities []FieldCapability) []TableColumn {
	excluded := map[string]struct{}{layout.Title.Field: {}}
	if layout.Eyebrow != nil {
		excluded[layout.Eyebrow.Field] = struct{}{}
	}
	if layout.Preview != nil {
		excluded[layout.Preview.Field] = struct{}{}
	}
	out := make([]TableColumn, 0, 4)
	if usesImportanceDefaults(capabilities) {
		for _, capability := range orderedDefaultCapabilities(capabilities) {
			if len(out) >= 4 {
				break
			}
			if _, ok := excluded[capability.Key]; ok {
				continue
			}
			out = append(out, TableColumn{Field: capability.Key, Label: firstNonEmpty(capability.Label, labelForField(capability.Key))})
			excluded[capability.Key] = struct{}{}
		}
		return out
	}
	if field := firstStatusCapabilityField(capabilities); field != "" {
		if column, ok := resolvedCardColumn(field, "", capabilities); ok {
			out = append(out, column)
			excluded[column.Field] = struct{}{}
		}
	}
	additional := 0
	for _, column := range tableColumns(def, capabilities) {
		if additional >= 3 {
			break
		}
		if _, ok := excluded[column.Field]; ok {
			continue
		}
		resolved, ok := resolvedCardColumn(column.Field, column.Label, capabilities)
		if !ok {
			continue
		}
		if _, ok := excluded[resolved.Field]; ok {
			continue
		}
		out = append(out, resolved)
		additional++
		excluded[resolved.Field] = struct{}{}
	}
	return out
}

func resolvedCardColumn(field string, label string, capabilities []FieldCapability) (TableColumn, bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return TableColumn{}, false
	}
	capability, ok := resolveCapability(capabilities, field)
	if !ok {
		return TableColumn{}, false
	}
	return TableColumn{Field: capability.Key, Label: firstNonEmpty(label, capability.Label, labelForField(capability.Key))}, true
}

func resolveCapability(capabilities []FieldCapability, field string) (FieldCapability, bool) {
	field = strings.TrimSpace(field)
	for _, capability := range capabilities {
		if capability.Key == field {
			return capability, true
		}
	}
	for _, capability := range capabilities {
		if capability.CanonicalField == field && capability.Key == capability.CanonicalField {
			return capability, true
		}
	}
	for _, capability := range capabilities {
		if capability.CanonicalField == field {
			return capability, true
		}
	}
	return FieldCapability{}, false
}

func capabilityField(capabilities []FieldCapability, field string) string {
	capability, ok := resolveCapability(capabilities, field)
	if !ok {
		return ""
	}
	return capability.Key
}

// defaultRoleField is the field with role that a layout may show by default:
// never a DETAIL field.
func defaultRoleField(capabilities []FieldCapability, role string) string {
	field := firstCapabilityField(capabilities, role)
	capability, ok := resolveCapability(capabilities, field)
	if !ok || effectiveFieldImportance(capability.Importance) == ontology.FieldImportanceDetail {
		return ""
	}
	return capability.Key
}

func firstCapabilityField(capabilities []FieldCapability, role string) string {
	for _, capability := range capabilities {
		if capability.SemanticRole == role && capability.Key == capability.CanonicalField {
			return capability.Key
		}
	}
	for _, capability := range capabilities {
		if capability.SemanticRole == role {
			return capability.Key
		}
	}
	return ""
}

func firstStatusCapabilityField(capabilities []FieldCapability) string {
	for _, capability := range capabilities {
		if capability.Key == capability.CanonicalField && statusLikeCapability(capability) {
			return capability.Key
		}
	}
	for _, capability := range capabilities {
		if statusLikeCapability(capability) {
			return capability.Key
		}
	}
	return ""
}

func statusLikeCapability(capability FieldCapability) bool {
	return capability.SemanticRole == "status"
}

// resolveBoardLayout lays out the kanban columns; group is the executed
// column grouping, whose values carry the configured column overrides.
func resolveBoardLayout(def viewconfig.ViewDefinition, group *viewconfig.GroupSpec, capabilities []FieldCapability, rows []TableRow, groups []TableGroup, page PageInfo) (*BoardLayout, *Warning) {
	kanban := def.Variants.Kanban
	if kanban == nil || group == nil {
		return nil, nil
	}
	field := strings.TrimSpace(group.Field)
	capability, ok := resolveCapability(capabilities, field)
	if !ok || boardFieldIsList(capability, rows) {
		return nil, &Warning{
			Code:    "view_kanban_column_unsupported",
			Message: fmt.Sprintf("kanban column field %q must resolve to a single-valued capability", field),
			Path:    "variants.kanban.columnField",
		}
	}
	overrides := groupValueOverrides(group)
	fullByValue := groupsByValue(groups)
	buckets := boardBuckets(field, capability, groups, overrides)
	columns := make([]BoardColumn, 0, len(buckets)+1)
	rowCursor := 0
	for _, bucket := range buckets {
		full, present := fullByValue[bucket.value]
		if kanban.HideEmptyColumns && full.TotalCount == 0 {
			continue
		}
		rowStart, rowEnd := rowCursor, rowCursor
		if present {
			rowStart, rowEnd = boardPageRange(full, page)
			rowCursor = rowEnd
		}
		columns = append(columns, BoardColumn{
			Key:                groupKey("", field, bucket.value),
			Value:              bucket.value,
			Label:              groupLabel(field, bucket.value, bucket.label, capability, overrides),
			Tone:               groupTone(bucket.value, capability),
			Count:              full.TotalCount,
			RowStart:           rowStart,
			RowEnd:             rowEnd,
			CollapsedByDefault: groupCollapsedByDefault(field, bucket.value, capability, overrides),
		})
	}
	if empty := fullByValue[""]; empty.TotalCount > 0 {
		rowStart, rowEnd := boardPageRange(empty, page)
		columns = append(columns, BoardColumn{
			Key:      groupKey("", field, ""),
			Value:    "",
			Label:    "(empty)",
			Count:    empty.TotalCount,
			RowStart: rowStart,
			RowEnd:   rowEnd,
		})
	}
	return &BoardLayout{
		ColumnField: capability.Key,
		Editable:    boardCapabilityEditable(capability),
		Columns:     columns,
	}, nil
}

func boardPageRange(group TableGroup, page PageInfo) (int, int) {
	start := min(max(group.RowStart-page.Offset, 0), page.Returned)
	end := min(max(group.RowEnd-page.Offset, 0), page.Returned)
	return start, end
}

func boardFieldIsList(capability FieldCapability, rows []TableRow) bool {
	if capability.List || capability.Edit != nil && capability.Edit.List {
		return true
	}
	if capability.cardinalityKnown {
		return false
	}
	for _, row := range rows {
		value, ok := capabilityFieldValue(row, capability.Key, capability)
		if !ok {
			continue
		}
		if isListValue(value) {
			return true
		}
	}
	return false
}

func boardCapabilityEditable(capability FieldCapability) bool {
	if capability.Edit == nil || capability.Edit.List {
		return false
	}
	switch capability.Edit.Kind {
	case "enum", "boolean", "node":
		return true
	default:
		return false
	}
}

func boardBuckets(field string, capability FieldCapability, groups []TableGroup, overrides map[string]viewconfig.GroupValueSpec) []rowBucket {
	domainBound := len(capability.EnumValues) > 0 || normalizedValueKind(capability.ValueKind) == "enum" || normalizedValueKind(capability.ValueKind) == "bool"
	if !domainBound {
		out := make([]rowBucket, 0, len(groups))
		for _, group := range groups {
			if group.Value != "" {
				out = append(out, rowBucket{value: group.Value, label: group.Label, sortValue: group.Value})
			}
		}
		return out
	}
	byValue := map[string]rowBucket{}
	for _, enumValue := range capability.EnumValues {
		if enumValue.Value != "" {
			byValue[enumValue.Value] = rowBucket{value: enumValue.Value, sortValue: enumValue.Value}
		}
	}
	if normalizedValueKind(capability.ValueKind) == "bool" {
		byValue["false"] = rowBucket{value: "false", sortValue: false}
		byValue["true"] = rowBucket{value: "true", sortValue: true}
	}
	out := make([]rowBucket, 0, len(byValue))
	for _, bucket := range byValue {
		out = append(out, bucket)
	}
	ranks := enumRankMap(capability)
	order := enumOrderMap(capability)
	sort.SliceStable(out, func(i, j int) bool {
		return groupBucketLess(field, &out[i], &out[j], capability, ranks, order, overrides)
	})
	// One note with a value outside the schema must not take the board away from
	// everyone else; it gets its own trailing column where it can be fixed.
	for _, group := range groups {
		if _, declared := byValue[group.Value]; !declared && group.Value != "" {
			out = append(out, rowBucket{value: group.Value, sortValue: group.Value})
		}
	}
	return out
}

func groupsByValue(groups []TableGroup) map[string]TableGroup {
	out := make(map[string]TableGroup, len(groups))
	for _, group := range groups {
		out[group.Value] = group
	}
	return out
}

func groupTone(value string, capability FieldCapability) string {
	for _, enumValue := range capability.EnumValues {
		if enumValue.Value == value {
			return enumValue.Tone
		}
	}
	if normalizedValueKind(capability.ValueKind) == "bool" {
		if parsed, ok := boolValue(value); ok {
			if parsed {
				return "success"
			}
			return "neutral"
		}
	}
	return ""
}

// kanbanGroupSpec groups board columns by the column field, applying authored
// group value metadata.
func kanbanGroupSpec(def viewconfig.ViewDefinition, field string) *viewconfig.GroupSpec {
	if def.Variants.Kanban == nil {
		return nil
	}
	return &viewconfig.GroupSpec{Field: field, Values: groupValuesForField(def.Defaults.Group, field)}
}

func groupValuesForField(group *viewconfig.GroupSpec, field string) []viewconfig.GroupValueSpec {
	if group == nil {
		return nil
	}
	fields := normalizedGroupFields(group)
	out := make([]viewconfig.GroupValueSpec, 0, len(group.Values))
	for _, value := range group.Values {
		valueField := strings.TrimSpace(value.Field)
		if valueField == field || valueField == "" && len(fields) == 1 && fields[0] == field {
			value.Field = field
			out = append(out, value)
		}
	}
	return out
}
