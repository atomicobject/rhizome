package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

// CodeGraphLinks returns note-code or code-note graph links incident to source.
//
// It is a light summary API for callers that need related code paths without
// pulling the full graph result.
func (s *Scope) CodeGraphLinks(ctx context.Context, source ontology.NodeRef, limit int) ([]GraphCodeLink, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return nil, nil
	}
	graphStore := s.service.Store
	source = NormalizeRef(source)
	if strings.TrimSpace(source.NotePath) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := graphDocEdges(ctx, graphStore, []string{source.NotePath}, nil, limit, true, false)
	if err != nil {
		return nil, err
	}
	links := make([]GraphCodeLink, 0, len(rows))
	for _, row := range rows {
		other, otherKind := "", ""
		switch source.NotePath {
		case row.SrcPath:
			other, otherKind = row.DstPath, row.TargetKind
		case row.DstPath:
			other, otherKind = row.SrcPath, row.SourceKind
		default:
			continue
		}
		kind, path := graphDocRowEndpoint(other, otherKind)
		if kind != GraphEndpointCode || path == "" {
			continue
		}
		weight := row.Weight
		if weight <= 0 {
			weight = 1
		}
		links = append(links, GraphCodeLink{
			Path:       path,
			Kind:       row.Kind,
			Provenance: row.Kind,
			Weight:     weight,
		})
	}
	return links, nil
}
