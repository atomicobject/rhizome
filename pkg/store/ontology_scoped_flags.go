package store

import (
	"context"
	"fmt"
	"strings"
)

// OntologyAssessmentFlagsByPaths reads materialized flags only for requested notes.
func (s *Store) OntologyAssessmentFlagsByPaths(ctx context.Context, paths []string) (map[string]OntologyAssessmentFlags, error) {
	paths = normalizeNonEmptyStrings(paths)
	out := make(map[string]OntologyAssessmentFlags, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		query := fmt.Sprintf("SELECT note_path, has_issues, type_ambiguous FROM ontology_note_assessments WHERE note_path IN (%s)", strings.TrimSuffix(strings.Repeat("?,", len(batch)), ","))
		rows, err := s.db.QueryContext(ctx, query, sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path string
			var flags OntologyAssessmentFlags
			if err := rows.Scan(&path, &flags.HasIssues, &flags.TypeAmbiguous); err != nil {
				rows.Close()
				return nil, err
			}
			out[path] = flags
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
