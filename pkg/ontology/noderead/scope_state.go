package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// TypesByPaths returns cached ontology type rows for note paths.
func (s *Scope) TypesByPaths(ctx context.Context, pathsList []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]semdb.OntologyNoteTypeRow{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.typesByPathsLocked(ctx, pathsList)
}

func (s *Scope) typesByPathsLocked(ctx context.Context, pathsList []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	pathsList = normalizeStrings(pathsList)
	missing := make([]string, 0, len(pathsList))
	out := make(map[string]semdb.OntologyNoteTypeRow, len(pathsList))
	for _, path := range pathsList {
		if s.typeRowsKnown[path] {
			s.diagnostics.TypeHits++
			if row, ok := s.typeRowsByPath[path]; ok {
				out[path] = row
			}
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		rows, err := s.service.Store.OntologyTypesByPaths(ctx, missing)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}
		s.diagnostics.TypeLoads++
		for _, path := range missing {
			s.typeRowsKnown[path] = true
			if row, ok := rows[path]; ok {
				s.typeRowsByPath[path] = row
				out[path] = row
			}
		}
	}
	return out, nil
}

// AssessmentsByPaths returns cached ontology validation assessments for note paths.
func (s *Scope) AssessmentsByPaths(ctx context.Context, pathsList []string) (map[string]*ontology.NoteAssessment, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]*ontology.NoteAssessment{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.assessmentsByPathsLocked(ctx, pathsList)
}

func (s *Scope) assessmentsByPathsLocked(ctx context.Context, pathsList []string) (map[string]*ontology.NoteAssessment, error) {
	pathsList = normalizeStrings(pathsList)
	missing := make([]string, 0, len(pathsList))
	out := make(map[string]*ontology.NoteAssessment, len(pathsList))
	for _, path := range pathsList {
		if s.assessmentKnown[path] {
			s.diagnostics.AssessmentHits++
			if assessment := s.assessmentByPath[path]; assessment != nil {
				out[path] = assessment
			}
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		rows, err := s.service.Store.OntologyAssessmentsByPaths(ctx, missing)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}
		s.diagnostics.AssessmentLoads++
		decoded := make(map[string]*ontology.NoteAssessment, len(rows))
		for _, path := range missing {
			row, ok := rows[path]
			if !ok {
				continue
			}
			assessment, err := ontology.AssessmentFromJSON(row.AssessmentJSON)
			if err != nil {
				return nil, err
			}
			decoded[path] = assessment
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, path := range missing {
			s.assessmentKnown[path] = true
			if assessment := decoded[path]; assessment != nil {
				s.assessmentByPath[path] = assessment
				out[path] = assessment
			}
		}
	}
	return out, nil
}
