package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"slices"
	"strings"
)

func deleteStaleOntologyNodesForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string, nodes []readmodel.NodeRow, preserveSemantic bool) error {
	notePaths = normalizeNonEmptyStrings(notePaths)
	if len(notePaths) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if id := strings.TrimSpace(node.NodeID); id != "" {
			keep[id] = struct{}{}
		}
	}
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT node_id FROM ontology_nodes WHERE note_path IN (%s)`,
			holders,
		), args...)
		if err != nil {
			return err
		}
		var staleIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if _, ok := keep[id]; !ok {
				staleIDs = append(staleIDs, id)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(staleIDs) == 0 {
			continue
		}
		idHolders := make([]string, len(staleIDs))
		idArgs := make([]any, len(staleIDs))
		for i, id := range staleIDs {
			idHolders[i] = "?"
			idArgs[i] = id
		}
		if !preserveSemantic {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(
				`DELETE FROM ontology_node_embedding_state
				 WHERE node_id IN (%s)`,
				strings.Join(idHolders, ","),
			), idArgs...); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(
				`DELETE FROM intel_chunks
				 WHERE owner_type = 'ontology_node'
				   AND owner_id IN (%s)`,
				strings.Join(idHolders, ","),
			), idArgs...); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM ontology_nodes WHERE node_id IN (%s)`,
			strings.Join(idHolders, ","),
		), idArgs...); err != nil {
			return err
		}
	}
	return nil
}
