package views

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
	"math"
	"strings"
	"time"
)

func applySearchAndFilters(rows []TableRow, search string, filters []viewconfig.FilterSpec, capabilities []FieldCapability) ([]TableRow, error) {
	out := make([]TableRow, 0, len(rows))
	search = strings.ToLower(strings.TrimSpace(search))
	caps := capabilityMap(capabilities)
	for _, filter := range filters {
		if cap, ok := caps[strings.TrimSpace(filter.Field)]; ok && cap.ValueKind == "relation" {
			op := strings.ToLower(strings.TrimSpace(filter.Op))
			if op != "eq" && op != "in" && op != "exists" && op != "missing" {
				return nil, fmt.Errorf("%w: link field %q supports eq, in, exists, or missing; %q does not test resolved target membership", ErrInvalidRequest, filter.Field, filter.Op)
			}
		}
	}
	matchers := make([]func(TableRow) (bool, error), 0, len(filters))
	for _, filter := range filters {
		if _, ok := caps[strings.TrimSpace(filter.Field)]; !ok && strings.TrimSpace(filter.Field) == staleFilterField {
			want, err := staleFilterValue(filter)
			if err != nil {
				return nil, err
			}
			lifecycle, _ := resolveCapability(capabilities, firstCapabilityField(capabilities, "status"))
			rule := newStaleRule(lifecycle, time.Now())
			matchers = append(matchers, func(row TableRow) (bool, error) { return rule.matches(row) == want, nil })
			continue
		}
		matchers = append(matchers, func(row TableRow) (bool, error) { return rowMatchesFilter(row, filter, caps) })
	}
	for _, row := range rows {
		if search != "" && !rowMatchesSearch(row, search) {
			continue
		}
		matches := true
		for _, match := range matchers {
			ok, err := match(row)
			if err != nil {
				return nil, err
			}
			if !ok {
				matches = false
				break
			}
		}
		if matches {
			out = append(out, row)
		}
	}
	return out, nil
}

// staleFilterField names the stale filter, {field: stale, op: eq, value:
// true}: rows in an active-stage lifecycle value unchanged for 30 days, the
// rule ExecutionStats.staleCount uses. A schema field named stale wins.
const staleFilterField = "stale"

func staleFilterValue(filter viewconfig.FilterSpec) (bool, error) {
	want, ok := boolValue(filter.Value)
	if strings.ToLower(strings.TrimSpace(filter.Op)) != "eq" || !ok {
		return false, fmt.Errorf("%w: the stale filter takes op eq and value true or false", ErrInvalidRequest)
	}
	return want, nil
}

func rowMatchesSearch(row TableRow, search string) bool {
	for _, field := range []string{row.Title, row.Path, row.ResolvedType} {
		if strings.Contains(strings.ToLower(field), search) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(strings.Join(row.Tags, " ")), search) {
		return true
	}
	for _, value := range row.Fields {
		if strings.Contains(strings.ToLower(valueString(value)), search) {
			return true
		}
	}
	return false
}

func rowMatchesFilter(row TableRow, filter viewconfig.FilterSpec, capabilities map[string]FieldCapability) (bool, error) {
	field := strings.TrimSpace(filter.Field)
	op := strings.ToLower(strings.TrimSpace(filter.Op))
	value, exists := fieldValue(row, field)
	linkLike := filterFieldUsesPathMatching(field, capabilities[field])
	if cap := capabilities[field]; len(cap.IndexedFilterOps) > 0 && !linkLike {
		predicate := readmodel.FieldPredicate{FieldName: field, Op: readmodel.FieldOperator(op)}
		wants := []string{filter.Value}
		if op == "in" {
			wants = filter.Values
		}
		for _, want := range wants {
			predicate.Values = append(predicate.Values, ontology.FieldValueRow(indexedScalarField(cap.ValueKind), want))
		}
		switch op {
		case "eq", "neq", "in", "contains", "exists", "missing", "gt", "gte", "lt", "lte":
			return pushdown.Match(predicate, indexedScalarRows(value, false, cap.ValueKind)), nil
		}
	}
	switch op {
	case "exists":
		return exists, nil
	case "missing":
		return !exists || !hasFieldValue(value), nil
	case "eq":
		return exists && (filterValueEquals(value, filter.Value, capabilities[field], linkLike) || rowLinksToCanonical(row, field, capabilities[field], filter.Value)), nil
	case "neq":
		return !exists || !(filterValueEquals(value, filter.Value, capabilities[field], linkLike) || rowLinksToCanonical(row, field, capabilities[field], filter.Value)), nil
	case "contains":
		return exists && strings.Contains(strings.ToLower(valueString(value)), strings.ToLower(filter.Value)), nil
	case "in":
		if !exists {
			return false, nil
		}
		for _, want := range filter.Values {
			if filterValueEquals(value, want, capabilities[field], linkLike) || rowLinksToCanonical(row, field, capabilities[field], want) {
				return true, nil
			}
		}
		return false, nil
	case "gt", "gte", "lt", "lte":
		return compareFilter(value, filter.Value, op, capabilities[field]), nil
	default:
		return false, fmt.Errorf("%w: unsupported filter operator %q", ErrInvalidRequest, filter.Op)
	}
}

