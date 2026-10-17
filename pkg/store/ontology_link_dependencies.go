package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"slices"
	"sort"
	"strings"
)

func insertOntologyNodeLinkDependenciesTx(ctx context.Context, tx *sql.Tx, dependencies []readmodel.LinkDependencyRow) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO ontology_node_field_value_dependencies (
			source_note_path, node_id, type_name, field_name, target_input, target_input_norm,
			resolved_target_note_path, resolved_target_type_name, schema_hash, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_note_path, node_id, field_name, target_input_norm) DO UPDATE SET
			type_name = excluded.type_name,
			target_input = excluded.target_input,
			resolved_target_note_path = excluded.resolved_target_note_path,
			resolved_target_type_name = excluded.resolved_target_type_name,
			schema_hash = excluded.schema_hash,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range dependencies {
		if strings.TrimSpace(row.SourceNotePath) == "" || strings.TrimSpace(row.FieldName) == "" || strings.TrimSpace(row.TargetInputNorm) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			row.SourceNotePath,
			row.NodeID,
			row.TypeName,
			row.FieldName,
			row.TargetInput,
			row.TargetInputNorm,
			row.ResolvedTargetNotePath,
			row.ResolvedTargetTypeName,
			row.SchemaHash,
			row.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	if *v {
		return 1
	}
	return 0
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableFloat64(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func deleteOntologyNodeFieldValuesForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string) error {
	notePaths = normalizeNonEmptyStrings(notePaths)
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM ontology_node_field_values WHERE note_path IN (%s)`,
			holders,
		), args...); err != nil {
			return err
		}
	}
	return nil
}

func deleteOntologyNodeLinkDependenciesForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string) error {
	notePaths = normalizeNonEmptyStrings(notePaths)
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM ontology_node_field_value_dependencies WHERE source_note_path IN (%s)`,
			holders,
		), args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) OntologyNodeLinkDependencySources(ctx context.Context, resolvedTargetNotePaths, targetInputNorms []string) ([]string, error) {
	return ontologyNodeLinkDependencySources(ctx, s.db, resolvedTargetNotePaths, targetInputNorms)
}

func ontologyNodeLinkDependencySources(ctx context.Context, queryer noteMetadataQueryer, resolvedTargetNotePaths, targetInputNorms []string) ([]string, error) {
	resolvedTargetNotePaths = normalizeNonEmptyStrings(resolvedTargetNotePaths)
	targetInputNorms = normalizeNonEmptyStrings(targetInputNorms)
	if len(resolvedTargetNotePaths) == 0 && len(targetInputNorms) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	querySources := func(column string, values []string) error {
		for batch := range slices.Chunk(values, 400) {
			holders, args := placeholders(batch)
			rows, err := queryer.QueryContext(ctx, fmt.Sprintf(
				`SELECT DISTINCT source_note_path FROM ontology_node_field_value_dependencies WHERE %s IN (%s)`,
				column,
				holders,
			), args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var source string
				if err := rows.Scan(&source); err != nil {
					rows.Close()
					return err
				}
				if strings.TrimSpace(source) != "" {
					seen[source] = struct{}{}
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
		}
		return nil
	}
	if err := querySources("resolved_target_note_path", resolvedTargetNotePaths); err != nil {
		return nil, err
	}
	if err := querySources("target_input_norm", targetInputNorms); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for source := range seen {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}
