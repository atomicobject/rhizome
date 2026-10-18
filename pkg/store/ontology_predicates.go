package store

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func ontologyNodePredicateSQL(idx int, typeNames []string, predicate readmodel.FieldPredicate) (string, []any, error) {
	fieldName := strings.ToLower(strings.TrimSpace(predicate.FieldName))
	switch fieldName {
	case "notepath", "path":
		return ontologyNodePathPredicateSQL(predicate)
	default:
		return ontologyFieldPredicateSQL(idx, typeNames, predicate)
	}
}

func ontologyNodePathPredicateSQL(predicate readmodel.FieldPredicate) (string, []any, error) {
	op := predicate.Op
	if op == "" {
		op = readmodel.FieldOpEq
	}
	switch op {
	case readmodel.FieldOpExists:
		return "n.note_path != ''", nil, nil
	case readmodel.FieldOpEq:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("notePath predicate requires value")
		}
		return "n.note_path = ?", []any{strings.TrimSpace(predicate.Values[0].ValueNorm)}, nil
	case readmodel.FieldOpIn:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("notePath in predicate requires values")
		}
		holders := strings.TrimSuffix(strings.Repeat("?,", len(predicate.Values)), ",")
		args := make([]any, 0, len(predicate.Values))
		for _, value := range predicate.Values {
			args = append(args, strings.TrimSpace(value.ValueNorm))
		}
		return fmt.Sprintf("n.note_path IN (%s)", holders), args, nil
	default:
		return "", nil, fmt.Errorf("unsupported notePath predicate op %q", op)
	}
}

func ontologyNodeBuiltinFieldSort(fieldName string, sortSpec readmodel.FieldSort, orderParts *[]string) bool {
	dir := "ASC"
	if sortSpec.Desc {
		dir = "DESC"
	}
	switch fieldName {
	case "notepath", "path":
		*orderParts = append(*orderParts, "n.note_path "+dir)
	case "updatedat":
		// The updatedAt record fact: the note's indexed modification time,
		// notes without one last in either direction (notes.path is unique).
		mtime := "(SELECT nm.mtime FROM notes nm WHERE nm.path = n.note_path AND nm.indexed_at > 0 AND nm.mtime > 0)"
		*orderParts = append(*orderParts, mtime+" IS NULL", mtime+" "+dir)
	default:
		return false
	}
	return true
}

func ontologyFieldPredicateSQL(idx int, typeNames []string, predicate readmodel.FieldPredicate) (string, []any, error) {
	fieldName := strings.ToLower(strings.TrimSpace(predicate.FieldName))
	if fieldName == "" {
		return "", nil, fmt.Errorf("ontology field predicate requires field name")
	}
	alias := fmt.Sprintf("pred_f%d", idx)
	conditions := []string{}
	args := []any{}
	if len(typeNames) == 1 {
		conditions = append(conditions, fmt.Sprintf("%s.type_name = ?", alias))
		args = append(args, typeNames[0])
	} else {
		placeholders := make([]string, len(typeNames))
		for i, name := range typeNames {
			placeholders[i] = "?"
			args = append(args, name)
		}
		conditions = append(conditions, fmt.Sprintf("%s.type_name IN (%s)", alias, strings.Join(placeholders, ",")))
	}
	conditions = append(conditions, fmt.Sprintf("%s.field_name = ?", alias))
	args = append(args, fieldName)
	op := predicate.Op
	if op == "" {
		op = readmodel.FieldOpEq
	}
	switch op {
	case readmodel.FieldOpExists:
		// "exists" means "field row present with non-empty normalized value".
		// The value_norm != '' filter lets SQLite use the partial index
		// idx_ontology_node_field_values_norm. Empty-value rows are deliberately
		// excluded so a Person with `assignee:` blank does not match
		// `assignee_exists: true`.
		conditions = append(conditions, fmt.Sprintf("%s.value_norm != ''", alias))
	case readmodel.FieldOpEq:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("eq predicate for %s requires value", fieldName)
		}
		condition, valueArgs := ontologyFieldValuePredicateSQL(alias, "=", predicate.Values[0])
		conditions = append(conditions, condition)
		args = append(args, valueArgs...)
	case readmodel.FieldOpIn:
		values := predicate.Values
		if len(values) == 0 {
			return "", nil, fmt.Errorf("in predicate for %s requires values", fieldName)
		}
		condition, valueArgs := ontologyFieldValueInPredicateSQL(alias, values)
		conditions = append(conditions, condition)
		args = append(args, valueArgs...)
	case readmodel.FieldOpGT, readmodel.FieldOpGTE, readmodel.FieldOpLT, readmodel.FieldOpLTE:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("%s predicate for %s requires value", op, fieldName)
		}
		sqlOp := map[readmodel.FieldOperator]string{
			readmodel.FieldOpGT:  ">",
			readmodel.FieldOpGTE: ">=",
			readmodel.FieldOpLT:  "<",
			readmodel.FieldOpLTE: "<=",
		}[op]
		condition, valueArgs := ontologyFieldValuePredicateSQL(alias, sqlOp, predicate.Values[0])
		conditions = append(conditions, condition)
		args = append(args, valueArgs...)
	default:
		return "", nil, fmt.Errorf("unsupported ontology field predicate op %q", op)
	}
	return fmt.Sprintf("n.node_id IN (SELECT %s.node_id FROM ontology_node_field_values %s WHERE %s)", alias, alias, strings.Join(conditions, " AND ")), args, nil
}
