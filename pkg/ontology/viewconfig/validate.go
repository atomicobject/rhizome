package viewconfig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var viewIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

func Validate(defs []ViewDefinition, opts ValidateOptions) ValidationResult {
	out := ValidationResult{Views: make([]ViewDefinition, 0, len(defs))}
	seen := map[string]Source{}
	for _, def := range defs {
		normalized := normalizeView(def)
		out.Views = append(out.Views, normalized)
		out.Issues = append(out.Issues, validateMetadata(normalized)...)
		if normalized.ID != "" {
			if first, ok := seen[normalized.ID]; ok {
				out.Issues = append(out.Issues,
					issue(normalized, "duplicate_view_id", "id", fmt.Sprintf("view id %q also appears in %s", normalized.ID, first.Path)),
				)
			} else {
				seen[normalized.ID] = normalized.Source
			}
			if _, ok := opts.GeneratedIDs[normalized.ID]; ok {
				out.Issues = append(out.Issues, issue(normalized, "duplicate_generated_view_id", "id", fmt.Sprintf("view id %q collides with a generated view", normalized.ID)))
			}
		}
		if normalized.SourceSpec.Kind == SourceKindCustom {
			out.Issues = append(out.Issues, validateCustom(def, normalized, opts.EntryFS)...)
			out.Issues = append(out.Issues, validateMount(normalized, opts)...)
			continue
		}
		out.Issues = append(out.Issues, validateSource(normalized, opts)...)
		out.Issues = append(out.Issues, validateMount(normalized, opts)...)
		out.Issues = append(out.Issues, validateDefaults(normalized)...)
		out.Issues = append(out.Issues, validateGroupBucket(normalized, opts)...)
		out.Issues = append(out.Issues, validateVariants(normalized)...)
	}
	out.Issues = append(out.Issues, validateMountedDefaults(out.Views, out.Issues)...)
	return out
}

func normalizeView(def ViewDefinition) ViewDefinition {
	def.APIVersion = strings.TrimSpace(def.APIVersion)
	def.ID = strings.TrimSpace(def.ID)
	def.Name = strings.TrimSpace(def.Name)
	def.SourceSpec.Kind = SourceKind(strings.TrimSpace(string(def.SourceSpec.Kind)))
	def.SourceSpec.Type = strings.TrimSpace(def.SourceSpec.Type)
	def.SourceSpec.Interface = strings.TrimSpace(def.SourceSpec.Interface)
	def.SourceSpec.QueryRecipe = strings.TrimSpace(def.SourceSpec.QueryRecipe)
	def.SourceSpec.ResultPath = strings.TrimSpace(def.SourceSpec.ResultPath)
	def.Mount.Kind = MountKind(strings.TrimSpace(string(def.Mount.Kind)))
	def.Mount.Type = strings.TrimSpace(def.Mount.Type)
	def.Mount.Interface = strings.TrimSpace(def.Mount.Interface)
	def.Mount.Group = strings.TrimSpace(def.Mount.Group)
	if def.Configuration != nil {
		def.Configuration = jsonValue(def.Configuration).(map[string]any)
	}
	if def.SourceSpec.Kind == SourceKindCustom {
		def.SourceSpec.Entry = strings.TrimSpace(def.SourceSpec.Entry)
		return def
	}
	normalizeVariants(&def.Variants)
	def.Defaults.Variant = strings.TrimSpace(def.Defaults.Variant)
	if def.Defaults.Variant == "" {
		def.Defaults.Variant = "table"
	} else if !variantExecutable(def, def.Defaults.Variant) && tableExecutable(def) {
		def.Defaults.Variant = "table"
	}
	if def.Defaults.First == 0 {
		def.Defaults.First = 200
	}
	if def.Generated && len(def.Defaults.Sort) == 0 {
		def.Defaults.Sort = []SortSpec{{Field: "title", Direction: "asc"}}
	}
	for i := range def.Defaults.Sort {
		def.Defaults.Sort[i].Field = strings.TrimSpace(def.Defaults.Sort[i].Field)
		def.Defaults.Sort[i].Direction = strings.TrimSpace(def.Defaults.Sort[i].Direction)
		if def.Defaults.Sort[i].Direction == "" {
			def.Defaults.Sort[i].Direction = "asc"
		}
	}
	if def.Defaults.Group != nil {
		def.Defaults.Group.Field = strings.TrimSpace(def.Defaults.Group.Field)
		def.Defaults.Group.Bucket = strings.TrimSpace(def.Defaults.Group.Bucket)
		for i := range def.Defaults.Group.Fields {
			def.Defaults.Group.Fields[i] = strings.TrimSpace(def.Defaults.Group.Fields[i])
		}
	}
	return def
}

