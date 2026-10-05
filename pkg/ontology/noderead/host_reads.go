package noderead

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func readCancellation(ctx context.Context, err error) error {
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// ensurePathStateLocked loads and memoizes only the requested host-note state.
// Metadata misses are known misses for this scope and do not widen into a vault scan.
func (s *Scope) ensurePathStateLocked(ctx context.Context, pathsList []string) error {
	pathsList = normalizeStrings(pathsList)
	if err := s.loadPathMetadataLocked(ctx, pathsList); err != nil {
		return err
	}
	knownPaths := make([]string, 0, len(pathsList))
	for _, path := range pathsList {
		if _, ok := s.noteRowByPath[path]; ok {
			knownPaths = append(knownPaths, path)
		}
	}
	_, err := s.typesByPathsLocked(ctx, knownPaths)
	if err != nil {
		return err
	}
	assessments, err := s.assessmentsByPathsLocked(ctx, knownPaths)
	if err != nil {
		return err
	}
	for _, path := range knownPaths {
		if ontology.AssessmentHasIssues(assessments[path]) {
			s.issueByPath[path] = true
		}
	}
	return nil
}

func (s *Scope) loadPathMetadataLocked(ctx context.Context, pathsList []string) error {
	missing := make([]string, 0, len(pathsList))
	seen := make(map[string]struct{}, len(pathsList))
	for _, path := range pathsList {
		if s.noteRowKnown[path] {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		load := s.service.Store.CurrentNoteMetadataRowsByPaths
		if s.service.ExactNoteMetadataRows != nil {
			load = s.service.ExactNoteMetadataRows
		}
		rows, err := load(ctx, missing)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
		for _, path := range missing {
			s.noteRowKnown[path] = true
			if row, ok := rows[path]; ok {
				s.noteRowByPath[path] = row
			}
		}
	}
	return nil
}

func (s *Scope) recordsFromSnapshotsLocked(ctx context.Context, pathsList []string) (map[string]NodeRecord, error) {
	out := make(map[string]NodeRecord, len(pathsList))
	for _, path := range pathsList {
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			continue
		}
		fm := snapshot.Frontmatter
		out[path] = NodeRecord{
			Ref:         ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
			Path:        path,
			Title:       noteTitle(path, fm),
			Content:     snapshot.Content,
			Frontmatter: cloneMap(fm),
			InlineProps: ontology.SectionInlineProperties(snapshot.Content),
			Tags:        extractTags(fm),
			TypeName:    stringValue(fm[ontology.TypeFieldName()]),
			UpdatedAt:   s.noteRowByPath[path].Mtime,
			HasIssues:   s.issueByPath[path],
		}
	}
	return out, nil
}

// NoteUpdatedAt returns the indexed modification time, in Unix seconds, of
// each known note path in one batched metadata read. Unknown paths are absent.
func (s *Scope) NoteUpdatedAt(ctx context.Context, pathsList []string) (map[string]int64, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]int64{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pathsList = normalizeStrings(pathsList)
	if err := s.loadPathMetadataLocked(ctx, pathsList); err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(pathsList))
	for _, path := range pathsList {
		if row, ok := s.noteRowByPath[path]; ok {
			out[path] = row.Mtime
		}
	}
	return out, nil
}
