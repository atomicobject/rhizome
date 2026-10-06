package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// RecentOntologyNotes caps and orders notes in SQL before callers enrich them.
func (s *Store) RecentOntologyNotes(ctx context.Context, limit, materializationVersion int) (readmodel.RecentNotes, error) {
	out := readmodel.RecentNotes{}
	if limit < 1 || limit > 500 {
		return out, fmt.Errorf("limit must be 1..500")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	state, err := currentNoteMetadataStateTx(ctx, tx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return out, err
	}
	var current bool
	err = tx.QueryRowContext(ctx, `SELECT ready AND materialization_version = ? FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1`, materializationVersion).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	assessmentColumn := "''"
	if !current {
		assessmentColumn = "COALESCE(a.assessment_json, '')"
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE indexed_at>0`).Scan(&out.Count)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.path, COALESCE(n.title,''), n.mtime, COALESCE(t.type_name,''), COALESCE(a.has_issues,0), COALESCE(a.type_ambiguous,0), `+assessmentColumn+`
 FROM notes n LEFT JOIN ontology_note_types t ON t.note_path=n.path
 LEFT JOIN ontology_note_assessments a ON a.note_path=n.path WHERE n.indexed_at>0
 ORDER BY n.mtime DESC, n.path LIMIT ?`, limit)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var n readmodel.ShapeNote
		if err := rows.Scan(&n.Path, &n.Title, &n.Changed, &n.Type, &n.HasIssues, &n.Ambiguous, &n.AssessmentJSON); err != nil {
			rows.Close()
			return out, err
		}
		out.Notes = append(out.Notes, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
