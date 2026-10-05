package query

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
)

func (e *executor) sectionMatchesRootFilters(section *sectionRecord, raw any) (bool, error) {
	return matchesRootFilters(raw, "embedded", e.schema.Types[section.TypeName], func(field string) []string {
		return e.sectionRootFieldValues(section, field)
	})
}

func (e *executor) noteMatchesRootFilters(note *noteRecord, raw any, linkTargets map[string]map[string][]string) (bool, error) {
	return matchesRootFilters(raw, "note", e.schema.Types[note.TypeName], func(field string) []string {
		if noteType := e.schema.Types[note.TypeName]; noteType != nil {
			if schemaField := noteType.ByName[field]; schemaField != nil && schemaField.Kind == ontology.FieldKindLink {
				return linkTargets[note.Path][field]
			}
		}
		return e.noteRootFieldValues(note, field)
	})
}

func matchesRootFilters(raw any, kind string, noteType *ontology.NoteType, fieldValues func(string) []string) (bool, error) {
	filters, _ := raw.([]any)
	for _, item := range filters {
		filter, ok := item.(map[string]any)
		if !ok {
			continue
		}
		field := strings.TrimSpace(stringValue(filter["field"]))
		if field == "" {
			continue
		}
		matches, err := rootFilterMatches(fieldValues(field), filter, kind, rootSchemaField(noteType, field))
		if err != nil || !matches {
			return false, err
		}
	}
	return true, nil
}

func rootFilterMatches(values []string, filter map[string]any, kind string, field *ontology.Field) (bool, error) {
	op := strings.ToLower(strings.TrimSpace(stringValue(filter["op"])))
	want := strings.TrimSpace(stringValue(filter["value"]))
	if pushdown.IsBuiltinFieldKey(stringValue(filter["field"])) && (op == "" || op == "eq" || op == "in") {
		wants := []string{want}
		if op == "in" {
			wants = stringListValue(filter["values"])
		}
		for _, want := range wants {
			for _, path := range pushdown.PathPredicateVariants(want) {
				if slices.Contains(values, path) {
					return true, nil
				}
			}
		}
		return false, nil
	}
	switch op {
	case "", "eq":
		return rootFilterAnyValueEquals(values, want, field), nil
	case "neq":
		return !rootFilterAnyValueEquals(values, want, field), nil
	case "exists":
		return len(values) > 0, nil
	case "contains":
		return rootFilterAnyValueContains(values, want), nil
	case "in":
		return rootFilterAnyValueIn(values, stringListValue(filter["values"]), field), nil
	case "gt", "gte", "lt", "lte":
		return rootFilterAnyValueCompares(values, want, op, field), nil
	default:
		return false, fmt.Errorf("unsupported %s root filter operator %q", kind, op)
	}
}

func (e *executor) sortEmbeddedRootSections(ctx context.Context, records []*sectionRecord, raw any) {
	updated := rootSortUpdatedAt(ctx, e, raw, records, func(section *sectionRecord) string {
		if section.Note == nil {
			return ""
		}
		return section.Note.Path
	})
	sortRootRecords(records, raw, func(section *sectionRecord, field string) []string {
		if field == recordFieldUpdatedAt && recordFactFree(e.schema, e.schema.Types[section.TypeName], field) {
			return updated(section)
		}
		return e.sectionRootSortFieldValues(section, field)
	}, func(section *sectionRecord, field string) *ontology.Field {
		return rootSchemaField(e.schema.Types[section.TypeName], field)
	}, e.rootSectionStableKey)
}

func (e *executor) sortRootNotes(ctx context.Context, records []*noteRecord, raw any) {
	updated := rootSortUpdatedAt(ctx, e, raw, records, func(note *noteRecord) string { return note.Path })
	sortRootRecords(records, raw, func(note *noteRecord, field string) []string {
		if field == recordFieldUpdatedAt && recordFactFree(e.schema, e.schema.Types[note.TypeName], field) {
			return updated(note)
		}
		values := e.noteRootFieldValues(note, field)
		schemaField := rootSchemaField(e.schema.Types[note.TypeName], field)
		return pushdown.PresentSortValues(schemaField, values, note.hasField(schemaField))
	}, func(note *noteRecord, field string) *ontology.Field {
		return rootSchemaField(e.schema.Types[note.TypeName], field)
	}, rootNoteStableKey)
}

