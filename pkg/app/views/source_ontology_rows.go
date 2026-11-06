package views

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func rowFromNodeListItem(item ontology.NodeListItem) TableRow {
	return TableRow{Ref: item.Ref, Path: item.NotePath, Title: item.Title, ResolvedType: item.ResolvedType, UpdatedAt: item.UpdatedAt, HasIssues: item.HasIssues, Fields: map[string]any{"title": item.Title, "path": item.NotePath, "resolvedType": item.ResolvedType, "updatedAt": item.UpdatedAt, "hasIssues": item.HasIssues}}
}
func (r defaultSourceResolver) enrichRowFromRecord(row TableRow, record noderead.NodeRecord) TableRow {
	row.Ref = record.Ref
	row.Path = firstNonEmpty(record.Path, row.Path)
	row.Title = firstNonEmpty(record.Title, row.Title)
	row.ResolvedType = firstNonEmpty(record.TypeName, row.ResolvedType)
	if record.UpdatedAt != 0 {
		row.UpdatedAt = record.UpdatedAt
	}
	row.HasIssues = record.HasIssues
	row.Tags = append([]string(nil), record.Tags...)
	if row.Fields == nil {
		row.Fields = map[string]any{}
	}
	row.Fields["frontmatter"] = cloneMap(record.Frontmatter)
	row.Fields["inline"] = cloneInline(record.InlineProps)
	if r.opts.Schema != nil {
		if noteType := r.opts.Schema.Types[row.ResolvedType]; noteType != nil {
			addSemanticFields(row.Fields, noteType, record)
		}
	}
	return row
}
func addSemanticFields(fields map[string]any, noteType *ontoschema.NoteType, record noderead.NodeRecord) {
	for _, field := range noteType.Fields {
		if field == nil || field.Name == "" {
			continue
		}
		if ontoschema.IsSectionSummary(field) {
			if values, ok := lookupInlineProp(record.InlineProps, field.Name); ok && len(values) > 0 {
				fields[field.Name] = strings.Join(values, " ")
			}
			continue
		}
		if field.SourcePath != "" {
			if value, ok := nestedRecordValue(record, field); ok {
				fields[field.Name] = value
			}
			continue
		}
		switch field.SourceKind {
		case ontoschema.FieldSourceFrontmatter:
			source := firstNonEmpty(field.Source, field.Name)
			if value, ok := record.Frontmatter[source]; ok {
				fields[field.Name] = value
			}
		case ontoschema.FieldSourceInline:
			source := firstNonEmpty(field.Source, field.Name)
			if values, ok := lookupInlineProp(record.InlineProps, source); ok {
				fields[field.Name] = append([]string(nil), values...)
			}
		case ontoschema.FieldSourceCheckbox:
			if values, ok := lookupInlineProp(record.InlineProps, field.Name); ok && len(values) > 0 {
				fields[field.Name] = values[0]
			}
		}
	}
}

// lookupInlineProp does a case-insensitive lookup against record.InlineProps.
// Catalog-hydrated records store field_name lowercased (so query-side filters
// can match without case games), while projection-hydrated records preserve
// the schema's original casing. Schema field names are commonly camelCase
// (assignedTo, displayName), so a case-sensitive lookup against the catalog
// path would silently miss.
func lookupInlineProp(props map[string][]string, key string) ([]string, bool) {
	if values, ok := props[key]; ok {
		return values, true
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower != key {
		if values, ok := props[lower]; ok {
			return values, true
		}
	}
	return nil, false
}
func (r defaultSourceResolver) nodeReadScope() *noderead.Scope {
	return noderead.NewService(r.opts.VaultDef, r.opts.NoteReader, r.opts.Store, r.opts.Schema).NewScope(noderead.ScopeOptions{ReadOverlay: r.opts.ReadOverlay})
}
func (r defaultSourceResolver) hydrateRowsWithScope(ctx context.Context, scope *noderead.Scope, rows []TableRow) ([]TableRow, []diag.Warning) {
	if r.opts.Store == nil || len(rows) == 0 {
		return rows, nil
	}
	refs := make([]ontology.NodeRef, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Ref.NotePath) == "" {
			continue
		}
		refs = append(refs, row.Ref)
	}
	if len(refs) == 0 {
		return rows, nil
	}
	// HydrateSummary serves view rows directly from the indexed catalog
	// (ontology_nodes + ontology_node_field_values). HydrateContent here
	// would force a per-host projection fallback that re-parses every note,
	// turning a ~50ms catalog read into a ~600ms snapshot scan for views
	// like action-items that span many notes.
	records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return rows, []diag.Warning{{Code: "view_source_hydration_failed", Message: err.Error()}}
	}
	recordByKey := map[string]noderead.NodeRecord{}
	relationValuesByKey := map[string]map[string][]TableRelationValue{}
	for _, record := range records {
		recordByKey[nodeRefKey(record.Ref)] = record
		if values := relationValuesForRecord(r.opts.Schema, record); len(values) > 0 {
			relationValuesByKey[noderead.RefIdentityKey(record.Ref)] = values
		}
	}
	overlayRelationValues, relationErr := resolveRawRelationValues(ctx, scope, r.opts.Schema, records)
	warnings := make([]diag.Warning, 0, 1)
	if relationErr != nil {
		warnings = append(warnings, diag.Warning{Code: "view_relation_resolution_failed", Message: relationErr.Error()})
	}
	for key, values := range overlayRelationValues {
		relationValuesByKey[key] = values
	}
	out := make([]TableRow, 0, len(rows))
	for _, row := range rows {
		if record, ok := recordByKey[nodeRefKey(row.Ref)]; ok {
			row = r.enrichRowFromRecord(row, record)
			row.RelationValues = relationValuesByKey[noderead.RefIdentityKey(record.Ref)]
		}
		out = append(out, row)
	}
	return out, warnings
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
func cloneInline(in map[string][]string) map[string]any {
	out := make(map[string]any, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func nestedRecordValue(record noderead.NodeRecord, field *ontoschema.Field) (any, bool) {
	if rows := record.FieldValues[strings.ToLower(field.Name)]; len(rows) > 0 {
		values := make([]string, len(rows))
		for i, row := range rows {
			values[i] = row.ValueText
		}
		if field.List {
			return values, true
		}
		return values[0], true
	}
	return ontoschema.FrontmatterPathValue(record.Frontmatter, field.SourcePath)
}
