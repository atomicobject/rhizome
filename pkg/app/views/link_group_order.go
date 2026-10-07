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
	for _, source := range append(ontology.FieldSourceNames(field), field.Name) {
		// Catalog-hydrated records lowercase frontmatter keys, as with inline props.
		value, ok := record.Frontmatter[source]
		if !ok {
			value = record.Frontmatter[strings.ToLower(source)]
		}
		if field.SourceKind == ontology.FieldSourceInline {
			if values, ok := lookupInlineProp(record.InlineProps, source); ok && len(values) > 0 {
				value = values[0]
			}
		}
		if rank, ok := numberValue(value); ok && !math.IsNaN(rank) {
			return rank, true
		}
	}
	return 0, false
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
