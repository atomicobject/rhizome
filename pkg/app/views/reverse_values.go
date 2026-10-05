package views

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

// reverseValuesPerRow bounds the linked records a row lists per reverse field.
const reverseValuesPerRow = 8

// hydrateReverseRelationValues lists, on each page row, up to eight records
// that link to it through each reverse field, listed by title, so record
// briefs and board cards can name them. Counts stay on the count fields.
// One relation read per field, limited to eight edges per row, hydrates
// their summaries.
func hydrateReverseRelationValues(ctx context.Context, scope *noderead.Scope, schema *ontology.Schema, fields []string, rows []TableRow) ([]TableRow, error) {
	if scope == nil || schema == nil || len(fields) == 0 || len(rows) == 0 {
		return rows, nil
	}
	refs := make([]ontology.NodeRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, rowSourceRef(row))
	}
	out := slices.Clone(rows)
	for _, field := range fields {
		selections := noderead.RelationCountSelectionsForField(schema, refs, field)
		if len(selections) == 0 {
			continue
		}
		for i := range selections {
			selections[i].Hydrate = noderead.HydrateOptions{Profile: noderead.HydrateSummary}
			selections[i].FirstPerSource = reverseValuesPerRow
		}
		result, err := scope.Execute(ctx, noderead.NodeReadPlan{Relations: selections})
		if err != nil {
			return rows, err
		}
		titles := make(map[string]string, len(result.Nodes))
		for _, node := range result.Nodes {
			titles[noderead.RefIdentityKey(node.Ref)] = node.Record.Title
		}
		bySource := map[string][]TableRelationValue{}
		seen := map[string]bool{}
		for _, group := range result.Groups {
			source := noderead.RefIdentityKey(group.Source)
			for _, edge := range group.Edges {
				target := edge.Target
				key := source + "\x00" + noderead.RefIdentityKey(target)
				if seen[key] {
					continue
				}
				seen[key] = true
				bySource[source] = append(bySource[source], TableRelationValue{
					Value: canonicalLinkValue(target),
					Ref:   &target,
					Title: titles[noderead.RefIdentityKey(target)],
				})
			}
		}
		for i := range out {
			values := bySource[noderead.RefIdentityKey(rowSourceRef(out[i]))]
			sort.SliceStable(values, func(a, b int) bool {
				left, right := strings.ToLower(values[a].Title), strings.ToLower(values[b].Title)
				if left != right {
					return left < right
				}
				return values[a].Value < values[b].Value
			})
			if len(values) == 0 {
				continue
			}
			next := make(map[string][]TableRelationValue, len(out[i].RelationValues)+1)
			for key, existing := range out[i].RelationValues {
				next[key] = existing
			}
			next[field] = values[:min(len(values), reverseValuesPerRow)]
			out[i].RelationValues = next
		}
	}
	return out, nil
}

func rowSourceRef(row TableRow) ontology.NodeRef {
	ref := row.Ref
	if ref.TypeName == "" {
		ref.TypeName = row.ResolvedType
	}
	return ref
}

// projectedReverseFields are the profile's reverse fields a request projects.
func projectedReverseFields(profile *ontology.TypeProfile, projected []string) []string {
	if profile == nil {
		return nil
	}
	var out []string
	for _, field := range projected {
		if slices.Contains(profile.ReverseFields, field) {
			out = append(out, field)
		}
	}
	return out
}
