package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strconv"
	"strings"
)

func (s *Scope) sortOverlayMergedItemsLocked(ctx context.Context, items []ontology.NodeListItem, sorts []readmodel.FieldSort) error {
	if len(items) == 0 || len(sorts) == 0 {
		return nil
	}
	refs := make([]ontology.NodeRef, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.Ref)
	}
	records, err := s.hydrateLocked(ctx, refs, HydrateOptions{Profile: HydrateSummary})
	if err != nil {
		return err
	}
	byKey := map[string]NodeRecord{}
	for _, record := range records {
		byKey[RefIdentityKey(record.Ref)] = record
	}
	pushdown.SortItems(items, sorts, func(item ontology.NodeListItem, name string) []readmodel.FieldValueRow {
		record := byKey[RefIdentityKey(item.Ref)]
		return s.scalarFieldRows(record, name, s.recordSchemaField(record, name))
	}, func(item ontology.NodeListItem) pushdown.StableKey {
		record := byKey[RefIdentityKey(item.Ref)]
		ref := item.Ref
		ref.TypeName = firstNonEmptyString(ref.TypeName, item.ResolvedType)
		if record.catalogNodeID != "" {
			return pushdown.StableKey{NotePath: ref.NotePath, StartByte: record.catalogStartByte, NodeID: record.catalogNodeID}
		}
		return pushdown.StableKey{NotePath: ref.NotePath, StartByte: ref.StartByte, NodeID: ontology.OntologyNodeID(ref)}
	})
	return nil
}

func (s *Scope) recordSchemaField(record NodeRecord, name string) *ontoschema.Field {
	if s.service.Schema == nil {
		return nil
	}
	_, field, _ := (pushdown.SchemaResolver{NoteType: s.service.Schema.Types[record.TypeName]}).Resolve(name)
	return field
}

func (s *Scope) scalarFieldRows(record NodeRecord, name string, field *ontoschema.Field) []readmodel.FieldValueRow {
	if pushdown.IsBuiltinFieldKey(name) {
		return []readmodel.FieldValueRow{{ValueText: record.Ref.NotePath, ValueNorm: record.Ref.NotePath}}
	}
	if rows := record.FieldValues[strings.ToLower(strings.TrimSpace(name))]; len(rows) > 0 {
		return rows
	}
	values := s.scalarFieldValues(record, name, field)
	rows := make([]readmodel.FieldValueRow, len(values))
	for i, value := range values {
		rows[i] = ontology.FieldValueRow(field, value)
	}
	return rows
}

func (s *Scope) scalarFieldValues(record NodeRecord, name string, field *ontoschema.Field) []string {
	var values []string
	if field != nil {
		if binding, ok := record.projectedFields[field.Name]; ok {
			values = pushdown.PresentSortValues(field, binding.values, binding.present)
		} else if extracted, present, applicable := s.schemaFieldValues(record, field); applicable {
			values = pushdown.PresentSortValues(field, extracted, present)
		} else {
			values = s.FieldValues(record, name)
		}
	} else {
		values = s.FieldValues(record, name)
	}
	return values
}

// FieldValues returns a record's values for a schema field: indexed or
// overlay-projected values first, otherwise the field's declared source read
// the way the indexer reads it. Callers outside the merged sort use it to show
// the same value the sort and index see.
func (s *Scope) FieldValues(record NodeRecord, field string) []string {
	field = strings.TrimSpace(field)
	if field == "" || len(record.FieldValues[field]) == 0 {
		// Committed note records carry raw metadata rather than indexed field
		// values. Read schema fields the way the indexer does, so a renamed
		// property key or an inline mention in prose cannot reorder the merge.
		if values, _, ok := s.schemaFieldValues(record, s.recordSchemaField(record, field)); ok {
			return values
		}
		return recordFieldValues(record, field)
	}
	rows := record.FieldValues[field]
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, fieldValueRowComparableText(row))
	}
	return values
}

// schemaFieldValues reads a note-level schema field from its declared source
// using its authored property names, matching ontology.extractFieldValues. It
// reports values, authored presence, and whether the rule applies. Embedded
// nodes author fields inside the body; some types use other sources.
func (s *Scope) schemaFieldValues(record NodeRecord, field *ontoschema.Field) ([]string, bool, bool) {
	if s.service.Schema == nil || (record.Ref.Kind != "" && record.Ref.Kind != ontology.NodeKindNote) {
		return nil, false, false
	}
	if field == nil {
		return nil, false, false
	}
	switch field.SourceKind {
	case ontoschema.FieldSourceInline:
		for _, name := range ontoschema.FieldSourceNames(field) {
			if values, present := caseInsensitiveValue(record.InlineProps, name); present {
				return append([]string(nil), values...), true, true
			}
		}
		return nil, false, true
	case ontoschema.FieldSourceFrontmatter, "":
		for _, name := range ontoschema.FieldSourceNames(field) {
			if value, present := caseInsensitiveValue(record.Frontmatter, name); present {
				return ontology.FieldTextValues(field, value), true, true
			}
		}
		return nil, false, true
	default:
		return nil, false, false
	}
}

func caseInsensitiveValue[V any](values map[string]V, key string) (V, bool) {
	if value, ok := values[key]; ok {
		return value, true
	}
	for current, value := range values {
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(key)) {
			return value, true
		}
	}
	var zero V
	return zero, false
}

func recordFieldValues(record NodeRecord, field string) []string {
	field = strings.TrimSpace(field)
	switch field {
	case "", "path", "notePath":
		return []string{record.Ref.NotePath}
	case "title":
		return []string{record.Title}
	case "resolvedType", "type":
		return []string{record.TypeName}
	}
	if rows := record.FieldValues[field]; len(rows) > 0 {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, firstNonEmptyString(row.ValueText, row.TargetSourceLocator, row.TargetNotePath, row.TargetRefJSON))
		}
		return out
	}
	if values := record.InlineProps[field]; len(values) > 0 {
		return append([]string(nil), values...)
	}
	if strings.HasPrefix(field, "frontmatter.") {
		return anyFieldValues(record.Frontmatter[strings.TrimPrefix(field, "frontmatter.")])
	}
	if strings.HasPrefix(field, "inline.") {
		return append([]string(nil), record.InlineProps[strings.TrimPrefix(field, "inline.")]...)
	}
	if values := anyFieldValues(record.Frontmatter[field]); len(values) > 0 {
		return values
	}
	return nil
}

func anyFieldValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case bool:
		if typed {
			return []string{"true"}
		}
		return []string{"false"}
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, anyFieldValues(item)...)
		}
		return out
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func fieldValueRowComparableText(value readmodel.FieldValueRow) string {
	if value.ValueBool != nil {
		return strconv.FormatBool(*value.ValueBool)
	}
	if value.ValueInt != nil {
		return strconv.FormatInt(*value.ValueInt, 10)
	}
	if value.ValueReal != nil {
		return strconv.FormatFloat(*value.ValueReal, 'g', -1, 64)
	}
	if value.ValueDate != nil {
		return *value.ValueDate
	}
	if value.ValueDateTime != nil {
		return *value.ValueDateTime
	}
	return firstNonEmptyString(value.ValueText, value.TargetSourceLocator, value.TargetNotePath, value.TargetRefJSON)
}
