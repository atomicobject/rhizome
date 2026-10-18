package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

func (s *Scope) hasOverlay() bool {
	if s == nil {
		return false
	}
	overlay := s.activeOverlay()
	if overlay == nil {
		return false
	}
	return !overlay.Empty()
}

func (s *Scope) overlayIndexLocked(ctx context.Context) (*ReadOverlayIndex, error) {
	overlay := s.activeOverlay()
	if overlay == nil || overlay.Empty() {
		return nil, nil
	}
	if overlay.Index != nil {
		return overlay.Index, nil
	}
	if s.overlayIndex != nil {
		return s.overlayIndex, nil
	}
	index, err := BuildReadOverlayIndex(ctx, s.service.Schema, overlay)
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	s.overlayIndex = index
	return index, nil
}

func (s *Scope) activeOverlay() *ReadOverlay {
	if s == nil {
		return nil
	}
	return s.opts.ReadOverlay
}

func (s *Scope) overlayNote(notePath string) (ReadOverlayNote, bool) {
	if !s.hasOverlay() {
		return ReadOverlayNote{}, false
	}
	overlay := s.activeOverlay()
	path := strings.TrimSpace(notePath)
	if note, ok := overlay.Notes[path]; ok {
		return note, true
	}
	if content, ok := overlay.UpdatedContentByPath[path]; ok {
		return ReadOverlayNote{Content: content}, true
	}
	return ReadOverlayNote{}, false
}

func (s *Scope) overlayContent(notePath string) (string, bool) {
	note, ok := s.overlayNote(notePath)
	if !ok || note.Deleted {
		return "", false
	}
	return note.Content, true
}

func (s *Scope) overlayHasPath(notePath string) bool {
	if !s.hasOverlay() {
		return false
	}
	path := strings.TrimSpace(notePath)
	overlay := s.activeOverlay()
	if overlay != nil && overlay.Index != nil {
		_, ok := overlay.Index.TouchedPaths[path]
		return ok
	}
	if _, ok := s.overlayNote(path); ok {
		return true
	}
	for _, touched := range overlay.TouchedPaths {
		if strings.TrimSpace(touched) == path {
			return true
		}
	}
	return false
}

func (s *Scope) overlayNoteRecordsLocked(ctx context.Context, pathsList []string) ([]NodeRecord, error) {
	if !s.hasOverlay() {
		return nil, nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NodeRecord, 0, len(pathsList))
	for _, path := range pathsList {
		if !s.overlayHasPath(path) {
			continue
		}
		if index != nil {
			key := RefIdentityKey(ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
			if record, ok := index.RecordsByRef[key]; ok {
				out = append(out, s.enrichOverlayRecordLocked(record))
				continue
			}
			if _, deleted := index.DeletedPaths[strings.TrimSpace(path)]; deleted {
				continue
			}
		}
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			continue
		}
		projection, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		if err != nil || projection == nil {
			continue
		}
		if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
			projection = fallback
		} else if fallback, ok := ontology.AsFallbackSectionProjection(projection); ok {
			projection = fallback
		}
		out = append(out, s.nodeRecordFromProjectionLocked(ctx, projection))
	}
	return out, nil
}

func (s *Scope) overlayRecordsByRefsLocked(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, []ontology.NodeRef, error) {
	index, err := s.overlayIndexLocked(ctx)
	if err != nil || index == nil {
		return nil, refs, err
	}
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, touched := index.TouchedPaths[ref.NotePath]; touched {
			paths = append(paths, ref.NotePath)
		}
	}
	if err := s.loadOverlayHostStateLocked(ctx, paths); err != nil {
		return nil, nil, err
	}
	touchedMissing := make([]ontology.NodeRef, 0)
	touchedPaths := make(map[string]struct{})
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if _, deleted := index.DeletedPaths[ref.NotePath]; deleted {
			continue
		}
		if _, touched := index.TouchedPaths[ref.NotePath]; !touched {
			continue
		}
		if _, ok := index.RecordsByRef[RefIdentityKey(ref)]; ok {
			continue
		}
		touchedMissing = append(touchedMissing, ref)
		touchedPaths[ref.NotePath] = struct{}{}
	}
	aliases := map[string]NodeRecord{}
	if len(touchedMissing) > 0 {
		candidates := make([]NodeRecord, 0)
		for _, record := range index.RecordsByRef {
			if _, ok := touchedPaths[record.Ref.NotePath]; ok {
				candidates = append(candidates, record)
			}
		}
		aliases = indexLoadedRecordsForRequests(touchedMissing, candidates)
	}
	out := make([]NodeRecord, 0, len(refs))
	missing := make([]ontology.NodeRef, 0)
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if _, deleted := index.DeletedPaths[ref.NotePath]; deleted {
			continue
		}
		if _, touched := index.TouchedPaths[ref.NotePath]; !touched {
			missing = append(missing, ref)
			continue
		}
		key := RefIdentityKey(ref)
		if record, ok := index.RecordsByRef[key]; ok {
			out = append(out, s.enrichOverlayRecordLocked(record))
			continue
		}
		if record, ok := aliases[key]; ok {
			out = append(out, s.enrichOverlayRecordLocked(record))
			continue
		}
		missing = append(missing, ref)
	}
	return out, missing, nil
}

func (s *Scope) enrichOverlayRecordLocked(record NodeRecord) NodeRecord {
	record = cloneNodeRecord(record)
	if record.Ref.NotePath == "" {
		return record
	}
	if row, ok := s.noteRowByPath[record.Ref.NotePath]; ok {
		if record.UpdatedAt == 0 {
			record.UpdatedAt = row.Mtime
		}
		if record.Ref.Kind == ontology.NodeKindNote {
			record.HasIssues = s.issueByPath[record.Ref.NotePath]
		}
	}
	if record.HasIssues == false && s.issueByPath[record.Ref.NotePath] {
		record.HasIssues = true
	}
	if record.Ref.Kind == ontology.NodeKindNote && sourceContractEmpty(record) {
		record = s.withSourceContract(record)
	}
	return record
}

// Overlay records use the staged timestamp when supplied, otherwise the indexed
// host timestamp. Load it explicitly so results never depend on earlier reads.
func (s *Scope) loadOverlayHostStateLocked(ctx context.Context, paths []string) error {
	if s.service.Store == nil {
		return nil
	}
	if err := s.loadPathMetadataLocked(ctx, paths); err != nil {
		return err
	}
	return s.ensureIssueFlagsLocked(ctx, paths)
}
