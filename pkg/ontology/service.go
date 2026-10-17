package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// Service reads current indexed ontology assessments and relations.
// Schema presentation belongs to schemadoc; request caches belong to noderead.Scope.
type Service struct {
	Store  *semdb.Store
	Schema *ontoschema.Schema
}

// NewService wires a store-backed ontology facade.
func NewService(store *semdb.Store, schema *ontoschema.Schema) *Service {
	return &Service{Store: store, Schema: schema}
}

// ResolvedType returns the indexed type row for a note path.
func (s *Service) ResolvedType(ctx context.Context, notePath string) (semdb.OntologyNoteTypeRow, bool, error) {
	if s == nil || s.Store == nil {
		return semdb.OntologyNoteTypeRow{}, false, nil
	}
	return s.Store.GetOntologyTypeByPath(ctx, notePath)
}

// Assessment returns the decoded indexed validation assessment for a note path.
func (s *Service) Assessment(ctx context.Context, notePath string) (*NoteAssessment, bool, error) {
	if s == nil || s.Store == nil {
		return nil, false, nil
	}
	row, ok, err := s.Store.GetOntologyAssessmentByPath(ctx, notePath)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	assessment, err := decodeAssessment(row.AssessmentJSON)
	if err != nil || assessment == nil {
		return nil, false, err
	}
	return assessment, true, nil
}