// hasFieldValue reports whether a field value holds anything: not nil, blank
// text, or a list of only those. false and 0 are values.
func hasFieldValue(value any) bool {
	if values, ok := listValues(value); ok {
		for _, item := range values {
			if hasFieldValue(item) {
				return true
			}
		}
		return false
	}
	return value != nil && strings.TrimSpace(valueString(value)) != ""
}

// rowLinksToCanonical matches a canonical link option, such as a facet value,
// against the targets the row's link values resolved to, whatever their spelling.
func rowLinksToCanonical(row TableRow, field string, cap FieldCapability, want string) bool {
	if cap.ValueKind != "relation" {
		return false
	}
	want = strings.TrimSpace(want)
	for _, value := range rowRelationValues(row, field, cap) {
		if value.Ref != nil && canonicalLinkValue(*value.Ref) == want {
			return true
		}
	}
	return false
}

func filterValueEquals(actual any, want string, cap FieldCapability, linkLike bool) bool {
	if fieldUsesBoolMatching(cap) {
		wantBool, ok := boolValue(want)
		if !ok {
			return false
		}
		for _, value := range filterComparableValues(actual) {
			actualBool, ok := boolValue(value)
			if ok && actualBool == wantBool {
				return true
			}
		}
		return false
	}
	for _, value := range filterComparableValues(actual) {
		if !linkLike {
			if value == want {
				return true
			}
			continue
		}
		if markdownLinkFilterValuesEqualCompat(value, want) {
			return true
		}
	}
	if linkLike {
		wantValues := linkComparableValues(want)
		for _, actualValue := range linkComparableValues(actual) {
			for _, wantValue := range wantValues {
				if markdownLinkFilterValuesEqualCompat(actualValue, wantValue) {
					return true
				}
			}
		}
	}
	return false
}

func filterFieldUsesPathMatching(field string, cap FieldCapability) bool {
	if cap.ValueKind == "relation" || (cap.Edit != nil && cap.Edit.Kind == "node") {
		return true
	}
	field = strings.ToLower(strings.TrimSpace(field))
	return field == "path" || field == "notepath" || field == "ref" ||
		strings.HasSuffix(field, ".path") || strings.HasSuffix(field, ".notepath") || strings.HasSuffix(field, ".ref")
}

func filterComparableValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, filterComparableValues(item)...)
		}
		return out
	default:
		return []string{valueString(value)}
	}
}

func linkComparableValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, linkComparableValues(item)...)
		}
		return out
	case map[string]any:
		keys := []string{"path", "notePath", "ref", "title", "name", "label", "aliases", "alias"}
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			if value, ok := typed[key]; ok {
				out = append(out, linkComparableValues(value)...)
			}
		}
		return out
	case map[string]string:
		keys := []string{"path", "notePath", "ref", "title", "name", "label", "aliases", "alias"}
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			if value := typed[key]; value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		raw := valueString(value)
		if raw == "" {
			return nil
		}
		out := []string{raw}
		if title := paths.NoteTitle(strings.TrimSpace(raw)); title != "" && title != raw {
			out = append(out, title)
		}
		return out
	}
}

// markdownLinkFilterValuesEqualCompat preserves extensionless Markdown link
// filters for legacy configured views. All explicit extensions remain exact.
func markdownLinkFilterValuesEqualCompat(actual string, want string) bool {
	actualNorm := normalizedLinkFilterValue(actual)
	wantNorm := normalizedLinkFilterValue(want)
	if actualNorm == wantNorm {
		return true
	}
	return strings.TrimSuffix(actualNorm, ".md") == strings.TrimSuffix(wantNorm, ".md")
}

func normalizedLinkFilterValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "[[") && strings.HasSuffix(value, "]]") {
		value = strings.TrimPrefix(strings.TrimSuffix(value, "]]"), "[[")
	}
	if idx := strings.Index(value, "|"); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func compareFilter(actual any, want string, op string, cap FieldCapability) bool {
	if values, ok := listValues(actual); ok {
		for _, value := range values {
			if compareFilter(value, want, op, cap) {
				return true
			}
		}
		return false
	}
	cmp, ok := compareFilterValues(actual, want, cap)
	if !ok {
		return false
	}
	switch op {
	case "gt":
		return cmp > 0
	case "gte":
		return cmp >= 0
	case "lt":
		return cmp < 0
	case "lte":
		return cmp <= 0
	default:
		return false
	}
}

func compareFilterValues(actual any, want string, cap FieldCapability) (int, bool) {
	switch normalizedValueKind(cap.ValueKind) {
	case "number", "real":
		left, ok := numberValue(actual)
		if !ok || math.IsNaN(left) {
			return 0, false
		}
		right, ok := numberValue(want)
		if !ok || math.IsNaN(right) {
			return 0, false
		}
		return compareFloat(left, right), true
	case "date":
		left, ok := timeValue(actual)
		if !ok {
			return 0, false
		}
		right, ok := timeValue(want)
		if !ok {
			return 0, false
		}
		return left.Compare(right), true
	default:
		return compareValues(actual, want), true
	}
}

func normalizedValueKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bool", "boolean":
		return "bool"
	case "int", "integer", "float", "number", "numeric":
		return "number"
	case "date", "datetime", "time", "timestamp":
		return "date"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}
func fieldUsesBoolMatching(cap FieldCapability) bool {
	return normalizedValueKind(cap.ValueKind) == "bool"
}