// jsonValue copies configuration into JSON-encodable form. yaml.v3 decodes a
// nested map with any non-string key (`1:`, `true:`, `2026-01-02:`, `~:`) as
// map[interface{}]interface{}; yamlKeyText turns those keys back into text.
func jsonValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = jsonValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[yamlKeyText(key)] = jsonValue(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = jsonValue(item)
		}
		return out
	default:
		return value
	}
}

// yamlKeyText approximates the authored scalar for a decoded non-string key.
func yamlKeyText(key any) string {
	switch key := key.(type) {
	case nil:
		return "null"
	case time.Time:
		if key.Equal(key.Truncate(24*time.Hour)) && key.Location() == time.UTC {
			return key.Format(time.DateOnly)
		}
		return key.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(key)
	}
}

func normalizeVariants(variants *VariantSet) {
	if variants == nil {
		return
	}
	if variants.Table != nil {
		normalizeColumns(variants.Table.Columns)
		variants.Table.Density = strings.TrimSpace(variants.Table.Density)
	}
	if variants.Card != nil {
		normalizeCard(variants.Card)
	}
	if variants.Kanban != nil {
		variants.Kanban.ColumnField = strings.TrimSpace(variants.Kanban.ColumnField)
		variants.Kanban.LaneField = strings.TrimSpace(variants.Kanban.LaneField)
		if variants.Kanban.Card != nil {
			normalizeCard(variants.Kanban.Card)
		}
	}
}

func normalizeCard(card *CardSpec) {
	card.Eyebrow = strings.TrimSpace(card.Eyebrow)
	card.Title = strings.TrimSpace(card.Title)
	card.Preview = strings.TrimSpace(card.Preview)
	normalizeColumns(card.Fields)
}

func normalizeColumns(columns []ViewColumn) {
	for i := range columns {
		columns[i].Field = strings.TrimSpace(columns[i].Field)
		columns[i].Label = strings.TrimSpace(columns[i].Label)
	}
}

func validateMetadata(def ViewDefinition) []Issue {
	var issues []Issue
	if _, err := json.Marshal(def.Configuration); err != nil {
		issues = append(issues, issue(def, "invalid_view_configuration", "configuration", "configuration must contain JSON values: "+err.Error()))
	}
	if def.APIVersion == "" {
		issues = append(issues, issue(def, "missing_required_field", "apiVersion", "apiVersion is required"))
	} else if def.APIVersion != APIVersion {
		issues = append(issues, issue(def, "unsupported_api_version", "apiVersion", fmt.Sprintf("expected %s", APIVersion)))
	}
	if def.ID == "" {
		issues = append(issues, issue(def, "missing_required_field", "id", "id is required"))
	} else if !viewIDPattern.MatchString(def.ID) {
		issues = append(issues, issue(def, "invalid_view_id", "id", "id must match [A-Za-z0-9][A-Za-z0-9._:-]*"))
	}
	if def.Name == "" {
		issues = append(issues, issue(def, "missing_required_field", "name", "name is required"))
	}
	return issues
}