func rootSchemaField(noteType *ontology.NoteType, name string) *ontology.Field {
	_, field, _ := (pushdown.SchemaResolver{NoteType: noteType}).Resolve(name)
	return field
}

// updatedAtSortResolver resolves the updatedAt record fact as a builtin sort
// key, which the store orders by the note's indexed modification time.
type updatedAtSortResolver struct{ pushdown.Resolver }

func (r updatedAtSortResolver) Resolve(key string) (pushdown.FieldKey, *ontology.Field, bool) {
	if strings.TrimSpace(key) == recordFieldUpdatedAt {
		return pushdown.FieldKey{Requested: key, Canonical: recordFieldUpdatedAt, Source: pushdown.FieldKeyBuiltin}, nil, true
	}
	return r.Resolver.Resolve(key)
}

// rootSortUpdatedAt reads the updatedAt record fact of every record in one
// metadata read when the sort names it, as RFC3339 UTC text, which orders
// like the time. A record without indexed metadata has no value.
func rootSortUpdatedAt[T any](ctx context.Context, e *executor, raw any, records []T, notePath func(T) string) func(T) []string {
	none := func(T) []string { return nil }
	sorts, _ := raw.([]any)
	named := slices.ContainsFunc(sorts, func(item any) bool {
		spec, _ := item.(map[string]any)
		return strings.TrimSpace(stringValue(spec["field"])) == recordFieldUpdatedAt
	})
	if !named {
		return none
	}
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, notePath(record))
	}
	loaded := e.loadRecordUpdatedAt(ctx, paths)
	return func(record T) []string {
		if mtime := loaded[notePath(record)]; mtime > 0 {
			return []string{time.Unix(mtime, 0).UTC().Format(time.RFC3339)}
		}
		return nil
	}
}

func sortRootRecords[T any](records []T, raw any, fieldValues func(T, string) []string, schemaField func(T, string) *ontology.Field, stableKey func(T) pushdown.StableKey) {
	sorts, ok := raw.([]any)
	if !ok || len(sorts) == 0 {
		return
	}
	type sortKey struct {
		field string
		desc  bool
	}
	keys := make([]sortKey, 0, len(sorts))
	for _, item := range sorts {
		spec, _ := item.(map[string]any)
		if field := strings.TrimSpace(stringValue(spec["field"])); field != "" {
			keys = append(keys, sortKey{field, strings.EqualFold(strings.TrimSpace(stringValue(spec["direction"])), "desc")})
		}
	}
	type sortableRecord struct {
		record T
		values []pushdown.Scalar
		stable pushdown.StableKey
	}
	prepared := make([]sortableRecord, len(records))
	for i, record := range records {
		values := make([]pushdown.Scalar, len(keys))
		for j, key := range keys {
			if pushdown.IsBuiltinFieldKey(key.field) {
				if paths := fieldValues(record, key.field); len(paths) > 0 {
					values[j] = pushdown.ScalarFromRow(codeanchor.IntelOntologyNodeFieldValue{ValueNorm: paths[0]}, "string")
				}
				continue
			}
			field := schemaField(record, key.field)
			capability, _ := ontology.IndexedFieldCapabilityForField(nil, field)
			values[j] = pushdown.AggregateSortValues(field, fieldValues(record, key.field), capability.ValueKind, key.desc)
		}
		prepared[i] = sortableRecord{record, values, stableKey(record)}
	}
	sort.SliceStable(prepared, func(i, j int) bool {
		for k, key := range keys {
			left, right := prepared[i].values[k], prepared[j].values[k]
			if order := left.CompareSort(right, key.desc, true); order != 0 {
				return order < 0
			}
		}
		return prepared[i].stable.Compare(prepared[j].stable) < 0
	})
	for i, record := range prepared {
		records[i] = record.record
	}
}

