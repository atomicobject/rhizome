package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

const (
	ownershipReconciliationGenerationMetadataKey   = "ownership_reconciliation_generation"
	ownershipReconciliationAcknowledgedMetadataKey = "ownership_reconciliation_acknowledged_generation"
)

// ownershipAffectedSourcePathsTx identifies source-owned evidence that a
// transition deletes because one of its durable targets changes ownership.
func ownershipAffectedSourcePathsTx(ctx context.Context, tx *sql.Tx, paths []string) ([]string, error) {
	seen := make(map[string]struct{})
	for start := 0; start < len(paths); start += 400 {
		end := start + 400
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, path := range batch {
			args[i] = path
		}
		for _, query := range []string{
			`SELECT source_note_path FROM ontology_node_field_value_dependencies WHERE resolved_target_note_path IN (%s)`,
			`SELECT src_path FROM graph_doc_edges WHERE dst_path IN (%s)`,
			`SELECT src_path FROM doc_links WHERE dst_kind = 'note' AND dst_path IN (%s)`,
			`SELECT dl.src_path
			 FROM doc_links dl
			 JOIN intel_code_anchors a ON a.anchor_id = dl.dst_id
			 WHERE dl.dst_kind = 'anchor' AND a.path IN (%s)`,
		} {
			rows, err := tx.QueryContext(ctx, fmt.Sprintf(query, holders), args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var source string
				if err := rows.Scan(&source); err != nil {
					_ = rows.Close()
					return nil, err
				}
				if strings.TrimSpace(source) != "" {
					seen[source] = struct{}{}
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if err := rows.Close(); err != nil {
				return nil, err
			}
		}
	}
	out := make([]string, 0, len(seen))
	for source := range seen {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}

func ownershipNoteAnchorIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT na.anchor_id
		FROM note_anchors na
		JOIN notes n ON n.id = na.note_id
		WHERE n.path = ?
		ORDER BY na.anchor_id
	`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var anchorIDs []int64
	for rows.Next() {
		var anchorID int64
		if err := rows.Scan(&anchorID); err != nil {
			return nil, err
		}
		anchorIDs = append(anchorIDs, anchorID)
	}
	return anchorIDs, rows.Err()
}

func deleteOrphanedOwnershipAnchorsTx(ctx context.Context, tx *sql.Tx, anchorIDs []int64) error {
	for start := 0; start < len(anchorIDs); start += 400 {
		end := start + 400
		if end > len(anchorIDs) {
			end = len(anchorIDs)
		}
		batch := anchorIDs[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, anchorID := range batch {
			args[i] = anchorID
		}
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.id
			FROM anchors a
			LEFT JOIN note_anchors na ON na.anchor_id = a.id
			WHERE a.id IN (%s)
			GROUP BY a.id
			HAVING COUNT(na.note_id) = 0
		`, holders), args...)
		if err != nil {
			return err
		}
		var orphanIDs []int64
		for rows.Next() {
			var anchorID int64
			if err := rows.Scan(&anchorID); err != nil {
				_ = rows.Close()
				return err
			}
			orphanIDs = append(orphanIDs, anchorID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(orphanIDs) == 0 {
			continue
		}
		orphanHolders := strings.TrimSuffix(strings.Repeat("?,", len(orphanIDs)), ",")
		orphanArgs := make([]any, len(orphanIDs))
		for i, anchorID := range orphanIDs {
			orphanArgs[i] = anchorID
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM anchor_scopes WHERE anchor_id IN (%s)`, orphanHolders), orphanArgs...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM anchors WHERE id IN (%s)`, orphanHolders), orphanArgs...); err != nil {
			return err
		}
	}
	return nil
}

func invalidateOwnershipMetadataStateTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM note_metadata_state`)
	return err
}

// PendingOwnershipReconciliation returns the latest durable ownership
// transition generation and whether it still needs a full reconciliation.
func (s *Store) PendingOwnershipReconciliation(ctx context.Context) (int64, bool, error) {
	if s == nil || s.db == nil {
		return 0, false, fmt.Errorf("ownership transition store is required")
	}
	generation, acknowledged, err := ownershipReconciliationMetadataSnapshot(ctx, s.db)
	if err != nil {
		return 0, false, err
	}
	return generation, generation > acknowledged, nil
}

// AcknowledgeOwnershipReconciliation marks exactly generation as reconciled.
// A newer transition leaves the acknowledgement unchanged and must be handled
// by another full reconciliation.
func (s *Store) AcknowledgeOwnershipReconciliation(ctx context.Context, generation int64) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("ownership transition store is required")
	}
	if generation <= 0 {
		return false, nil
	}
	acknowledged := false
	ctx = indexingperf.WithOp(ctx, "intel.ack_ownership_reconciliation")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		current, err := ownershipReconciliationMetadataGeneration(ctx, tx, ownershipReconciliationGenerationMetadataKey)
		if err != nil {
			return err
		}
		if current != generation {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, ownershipReconciliationAcknowledgedMetadataKey, strconv.FormatInt(generation, 10)); err != nil {
			return err
		}
		acknowledged = true
		return nil
	})
	return acknowledged, err
}

func advanceOwnershipReconciliationGenerationTx(ctx context.Context, tx *sql.Tx) (int64, error) {
	current, err := ownershipReconciliationMetadataGeneration(ctx, tx, ownershipReconciliationGenerationMetadataKey)
	if err != nil {
		return 0, err
	}
	if current == int64(^uint64(0)>>1) {
		return 0, fmt.Errorf("ownership reconciliation generation overflow")
	}
	next := current + 1
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO index_metadata(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, ownershipReconciliationGenerationMetadataKey, strconv.FormatInt(next, 10)); err != nil {
		return 0, err
	}
	return next, nil
}

type ownershipMetadataReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ownershipReconciliationMetadataSnapshot reads both values in one SQLite
// statement. A single statement has one read snapshot, so a committed
// transition cannot appear between the generation and acknowledgement values.
func ownershipReconciliationMetadataSnapshot(ctx context.Context, db *sql.DB) (int64, int64, error) {
	var generationRaw, acknowledgedRaw sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT
			(SELECT value FROM index_metadata WHERE key = ?),
			(SELECT value FROM index_metadata WHERE key = ?)
	`, ownershipReconciliationGenerationMetadataKey, ownershipReconciliationAcknowledgedMetadataKey).Scan(&generationRaw, &acknowledgedRaw)
	if err != nil {
		return 0, 0, err
	}
	generation, err := parseOwnershipReconciliationGeneration(generationRaw)
	if err != nil {
		return 0, 0, err
	}
	acknowledged, err := parseOwnershipReconciliationGeneration(acknowledgedRaw)
	if err != nil {
		return 0, 0, err
	}
	return generation, acknowledged, nil
}

func ownershipReconciliationMetadataGeneration(ctx context.Context, reader ownershipMetadataReader, key string) (int64, error) {
	var raw string
	err := reader.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return parseOwnershipReconciliationGeneration(sql.NullString{String: raw, Valid: true})
}

func parseOwnershipReconciliationGeneration(raw sql.NullString) (int64, error) {
	if !raw.Valid {
		return 0, nil
	}
	generation, err := strconv.ParseInt(strings.TrimSpace(raw.String), 10, 64)
	if err != nil || generation < 0 {
		return 0, fmt.Errorf("invalid ownership reconciliation generation %q", raw.String)
	}
	return generation, nil
}

func sortedUniqueInt64(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func filterTransitionedSourcePaths(sources, transitionedPaths []string) []string {
	transitioned := make(map[string]struct{}, len(transitionedPaths))
	for _, path := range transitionedPaths {
		transitioned[path] = struct{}{}
	}
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		if _, ok := transitioned[source]; !ok {
			out = append(out, source)
		}
	}
	return out
}