func validateSource(def ViewDefinition, opts ValidateOptions) []Issue {
	var issues []Issue
	if def.SourceSpec.Entry != "" {
		issues = append(issues, issue(def, "unexpected_field", "source.entry", "source.entry is only supported for custom sources"))
	}
	switch def.SourceSpec.Kind {
	case SourceKindOntologyType:
		if def.SourceSpec.Type == "" {
			return []Issue{issue(def, "missing_required_field", "source.type", "source.type is required for ontology_type sources")}
		}
		if opts.CheckReferences && !contains(opts.TypeNames, def.SourceSpec.Type) {
			unknown := issue(def, "unknown_ontology_type", "source.type", fmt.Sprintf("ontology type %q does not exist", def.SourceSpec.Type))
			unknown.Variant = def.SourceSpec.Type
			issues = append(issues, unknown)
		}
		issues = append(issues, unexpectedSourceFields(def, "source.interface", def.SourceSpec.Interface, "source.queryRecipe", def.SourceSpec.QueryRecipe, "source.resultPath", def.SourceSpec.ResultPath)...)
		if len(def.SourceSpec.Inputs) > 0 {
			issues = append(issues, issue(def, "unexpected_field", "source.inputs", "source.inputs is only supported for query_recipe sources"))
		}
	case SourceKindOntologyInterface:
		if def.SourceSpec.Interface == "" {
			return []Issue{issue(def, "missing_required_field", "source.interface", "source.interface is required for ontology_interface sources")}
		}
		if opts.CheckReferences && !contains(opts.InterfaceNames, def.SourceSpec.Interface) {
			unknown := issue(def, "unknown_ontology_interface", "source.interface", fmt.Sprintf("ontology interface %q does not exist", def.SourceSpec.Interface))
			unknown.Variant = def.SourceSpec.Interface
			issues = append(issues, unknown)
		}
		issues = append(issues, unexpectedSourceFields(def, "source.type", def.SourceSpec.Type, "source.queryRecipe", def.SourceSpec.QueryRecipe, "source.resultPath", def.SourceSpec.ResultPath)...)
		if len(def.SourceSpec.Inputs) > 0 {
			issues = append(issues, issue(def, "unexpected_field", "source.inputs", "source.inputs is only supported for query_recipe sources"))
		}
	case SourceKindQueryRecipe:
		if def.SourceSpec.QueryRecipe == "" {
			return []Issue{issue(def, "missing_required_field", "source.queryRecipe", "source.queryRecipe is required for query_recipe sources")}
		}
		if opts.CheckReferences && !contains(opts.QueryRecipeIDs, def.SourceSpec.QueryRecipe) {
			issues = append(issues, issue(def, "unknown_query_recipe", "source.queryRecipe", fmt.Sprintf("query recipe %q does not exist", def.SourceSpec.QueryRecipe)))
		}
		if def.SourceSpec.ResultPath == "" {
			recipeRowPath := opts.QueryRecipeRowPaths[def.SourceSpec.QueryRecipe]
			if recipeRowPath == "" {
				issues = append(issues, issueWithSeverity(def, "missing_recipe_row_path", "source.resultPath",
					fmt.Sprintf("source.resultPath is empty and recipe %q declares no outputContract.rowPath; runtime will emit view_result_path_required and return no rows", def.SourceSpec.QueryRecipe),
					IssueWarning))
			}
		}
		issues = append(issues, unexpectedSourceFields(def, "source.type", def.SourceSpec.Type, "source.interface", def.SourceSpec.Interface)...)
	case "":
		return []Issue{issue(def, "missing_required_field", "source.kind", "source.kind is required")}
	default:
		return []Issue{issue(def, "unsupported_source_kind", "source.kind", fmt.Sprintf("unsupported source kind %q", def.SourceSpec.Kind))}
	}
	return issues
}

