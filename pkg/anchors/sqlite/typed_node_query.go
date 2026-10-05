package sqlite

import (
	"context"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func (s *Store) OntologyNodesByTypePlan(ctx context.Context, plan codeanchor.OntologyNodeQueryPlan) ([]codeanchor.IntelOntologyNode, error) {
	query, queryArgs, err := ontologyNodesByTypePlanSQL(plan)
	if err != nil {
		return nil, err
	}
	if query == "" {
		return []codeanchor.IntelOntologyNode{}, nil
	}
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelOntologyNode
	for rows.Next() {
		var node codeanchor.IntelOntologyNode
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func ontologyNodesByTypePlanSQL(plan codeanchor.OntologyNodeQueryPlan) (string, []any, error) {
	typeNames := make([]string, 0, len(plan.TypeNames))
	seen := map[string]struct{}{}
	for _, name := range plan.TypeNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		typeNames = append(typeNames, name)
	}
	if len(typeNames) == 0 {
		return "", nil, nil
	}
	conditions := []string{}
	args := []any{}
	if len(typeNames) == 1 {
		conditions = append(conditions, "n.type_name = ?")
		args = append(args, typeNames[0])
	} else {
		placeholders := make([]string, len(typeNames))
		for i, name := range typeNames {
			placeholders[i] = "?"
			args = append(args, name)
		}
		conditions = append(conditions, fmt.Sprintf("n.type_name IN (%s)", strings.Join(placeholders, ",")))
	}
	for i, predicate := range plan.Predicates {
		existsSQL, existsArgs, err := ontologyNodePredicateSQL(i, typeNames, predicate)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, existsSQL)
		args = append(args, existsArgs...)
	}
	joins := []string{}
	joinArgs := []any{}
	orderParts := []string{}
	for i, sortSpec := range plan.Sort {
		fieldName := strings.ToLower(strings.TrimSpace(sortSpec.FieldName))
		if fieldName == "" {
			continue
		}
		if ontologyNodeBuiltinFieldSort(fieldName, sortSpec, &orderParts) {
			continue
		}
		alias := fmt.Sprintf("sort_f%d", i)
		column := ontologyFieldValueColumn(sortSpec.ValueKind)
		aggregate := "MIN"
		if sortSpec.Desc {
			aggregate = "MAX"
		}
		joins = append(joins, fmt.Sprintf(`LEFT JOIN (
			SELECT node_id, %s(%s) AS sort_value
			FROM ontology_node_field_values
			WHERE field_name = ? AND node_id IN (
				SELECT node_id FROM ontology_nodes WHERE type_name IN (%s)
			)
			GROUP BY node_id
		) %s ON %s.node_id = n.node_id`, aggregate, column, strings.TrimSuffix(strings.Repeat("?,", len(typeNames)), ","), alias, alias))
		joinArgs = append(joinArgs, fieldName)
		joinArgs = append(joinArgs, sliceAny(typeNames)...)
		if sortSpec.NullsLast {
			orderParts = append(orderParts, fmt.Sprintf("%s.sort_value IS NULL", alias))
		}
		dir := "ASC"
		if sortSpec.Desc {
			dir = "DESC"
		}
		orderParts = append(orderParts, fmt.Sprintf("%s.sort_value %s", alias, dir))
	}
	orderParts = append(orderParts, "n.note_path ASC", "n.start_byte ASC", "n.node_id ASC")
	if plan.Limit <= 0 {
		return "", nil, fmt.Errorf("ontology node query plan requires positive Limit (got %d)", plan.Limit)
	}
	limit := plan.Limit
	offset := plan.Offset
	if offset < 0 {
		offset = 0
	}
	queryArgs := append([]any{}, joinArgs...)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, limit, offset)
	query := fmt.Sprintf(`
		SELECT n.node_id, n.note_path, n.node_ref_json, n.node_kind, n.type_name,
			n.parent_node_id, n.parent_type_name, n.title, n.source_locator,
			n.fragment, n.block_id, n.display_label, n.locator_status,
			n.start_byte, n.end_byte, n.structural_fingerprint, n.schema_hash, n.updated_at
		FROM ontology_nodes n
		%s
		WHERE %s
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, strings.Join(joins, "\n"), strings.Join(conditions, " AND "), strings.Join(orderParts, ", "))
	return query, queryArgs, nil
}
