package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"slices"
	"strings"
	"time"
)

func (e *executor) sectionMatchesRootFilters(section *sectionRecord, raw any) (bool, error) {
	return matchesRootFilters(raw, "embedded", e.schema.Types[section.TypeName], func(field string) []string {
		return e.sectionRootFieldValues(section, field)
	})
}

func (e *executor) noteMatchesRootFilters(note *noteRecord, raw any, linkTargets map[string]map[string][]string) (bool, error) {
	return matchesRootFilters(raw, "note", e.schema.Types[note.TypeName], func(field string) []string {
		if noteType := e.schema.Types[note.TypeName]; noteType != nil {
			if schemaField := noteType.ByName[field]; schemaField != nil && schemaField.Kind == ontoschema.FieldKindLink {
				return linkTargets[note.Path][field]
			}
		}
		return e.noteRootFieldValues(note, field)
	})
}

func matchesRootFilters(raw any, kind string, noteType *ontoschema.NoteType, fieldValues func(string) []string) (bool, error) {
	for _, filter := range rootFilterInputs(raw) {
		field := strings.TrimSpace(filter.Field)
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

func rootFilterMatches(values []string, filter pushdown.FilterInput, kind string, field *ontoschema.Field) (bool, error) {
	op := strings.ToLower(strings.TrimSpace(filter.Op))
	if op == "" {
		op = "eq"
	}
	switch op {
	case "eq", "neq", "contains", "in", "exists", "gt", "gte", "lt", "lte":
	default:
		return false, fmt.Errorf("unsupported %s root filter operator %q", kind, op)
	}
	predicate := readmodel.FieldPredicate{FieldName: filter.Field, Op: readmodel.FieldOperator(op)}
	wants := []string{filter.Value}
	if op == "in" {
		wants = filter.Values
	}
	rows := make([]readmodel.FieldValueRow, 0, len(values))
	pathMembership := pushdown.IsBuiltinFieldKey(predicate.FieldName) && (op == "eq" || op == "in")
	for _, value := range values {
		row := ontology.FieldValueRow(field, value)
		if pathMembership {
			row.ValueNorm = value
		}
		rows = append(rows, row)
	}
	for _, want := range wants {
		if pathMembership {
			for _, path := range pushdown.PathPredicateVariants(want) {
				predicate.Values = append(predicate.Values, readmodel.FieldValueRow{ValueNorm: path})
			}
		} else {
			predicate.Values = append(predicate.Values, ontology.FieldValueRow(field, want))
		}
	}
	return pushdown.Match(predicate, rows), nil
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
	}, func(section *sectionRecord, field string) *ontoschema.Field {
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
	}, func(note *noteRecord, field string) *ontoschema.Field {
		return rootSchemaField(e.schema.Types[note.TypeName], field)
	}, rootNoteStableKey)
}

func rootSchemaField(noteType *ontoschema.NoteType, name string) *ontoschema.Field {
	_, field, _ := (pushdown.SchemaResolver{NoteType: noteType}).Resolve(name)
	return field
}

// updatedAtSortResolver resolves the updatedAt record fact as a builtin sort
// key, which the store orders by the note's indexed modification time.
type updatedAtSortResolver struct{ pushdown.Resolver }

func (r updatedAtSortResolver) Resolve(key string) (pushdown.FieldKey, *ontoschema.Field, bool) {
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
	sorts := rootSortInputs(raw)
	named := slices.ContainsFunc(sorts, func(spec pushdown.SortInput) bool {
		return strings.TrimSpace(spec.Field) == recordFieldUpdatedAt
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

func sortRootRecords[T any](records []T, raw any, fieldValues func(T, string) []string, schemaField func(T, string) *ontoschema.Field, stableKey func(T) pushdown.StableKey) {
	items := rootSortInputs(raw)
	sorts := make([]readmodel.FieldSort, 0, len(items))
	for _, spec := range items {
		name := strings.TrimSpace(spec.Field)
		if name == "" {
			continue
		}
		kind := "string"
		if len(records) > 0 {
			capability, _ := ontoschema.FieldQueryCapabilityForField(nil, schemaField(records[0], name))
			if capability.ValueKind != "" {
				kind = capability.ValueKind
			}
		}
		sorts = append(sorts, readmodel.FieldSort{FieldName: name, ValueKind: kind, Desc: strings.EqualFold(spec.Direction, "desc"), NullsLast: true})
	}
	pushdown.SortItems(records, sorts, func(record T, name string) []readmodel.FieldValueRow {
		values := fieldValues(record, name)
		rows := make([]readmodel.FieldValueRow, len(values))
		for i, value := range values {
			rows[i] = ontology.FieldValueRow(schemaField(record, name), value)
			if pushdown.IsBuiltinFieldKey(name) {
				rows[i].ValueNorm = value
			}
		}
		return rows
	}, stableKey)
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
