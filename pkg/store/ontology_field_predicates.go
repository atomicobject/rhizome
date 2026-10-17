package store

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
)

func ontologyFieldValueInPredicateSQL(alias string, values []readmodel.FieldValueRow) (string, []any) {
	parts := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		condition, valueArgs := ontologyFieldValuePredicateSQL(alias, "=", value)
		parts = append(parts, "("+condition+")")
		args = append(args, valueArgs...)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func ontologyFieldValuePredicateSQL(alias, op string, value readmodel.FieldValueRow) (string, []any) {
	if value.ValueBool != nil {
		return fmt.Sprintf("%s.value_bool %s ?", alias, op), []any{nullableBool(value.ValueBool)}
	}
	if value.ValueInt != nil {
		return fmt.Sprintf("%s.value_int %s ?", alias, op), []any{*value.ValueInt}
	}
	if value.ValueReal != nil {
		return fmt.Sprintf("%s.value_real %s ?", alias, op), []any{*value.ValueReal}
	}
	if value.ValueDate != nil {
		return fmt.Sprintf("%s.value_date %s ?", alias, op), []any{*value.ValueDate}
	}
	if value.ValueDateTime != nil {
		return fmt.Sprintf("%s.value_datetime %s ?", alias, op), []any{*value.ValueDateTime}
	}
	if strings.TrimSpace(value.TargetNodeID) != "" {
		// Explicit target_node_id != '' lets the partial index
		// idx_ontology_node_field_values_target serve the lookup.
		return fmt.Sprintf("%s.target_node_id != '' AND %s.target_node_id %s ?", alias, alias, op), []any{strings.TrimSpace(value.TargetNodeID)}
	}
	if strings.TrimSpace(value.TargetNotePath) != "" {
		targets := ontologyTargetNotePathVariants(value.TargetNotePath)
		norm := strings.TrimSpace(value.ValueNorm)
		var normCondition string
		var normArgs []any
		if op == "=" && norm != "" {
			normCondition = fmt.Sprintf("%s.value_norm != '' AND %s.value_norm = ?", alias, alias)
			normArgs = []any{norm}
		}
		// Explicit target_note_path != '' lets the partial index
		// idx_ontology_node_field_values_target_note serve the lookup.
		var targetCondition string
		var targetArgs []any
		if op == "=" && len(targets) > 1 {
			holders := strings.TrimSuffix(strings.Repeat("?,", len(targets)), ",")
			args := make([]any, 0, len(targets))
			for _, target := range targets {
				args = append(args, target)
			}
			targetCondition = fmt.Sprintf("%s.target_note_path != '' AND %s.target_note_path IN (%s)", alias, alias, holders)
			targetArgs = args
		} else {
			targetCondition = fmt.Sprintf("%s.target_note_path != '' AND %s.target_note_path %s ?", alias, alias, op)
			targetArgs = []any{targets[0]}
		}
		if normCondition != "" {
			args := append(targetArgs, normArgs...)
			return fmt.Sprintf("((%s) OR (%s))", targetCondition, normCondition), args
		}
		return targetCondition, targetArgs
	}
	if strings.TrimSpace(value.ValueNorm) != "" {
		return fmt.Sprintf("%s.value_norm != '' AND %s.value_norm %s ?", alias, alias, op), []any{strings.TrimSpace(value.ValueNorm)}
	}
	return fmt.Sprintf("%s.value_norm %s ?", alias, op), []any{strings.TrimSpace(value.ValueNorm)}
}

func ontologyTargetNotePathVariants(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return []string{""}
	}
	normalized := string(paths.Normalize(trimmed))
	withSuffix := string(paths.EnsureNoteExtension(trimmed, ".md"))
	if normalized == withSuffix {
		return []string{normalized}
	}
	return []string{normalized, withSuffix}
}

func ontologyFieldValueColumn(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bool", "boolean":
		return "value_bool"
	case "int", "integer":
		return "value_int"
	case "float", "real", "number":
		return "value_real"
	case "date":
		return "value_date"
	case "datetime":
		return "value_datetime"
	default:
		return "value_norm"
	}
}
