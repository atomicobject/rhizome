package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// OntologyShapeSnapshot reads all requested aggregate inputs in one SQLite
// snapshot. There is no hydration, source I/O, schema mutation, or writer lane.
func (s *Store) OntologyShapeSnapshot(ctx context.Context, fields []string, links bool, materializationVersion int) (readmodel.ShapeSnapshot, error) {
	out := readmodel.ShapeSnapshot{}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	state, err := currentNoteMetadataStateTx(ctx, tx)
	if err != nil {
		return out, err
	}
	out.Rebuilding = !state.Ready || state.LoadedAt == 0
	var ontologyReady bool
	err = tx.QueryRowContext(ctx, `SELECT ready AND materialization_version = ? AND notes_hash = ? AND loaded_at = ?, schema_hash FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1`, materializationVersion, state.NotesHash, state.LoadedAt).Scan(&ontologyReady, &out.SchemaHash)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	assessmentColumn := "''"
	if !ontologyReady {
		assessmentColumn = "COALESCE(a.assessment_json, '')"
		out.Rebuilding = out.Rebuilding || materializationVersion > 0
	}

	read := func(query string, args []any, scan func(*sql.Rows) error) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	err = read(`SELECT n.path, COALESCE(t.type_name, ''), COALESCE(o.node_id, ''), n.mtime,
 COALESCE(a.has_issues, 0), COALESCE(a.type_ambiguous, 0), `+assessmentColumn+`
 FROM notes n LEFT JOIN ontology_note_types t ON t.note_path = n.path
 LEFT JOIN ontology_note_assessments a ON a.note_path = n.path
 LEFT JOIN ontology_nodes o ON o.note_path = n.path AND o.node_kind = 'NOTE'
 WHERE n.indexed_at > 0`, nil, func(rows *sql.Rows) error {
		var n readmodel.ShapeNote
		if err := rows.Scan(&n.Path, &n.Type, &n.NodeID, &n.Changed, &n.HasIssues, &n.Ambiguous, &n.AssessmentJSON); err != nil {
			return err
		}
		out.Notes = append(out.Notes, n)
		return nil
	})
	if err != nil {
		return out, err
	}
	if len(fields) > 0 {
		args := make([]any, len(fields))
		for i, field := range fields {
			args[i] = strings.ToLower(field)
		}
		err = read(`SELECT f.note_path, f.field_name, f.value_text FROM ontology_node_field_values f
  JOIN ontology_nodes n ON n.node_id = f.node_id AND n.node_kind = 'NOTE'
  WHERE f.field_name IN (`+strings.TrimSuffix(strings.Repeat("?,", len(fields)), ",")+`)`, args, func(rows *sql.Rows) error {
			var f readmodel.ShapeField
			if err := rows.Scan(&f.Path, &f.Field, &f.Value); err != nil {
				return err
			}
			out.Fields = append(out.Fields, f)
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	if links {
		err = read(`SELECT src_path, dst_path, COALESCE(src_node_id, ''), COALESCE(dst_node_id, ''), relation_name, structural FROM ontology_edges`, nil, func(rows *sql.Rows) error {
			var e readmodel.ShapeEdge
			if err := rows.Scan(&e.Source, &e.Target, &e.SourceNode, &e.TargetNode, &e.Field, &e.Relation); err != nil {
				return err
			}
			out.Edges = append(out.Edges, e)
			return nil
		})
		if err != nil {
			return out, err
		}
		err = read(`SELECT src_path, dst_path FROM graph_doc_edges
  WHERE kind IN ('wikilink', 'mdlink') OR kind GLOB 'note_link:*'`, nil, func(rows *sql.Rows) error {
			var e readmodel.ShapeEdge
			if err := rows.Scan(&e.Source, &e.Target); err != nil {
				return err
			}
			out.Edges = append(out.Edges, e)
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}
