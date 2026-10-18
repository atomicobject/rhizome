package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// MetadataInventory returns committed indexed metadata for whole-vault queries.
func (s *Scope) MetadataInventory(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadInventoryMetadataLocked(ctx); err != nil {
		return nil, err
	}
	return append([]semdb.NoteMetadataRow(nil), s.noteRows...), nil
}

func (s *Scope) ensureIssueFlagsLocked(ctx context.Context, paths []string) error {
	missing := make([]string, 0, len(paths))
	for _, path := range normalizeStrings(paths) {
		if !s.flagKnown[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	state, err := s.service.Store.GetOntologySchemaState(ctx)
	if err != nil {
		return err
	}
	var flags map[string]semdb.OntologyAssessmentFlags
	if state.Ready && state.MaterializationVersion == ontology.OntologyMaterializationVersion {
		flags, err = s.service.Store.OntologyAssessmentFlagsByPaths(ctx, missing)
		s.diagnostics.AssessmentFlagLoads++
	} else {
		var assessments map[string]*ontology.NoteAssessment
		assessments, err = s.assessmentsByPathsLocked(ctx, missing)
		flags = make(map[string]semdb.OntologyAssessmentFlags, len(assessments))
		for path, a := range assessments {
			flags[path] = ontology.FlagsForAssessment(a)
		}
	}
	if canceled := readCancellation(ctx, err); canceled != nil {
		return canceled
	}
	if err != nil {
		return err
	}
	for _, path := range missing {
		s.flagKnown[path] = true
		s.flagsByPath[path] = flags[path]
		s.issueByPath[path] = flags[path].HasIssues
	}
	return nil
}

func (s *Scope) loadInventoryMetadataLocked(ctx context.Context) error {
	if s.noteRowsLoaded {
		return nil
	}
	rows, err := s.service.Store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	s.noteRows = rows
	s.allNotePaths = make([]string, 0, len(rows))
	for _, row := range rows {
		s.allNotePaths = append(s.allNotePaths, row.Path)
		s.noteRowByPath[row.Path] = row
		s.noteRowKnown[row.Path] = true
	}
	s.noteRowsLoaded = true
	return nil
}
