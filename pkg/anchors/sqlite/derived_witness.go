package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// DerivedSourceFingerprint witnesses structural inputs independently of dirty
// tickets. Direct saves can publish new projections before the watcher marks
// their work; those publications must also reject an older provider result.
// Derived vector, chunk and score writes are deliberately excluded.
func (s *Store) DerivedSourceFingerprint(ctx context.Context, scope codeanchor.DerivedScope) (string, error) {
	scope, err := cleanDerivedScope(scope)
	if err != nil {
		return "", err
	}
	queries := []string{
		`SELECT json_array(schema_hash,materialization_version,ready) FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1`,
	}
	if scope.Kind != codeanchor.DerivedCode {
		queries = append(queries,
			`SELECT json_array(n.path,n.content_hash,n.indexer_version,n.format_id,p.provider_version,p.projection_version,p.source_content_hash,p.status) FROM notes n LEFT JOIN note_projection_state p ON p.note_id=n.id WHERE (?='' OR n.path=?) ORDER BY n.path`,
			`SELECT json_array(node_id,structural_fingerprint,schema_hash) FROM ontology_nodes WHERE (?='' OR note_path=?) ORDER BY node_id`)
	}
	if scope.Kind == codeanchor.DerivedCode || scope.Kind == codeanchor.DerivedGraph {
		queries = append(queries, `SELECT json_array(path,hash,indexer_version,call_edges_stale) FROM files WHERE (?='' OR path=?) ORDER BY path`,
			`SELECT json_array(anchor_id,fingerprint) FROM intel_code_anchors WHERE (?='' OR path=?) ORDER BY anchor_id`)
	}

	if scope.Kind == codeanchor.DerivedCode {
		queries = append(queries,
			`SELECT json_array(path,content_hash,indexer_version,format_id) FROM notes WHERE (?='' OR ?<>'') ORDER BY path`,
			`SELECT json_array(anchor_id,symbol_fqn,call_file) FROM anchor_scopes WHERE (?='' OR ?<>'') ORDER BY anchor_id,symbol_fqn,call_file`,
			`SELECT json_array(note_id,anchor_id) FROM note_anchors WHERE (?='' OR ?<>'') ORDER BY note_id,anchor_id`,
			`SELECT json_array(src_type,src_path,src_id,dst_kind,dst_id,dst_path,lang,label,snippet,meta_json) FROM doc_links WHERE (?='' OR ?<>'') ORDER BY src_type,src_path,src_id,dst_kind,dst_id,dst_path,lang,label,snippet,meta_json`)
	}
	if scope.Kind == codeanchor.DerivedGraph {
		queries = append(queries, `SELECT json_array(src_type,src_path,dst_kind,dst_path) FROM doc_links WHERE src_type='code' AND dst_kind='note' AND (?='' OR ?<>'') ORDER BY src_type,src_path,dst_kind,dst_path`)
	}
	if scope.Kind == codeanchor.DerivedGraph || scope.Kind == codeanchor.DerivedCode {
		queries = append(queries,
			`SELECT json_array(src_type,src_row_id,dst_type,dst_row_id,kind) FROM intel_edges WHERE (?='' OR ?<>'') ORDER BY src_type,src_row_id,dst_type,dst_row_id,kind`,
			`SELECT json_array(src_path,dst_path,kind) FROM graph_doc_edges WHERE (?='' OR ?<>'') ORDER BY src_path,dst_path,kind`)
	}
	digest := sha256.New()
	for i, query := range queries {
		var args []any
		if i > 0 {
			args = []any{scope.Path, scope.Path}
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				rows.Close()
				return "", err
			}
			fmt.Fprintf(digest, "%d:%d:%s\n", i, len(row), row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
