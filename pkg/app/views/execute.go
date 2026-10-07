package views

import (
	"cmp"
	"encoding/json"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func normalizeState(def viewconfig.ViewDefinition, req ExecuteRequest) ExecutionState {
	variant := stateVariant(def, req)
	page := PageRequest{}
	if def.Defaults.Page != nil {
		page.Offset = max(0, def.Defaults.Page.Offset)
		page.First = def.Defaults.Page.First
	}
	if page.First <= 0 {
		page.First = def.Defaults.First
	}
	if page.First <= 0 {
		page.First = 200
	}
	// A board needs every column's cards, so only an explicit request size may
	// go below the kanban floor; the browser sends an empty page on most changes.
	if variant == "kanban" && req.Page.First <= 0 {
		page.First = max(page.First, kanbanDefaultPageSize)
	}
	if req.PageSet {
		page.Offset = max(0, req.Page.Offset)
	} else if req.Page.Offset > 0 {
		page.Offset = req.Page.Offset
	}
	if req.Page.First > 0 {
		page.First = req.Page.First
	}
	state := ExecutionState{
		Variant:      variant,
		FilterPreset: strings.TrimSpace(req.FilterPreset),
		Search:       firstNonEmpty(req.Search, def.Defaults.Search),
		Filters:      append([]viewconfig.FilterSpec(nil), def.Defaults.Filters...),
		Sort:         append([]viewconfig.SortSpec(nil), def.Defaults.Sort...),
		Group:        def.Defaults.Group,
		Page:         page,
		Source:       req.Source,
		Inputs:       map[string]string{},
	}
	if req.Filters != nil {
		state.Filters = append([]viewconfig.FilterSpec(nil), req.Filters...)
	}
	// A preset narrows whatever is already applied; the browser keeps sending its
	// manual filter list (even when empty) after the first manual edit.
	if state.FilterPreset != "" {
		state.Filters = append(state.Filters, filtersForPreset(def, state.FilterPreset)...)
	}
	if req.Sort != nil {
		state.Sort = append([]viewconfig.SortSpec(nil), req.Sort...)
	}
	if req.Group != nil {
		state.Group = req.Group
	}
	if variant == "kanban" && def.Variants.Kanban != nil {
		state.ColumnField = firstNonEmpty(strings.TrimSpace(req.ColumnField), strings.TrimSpace(def.Variants.Kanban.ColumnField))
		state.LaneField = firstNonEmpty(strings.TrimSpace(req.LaneField), strings.TrimSpace(def.Variants.Kanban.LaneField))
		state.Group = kanbanGroupSpec(def, state.ColumnField)
	}
	if req.Inputs != nil {
		for key, value := range req.Inputs {
			state.Inputs[key] = value
		}
	}
	for i := range state.Sort {
		state.Sort[i].Direction = strings.ToLower(strings.TrimSpace(state.Sort[i].Direction))
		if state.Sort[i].Direction == "" {
			state.Sort[i].Direction = "asc"
		}
	}
	return state
}

func filtersForPreset(def viewconfig.ViewDefinition, id string) []viewconfig.FilterSpec {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for _, preset := range def.FilterPresets {
		if strings.TrimSpace(preset.ID) == id {
			return append([]viewconfig.FilterSpec(nil), preset.Filters...)
		}
	}
	return nil
}

func stateVariant(def viewconfig.ViewDefinition, req ExecuteRequest) string {
	if strings.TrimSpace(req.Variant) != "" {
		return strings.TrimSpace(req.Variant)
	}
	if strings.TrimSpace(def.Defaults.Variant) != "" {
		return strings.TrimSpace(def.Defaults.Variant)
	}
	return "table"
}

func normalizeRows(rows []TableRow) []TableRow {
	out := make([]TableRow, 0, len(rows))
	for _, row := range rows {
		if row.Fields == nil {
			row.Fields = map[string]any{}
		}
		if row.Path == "" {
			row.Path = row.Ref.NotePath
		}
		if row.Title == "" {
			if value, ok := nestedFieldValue(row.Fields, "title"); ok {
				row.Title = valueString(value)
			}
		}
		if row.Tags == nil {
			row.Tags = []string{}
		}
		row.Fields["title"] = row.Title
		row.Fields["path"] = row.Path
		row.Fields["resolvedType"] = row.ResolvedType
		row.Fields["updatedAt"] = row.UpdatedAt
		row.Fields["hasIssues"] = row.HasIssues
		row.Fields["tags"] = row.Tags
		out = append(out, row)
	}
	return out
}

// compareSortValues classifies each value independently so mixed values cannot
// create comparison cycles. Filters retain their pairwise coercion semantics.
func compareSortValues(left any, right any) int {
	ltext, rtext := valueString(left), valueString(right)
	if ltext == "" || rtext == "" {
		return strings.Compare(ltext, rtext)
	}
	lnum, lnumeric := numberValue(left)
	rnum, rnumeric := numberValue(right)
	lnumeric = lnumeric && !math.IsNaN(lnum)
	rnumeric = rnumeric && !math.IsNaN(rnum)
	if lnumeric && rnumeric {
		return compareFloat(lnum, rnum)
	}
	if lnumeric {
		return -1
	}
	if rnumeric {
		return 1
	}
	ltime, ldate := timeValue(left)
	rtime, rdate := timeValue(right)
	if ldate && rdate {
		return ltime.Compare(rtime)
	}
	if ldate {
		return -1
	}
	if rdate {
		return 1
	}
	return strings.Compare(ltext, rtext)
}

func compareValues(left any, right any) int {
	if l, ok := numberValue(left); ok {
		if r, ok := numberValue(right); ok {
			return compareFloat(l, r)
		}
	}
	if l, ok := timeValue(left); ok {
		if r, ok := timeValue(right); ok {
			return l.Compare(r)
		}
	}
	return strings.Compare(valueString(left), valueString(right))
}

func compareFloat(left float64, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(typed)))
		return parsed, err == nil
	default:
		return false, false
	}
}