func validateMount(def ViewDefinition, opts ValidateOptions) []Issue {
	var issues []Issue
	switch def.Mount.Kind {
	case MountKindType, MountKindNode:
		if def.Mount.Type == "" {
			return []Issue{issue(def, "missing_required_field", "mount.type", fmt.Sprintf("mount.type is required for %s mounts", def.Mount.Kind))}
		}
		// `type: "*"` names every type collection; a node mount names one type.
		if opts.CheckReferences && !(def.Mount.Kind == MountKindType && def.Mount.Type == "*") && !contains(opts.TypeNames, def.Mount.Type) {
			issues = append(issues, issue(def, "invalid_mount_target", "mount.type", fmt.Sprintf("ontology type %q does not exist", def.Mount.Type)))
		} else if opts.CheckReferences && def.Mount.Kind == MountKindNode && !contains(opts.NodeTypeNames, def.Mount.Type) {
			issues = append(issues, issue(def, "invalid_mount_target", "mount.type", fmt.Sprintf("ontology type %q is not a note or embedded node type", def.Mount.Type)))
		}
		issues = append(issues, unexpectedMountFields(def, "mount.interface", def.Mount.Interface)...)
		issues = append(issues, ignoredMountGroup(def)...)
	case MountKindInterface:
		if def.Mount.Interface == "" {
			return []Issue{issue(def, "missing_required_field", "mount.interface", "mount.interface is required for interface mounts")}
		}
		if opts.CheckReferences && def.Mount.Interface != "*" && !contains(opts.InterfaceNames, def.Mount.Interface) {
			issues = append(issues, issue(def, "invalid_mount_target", "mount.interface", fmt.Sprintf("ontology interface %q does not exist", def.Mount.Interface)))
		}
		issues = append(issues, unexpectedMountFields(def, "mount.type", def.Mount.Type)...)
		issues = append(issues, ignoredMountGroup(def)...)
	case MountKindGroup:
		if def.Mount.Group == "" {
			return []Issue{issue(def, "missing_required_field", "mount.group", "mount.group is required for group mounts")}
		}
		if opts.CheckReferences && opts.GroupNames != nil && def.Mount.Group != "*" && !contains(opts.GroupNames, def.Mount.Group) {
			issues = append(issues, issue(def, "invalid_mount_target", "mount.group", fmt.Sprintf("display group %q does not exist in navigation", def.Mount.Group)))
		}
		issues = append(issues, unexpectedMountFields(def, "mount.type", def.Mount.Type, "mount.interface", def.Mount.Interface)...)
	case MountKindWorkspace:
		issues = append(issues, unexpectedMountFields(def, "mount.type", def.Mount.Type, "mount.interface", def.Mount.Interface, "mount.group", def.Mount.Group)...)
	case MountKindStandalone:
		issues = append(issues, unexpectedMountFields(def, "mount.type", def.Mount.Type, "mount.interface", def.Mount.Interface)...)
	case "":
		return []Issue{issue(def, "missing_required_field", "mount.kind", "mount.kind is required")}
	default:
		return []Issue{issue(def, "unsupported_mount_kind", "mount.kind", fmt.Sprintf("unsupported mount kind %q", def.Mount.Kind))}
	}
	if def.Mount.Kind == MountKindNode && def.SourceSpec.Kind != SourceKindCustom {
		issues = append(issues, issue(def, "unsupported_mount_kind", "mount.kind", "node mounts require a custom source"))
	}
	if def.SourceSpec.Kind != SourceKindCustom && (def.Mount.Kind == MountKindType || def.Mount.Kind == MountKindInterface) {
		if field, value := mountTarget(def.Mount); value == "*" {
			// Still listed: a warning, like other mounts that read oddly but work.
			issues = append(issues, issueWithSeverity(def, "generic_native_mount", field, fmt.Sprintf("%s \"*\" shows this native view's own source on every %s; mount it on one %s, or use a custom view, which reads the collection it opens on", field, def.Mount.Kind, def.Mount.Kind), IssueWarning))
		}
	}
	if def.Mount.ReplaceGenerated && !ReplacesGenerated(def) {
		// Like ignoredMountGroup: warn and ignore so the view stays usable.
		issues = append(issues, issueWithSeverity(def, "unexpected_field", "mount.replaceGenerated", "mount.replaceGenerated is ignored; it applies only to native views mounted on one type or interface", IssueWarning))
	}
	return issues
}

// mountTarget names a type or interface mount's target field and value.
func mountTarget(mount MountSpec) (string, string) {
	if mount.Kind == MountKindInterface {
		return "mount.interface", mount.Interface
	}
	return "mount.type", mount.Type
}

// ignoredMountGroup warns rather than fails: earlier v1 files may carry a stray
// group on subject mounts, and ignoring it keeps those views working.
func ignoredMountGroup(def ViewDefinition) []Issue {
	if def.Mount.Group == "" {
		return nil
	}
	return []Issue{issueWithSeverity(def, "unexpected_field", "mount.group", fmt.Sprintf("mount.group is ignored for mount kind %q; it applies only to group and standalone mounts", def.Mount.Kind), IssueWarning)}
}

func unexpectedSourceFields(def ViewDefinition, pairs ...string) []Issue {
	return unexpectedFields(def, "source", pairs...)
}

func unexpectedMountFields(def ViewDefinition, pairs ...string) []Issue {
	return unexpectedFields(def, "mount", pairs...)
}

func unexpectedFields(def ViewDefinition, owner string, pairs ...string) []Issue {
	var issues []Issue
	for i := 0; i+1 < len(pairs); i += 2 {
		field, value := pairs[i], pairs[i+1]
		if strings.TrimSpace(value) == "" {
			continue
		}
		issues = append(issues, issue(def, "unexpected_field", field, fmt.Sprintf("%s is not supported for %s kind %q", field, owner, kindForOwner(def, owner))))
	}
	return issues
}

func kindForOwner(def ViewDefinition, owner string) string {
	switch owner {
	case "source":
		return string(def.SourceSpec.Kind)
	case "mount":
		return string(def.Mount.Kind)
	default:
		return ""
	}
}

