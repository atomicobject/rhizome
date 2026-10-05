package views

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func relationCountCapability(field *ontology.Field) (FieldCapability, bool) {
	if field == nil || (field.Kind != ontology.FieldKindNeighbor && field.Kind != ontology.FieldKindReverse) || !field.List {
		return FieldCapability{}, false
	}
	ops := []string{"eq", "in", "exists", "gt", "gte", "lt", "lte"}
	return FieldCapability{
		Key:               field.Name,
		Label:             labelForField(field.Name),
		ValueKind:         "number",
		CanonicalField:    field.Name,
		SourceKeys:        []string{field.Name},
		SourceKind:        "computed",
		SemanticRole:      "count",
		Importance:        field.Display.EffectiveImportance(),
		Sortable:          true,
		ResidualSortable:  true,
		Groupable:         false,
		FilterOps:         append([]string(nil), ops...),
		ResidualFilterOps: append([]string(nil), ops...),
		Source:            "computed",
		Filter:            &FilterCapability{Ops: append([]string(nil), ops...)},
		cardinalityKnown:  true,
	}, true
}

func (r defaultSourceResolver) projectReferencedRelationCounts(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest, rows []TableRow, capabilities []FieldCapability) ([]TableRow, error) {
	if scope == nil || len(rows) == 0 {
		return rows, nil
	}
	referenced := projectedCountFields(r.opts.Schema, view, req, capabilities)
	for _, fieldName := range referenced {
		refs := make([]ontology.NodeRef, 0, len(rows))
		for _, row := range rows {
			ref := row.Ref
			if ref.TypeName == "" {
				ref.TypeName = row.ResolvedType
			}
			if ref.NotePath != "" {
				refs = append(refs, ref)
			}
		}
		selections := noderead.RelationCountSelectionsForField(r.opts.Schema, refs, fieldName)
		if len(selections) == 0 {
			continue
		}
		applicable := make([]ontology.NodeRef, 0)
		for _, selection := range selections {
			applicable = append(applicable, selection.Sources...)
		}
		result, err := scope.RelationCounts(ctx, noderead.RelationCountsRequest{Sources: applicable, Selections: selections})
		if err != nil {
			return nil, err
		}
		for i := range rows {
			ref := rows[i].Ref
			if ref.TypeName == "" {
				ref.TypeName = rows[i].ResolvedType
			}
			count, ok := result.BySource[noderead.RefIdentityKey(ref)]
			if !ok {
				continue
			}
			if rows[i].Fields == nil {
				rows[i].Fields = map[string]any{}
			}
			rows[i].Fields[fieldName] = count.Count
		}
	}
	return rows, nil
}

// projectedCountFields lists the count fields a request reads: those its
// filters, sort, and layout reference, plus, on board and card layouts, the
// source profile's reverse fields, which SPEC-0112 cards show as counts.
func projectedCountFields(schema *ontology.Schema, view viewconfig.ViewDefinition, req ExecuteRequest, capabilities []FieldCapability) []string {
	referenced := referencedRelationCountFields(view, req, capabilities)
	if req.Variant != "kanban" && req.Variant != "card" {
		return referenced
	}
	if profile, ok := sourceProfile(schema, view); ok {
		for _, field := range profile.ReverseFields {
			if !slices.Contains(referenced, field) && capabilityField(capabilities, field) == field {
				referenced = append(referenced, field)
			}
		}
	}
	return referenced
}

func referencedRelationCountFields(view viewconfig.ViewDefinition, req ExecuteRequest, capabilities []FieldCapability) []string {
	countFields := map[string]struct{}{}
	for _, capability := range capabilities {
		if capability.SemanticRole == "count" {
			countFields[capability.Key] = struct{}{}
		}
	}
	referenced := map[string]struct{}{}
	add := func(field string) {
		field = strings.TrimSpace(field)
		if _, ok := countFields[field]; ok {
			referenced[field] = struct{}{}
		}
	}
	for _, filter := range req.Filters {
		add(filter.Field)
	}
	for _, sortSpec := range req.Sort {
		add(sortSpec.Field)
	}
	switch req.Variant {
	case "card", "kanban":
		layout := resolveCardLayout(view, req.Variant, capabilities)
		add(layout.Title.Field)
		if layout.Eyebrow != nil {
			add(layout.Eyebrow.Field)
		}
		if layout.Preview != nil {
			add(layout.Preview.Field)
		}
		for _, field := range layout.Fields {
			add(field.Field)
		}
	default:
		for _, column := range tableColumns(view, capabilities) {
			add(column.Field)
		}
	}
	out := make([]string, 0, len(referenced))
	for field := range referenced {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}
