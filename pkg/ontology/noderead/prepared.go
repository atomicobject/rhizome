package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// One scope sees one host generation. Preview bytes have a separate key and
// never acquire committed provider facts or a committed prepared document.
func (s *Scope) preparedForPathLocked(ctx context.Context, path string) (*ontology.PreparedDocument, error) {
	if s.service == nil || s.service.Store == nil {
		return nil, nil
	}
	key := path
	if _, ok := s.overlayNote(path); ok {
		key = "overlay:" + path
	}
	if prepared, known := s.preparedByPath[key]; known {
		return prepared, nil
	}
	if s.hasOverlay() {
		if index, err := s.overlayIndexLocked(ctx); err != nil {
			return nil, err
		} else if index != nil {
			if _, deleted := index.DeletedPaths[path]; deleted {
				s.preparedByPath[key] = nil
				return nil, nil
			}
			if snapshot, ok := index.SnapshotsByPath[path]; ok {
				prepared, err := snapshot.Prepare(s.service.Schema)
				if err == nil {
					s.preparedByPath[key] = prepared
				}
				return prepared, err
			}
		}
	}
	if _, ok := s.overlayNote(path); ok {
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if err != nil || snapshot == nil {
			return nil, err
		}
		prepared, err := snapshot.Prepare(s.service.Schema)
		if err == nil {
			s.preparedByPath[key] = prepared
		}
		return prepared, err
	}
	if err := s.ensurePathStateLocked(ctx, []string{path}); err != nil {
		return nil, err
	}
	row, ok := s.noteRowByPath[path]
	if !ok || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		s.preparedByPath[key] = nil
		return nil, nil
	}
	format := noteformat.FormatID(row.FormatID)
	if _, registered := s.service.NoteFormats.Provider(format); registered {
		root, err := s.providerRootSnapshotLocked(ctx, row)
		if err != nil || root == nil {
			return nil, err
		}
		prepared, err := ontology.PrepareDocument(root, s.service.Schema)
		if err == nil {
			s.preparedByPath[key] = prepared
		}
		return prepared, err
	}
	if _, selected := s.service.NoteFormats.ProviderForPath(paths.RelPath(path)); selected {
		s.preparedByPath[key] = nil
		return nil, nil
	}
	snapshot, err := s.snapshotForPathLocked(ctx, path)
	if err != nil || snapshot == nil {
		return nil, err
	}
	prepared, err := snapshot.Prepare(s.service.Schema)
	if err == nil {
		s.preparedByPath[key] = prepared
	}
	return prepared, err
}
