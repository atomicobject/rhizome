package store

import "context"

// Endpoint inventory needs no edge weights, confidence, or ordering. Keep these
// source selections equivalent to graphDocEdgesReadmodelQuery, including code
// evidence whose note endpoint belongs in a notes-only graph.
func (s *Store) addGraphEndpointPaths(ctx context.Context, kinds map[string]string) error {
	sources := []struct{ query, sourceKind, targetKind string }{
		{`SELECT DISTINCT src_path, dst_path FROM graph_doc_edges
		 WHERE kind IN ('wikilink','mdlink') OR kind GLOB 'note_link:*'`, "note", "note"},
		{`SELECT DISTINCT d.path, a.path FROM intel_edges e
		 JOIN intel_doc_sections d ON e.src_type='doc_section' AND d.id=e.src_row_id
		 JOIN intel_code_anchors a ON e.dst_type='anchor' AND a.id=e.dst_row_id
		 WHERE e.kind='mentions'`, "note", "code"},
		{`SELECT DISTINCT src_path, dst_path FROM doc_links WHERE src_type='code' AND dst_kind='note'`, "code", "note"},
		{`SELECT DISTINCT src.path, dst.path FROM intel_edges e
		 JOIN intel_code_anchors src ON e.src_type='anchor' AND src.id=e.src_row_id
		 JOIN intel_code_anchors dst ON e.dst_type='anchor' AND dst.id=e.dst_row_id
		 WHERE e.kind NOT IN ('defines','mentions')`, "code", "code"},
	}
	for _, source := range sources {
		rows, err := s.db.QueryContext(ctx, source.query)
		if err != nil {
			return err
		}
		for rows.Next() {
			edge := GraphDocEdge{SourceKind: source.sourceKind, TargetKind: source.targetKind}
			if err := rows.Scan(&edge.SrcPath, &edge.DstPath); err != nil {
				rows.Close()
				return err
			}
			src, dst := graphDocEdgeEndpointKinds(edge)
			setGraphReadmodelPathKind(kinds, edge.SrcPath, src)
			setGraphReadmodelPathKind(kinds, edge.DstPath, dst)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