func (e *executor) sectionRootSortFieldValues(section *sectionRecord, fieldName string) []string {
	field := rootSchemaField(e.schema.Types[section.TypeName], fieldName)
	if field == nil {
		return e.sectionRootFieldValues(section, fieldName)
	}
	if rows := section.indexedFields[strings.ToLower(strings.TrimSpace(field.Name))]; len(rows) > 0 {
		values := make([]string, len(rows))
		for i, row := range rows {
			values[i] = row.ValueText
		}
		return values
	}
	if section.Projection != nil {
		if binding, ok := section.Projection.Fields[field.Name]; ok {
			return pushdown.PresentSortValues(field, binding.Values, binding.Present)
		}
	}
	return pushdown.PresentSortValues(field, section.fieldValues(field, section.PropertyCase), section.hasFieldValue(field, section.PropertyCase))
}

func (e *executor) sectionRootFieldValues(section *sectionRecord, fieldName string) []string {
	if section == nil {
		return nil
	}
	switch strings.TrimSpace(fieldName) {
	case "title":
		if section.Projection != nil {
			return []string{nodeCatalogTitleFromProjection(section.Projection)}
		}
		if section.Node != nil {
			return []string{section.Node.Title}
		}
	case "notePath", "path":
		if section.Note != nil {
			return []string{section.Note.Path}
		}
	case "ref":
		return []string{e.sectionNodeRef(section).String()}
	}
	noteType := e.schema.Types[section.TypeName]
	if noteType == nil {
		return nil
	}
	field := noteType.ByName[fieldName]
	if field == nil {
		return nil
	}
	return section.fieldValues(field, section.PropertyCase)
}

func (e *executor) noteRootFieldValues(note *noteRecord, fieldName string) []string {
	if note == nil {
		return nil
	}
	switch strings.TrimSpace(fieldName) {
	case "title":
		return []string{note.Title}
	case "notePath", "path":
		return []string{note.Path}
	case "ref":
		return []string{ontology.NodeRef{NotePath: note.Path, TypeName: note.TypeName, Kind: ontology.NodeKindNote}.String()}
	}
	noteType := e.schema.Types[note.TypeName]
	if noteType == nil {
		return nil
	}
	field := noteType.ByName[fieldName]
	if field == nil {
		return nil
	}
	return note.fieldValues(field)
}

func rootFilterAnyValueEquals(values []string, want string, field *ontology.Field) bool {
	return rootFilterAnyValueCompares(values, want, "eq", field)
}

func rootFilterAnyValueContains(values []string, want string) bool {
	want = normalizeRootFilterValue(want)
	for _, value := range values {
		if strings.Contains(normalizeRootFilterValue(value), want) {
			return true
		}
	}
	return false
}

func rootFilterAnyValueIn(values []string, wants []string, field *ontology.Field) bool {
	for _, want := range wants {
		if rootFilterAnyValueEquals(values, want, field) {
			return true
		}
	}
	return false
}

func rootFilterAnyValueCompares(values []string, want string, op string, field *ontology.Field) bool {
	operand := pushdown.FieldValueRow(field, want, normalizeRootFilterValue)
	kind := pushdown.PredicateValueKind(operand)
	right := pushdown.ScalarFromRow(operand, kind)
	for _, value := range values {
		left := pushdown.StoredScalar(field, value, kind)
		if left.Matches(right, op) {
			return true
		}
	}
	return false
}

func normalizeRootFilterValue(value string) string {
	return pushdown.NormalizeScalarText(value)
}

func (e *executor) rootSectionStableKey(section *sectionRecord) pushdown.StableKey {
	if section == nil {
		return pushdown.StableKey{}
	}
	ref := e.sectionNodeRef(section)
	ref.TypeName = firstNonEmpty(ref.TypeName, section.TypeName)
	if ref.NotePath == "" && section.Note != nil {
		ref.NotePath = section.Note.Path
	}
	key := pushdown.StableKey{NotePath: ref.NotePath, StartByte: ref.StartByte, NodeID: ontology.OntologyNodeID(ref)}
	if section.Node != nil {
		key.StartByte = section.Node.StartByte
	}
	return key
}

func rootNoteStableKey(note *noteRecord) pushdown.StableKey {
	if note == nil {
		return pushdown.StableKey{}
	}
	return pushdown.StableKey{NotePath: note.Path}
}
