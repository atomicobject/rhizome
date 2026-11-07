package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func summarySnapshotReady(ctx context.Context, tx *sql.Tx, version int) (bool, error) {
	state, err := currentNoteMetadataStateTx(ctx, tx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return false, err
	}
	if version == 0 {
		return true, nil
	}
	var ready bool
	err = tx.QueryRowContext(ctx, `SELECT ready AND materialization_version = ? FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1`, version).Scan(&ready)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return ready, err
}

// VisitOntologySummaryRows streams one read snapshot in the same encounter order as type listings.
// The visitor must not issue store reads while the transaction is open.
func (s *Store) VisitOntologySummaryRows(ctx context.Context, names []string, version int, visit func(readmodel.SummaryRow) error) (bool, error) {
	names = normalizeNonEmptyStrings(names)
	sort.Strings(names)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	ready, err := summarySnapshotReady(ctx, tx, version)
	if err != nil || !ready {
		return false, err
	}
	for batch := range slices.Chunk(names, 400) {
		holders, args := placeholders(batch)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.node_id, c.note_path, c.node_kind, c.type_name, c.node_ref_json,
		c.fragment, c.structural_fingerprint, c.display_label, c.title, c.source_locator, c.updated_at, c.record_identity_json,
		COALESCE(m.title,''), COALESCE(m.mtime,0), COALESCE(a.has_issues,0)
		FROM ontology_nodes c
		LEFT JOIN notes m ON m.path=c.note_path AND m.indexed_at>0
		LEFT JOIN ontology_note_assessments a ON a.note_path=c.note_path
		WHERE c.type_name IN (%s) AND c.node_kind IN ('NOTE','EMBEDDED')
		ORDER BY c.type_name, c.note_path, c.node_kind, c.node_id`, holders), args...)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var row readmodel.SummaryRow
			n := &row.Node
			err = rows.Scan(&n.NodeID, &n.NotePath, &n.NodeKind, &n.TypeName, &n.NodeRefJSON,
				&n.Fragment, &n.StructuralFingerprint, &n.DisplayLabel, &n.Title, &n.SourceLocator, &n.UpdatedAt, &n.RecordJSON,
				&row.MetadataTitle, &row.MetadataMtime, &row.HasIssues)
			if err == nil {
				err = visit(row)
			}
			if err != nil {
				rows.Close()
				return false, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// OntologySummaryTotals counts current notes without hydrating a note inventory.
// Version zero requests metadata-only totals for a vault without a schema.
func (s *Store) OntologySummaryTotals(ctx context.Context, version int) (readmodel.SummaryTotals, bool, error) {
	var out readmodel.SummaryTotals
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, false, err
	}
	defer tx.Rollback()
	ready, err := summarySnapshotReady(ctx, tx, version)
	if err != nil || !ready {
		return out, false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),
	COALESCE(SUM(EXISTS(SELECT 1 FROM ontology_note_types t WHERE t.note_path=n.path)),0),
	COALESCE(SUM(COALESCE(a.has_issues,0)),0), COALESCE(SUM(COALESCE(a.type_ambiguous,0)),0)
	FROM notes n LEFT JOIN ontology_note_assessments a ON a.note_path=n.path WHERE n.indexed_at>0`).Scan(
		&out.Notes, &out.Typed, &out.Issues, &out.Ambiguous)
	if err != nil {
		return out, false, err
	}
	return out, true, tx.Commit()
}
