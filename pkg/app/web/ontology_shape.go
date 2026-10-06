package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

func (s *Server) handleOntologyShape(w http.ResponseWriter, r *http.Request) {
	parts := noderead.ShapeParts{}
	selector := r.URL.Query().Get("parts")
	if selector == "" {
		selector = "members,links,folders"
	}
	for _, part := range strings.Split(selector, ",") {
		switch part {
		case "members":
			parts.Members = true
		case "links":
			parts.Links = true
		case "folders":
			parts.Folders = true
		default:
			writeError(w, http.StatusBadRequest, errors.New("parts must be a subset of members,links,folders"))
			return
		}
	}
	defs, err := s.ontologyDefinitions()
	if err != nil {
		if errors.Is(err, errIndexInitializing) {
			w.Header().Set("Retry-After", "5")
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	store := s.runtime.Intel()
	if store == nil {
		w.Header().Set("Retry-After", "5")
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
		return
	}
	var schema *ontology.Schema
	if defs != nil {
		schema = defs.schema
	}
	resp, err := noderead.OntologyShape(r.Context(), store, schema, parts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if resp.Members != nil {
		if err := s.countShapeIssues(r.Context(), resp.Members); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) countShapeIssues(ctx context.Context, members *noderead.ShapeMembers) error {
	scopes := make([]semdb.ValidationScope, 0, len(members.Types)+len(members.Interfaces))
	for _, member := range members.Types {
		scopes = append(scopes, semdb.ValidationScope{Kind: "type", Key: member.Name})
	}
	implementors := map[string][]string{}
	for _, member := range members.Interfaces {
		scopes = append(scopes, semdb.ValidationScope{Kind: "interface", Key: member.Name})
		implementors[member.Name] = member.Implementors
	}
	counts, err := s.validationScopeIssueCounts(ctx, scopes, implementors)
	if err != nil {
		return err
	}
	for i := range members.Types {
		members.Types[i].IssueCount = counts[i]
	}
	for i := range members.Interfaces {
		members.Interfaces[i].IssueCount = counts[len(members.Types)+i]
	}
	return nil
}