func validateVariants(def ViewDefinition) []Issue {
	var issues []Issue
	if def.Variants.cardDecodeError != "" {
		issues = append(issues, issue(def, "invalid_card_variant", "variants.card", def.Variants.cardDecodeError))
	}
	if def.Variants.kanbanDecodeError != "" {
		issues = append(issues, issue(def, "invalid_kanban_variant", "variants.kanban", def.Variants.kanbanDecodeError))
	}
	if def.Variants.Table != nil {
		if len(def.Variants.Table.Columns) == 0 {
			issues = append(issues, issue(def, "missing_required_field", "variants.table.columns", "table columns are required"))
		}
		switch def.Variants.Table.Density {
		case "", TableDensityTwoLine, TableDensityOneLine:
		default:
			issues = append(issues, issue(def, "unsupported_table_density", "variants.table.density", fmt.Sprintf("table density %q must be %q or %q", def.Variants.Table.Density, TableDensityTwoLine, TableDensityOneLine)))
		}
		issues = append(issues, validateColumns(def, "variants.table.columns", def.Variants.Table.Columns)...)
	}
	if def.Variants.Card != nil {
		if !cardExecutable(def.Variants.Card) {
			issues = append(issues, issue(def, "invalid_card_variant", "variants.card.fields.field", "card fields require a field"))
		}
	}
	if def.Variants.Kanban != nil {
		if def.Variants.Kanban.ColumnField == "" {
			issues = append(issues, issue(def, "missing_kanban_column_field", "variants.kanban.columnField", "kanban columnField is required"))
		}
		if def.Variants.Kanban.Card != nil && !cardExecutable(def.Variants.Kanban.Card) {
			issues = append(issues, issue(def, "invalid_kanban_variant", "variants.kanban.card.fields.field", "kanban card fields require a field"))
		}
	}
	if !hasExecutableVariant(def) {
		issues = append(issues, issue(def, "missing_table_variant", "variants", "at least one executable table, card, or kanban variant is required"))
	}
	if def.Defaults.Variant != "table" && !variantExecutable(def, def.Defaults.Variant) {
		issues = append(issues, issue(def, "unsupported_variant", "defaults.variant", fmt.Sprintf("variant %q is not declared or executable", def.Defaults.Variant)))
	}
	return issues
}

func validateColumns(def ViewDefinition, fieldPath string, columns []ViewColumn) []Issue {
	var issues []Issue
	for _, column := range columns {
		if column.Field == "" {
			issues = append(issues, issue(def, "missing_required_field", fieldPath+".field", "column field is required"))
		}
	}
	return issues
}

func hasExecutableVariant(def ViewDefinition) bool {
	return tableExecutable(def) || cardExecutable(def.Variants.Card) || kanbanExecutable(def.Variants.Kanban)
}

func tableExecutable(def ViewDefinition) bool {
	if def.Variants.Table != nil {
		return len(def.Variants.Table.Columns) > 0 && columnsExecutable(def.Variants.Table.Columns)
	}
	return cardExecutable(def.Variants.Card) || kanbanExecutable(def.Variants.Kanban)
}

func variantExecutable(def ViewDefinition, variant string) bool {
	switch strings.TrimSpace(variant) {
	case "table":
		return tableExecutable(def)
	case "card":
		return cardExecutable(def.Variants.Card)
	case "kanban":
		return kanbanExecutable(def.Variants.Kanban)
	default:
		return false
	}
}

func cardExecutable(card *CardSpec) bool {
	return card != nil && columnsExecutable(card.Fields)
}

func kanbanExecutable(kanban *KanbanVariant) bool {
	return kanban != nil && kanban.ColumnField != "" && (kanban.Card == nil || cardExecutable(kanban.Card))
}

func columnsExecutable(columns []ViewColumn) bool {
	for _, column := range columns {
		if column.Field == "" {
			return false
		}
	}
	return true
}

