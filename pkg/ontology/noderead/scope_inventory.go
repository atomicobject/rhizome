package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func (s *Scope) ensureNoteState(ctx context.Context) error {
	if s.noteStateLoaded {
		return nil
	}
	if err := s.loadInventoryMetadataLocked(ctx); err != nil {
		return err
	}
	_, err := s.typesByPathsLocked(ctx, s.allNotePaths)
	if err != nil {
		return err
	}
	flags, err := s.inventoryFlagsLocked(ctx)
	if err != nil {
		return err
	}
	for _, path := range s.allNotePaths {
		s.flagKnown[path] = true
		row, ok := flags[path]
		if !ok {
			continue
		}
		s.flagsByPath[path] = row
		if row.HasIssues {
			s.issueByPath[path] = true
		}
	}
	s.noteStateLoaded = true
	return nil
}

// inventoryFlagsLocked returns has-issues/type-ambiguity for every assessment
// row. The materialized columns are trusted only when the persisted
// materialization version is the one this binary writes; a migrated index
// that has not yet been rebuilt carries the migration's default flags, so
// during that window the flags are derived from assessment_json instead.
func (s *Scope) inventoryFlagsLocked(ctx context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	state, err := s.service.Store.GetOntologySchemaState(ctx)
	if err != nil {
		return nil, err
	}
	if state.Ready && state.MaterializationVersion == ontology.OntologyMaterializationVersion {
		flags, err := s.service.Store.OntologyAssessmentFlags(ctx)
		if err != nil {
			return nil, err
		}
		s.diagnostics.AssessmentFlagLoads++
		return flags, nil
	}
	assessments, err := s.assessmentsByPathsLocked(ctx, s.allNotePaths)
	if err != nil {
		return nil, err
	}
	flags := make(map[string]semdb.OntologyAssessmentFlags, len(assessments))
	for path, assessment := range assessments {
		flags[path] = ontology.FlagsForAssessment(assessment)
	}
	return flags, nil
}

// AssessmentFlags returns the materialized inventory flags for every current
// note. The returned map is a copy; callers may not mutate scope state.
func (s *Scope) AssessmentFlags(ctx context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]semdb.OntologyAssessmentFlags{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureNoteState(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]semdb.OntologyAssessmentFlags, len(s.flagsByPath))
	for path, flags := range s.flagsByPath {
		out[path] = flags
	}
	return out, nil
}
