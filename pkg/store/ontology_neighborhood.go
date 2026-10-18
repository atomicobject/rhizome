package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

// OntologyNeighborhood reads all requested relations and one direction in a
// batch. Each endpoint remains exact, including note-root versus embedded IDs.
func (s *Store) OntologyNeighborhood(ctx context.Context, q readmodel.NeighborhoodQuery) ([]OntologyEdgeRow, error) {
	if len(q.Sources) == 0 {
		return nil, nil
	}
	if q.Direction != "outbound" && q.Direction != "inbound" && q.Direction != "both" {
		return nil, fmt.Errorf("invalid neighborhood direction %q", q.Direction)
	}
	uniqueSources := make([]readmodel.EdgeEndpoint, 0, len(q.Sources))
	seenSources := map[readmodel.EdgeEndpoint]bool{}
	for _, source := range q.Sources {
		if !seenSources[source] {
			seenSources[source] = true
			uniqueSources = append(uniqueSources, source)
		}
	}
	q.Sources = uniqueSources
	var out []OntologyEdgeRow
	seen := map[OntologyEdgeRow]bool{}
	relations := normalizeNonEmptyStrings(q.Relations)
	provenance := normalizeNonEmptyStrings(q.Provenance)
	batchSize := (sqliteValuesBatchMaxParams - len(relations) - len(provenance) - 1) / 2
	if batchSize < 1 {
		return nil, fmt.Errorf("too many neighborhood filter values")
	}
	for start := 0; start < len(q.Sources); start += batchSize {
		batch := q.Sources[start:min(start+batchSize, len(q.Sources))]
		args := make([]any, 0, len(batch)*2+len(relations)+len(provenance)+1)
		for _, src := range batch {
			args = append(args, src.Path, src.NodeID)
		}
		srcMatch := "e.src_path=r.path AND COALESCE(e.src_node_id,'')=r.node_id"
		dstMatch := "e.dst_path=r.path AND COALESCE(e.dst_node_id,'')=r.node_id"
		join := srcMatch
		if q.Direction == "inbound" {
			join = dstMatch
		} else if q.Direction == "both" {
			join = "(" + srcMatch + ") OR (" + dstMatch + ")"
		}
		conditions := []string{"1=1"}
		if !q.IncludeAmbient {
			conditions = append(conditions, "e.structural=1")
		}
		if !q.IncludeStructural {
			conditions = append(conditions, "e.structural=0")
		}
		for _, filter := range []struct {
			column string
			values []string
		}{{"relation_name", relations}, {"provenance", provenance}} {
			if len(filter.values) > 0 {
				conditions = append(conditions, fmt.Sprintf("e.%s IN (%s)", filter.column, strings.TrimSuffix(strings.Repeat("?,", len(filter.values)), ",")))
				args = append(args, sliceAny(filter.values)...)
			}
		}
		columns := "e.src_path, COALESCE(e.src_node_id,'') AS src_node_id, e.relation_name, e.dst_path, COALESCE(e.dst_node_id,'') AS dst_node_id, e.dst_type, e.provenance, e.structural, e.schema_hash, e.updated_at"
		embeddedTarget := "COALESCE(e.dst_node_id,'')<>''"
		if q.Direction == "inbound" {
			embeddedTarget = "COALESCE(e.src_node_id,'')<>''"
		}
		if q.Direction == "both" {
			embeddedTarget = "COALESCE(e.src_node_id,'')<>'' OR COALESCE(e.dst_node_id,'')<>''"
		}
		query := fmt.Sprintf("WITH requested(path,node_id) AS (VALUES %s), matches AS (SELECT %s, ROW_NUMBER() OVER (PARTITION BY r.path,r.node_id ORDER BY e.structural DESC,e.provenance,e.relation_name,e.dst_path,e.dst_node_id,e.src_path,e.src_node_id) AS ordinal, MAX(CASE WHEN %s THEN 1 ELSE 0 END) OVER (PARTITION BY r.path,r.node_id) AS has_embedded_target FROM ontology_edges e JOIN requested r ON %s WHERE %s) SELECT DISTINCT src_path,src_node_id,relation_name,dst_path,dst_node_id,dst_type,provenance,structural,schema_hash,updated_at FROM matches", strings.TrimSuffix(strings.Repeat("(?,?),", len(batch)), ","), columns, embeddedTarget, join, strings.Join(conditions, " AND "))
		if q.PerSourceLimit > 0 {
			// Embedded refs need catalog canonicalization before their final ordering.
			query += " WHERE has_embedded_target=1 OR ordinal<=?"
			args = append(args, q.PerSourceLimit)
		}
		query += " ORDER BY structural DESC,provenance,relation_name,src_path,src_node_id,dst_path,dst_node_id"
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyEdgeRow
			if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &row.Structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			if !seen[row] {
				seen[row] = true
				out = append(out, row)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
