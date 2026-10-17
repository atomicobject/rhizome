package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func insertOntologyNodesTx(ctx context.Context, tx *sql.Tx, nodes []readmodel.NodeRow) error {
	stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO ontology_nodes (
				node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
				parent_type_name, title, source_locator, fragment, block_id, display_label,
				locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint,
				schema_hash, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				note_path = excluded.note_path,
				node_ref_json = excluded.node_ref_json,
				node_kind = excluded.node_kind,
				type_name = excluded.type_name,
				parent_node_id = excluded.parent_node_id,
				parent_type_name = excluded.parent_type_name,
				title = excluded.title,
				source_locator = excluded.source_locator,
				fragment = excluded.fragment,
				block_id = excluded.block_id,
				display_label = excluded.display_label,
				locator_status = excluded.locator_status,
				start_byte = excluded.start_byte,
				end_byte = excluded.end_byte,
				structural_fingerprint = excluded.structural_fingerprint,
				relocation_fingerprint = excluded.relocation_fingerprint,
				schema_hash = excluded.schema_hash,
				updated_at = excluded.updated_at
		`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.NotePath) == "" || strings.TrimSpace(node.NodeRefJSON) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			node.NodeID,
			node.NotePath,
			node.NodeRefJSON,
			node.NodeKind,
			node.TypeName,
			node.ParentNodeID,
			node.ParentTypeName,
			node.Title,
			node.SourceLocator,
			node.Fragment,
			node.BlockID,
			node.DisplayLabel,
			node.LocatorStatus,
			node.StartByte,
			node.EndByte,
			node.StructuralFingerprint,
			node.RelocationFingerprint,
			node.SchemaHash,
			node.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}
