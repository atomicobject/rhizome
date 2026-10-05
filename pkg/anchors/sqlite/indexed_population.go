package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// IndexedPopulation describes the persisted source population used by search.
// Its fingerprint is logical and independent of SQLite page layout.
type IndexedPopulation struct {
	Fingerprint            string
	IndexGeneration        string
	Files                  int
	Anchors                int
	DocSections            int
	OntologyNodes          int
	Chunks                 int
	GraphEdges             int
	IntelEdges             int
	UnresolvedChunkOwner   int
	UnresolvedIntelOwner   int
	UnresolvedGraphSource  int
	UnresolvedGraphTarget  int
	Paths                  []string
	Identities             []string
	UnresolvedGraphTargets []string
}

func (s *Store) IndexedPopulation(ctx context.Context) (IndexedPopulation, error) {
	var out IndexedPopulation
	h := sha256.New()
	paths := map[string]struct{}{}
	identities := map[string]struct{}{}
	add := func(value string) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(value))
	}
	scan := func(label, query string, count *int, pathColumns ...int) error {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		add(label)
		for rows.Next() {
			values := make([]string, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				return err
			}
			for _, value := range values {
				add(value)
			}
			for _, column := range pathColumns {
				if value := strings.TrimSpace(values[column]); value != "" {
					paths[value] = struct{}{}
				}
			}
			switch label {
			case "anchors":
				if values[2] != "" {
					identities[strings.Join([]string{"fqn", values[2], values[1]}, "\x00")] = struct{}{}
				}
			case "nodes":
				identities[strings.Join([]string{"node", values[1], values[0], values[2], values[3]}, "\x00")] = struct{}{}
			}
			*count++
		}
		return rows.Err()
	}
	queries := []struct {
		label, query string
		count        *int
		pathColumns  []int
	}{
		{"files", `SELECT path,lang,COALESCE(hash,'') FROM files ORDER BY path`, &out.Files, []int{0}},
		{"anchors", `SELECT anchor_id,path,COALESCE(fqn,'') FROM intel_code_anchors ORDER BY anchor_id`, &out.Anchors, []int{1}},
		{"sections", `SELECT section_id,path,fingerprint FROM intel_doc_sections ORDER BY section_id`, &out.DocSections, []int{1}},
		{"nodes", `SELECT node_id,note_path,fragment,COALESCE(structural_fingerprint,''),start_byte,end_byte FROM ontology_nodes ORDER BY node_id`, &out.OntologyNodes, []int{1}},
		{"chunks", `SELECT c.chunk_id,c.owner_type,COALESCE(a.path,s.path,n.note_path,'') FROM intel_chunks c LEFT JOIN intel_code_anchors a ON a.id=c.owner_row_id AND c.owner_type='anchor' LEFT JOIN intel_doc_sections s ON s.id=c.owner_row_id AND c.owner_type='doc_section' LEFT JOIN ontology_nodes n ON n.node_id=c.owner_id AND c.owner_type='ontology_node' ORDER BY c.chunk_id`, &out.Chunks, []int{2}},
		{"graph", `SELECT e.src_path,e.dst_path,e.kind,CASE WHEN sn.id IS NOT NULL OR sf.path IS NOT NULL THEN e.src_path ELSE '' END,CASE WHEN dn.id IS NOT NULL OR df.path IS NOT NULL THEN e.dst_path ELSE '' END FROM graph_doc_edges e LEFT JOIN notes sn ON sn.path=e.src_path AND sn.indexed_at>0 LEFT JOIN files sf ON sf.path=e.src_path LEFT JOIN notes dn ON dn.path=e.dst_path AND dn.indexed_at>0 LEFT JOIN files df ON df.path=e.dst_path ORDER BY e.src_path,e.dst_path,e.kind,e.source_location`, &out.GraphEdges, []int{3, 4}},
		{"intel_edges", `SELECT e.src_type,COALESCE(sa.anchor_id,ss.section_id,''),e.dst_type,COALESCE(da.anchor_id,ds.section_id,''),e.kind,COALESCE(sa.path,ss.path,''),COALESCE(da.path,ds.path,'') FROM intel_edges e LEFT JOIN intel_code_anchors sa ON e.src_type='anchor' AND sa.id=e.src_row_id LEFT JOIN intel_doc_sections ss ON e.src_type='doc_section' AND ss.id=e.src_row_id LEFT JOIN intel_code_anchors da ON e.dst_type='anchor' AND da.id=e.dst_row_id LEFT JOIN intel_doc_sections ds ON e.dst_type='doc_section' AND ds.id=e.dst_row_id ORDER BY e.src_type,2,e.dst_type,4,e.kind`, &out.IntelEdges, []int{5, 6}},
	}
	for _, query := range queries {
		if err := scan(query.label, query.query, query.count, query.pathColumns...); err != nil {
			return IndexedPopulation{}, fmt.Errorf("inventory %s: %w", query.label, err)
		}
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_chunks c LEFT JOIN intel_code_anchors a ON a.id=c.owner_row_id AND c.owner_type='anchor' LEFT JOIN intel_doc_sections s ON s.id=c.owner_row_id AND c.owner_type='doc_section' LEFT JOIN ontology_nodes n ON n.node_id=c.owner_id AND c.owner_type='ontology_node' WHERE a.id IS NULL AND s.id IS NULL AND n.node_id IS NULL`).Scan(&out.UnresolvedChunkOwner); err != nil {
		return IndexedPopulation{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges e LEFT JOIN intel_code_anchors sa ON e.src_type='anchor' AND sa.id=e.src_row_id LEFT JOIN intel_doc_sections ss ON e.src_type='doc_section' AND ss.id=e.src_row_id LEFT JOIN intel_code_anchors da ON e.dst_type='anchor' AND da.id=e.dst_row_id LEFT JOIN intel_doc_sections ds ON e.dst_type='doc_section' AND ds.id=e.dst_row_id WHERE (sa.id IS NULL AND ss.id IS NULL) OR (da.id IS NULL AND ds.id IS NULL)`).Scan(&out.UnresolvedIntelOwner); err != nil {
		return IndexedPopulation{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_doc_edges e LEFT JOIN notes n ON n.path=e.src_path AND n.indexed_at>0 LEFT JOIN files f ON f.path=e.src_path WHERE n.id IS NULL AND f.path IS NULL`).Scan(&out.UnresolvedGraphSource); err != nil {
		return IndexedPopulation{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_doc_edges e LEFT JOIN notes n ON n.path=e.dst_path AND n.indexed_at>0 LEFT JOIN files f ON f.path=e.dst_path WHERE n.id IS NULL AND f.path IS NULL`).Scan(&out.UnresolvedGraphTarget); err != nil {
		return IndexedPopulation{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT e.dst_path FROM graph_doc_edges e LEFT JOIN notes n ON n.path=e.dst_path AND n.indexed_at>0 LEFT JOIN files f ON f.path=e.dst_path WHERE n.id IS NULL AND f.path IS NULL ORDER BY e.dst_path`)
	if err != nil {
		return IndexedPopulation{}, err
	}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			_ = rows.Close()
			return IndexedPopulation{}, err
		}
		out.UnresolvedGraphTargets = append(out.UnresolvedGraphTargets, path)
	}
	if err := rows.Close(); err != nil {
		return IndexedPopulation{}, err
	}
	out.Paths = make([]string, 0, len(paths))
	for path := range paths {
		out.Paths = append(out.Paths, path)
	}
	sort.Strings(out.Paths)
	out.Identities = make([]string, 0, len(identities))
	for identity := range identities {
		out.Identities = append(out.Identities, identity)
	}
	sort.Strings(out.Identities)
	out.Fingerprint = fmt.Sprintf("sha256:%x", h.Sum(nil))
	return out, nil
}