func validateDefaults(def ViewDefinition) []Issue {
	var issues []Issue
	issues = append(issues, validateFilters(def, "defaults.filters", def.Defaults.Filters)...)
	presetIDs := map[string]struct{}{}
	for _, preset := range def.FilterPresets {
		id := strings.TrimSpace(preset.ID)
		if id == "" {
			issues = append(issues, issue(def, "missing_required_field", "filterPresets.id", "filter preset id is required"))
		} else if _, ok := presetIDs[id]; ok {
			issues = append(issues, issue(def, "duplicate_filter_preset", "filterPresets.id", fmt.Sprintf("filter preset %q is declared more than once", id)))
		}
		presetIDs[id] = struct{}{}
		if len(preset.Filters) == 0 {
			issues = append(issues, issue(def, "missing_required_field", "filterPresets.filters", "filter preset filters are required"))
		}
		issues = append(issues, validateFilters(def, "filterPresets.filters", preset.Filters)...)
	}
	for _, sort := range def.Defaults.Sort {
		if strings.TrimSpace(sort.Field) == "" {
			issues = append(issues, issue(def, "missing_required_field", "defaults.sort.field", "default sort field is required"))
		}
		switch strings.ToLower(strings.TrimSpace(sort.Direction)) {
		case "", "asc", "desc":
		default:
			issues = append(issues, issue(def, "unsupported_sort_direction", "defaults.sort.direction", fmt.Sprintf("unsupported sort direction %q", sort.Direction)))
		}
	}
	if def.Defaults.Group != nil {
		fields := def.Defaults.Group.Fields
		if len(fields) == 0 && strings.TrimSpace(def.Defaults.Group.Field) != "" {
			fields = []string{def.Defaults.Group.Field}
		}
		if len(fields) == 0 {
			issues = append(issues, issue(def, "missing_required_field", "defaults.group.fields", "default group fields are required"))
		}
		for _, field := range fields {
			if strings.TrimSpace(field) == "" {
				issues = append(issues, issue(def, "missing_required_field", "defaults.group.fields", "default group fields cannot be empty"))
				break
			}
		}
		seenValues := map[string]struct{}{}
		for _, value := range def.Defaults.Group.Values {
			if strings.TrimSpace(value.Value) == "" {
				issues = append(issues, issue(def, "missing_required_field", "defaults.group.values.value", "default group value is required"))
			}
			field := strings.TrimSpace(value.Field)
			if len(fields) > 1 && field == "" {
				issues = append(issues, issue(def, "missing_required_field", "defaults.group.values.field", "group value field is required when grouping by multiple fields"))
			}
			key := field + "\x00" + strings.TrimSpace(value.Value)
			if _, ok := seenValues[key]; ok {
				issues = append(issues, issue(def, "duplicate_group_value", "defaults.group.values", fmt.Sprintf("group value %q is declared more than once", value.Value)))
			}
			seenValues[key] = struct{}{}
		}
	}
	if def.Defaults.Page != nil {
		if def.Defaults.Page.Offset < 0 {
			issues = append(issues, issue(def, "invalid_page", "defaults.page.offset", "default page offset must be non-negative"))
		}
		if def.Defaults.Page.First < 0 {
			issues = append(issues, issue(def, "invalid_page", "defaults.page.first", "default page first must be non-negative"))
		}
	}
	if def.Defaults.SourceCap < 0 {
		issues = append(issues, issue(def, "invalid_source_cap", "defaults.sourceCap", "default source cap must be non-negative"))
	}
	return issues
}

func validateFilters(def ViewDefinition, fieldPath string, filters []FilterSpec) []Issue {
	var issues []Issue
	for _, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		op := strings.ToLower(strings.TrimSpace(filter.Op))
		if field == "" {
			issues = append(issues, issue(def, "missing_required_field", fieldPath+".field", "filter field is required"))
		}
		hasValue := strings.TrimSpace(filter.Value) != "" || strings.TrimSpace(filter.ValueFrom) != ""
		switch op {
		case "exists", "missing":
		case "eq", "neq", "contains", "gt", "gte", "lt", "lte":
			if !hasValue {
				issues = append(issues, issue(def, "missing_required_field", fieldPath+".value", fmt.Sprintf("filter value is required for %s filters", op)))
			}
		case "in":
			if len(filter.Values) == 0 {
				issues = append(issues, issue(def, "missing_required_field", fieldPath+".values", "in filters require at least one value"))
			}
		case "":
			issues = append(issues, issue(def, "missing_required_field", fieldPath+".op", "filter op is required"))
		default:
			issues = append(issues, issue(def, "unsupported_filter_operator", fieldPath+".op", fmt.Sprintf("unsupported filter operator %q", filter.Op)))
		}
	}
	return issues
}

func issue(def ViewDefinition, code, field, message string) Issue {
	return issueWithSeverity(def, code, field, message, IssueFatal)
}

func issueWithSeverity(def ViewDefinition, code, field, message string, severity IssueSeverity) Issue {
	return Issue{
		Code:     code,
		Severity: severity,
		Message:  message,
		View:     def.ID,
		Path:     def.Source.Path,
		Line:     def.Source.Line,
		Field:    field,
	}
}

func contains(values map[string]struct{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	_, ok := values[key]
	return ok
}
