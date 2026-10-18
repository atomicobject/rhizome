package noderead

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type neighborhoodRowCacheKey struct {
	endpoint readmodel.EdgeEndpoint
	query    string
}

func (s *Scope) neighborhoodRowsLocked(ctx context.Context, query readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error) {
	sources := query.Sources
	query.Sources = nil
	query.Relations = normalizeStrings(query.Relations)
	query.Provenance = normalizeStrings(query.Provenance)
	slices.Sort(query.Relations)
	slices.Sort(query.Provenance)
	query.Relations = slices.Compact(query.Relations)
	query.Provenance = slices.Compact(query.Provenance)
	encoded, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	shape := string(encoded)
	keys := make([]neighborhoodRowCacheKey, 0, len(sources))
	missing := make(map[readmodel.EdgeEndpoint][]semdb.OntologyEdgeRow)
	seenSources := make(map[readmodel.EdgeEndpoint]bool, len(sources))
	hit := false
	for _, source := range sources {
		if seenSources[source] {
			continue
		}
		seenSources[source] = true
		key := neighborhoodRowCacheKey{endpoint: source, query: shape}
		keys = append(keys, key)
		if _, ok := s.neighborhoodRowsByKey[key]; ok {
			hit = true
		} else {
			query.Sources = append(query.Sources, source)
			missing[source] = nil
		}
	}
	if hit {
		s.diagnostics.EdgeHits++
	}
	if len(query.Sources) > 0 {
		rows, err := s.service.Store.OntologyNeighborhood(ctx, query)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}
		// Partition raw rows before target filtering or paging. Exact endpoint
		// keys keep note roots separate from embedded sources in the same host.
		for _, row := range rows {
			source := readmodel.EdgeEndpoint{Path: row.SrcPath, NodeID: row.SrcNodeID}
			target := readmodel.EdgeEndpoint{Path: row.DstPath, NodeID: row.DstNodeID}
			if query.Direction != "inbound" {
				if _, ok := missing[source]; ok {
					missing[source] = append(missing[source], row)
				}
			}
			if query.Direction != "outbound" && (query.Direction == "inbound" || source != target) {
				if _, ok := missing[target]; ok {
					missing[target] = append(missing[target], row)
				}
			}
		}
		s.diagnostics.EdgeLoads++
		for endpoint, rows := range missing {
			s.neighborhoodRowsByKey[neighborhoodRowCacheKey{endpoint: endpoint, query: shape}] = rows
		}
	}
	if len(keys) == 1 {
		return s.neighborhoodRowsByKey[keys[0]], nil
	}
	// A bidirectional row may belong to two requested endpoints. Preserve the
	// store's distinct union when reassembling cached source partitions.
	seenRows := make(map[semdb.OntologyEdgeRow]bool)
	var rows []semdb.OntologyEdgeRow
	for _, key := range keys {
		for _, row := range s.neighborhoodRowsByKey[key] {
			if !seenRows[row] {
				seenRows[row] = true
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}
