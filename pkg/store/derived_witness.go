package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// DerivedSourceFingerprint excludes derived outputs from their own witness.
func (s *Store) DerivedSourceFingerprint(ctx context.Context, scope codeanchor.DerivedScope) (string, error) {
	fingerprints, err := s.DerivedSourceFingerprints(ctx, scope.Kind, []string{scope.Path})
	return fingerprints[scope.Path], err
}

// DerivedSourceFingerprints reads table-wide dependencies once for the lease,
// then witnesses each consumed path independently of dirty tickets.
func (s *Store) DerivedSourceFingerprints(ctx context.Context, kind codeanchor.DerivedKind, paths []string) (map[string]string, error) {
	if _, err := cleanDerivedScope(codeanchor.DerivedScope{Kind: kind}); err != nil {
		return nil, err
	}
	common := []string{`SELECT json_array(schema_hash,materialization_version,ready) FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1`}
	var local []string
	if kind != codeanchor.DerivedCode {
		local = append(local,
			`SELECT json_array(n.path,n.content_hash,n.indexer_version,n.format_id,p.provider_version,p.projection_version,p.source_content_hash,p.status) FROM notes n LEFT JOIN note_projection_state p ON p.note_id=n.id WHERE (?='' OR n.path=?) ORDER BY n.path`,
			`SELECT json_array(node_id,structural_fingerprint,schema_hash) FROM ontology_nodes WHERE (?='' OR note_path=?) ORDER BY node_id`)
	}
	if kind == codeanchor.DerivedCode || kind == codeanchor.DerivedGraph {
		local = append(local,
			`SELECT json_array(path,hash,indexer_version,call_edges_stale) FROM files WHERE (?='' OR path=?) ORDER BY path`,
			`SELECT json_array(anchor_id,fingerprint) FROM intel_code_anchors WHERE (?='' OR path=?) ORDER BY anchor_id`)
	}
	if kind == codeanchor.DerivedCode {
		common = append(common,
			`SELECT json_array(path,content_hash,indexer_version,format_id) FROM notes ORDER BY path`,
			`SELECT json_array(anchor_id,symbol_fqn,call_file) FROM anchor_scopes ORDER BY anchor_id,symbol_fqn,call_file`,
			`SELECT json_array(note_id,anchor_id) FROM note_anchors ORDER BY note_id,anchor_id`,
			`SELECT json_array(src_type,src_path,src_id,dst_kind,dst_id,dst_path,lang,label,snippet,meta_json) FROM doc_links ORDER BY src_type,src_path,src_id,dst_kind,dst_id,dst_path,lang,label,snippet,meta_json`)
	}
	if kind == codeanchor.DerivedGraph {
		common = append(common, `SELECT json_array(src_type,src_path,dst_kind,dst_path) FROM doc_links WHERE src_type='code' AND dst_kind='note' ORDER BY src_type,src_path,dst_kind,dst_path`)
	}
	if kind == codeanchor.DerivedGraph || kind == codeanchor.DerivedCode {
		common = append(common,
			`SELECT json_array(src_type,src_row_id,dst_type,dst_row_id,kind) FROM intel_edges ORDER BY src_type,src_row_id,dst_type,dst_row_id,kind`,
			`SELECT json_array(src_path,dst_path,kind) FROM graph_doc_edges ORDER BY src_path,dst_path,kind`)
	}
	shared, err := s.derivedFingerprintRows(ctx, common)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(paths))
	for _, path := range paths {
		scope, err := cleanDerivedScope(codeanchor.DerivedScope{Kind: kind, Path: path})
		if err != nil {
			return nil, err
		}
		fingerprint, err := s.derivedFingerprintRows(ctx, local, scope.Path, scope.Path)
		if err != nil {
			return nil, err
		}
		out[path] = shared + fingerprint
	}
	return out, nil
}

func (s *Store) derivedFingerprintRows(ctx context.Context, queries []string, args ...any) (string, error) {
	digest := sha256.New()
	for i, query := range queries {
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
