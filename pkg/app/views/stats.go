package views

import (
	"slices"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// staleAfter is how long a record may sit in an active lifecycle value
// before views call it stale.
const staleAfter = 30 * 24 * time.Hour

// staleRule matches rows that hold an active-stage lifecycle value and have
// not changed for staleAfter by the server clock. Statistics and the stale
// filter share it so the header's count and its filter agree.
type staleRule struct {
	lifecycle FieldCapability
	active    map[string]bool
	before    int64
}

func newStaleRule(lifecycle FieldCapability, now time.Time) staleRule {
	active := map[string]bool{}
	for _, value := range lifecycle.EnumValues {
		if value.Stage == ontology.StageActive {
			active[value.Value] = true
		}
	}
	return staleRule{lifecycle: lifecycle, active: active, before: now.Add(-staleAfter).Unix()}
}

func (r staleRule) matches(row TableRow) bool {
	if len(r.active) == 0 || row.UpdatedAt <= 0 || row.UpdatedAt >= r.before {
		return false
	}
	raw, ok := capabilityFieldValue(row, r.lifecycle.Key, r.lifecycle)
	return ok && r.active[valueString(raw)]
}

// executionStats summarizes every row that matched a type or interface view's
// search and filters, before paging (SPEC-0112 "Execution statistics").
func executionStats(schema *ontology.Schema, def viewconfig.ViewDefinition, profile ontology.TypeProfile, rows []TableRow, capabilities []FieldCapability, counted []string, now time.Time) *ExecutionStats {
	stats := &ExecutionStats{Total: len(rows)}
	lifecycle, hasLifecycle := resolveCapability(capabilities, profile.LifecycleField)
	stale := newStaleRule(lifecycle, now)
	lifecycleCounts := map[string]int{}
	kinds := map[string]int{}
	for _, row := range rows {
		if row.HasIssues {
			stats.IssueCount++
		}
		if def.SourceSpec.Kind == viewconfig.SourceKindOntologyInterface && row.ResolvedType != "" {
			kinds[row.ResolvedType]++
		}
		if !hasLifecycle {
			continue
		}
		value := ""
		if raw, ok := capabilityFieldValue(row, lifecycle.Key, lifecycle); ok {
			value = valueString(raw)
		}
		if value == "" {
			continue
		}
		lifecycleCounts[value]++
		if stale.matches(row) {
			stats.StaleCount++
		}
	}
	stats.Lifecycle = enumOrderedCounts(lifecycle.EnumValues, lifecycleCounts)
	stats.Kinds = countsByFrequency(kinds)
	stats.Fields = fieldFillCounts(schema, def, profile, rows, capabilities, counted)
	stats.Dates = dateRange(profile.PrimaryDateField, rows, capabilities)
	return stats
}

// enumOrderedCounts lists populated values in enum order, then any values
// outside the enum by name.
func enumOrderedCounts(values []FieldEnumValue, counts map[string]int) []StatsValueCount {
	var out []StatsValueCount
	for _, value := range values {
		if count := counts[value.Value]; count > 0 {
			out = append(out, StatsValueCount{Value: value.Value, Count: count})
			delete(counts, value.Value)
		}
	}
	extra := countsByFrequency(counts)
	sort.SliceStable(extra, func(i, j int) bool { return extra[i].Value < extra[j].Value })
	return append(out, extra...)
}

func countsByFrequency(counts map[string]int) []StatsValueCount {
	out := make([]StatsValueCount, 0, len(counts))
	for value, count := range counts {
		out = append(out, StatsValueCount{Value: value, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// fieldFillCounts counts rows that fill each generated column field and each
// KEY field: generated columns first, in column order, then other KEY fields.
// Reverse count fields are reported only when the request projected them
// (counted lists the projected ones), so an unread count never reads as empty.
func fieldFillCounts(schema *ontology.Schema, def viewconfig.ViewDefinition, profile ontology.TypeProfile, rows []TableRow, capabilities []FieldCapability, counted []string) []StatsFieldFill {
	fields := subjectFields(schema, sourceSubject(def))
	var names []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] && schemaFieldByName(fields, name) != nil {
			seen[name] = true
			names = append(names, name)
		}
	}
	interfaceSource := def.SourceSpec.Kind == viewconfig.SourceKindOntologyInterface
	for _, column := range generatedColumns(schema, fields, profile, generatedIdentifierField(fields), interfaceSource) {
		add(column.Field)
	}
	for _, field := range fields {
		if field != nil && field.Display.EffectiveImportance() == ontology.FieldImportanceKey {
			add(field.Name)
		}
	}
	out := make([]StatsFieldFill, 0, len(names))
	for _, name := range names {
		capability, ok := resolveCapability(capabilities, name)
		if ok && capability.SemanticRole == "count" && !slices.Contains(counted, capability.Key) {
			continue
		}
		filled := 0
		for _, row := range rows {
			if ok && rowFillsField(row, capability) {
				filled++
			}
		}
		out = append(out, StatsFieldFill{Field: name, Filled: filled})
	}
	return out
}

func rowFillsField(row TableRow, capability FieldCapability) bool {
	value, ok := capabilityFieldValue(row, capability.Key, capability)
	if !ok {
		return false
	}
	if capability.SemanticRole == "count" {
		count, numeric := numberValue(value)
		return numeric && count > 0
	}
	return hasFieldValue(value)
}

// dateRange reports the first and last primary dates as YYYY-MM-DD and the
// rows per calendar month, oldest first.
func dateRange(field string, rows []TableRow, capabilities []FieldCapability) *StatsDateRange {
	if field == "" {
		return nil
	}
	out := &StatsDateRange{Field: field}
	capability, ok := resolveCapability(capabilities, field)
	if !ok {
		return out
	}
	months := map[string]int{}
	var first, last time.Time
	for _, row := range rows {
		raw, ok := capabilityFieldValue(row, capability.Key, capability)
		if !ok {
			continue
		}
		at, ok := rowDate(raw)
		if !ok {
			continue
		}
		if first.IsZero() || at.Before(first) {
			first = at
		}
		if last.IsZero() || at.After(last) {
			last = at
		}
		months[at.Format("2006-01")]++
	}
	if first.IsZero() {
		return out
	}
	out.First, out.Last = first.Format(time.DateOnly), last.Format(time.DateOnly)
	for month, count := range months {
		out.Months = append(out.Months, StatsValueCount{Value: month, Count: count})
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].Value < out.Months[j].Value })
	return out
}

// rowDate parses a Date or DateTime value, keeping its authored offset.
func rowDate(value any) (time.Time, bool) {
	for _, item := range groupValues(value) {
		if parsed, ok := item.(time.Time); ok {
			return parsed, true
		}
		if parsed, ok := timeValue(item); ok {
			return parsed, true
		}
	}
	return time.Time{}, false
}
