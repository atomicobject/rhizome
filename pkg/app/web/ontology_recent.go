package web

import (
	"context"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

func (s *Server) ontologyRecentNotes(ctx context.Context, limit int) (OntologyTypeResponse, error) {
	_, defs, err := s.ontologyContext()
	if err != nil {
		return OntologyTypeResponse{}, err
	}
	store := s.runtime.Intel()
	if store == nil {
		return OntologyTypeResponse{}, errIndexInitializing
	}
	var schema *ontology.Schema
	if defs != nil {
		schema = defs.schema
	}
	scope := noderead.NewService(s.cfg.VaultDef, s.defaultOntologyNoteReader(), store, schema).NewScope(ctx, noderead.ScopeOptions{})
	result, err := scope.RecentNotes(ctx, limit)
	if err != nil {
		return OntologyTypeResponse{}, err
	}
	resp, err := s.enrichOntologyType(ctx, pseudoTypeAll, nil, schema, scope, result)
	sort.SliceStable(resp.Notes, func(i, j int) bool {
		if resp.Notes[i].UpdatedAt != resp.Notes[j].UpdatedAt {
			return resp.Notes[i].UpdatedAt > resp.Notes[j].UpdatedAt
		}
		return resp.Notes[i].Ref.NotePath < resp.Notes[j].Ref.NotePath
	})
	return resp, err
}
