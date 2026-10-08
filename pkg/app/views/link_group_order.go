package views

import (
	"context"
	"math"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

// hydrateLinkRanks sets the RANK field value of every link target in the
// given link fields on rows, so link groups and relation lanes list targets as
// their type ranks them.
func hydrateLinkRanks(ctx context.Context, scope *noderead.Scope, schema *ontology.Schema, rows []TableRow, capabilities []FieldCapability, fields []string) ([]TableRow, error) {
	keys := linkFieldKeys(capabilities, fields)
	if len(keys) == 0 || scope == nil || schema == nil {
		return rows, nil
	}
	seen := map[string]bool{}
	var refs []ontology.NodeRef
	for _, row := range rows {
		for key := range keys {
			for _, value := range row.RelationValues[key] {
				if value.Ref != nil && !seen[noderead.RefIdentityKey(*value.Ref)] {
					seen[noderead.RefIdentityKey(*value.Ref)] = true
					refs = append(refs, *value.Ref)
				}
			}
		}
	}
	if len(refs) == 0 {
		return rows, nil
	}
	records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return rows, err
	}
	ranks := map[string]float64{}
	for _, record := range records {
		if rank, ok := recordRank(schema, record); ok {
			ranks[noderead.RefIdentityKey(record.Ref)] = rank
		}
	}
	if len(ranks) == 0 {
		return rows, nil
	}
	out := make([]TableRow, len(rows))
	for i, row := range rows {
		out[i] = row
		next := make(map[string][]TableRelationValue, len(row.RelationValues))
		for key, values := range row.RelationValues {
			if keys[key] {
				values = withRelationRanks(values, ranks)
			}
			next[key] = values
		}
		out[i].RelationValues = next
	}
	return out, nil
}

// withRelationRanks copies values, setting their ranks.
func withRelationRanks(values []TableRelationValue, ranks map[string]float64) []TableRelationValue {
	values = append([]TableRelationValue(nil), values...)
	for i := range values {
		if values[i].Ref != nil {
			values[i].rank, values[i].ranked = ranks[noderead.RefIdentityKey(*values[i].Ref)]
		}
	}
	return values
}

// recordRank reads a record's RANK field value, when its type declares one
// and the record holds a number there.
func recordRank(schema *ontology.Schema, record noderead.NodeRecord) (float64, bool) {
	noteType := schema.Types[record.TypeName]
	if noteType == nil {
		return 0, false
	}
	field := ontology.RankField(noteType.Fields)
	if field == nil {
		return 0, false
	}
	// The first authored source decides, as in indexed field extraction.
	for _, source := range append(ontology.FieldSourceNames(field), field.Name) {
		value, ok := rankSourceValue(record, field.SourceKind, source)
		if !ok {
			continue
		}
		rank, ok := numberValue(value)
		return rank, ok && !math.IsNaN(rank)
	}
	return 0, false
}

// rankSourceValue reads one source of a RANK field from the place its
// source kind names.
func rankSourceValue(record noderead.NodeRecord, kind ontology.FieldSource, source string) (any, bool) {
	if kind == ontology.FieldSourceInline {
		values, ok := lookupInlineProp(record.InlineProps, source)
		if !ok || len(values) == 0 {
			return nil, false
		}
		return values[0], true
	}
	// Catalog-hydrated records lowercase frontmatter keys, as with inline props.
	if value, ok := record.Frontmatter[source]; ok {
		return value, true
	}
	value, ok := record.Frontmatter[strings.ToLower(source)]
	return value, ok
}

// linkFieldKeys lists every key the given link fields' values may sit under.
func linkFieldKeys(capabilities []FieldCapability, fields []string) map[string]bool {
	caps := capabilityMap(capabilities)
	keys := map[string]bool{}
	for _, field := range fields {
		cap, ok := caps[field]
		if !ok || normalizedValueKind(cap.ValueKind) != "relation" {
			continue
		}
		for _, key := range append([]string{field, cap.Key, cap.CanonicalField}, cap.SourceKeys...) {
			if key = strings.TrimSpace(key); key != "" {
				keys[key] = true
			}
		}
	}
	return keys
}
