package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"strings"
)

// snapshotForPathLocked returns a cached DocumentSnapshot for a note path, loading
// once on first miss. The caller must hold s.mu.
func (s *Scope) snapshotForPathLocked(ctx context.Context, notePath string) (*ontology.DocumentSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(notePath) == "" {
		return nil, nil
	}
	if s.hasOverlay() {
		index, err := s.overlayIndexLocked(ctx)
		if err != nil {
			return nil, err
		}
		if index != nil {
			path := strings.TrimSpace(notePath)
			if _, deleted := index.DeletedPaths[path]; deleted {
				return nil, nil
			}
			if snapshot, ok := index.SnapshotsByPath[path]; ok {
				return snapshot, nil
			}
		}
	}
	if note, ok := s.overlayNote(notePath); ok {
		key := "overlay:" + strings.TrimSpace(notePath)
		if cached, ok := s.snapshotByPath[key]; ok {
			return cached, nil
		}
		if err, ok := s.snapshotKnownErr[key]; ok {
			return nil, err
		}
		if note.Deleted {
			s.snapshotByPath[key] = nil
			return nil, nil
		}
		snapshot, err := markdownOverlaySnapshotCompat(s.activeOverlay(), notePath, note)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			s.snapshotKnownErr[key] = err
			return nil, err
		}
		s.snapshotByPath[key] = snapshot
		return snapshot, nil
	}
	if cached, ok := s.snapshotByPath[notePath]; ok {
		return cached, nil
	}
	if err, ok := s.snapshotKnownErr[notePath]; ok {
		return nil, err
	}
	if s.service.Store == nil {
		// A raw NoteReader has no provider identity. Do not make it an implicit
		// Markdown parser route.
		s.snapshotByPath[notePath] = nil
		return nil, nil
	}
	if err := s.ensurePathStateLocked(ctx, []string{notePath}); err != nil {
		return nil, err
	}
	row, ok := s.noteRowByPath[notePath]
	if !ok || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		s.snapshotByPath[notePath] = nil
		return nil, nil
	}
	snapshot, root, err := s.markdownStructuralSnapshotCompat(ctx, row)
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		s.snapshotKnownErr[notePath] = err
		return nil, err
	}
	if root != nil {
		root.MarkdownStructure = snapshot
		s.rootSnapshotByPath[notePath] = root
	}
	s.snapshotByPath[notePath] = snapshot
	return snapshot, nil
}
