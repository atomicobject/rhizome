package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func (s *Store) GraphOntologyNodes(ctx context.Context, query readmodel.GraphNodeQuery) ([]readmodel.GraphNodeRow, error) {
	// Docs: [[ontology-indexed-read-model-contract#^spec-0040-endpoint-selector]]
	// requires endpoint selectors to resolve by catalog node id, block/fragment,
	// source_locator, and structured NodeRefJSON without broadening scope.
	var rows []readmodel.NodeRow
	var err error
	switch {
	case len(query.EndpointSelectors) > 0:
		rows, err = s.ontologyNodesByEndpointSelectors(ctx, query.EndpointSelectors, query.Limit)
	case len(query.Paths) > 0:
		rows, err = s.OntologyNodesByPaths(ctx, query.Paths)
	case len(query.PathPrefixes) > 0:
		rows, err = s.ontologyNodesByPathPrefixes(ctx, query.PathPrefixes, query.Limit)
	default:
		rows, err = s.AllOntologyNodes(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.GraphNodeRow, 0, len(rows))
	for _, row := range rows {
		if len(query.PathPrefixes) > 0 && !pathMatchesAnyPrefix(row.NotePath, query.PathPrefixes) {
			continue
		}
		out = append(out, graphNodeRowFromIntel(row))
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

func (s *Store) GraphOntologyEdges(ctx context.Context, query readmodel.GraphEdgeQuery) ([]readmodel.GraphTypedEdgeRow, error) {
	limit := query.Limit
	var rows []OntologyEdgeRow
	var err error
	switch {
	case len(query.EndpointSelectors) > 0:
		rows, err = s.ontologyEdgesForEndpointSelectors(ctx, query.EndpointSelectors, query.IncludeAmbient, limit)
	case len(query.Paths) > 0:
		rows, err = s.OntologyEdgesForPaths(ctx, query.Paths, query.IncludeAmbient, "", limit)
	case len(query.PathPrefixes) > 0:
		rows, err = s.ontologyEdgesForPathPrefixes(ctx, query.PathPrefixes, query.IncludeAmbient, limit)
	default:
		rows, err = s.AllOntologyEdges(ctx, query.IncludeAmbient, limit)
	}
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.GraphTypedEdgeRow, 0, len(rows))
	for _, row := range rows {
		if len(query.PathPrefixes) > 0 && !pathMatchesAnyPrefix(row.SrcPath, query.PathPrefixes) && !pathMatchesAnyPrefix(row.DstPath, query.PathPrefixes) {
			continue
		}
		out = append(out, graphTypedEdgeRowFromSQLite(row))
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

func (s *Store) GraphDocEdges(ctx context.Context, query readmodel.GraphDocEdgeQuery) ([]readmodel.GraphDocEdgeRow, error) {
	// Docs: [[ontology-indexed-read-model-contract]]
	// keeps code-code edges behind an explicit IncludeCodeEdges request.
	limit := query.Limit
	if query.IncludeCodeEdges {
		query.IncludeCode = true
	}
	var rows []GraphDocEdge
	var err error
	switch {
	case len(query.PathPrefixes) > 0 && len(query.Paths) == 0:
		for _, prefix := range query.PathPrefixes {
			var prefixRows []GraphDocEdge
			prefixRows, err = s.graphDocEdgesForPrefixReadmodel(ctx, prefix, limit, query.IncludeCode, query.IncludeCodeEdges)
			if err != nil {
				return nil, err
			}
			rows = append(rows, prefixRows...)
			if limit > 0 && len(rows) >= limit {
				rows = rows[:limit]
				break
			}
		}
	case len(query.Paths) > 0:
		rows, err = s.graphDocEdgesForPathsReadmodel(ctx, query.Paths, limit, query.IncludeCode, query.IncludeCodeEdges)
	default:
		rows, err = s.allGraphDocEdgesReadmodel(ctx, limit, query.IncludeCode, query.IncludeCodeEdges)
	}
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.GraphDocEdgeRow, 0, len(rows))
	for _, row := range rows {
		if len(query.PathPrefixes) > 0 && !pathMatchesAnyPrefix(row.SrcPath, query.PathPrefixes) && !pathMatchesAnyPrefix(row.DstPath, query.PathPrefixes) {
			continue
		}
		sourceKind, targetKind := graphDocEdgeEndpointKinds(row)
		if sourceKind == "" || targetKind == "" {
			continue
		}
		if !query.IncludeCode && (sourceKind != "note" || targetKind != "note") {
			continue
		}
		out = append(out, readmodel.GraphDocEdgeRow{
			SrcPath:         row.SrcPath,
			DstPath:         row.DstPath,
			SourceKind:      sourceKind,
			TargetKind:      targetKind,
			Kind:            row.Kind,
			Weight:          row.Weight,
			Confidence:      row.Confidence,
			ConfidenceScore: row.ConfidenceScore,
		})
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}