func timeValue(value any) (time.Time, bool) {
	raw := strings.TrimSpace(valueString(value))
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// groupRows groups rows by the group spec's fields. generated marks a
// generated view, whose lifecycle groups follow inferred stages too.
func groupRows(rows []TableRow, group *viewconfig.GroupSpec, capabilities []FieldCapability, generated bool) ([]TableRow, []TableGroup) {
	if group == nil {
		return rows, nil
	}
	fields := normalizedGroupFields(group)
	if len(fields) == 0 {
		return rows, nil
	}
	monthField := ""
	if group.Bucket == viewconfig.GroupBucketMonth {
		monthField = fields[0]
	}
	groupedRows, groups := buildGroups(rows, fields, 0, "", 0, capabilityMap(capabilities), groupValueOverrides(group), monthField, generated)
	return groupedRows, groups
}

func groupsForPage(groups []TableGroup, offset int, returned int) []TableGroup {
	if returned <= 0 || len(groups) == 0 {
		return nil
	}
	return groupsForWindow(groups, offset, offset+returned, offset)
}

func groupsForWindow(groups []TableGroup, windowStart int, windowEnd int, base int) []TableGroup {
	out := make([]TableGroup, 0, len(groups))
	for _, group := range groups {
		start := max(group.RowStart, windowStart)
		end := min(group.RowEnd, windowEnd)
		if start >= end {
			continue
		}
		next := group
		next.RowStart = start - base
		next.RowEnd = end - base
		next.Count = next.RowEnd - next.RowStart
		next.Children = groupsForWindow(group.Children, windowStart, windowEnd, base)
		out = append(out, next)
	}
	return out
}

func normalizedGroupFields(group *viewconfig.GroupSpec) []string {
	raw := group.Fields
	if len(raw) == 0 && strings.TrimSpace(group.Field) != "" {
		raw = []string{group.Field}
	}
	out := make([]string, 0, len(raw))
	for _, field := range raw {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	if len(out) == 1 && out[0] == viewconfig.GroupNone {
		return nil
	}
	return out
}

type rowBucket struct {
	value        string
	label        string
	sortValue    any
	sortKey      groupSortValue
	sortKeyReady bool
	rows         []TableRow
}

// buildGroups nests groups by fields in order; monthField, when set, groups
// that date field by calendar month instead of by value.
func buildGroups(rows []TableRow, fields []string, depth int, parentKey string, rowStart int, caps map[string]FieldCapability, overrides map[string]viewconfig.GroupValueSpec, monthField string, generated bool) ([]TableRow, []TableGroup) {
	if depth >= len(fields) {
		return rows, nil
	}
	field := fields[depth]
	var buckets []rowBucket
	if field == monthField {
		buckets = monthBuckets(rows, field, caps[field])
	} else {
		buckets = orderedBuckets(rows, field, caps[field], overrides, generated)
	}
	outRows := make([]TableRow, 0, len(rows))
	groups := make([]TableGroup, 0, len(buckets))
	nextStart := rowStart
	for _, bucket := range buckets {
		groupKey := groupKey(parentKey, field, bucket.value)
		childrenRows, children := buildGroups(bucket.rows, fields, depth+1, groupKey, nextStart, caps, overrides, monthField, generated)
		start := nextStart
		outRows = append(outRows, childrenRows...)
		nextStart += len(childrenRows)
		groups = append(groups, TableGroup{
			Field:              field,
			Key:                groupKey,
			Label:              groupLabel(field, bucket.value, bucket.label, caps[field], overrides),
			Value:              bucket.value,
			Count:              len(childrenRows),
			TotalCount:         len(childrenRows),
			Tone:               groupTone(bucket.value, caps[field]),
			Depth:              depth,
			RowStart:           start,
			RowEnd:             nextStart,
			CollapsedByDefault: groupCollapsedByDefault(field, bucket.value, caps[field], overrides),
			Children:           children,
		})
	}
	return outRows, groups
}

func orderedBuckets(rows []TableRow, field string, cap FieldCapability, overrides map[string]viewconfig.GroupValueSpec, generated bool) []rowBucket {
	relation := normalizedValueKind(cap.ValueKind) == "relation"
	byValue := map[string]int{}
	out := make([]rowBucket, 0)
	for _, row := range rows {
		values := []any{""}
		if raw, ok := capabilityFieldValue(row, field, cap); ok {
			values = groupValues(raw)
		}
		var links map[string]TableRelationValue
		if relation {
			links = rowRelationValues(row, field, cap)
		}
		seen := map[string]struct{}{}
		for _, raw := range values {
			value := groupBucketValue(raw, cap)
			identity, label := value, ""
			var link TableRelationValue
			if relation && value != "" {
				link = links[strings.TrimSpace(value)]
				identity, value, label = relationGroupIdentity(value, links)
			}
			if _, ok := seen[identity]; ok {
				continue
			}
			seen[identity] = struct{}{}
			idx, ok := byValue[identity]
			if !ok {
				idx = len(out)
				byValue[identity] = idx
				out = append(out, rowBucket{value: value, sortValue: raw})
			}
			// Unresolved spellings of one target keep the least, so the
			// group's value and key do not depend on row order.
			if label != "" && (!ok || value < out[idx].value) {
				// Link groups follow their targets' rank, then title,
				// case-insensitively.
				out[idx].value, out[idx].label = value, label
				out[idx].sortKey, out[idx].sortKeyReady = groupSortValue{kind: 3, text: strings.ToLower(label)}, true
				if link.ranked {
					out[idx].sortKey = groupSortValue{kind: 1, number: link.rank}
				}
			}
			out[idx].rows = append(out[idx].rows, row)
		}
	}
	enumRanks := enumRankMap(cap)
	if ranks := lifecycleGroupRanks(cap, generated); ranks != nil {
		enumRanks = ranks
	}
	enumOrder := enumOrderMap(cap)
	sort.SliceStable(out, func(i, j int) bool {
		return groupBucketLess(field, &out[i], &out[j], cap, enumRanks, enumOrder, overrides)
	})
	return out
}

// lifecycleGroupRanks orders a lifecycle field's groups by stage (active,
// open, done, dropped), then declaration order (SPEC-0112). It applies to
// generated views and to enums that declare their stages; an authored view
// over inferred stages keeps the enum's order. Authored group value orders
// still win; board columns keep the enum's order.
func lifecycleGroupRanks(cap FieldCapability, generated bool) map[string]int {
	if cap.SemanticRole != "status" || len(cap.EnumValues) == 0 {
		return nil
	}
	out := make(map[string]int, len(cap.EnumValues))
	for index, value := range cap.EnumValues {
		if value.Stage == "" || (!generated && !value.StageDeclared) {
			return nil
		}
		out[value.Value] = stageRank(value.Stage)*len(cap.EnumValues) + index
	}
	return out
}

func groupBucketValue(value any, capability FieldCapability) string {
	if normalizedValueKind(capability.ValueKind) == "bool" {
		if parsed, ok := boolValue(value); ok {
			return strconv.FormatBool(parsed)
		}
	}
	return valueString(value)
}

func groupValues(value any) []any {
	if value == nil {
		return []any{""}
	}
	if values, ok := listValues(value); ok {
		if len(values) == 0 {
			return []any{""}
		}
		out := make([]any, 0, len(values))
		for _, item := range values {
			out = append(out, groupValues(item)...)
		}
		if len(out) == 0 {
			return []any{""}
		}
		return out
	}
	return []any{value}
}

func groupBucketLess(field string, left *rowBucket, right *rowBucket, cap FieldCapability, enumRanks map[string]int, enumOrder map[string]int, overrides map[string]viewconfig.GroupValueSpec) bool {
	if left.value == "" || right.value == "" {
		return left.value != "" && right.value == ""
	}
	leftRank, leftKnown := groupValueRank(field, left.value, enumRanks, overrides)
	rightRank, rightKnown := groupValueRank(field, right.value, enumRanks, overrides)
	switch {
	case leftKnown && rightKnown && leftRank != rightRank:
		return leftRank < rightRank
	case leftKnown != rightKnown:
		return leftKnown
	}
	if leftOrder, leftOK := enumOrder[left.value]; leftOK {
		if rightOrder, rightOK := enumOrder[right.value]; rightOK && leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
	}
	if cmp := left.sortOrder().compare(right.sortOrder()); cmp != 0 {
		return cmp < 0
	}
	if cmp := strings.Compare(groupLabel(field, left.value, left.label, cap, overrides), groupLabel(field, right.value, right.label, cap, overrides)); cmp != 0 {
		return cmp < 0
	}
	return left.value < right.value
}

func groupValueOverrides(group *viewconfig.GroupSpec) map[string]viewconfig.GroupValueSpec {
	out := map[string]viewconfig.GroupValueSpec{}
	if group == nil {
		return out
	}
	fields := normalizedGroupFields(group)
	for _, value := range group.Values {
		field := strings.TrimSpace(value.Field)
		if field == "" && len(fields) == 1 {
			field = fields[0]
		}
		key := groupOverrideKey(field, value.Value)
		out[key] = value
	}
	return out
}

func groupValueRank(field string, value string, enumRanks map[string]int, overrides map[string]viewconfig.GroupValueSpec) (int, bool) {
	if override, ok := overrides[groupOverrideKey(field, value)]; ok && override.Order != 0 {
		return override.Order, true
	}
	rank, ok := enumRanks[value]
	return rank, ok
}

func capabilityMap(capabilities []FieldCapability) map[string]FieldCapability {
	out := make(map[string]FieldCapability, len(capabilities))
	for _, cap := range capabilities {
		if cap.Key != "" {
			out[cap.Key] = cap
		}
	}
	for _, cap := range capabilities {
		aliases := append(append([]string(nil), cap.SourceKeys...), cap.CanonicalField)
		for _, alias := range aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			if _, ok := out[alias]; !ok {
				out[alias] = cap
			}
		}
	}
	return out
}

func enumRankMap(cap FieldCapability) map[string]int {
	out := map[string]int{}
	for _, value := range cap.EnumValues {
		if value.Value != "" {
			out[value.Value] = value.Rank
		}
	}
	if normalizedValueKind(cap.ValueKind) == "bool" {
		out["false"] = 0
		out["true"] = 1
	}
	return out
}

func enumOrderMap(cap FieldCapability) map[string]int {
	out := map[string]int{}
	for index, value := range cap.EnumValues {
		if value.Value != "" {
			out[value.Value] = index
		}
	}
	return out
}

// groupLabel prefers an authored override, then an enum label, then the
// bucket's own display label, such as a link target's title.
func groupLabel(field string, value string, display string, cap FieldCapability, overrides map[string]viewconfig.GroupValueSpec) string {
	if override, ok := overrides[groupOverrideKey(field, value)]; ok && strings.TrimSpace(override.Label) != "" {
		return override.Label
	}
	for _, enumValue := range cap.EnumValues {
		if enumValue.Value == value && strings.TrimSpace(enumValue.Label) != "" {
			return enumValue.Label
		}
	}
	if display != "" {
		return display
	}
	if strings.TrimSpace(value) == "" {
		return "(empty)"
	}
	return value
}

// rowRelationValues indexes a row's link values for field by their raw text.
func rowRelationValues(row TableRow, field string, cap FieldCapability) map[string]TableRelationValue {
	for _, key := range append([]string{field, cap.Key, cap.CanonicalField}, cap.SourceKeys...) {
		values := row.RelationValues[strings.TrimSpace(key)]
		if len(values) == 0 {
			continue
		}
		out := make(map[string]TableRelationValue, len(values))
		for _, value := range values {
			out[strings.TrimSpace(value.Value)] = value
		}
		return out
	}
	return nil
}

// relationGroupIdentity keys a link value on its resolved target, so links
// that differ only by alias or folder share a group whose value is the
// target's canonical link and whose label is its title. Unresolved links key
// on their target text and keep their authored spelling and alias.
func relationGroupIdentity(value string, links map[string]TableRelationValue) (identity, groupValue, label string) {
	link := links[strings.TrimSpace(value)]
	if link.Ref == nil {
		return "link:" + strings.ToLower(unwrapLinkText(value)), value, LinkDisplayText(value)
	}
	canonical := canonicalLinkValue(*link.Ref)
	return "ref:" + noderead.RefIdentityKey(*link.Ref), canonical, cmp.Or(strings.TrimSpace(link.Title), LinkDisplayText(canonical))
}

func unwrapLinkText(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") {
		raw = raw[2 : len(raw)-2]
		if idx := strings.Index(raw, "|"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

func groupCollapsedByDefault(field string, value string, cap FieldCapability, overrides map[string]viewconfig.GroupValueSpec) bool {
	if override, ok := overrides[groupOverrideKey(field, value)]; ok {
		if override.CollapsedByDefault != nil {
			return *override.CollapsedByDefault
		}
		if override.Collapsed != nil {
			return *override.Collapsed
		}
	}
	for _, enumValue := range cap.EnumValues {
		if enumValue.Value == value {
			return enumValue.CollapsedByDefault
		}
	}
	return false
}

func groupOverrideKey(field string, value string) string {
	return strings.TrimSpace(field) + "\x00" + value
}

func groupKey(parent string, field string, value string) string {
	current := url.QueryEscape(field) + "=" + url.QueryEscape(value)
	if parent == "" {
		return current
	}
	return parent + "/" + current
}

func paginateRows(rows []TableRow, page PageRequest) ([]TableRow, PageInfo) {
	first := page.First
	if first <= 0 {
		first = 200
	}
	offset := max(0, page.Offset)
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + first
	if end > len(rows) {
		end = len(rows)
	}
	pageRows := append([]TableRow(nil), rows[offset:end]...)
	return pageRows, PageInfo{
		Total:    len(rows),
		Offset:   offset,
		First:    first,
		Returned: len(pageRows),
		HasMore:  end < len(rows),
	}
}

func nodeRefKey(ref ontology.NodeRef) string {
	// WHY: structural is intentionally excluded. View hydration matches freshly
	// fetched row refs against freshly fetched catalog refs from the same
	// snapshot, so NotePath + Fragment + NodeID + Kind already uniquely keys
	// the row. Including Structural forced every view's GraphQL ref selection
	// to opt into requesting it; omitting it there silently broke field
	// enrichment because row.Ref.Structural stayed empty while the catalog
	// ref had a fingerprint, and the lookup missed.
	return ref.NotePath + "|" + ref.Fragment + "|" + ref.NodeID + "|" + string(ref.Kind)
}
