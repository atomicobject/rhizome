package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/anchors"
)

// IntelEdges returns all stored intel edges.
func (s *Store) IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT
                        CASE WHEN e.src_type = 'anchor' THEN src.anchor_id ELSE srcs.section_id END AS src_id,
                        CASE WHEN e.dst_type = 'anchor' THEN dst.anchor_id ELSE dsts.section_id END AS dst_id,
                        e.kind,
                        e.meta_json
                FROM intel_edges e
                LEFT JOIN intel_code_anchors src ON src.id = e.src_row_id AND e.src_type = 'anchor'
                LEFT JOIN intel_doc_sections srcs ON srcs.id = e.src_row_id AND e.src_type = 'doc_section'
                LEFT JOIN intel_code_anchors dst ON dst.id = e.dst_row_id AND e.dst_type = 'anchor'
                LEFT JOIN intel_doc_sections dsts ON dsts.id = e.dst_row_id AND e.dst_type = 'doc_section'
                ORDER BY src_id, dst_id
        `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []codeanchor.IntelEdge
	for rows.Next() {
		var e codeanchor.IntelEdge
		if err := rows.Scan(&e.SrcID, &e.DstID, &e.Kind, &e.MetaJSON); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}
