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
	q.Relations = normalizeNonEmptyStrings(q.Relations)
	q.Provenance = normalizeNonEmptyStrings(q.Provenance)
	q.DstTypes = normalizeNonEmptyStrings(q.DstTypes)
	batchSize := (sqliteValuesBatchMaxParams - len(q.Relations) - len(q.Provenance) - len(q.DstTypes) - 1) / 2
	if batchSize < 1 {
		return nil, fmt.Errorf("too many neighborhood filter values")
	}
	for start := 0; start < len(q.Sources); start += batchSize {
		query, args := ontologyNeighborhoodSQL(q.Sources[start:min(start+batchSize, len(q.Sources))], q)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyEdgeRow
			if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &row.Structural, &row.SchemaHash, &row.UpdatedAt, &row.OccurrencesJSON); err != nil {
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

// ontologyNeighborhoodSQL builds one batch's statement over normalized
// filters. The requested endpoints drive the join (CROSS JOIN fixes the loop
// order) and the NOT NULL endpoint columns are compared directly, so every
// parent seeks the edge index at any batch size. A COALESCE around them, or an
// OR across both endpoints, stops SQLite from planning through the index above
// about 150 parents.
func ontologyNeighborhoodSQL(batch []readmodel.EdgeEndpoint, q readmodel.NeighborhoodQuery) (string, []any) {
	conditions := []string{"1=1"}
	var conditionArgs []any
	if !q.IncludeAmbient {
		conditions = append(conditions, "e.structural=1")
	}
	if !q.IncludeStructural {
		conditions = append(conditions, "e.structural=0")
	}
	for _, filter := range []struct {
		column string
		values []string
	}{{"relation_name", q.Relations}, {"provenance", q.Provenance}} {
		if len(filter.values) > 0 {
			conditions = append(conditions, fmt.Sprintf("e.%s IN (%s)", filter.column, sqlPlaceholderList(len(filter.values))))
			conditionArgs = append(conditionArgs, sliceAny(filter.values)...)
		}
	}
	if len(q.DstTypes) > 0 && q.Direction == "outbound" {
		// Untyped note targets and embedded targets keep the caller's catalog
		// type lookup, so they pass through.
		conditions = append(conditions, fmt.Sprintf("(e.dst_type IN (%s) OR e.dst_type='' OR e.dst_node_id<>'')", sqlPlaceholderList(len(q.DstTypes))))
		conditionArgs = append(conditionArgs, sliceAny(q.DstTypes)...)
	}
	args := make([]any, 0, len(batch)*2+2*len(conditionArgs)+1)
	for _, src := range batch {
		args = append(args, src.Path, src.NodeID)
	}
	// Each direction is its own seek; UNION keeps a self-loop once per parent.
	branch := func(endpoint string) string {
		args = append(args, conditionArgs...)
		return fmt.Sprintf("SELECT r.path AS r_path, r.node_id AS r_node_id, e.src_path, e.src_node_id, e.relation_name, e.dst_path, e.dst_node_id, e.dst_type, e.provenance, e.structural, e.schema_hash, e.updated_at, e.occurrences_json FROM requested r CROSS JOIN ontology_edges e ON e.%[1]s_path=r.path AND e.%[1]s_node_id=r.node_id WHERE %[2]s", endpoint, strings.Join(conditions, " AND "))
	}
	var joined, embeddedTarget string
	switch q.Direction {
	case "inbound":
		joined, embeddedTarget = branch("dst"), "src_node_id<>''"
	case "both":
		joined, embeddedTarget = branch("src")+" UNION "+branch("dst"), "src_node_id<>'' OR dst_node_id<>''"
	default:
		joined, embeddedTarget = branch("src"), "dst_node_id<>''"
	}
	query := fmt.Sprintf("WITH requested(path,node_id) AS MATERIALIZED (VALUES %s), joined AS (%s), matches AS (SELECT *, ROW_NUMBER() OVER (PARTITION BY r_path,r_node_id ORDER BY structural DESC,provenance,relation_name,dst_path,dst_node_id,src_path,src_node_id) AS ordinal, MAX(CASE WHEN %s THEN 1 ELSE 0 END) OVER (PARTITION BY r_path,r_node_id) AS has_embedded_target FROM joined) SELECT DISTINCT src_path,src_node_id,relation_name,dst_path,dst_node_id,dst_type,provenance,structural,schema_hash,updated_at,occurrences_json FROM matches", strings.TrimSuffix(strings.Repeat("(?,?),", len(batch)), ","), joined, embeddedTarget)
	if q.PerSourceLimit > 0 {
		// Embedded refs need catalog canonicalization before their final ordering.
		query += " WHERE has_embedded_target=1 OR ordinal<=?"
		args = append(args, q.PerSourceLimit)
	}
	query += " ORDER BY structural DESC,provenance,relation_name,src_path,src_node_id,dst_path,dst_node_id"
	return query, args
}

func sqlPlaceholderList(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
