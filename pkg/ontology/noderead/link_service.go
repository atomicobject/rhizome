package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"sort"
)

func (s *Scope) linkServiceLocked(ctx context.Context) (*ontology.NodeLinkService, error) {
	if s.nodeLinkService != nil {
		return s.nodeLinkService, nil
	}
	cache, err := s.notePathCacheLocked(ctx)
	if err != nil {
		return nil, err
	}
	var notes []string
	if cache != nil {
		notes = make([]string, 0, len(cache.NotePaths))
		for path := range cache.NotePaths {
			notes = append(notes, path)
		}
	}
	sort.Strings(notes)
	s.nodeLinkService = (&ontology.NodeLinkService{VaultDef: s.service.VaultDef, NoteReader: s.service.NoteReader, Schema: s.service.Schema, LoadSnapshot: s.snapshotForPathLocked}).WithNotePaths(notes)
	return s.nodeLinkService, nil
}
