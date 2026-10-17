package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"slices"
	"strings"
)

// OntologyNodesByPaths returns ontology-node owners for the supplied note paths.
func (s *Store) OntologyNodesByPaths(ctx context.Context, notePaths []string) ([]readmodel.NodeRow, error) {
	notePaths = normalizeNonEmptyStrings(notePaths)
	if len(notePaths) == 0 {
		return nil, nil
	}
	var out []readmodel.NodeRow
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
				parent_type_name, title, source_locator, fragment, block_id, display_label,
				locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint, schema_hash, updated_at
			FROM ontology_nodes
			WHERE note_path IN (%s)
			ORDER BY note_path, node_id
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node readmodel.NodeRow
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, node)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// OntologyNodesBySourceLocators returns ontology-node owners keyed by source locator.
func (s *Store) OntologyNodesBySourceLocators(ctx context.Context, locators []string) (map[string]readmodel.NodeRow, error) {
	locators = normalizeNonEmptyStrings(locators)
	out := make(map[string]readmodel.NodeRow, len(locators))
	if len(locators) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(locators, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT node_id, note_path, node_ref_json, node_kind, COALESCE(type_name, ''),
				COALESCE(parent_node_id, ''), COALESCE(parent_type_name, ''), COALESCE(title, ''),
				COALESCE(source_locator, ''), COALESCE(fragment, ''), COALESCE(block_id, ''),
				COALESCE(display_label, ''), COALESCE(locator_status, ''),
				start_byte, end_byte, COALESCE(structural_fingerprint, ''), COALESCE(relocation_fingerprint, ''), COALESCE(schema_hash, ''), updated_at
			FROM ontology_nodes
			WHERE source_locator IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node readmodel.NodeRow
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			out[node.SourceLocator] = node
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// OntologyNodesByNoteFragments returns ontology-node owners keyed by note_path#fragment.
func (s *Store) OntologyNodesByNoteFragments(ctx context.Context, locators []string) (map[string]readmodel.NodeRow, error) {
	locators = normalizeNonEmptyStrings(locators)
	out := make(map[string]readmodel.NodeRow, len(locators))
	if len(locators) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(locators, 400) {
		parts := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*2)
		for _, locator := range batch {
			notePath, fragment := splitOntologyNodeLocator(locator)
			if notePath == "" || fragment == "" {
				continue
			}
			parts = append(parts, "(note_path = ? AND (fragment = ? OR block_id = ?))")
			args = append(args, notePath, fragment, strings.TrimPrefix(fragment, "^"))
		}
		if len(parts) == 0 {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT node_id, note_path, node_ref_json, node_kind, COALESCE(type_name, ''),
				COALESCE(parent_node_id, ''), COALESCE(parent_type_name, ''), COALESCE(title, ''),
				COALESCE(source_locator, ''), COALESCE(fragment, ''), COALESCE(block_id, ''),
				COALESCE(display_label, ''), COALESCE(locator_status, ''),
				start_byte, end_byte, COALESCE(structural_fingerprint, ''), COALESCE(relocation_fingerprint, ''), COALESCE(schema_hash, ''), updated_at
			FROM ontology_nodes
			WHERE `+strings.Join(parts, " OR "), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node readmodel.NodeRow
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			if node.SourceLocator != "" {
				out[node.SourceLocator] = node
			}
			if node.NotePath != "" && node.Fragment != "" {
				out[node.NotePath+"#"+node.Fragment] = node
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

type ontologyNodeScanner interface {
	Scan(dest ...any) error
}

func scanOntologyNode(rows ontologyNodeScanner, node *readmodel.NodeRow) error {
	return rows.Scan(
		&node.NodeID,
		&node.NotePath,
		&node.NodeRefJSON,
		&node.NodeKind,
		&node.TypeName,
		&node.ParentNodeID,
		&node.ParentTypeName,
		&node.Title,
		&node.SourceLocator,
		&node.Fragment,
		&node.BlockID,
		&node.DisplayLabel,
		&node.LocatorStatus,
		&node.StartByte,
		&node.EndByte,
		&node.StructuralFingerprint,
		&node.RelocationFingerprint,
		&node.SchemaHash,
		&node.UpdatedAt,
	)
}

func splitOntologyNodeLocator(locator string) (string, string) {
	locator = strings.TrimSpace(locator)
	idx := strings.LastIndex(locator, "#")
	if idx <= 0 || idx >= len(locator)-1 {
		return "", ""
	}
	return locator[:idx], locator[idx+1:]
}
