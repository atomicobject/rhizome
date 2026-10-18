package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
)

func (s *Scope) mergeOverlayTypeItemsLocked(ctx context.Context, typeName string, committed []ontology.NodeListItem, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	overlayItems, err := s.overlayTypeItemsLocked(ctx, typeName, req)
	if err != nil {
		return nil, err
	}
	if len(overlayItems) == 0 && !s.hasOverlay() {
		return committed, nil
	}
	touched := s.overlayTouchedPathSetLocked(ctx)
	byKey := map[string]ontology.NodeListItem{}
	for _, item := range committed {
		if touched[item.NotePath] {
			continue
		}
		byKey[nodeListItemKey(item)] = item
	}
	for _, item := range overlayItems {
		byKey[nodeListItemKey(item)] = item
	}
	out := make([]ontology.NodeListItem, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	if len(req.Sort) > 0 {
		if err := s.sortOverlayMergedItemsLocked(ctx, out, req.Sort); err != nil {
			return nil, err
		}
	} else {
		sortNodeListItems(out)
	}
	if req.Offset > 0 {
		if req.Offset >= len(out) {
			return nil, nil
		}
		out = out[req.Offset:]
	}
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

func (s *Scope) overlayTypeItemsLocked(ctx context.Context, typeName string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	if !s.hasOverlay() || s.service.Schema == nil {
		return nil, nil
	}
	acceptedTypes := s.overlayAcceptedTypes(typeName)
	if len(acceptedTypes) == 0 {
		return nil, nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	if index != nil {
		paths := make([]string, 0, len(index.ItemsByType[typeName]))
		for _, item := range index.ItemsByType[typeName] {
			paths = append(paths, item.NotePath)
		}
		if err := s.loadOverlayHostStateLocked(ctx, paths); err != nil {
			return nil, err
		}
		return s.overlayTypeItemsFromIndexLocked(ctx, index, typeName, acceptedTypes, req)
	}
	var out []ontology.NodeListItem
	for path := range s.overlayTouchedPathSetLocked(ctx) {
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			continue
		}
		root, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		if err != nil || root == nil {
			continue
		}
		if fallback, ok := ontology.AsFallbackNoteProjection(root); ok {
			root = fallback
		}
		items, err := s.overlayProjectionTypeItemsLocked(ctx, root, acceptedTypes, req)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		globalRefs, err := ontology.GlobalSourceNodeRefsFromSnapshot(snapshot, s.service.Schema)
		if err == nil {
			for _, ref := range globalRefs {
				projection, err := ontology.ProjectBoundNodeFromSnapshot(snapshot, s.service.Schema, ref)
				if err != nil || projection == nil {
					continue
				}
				items, err := s.overlayProjectionTypeItemsLocked(ctx, projection, acceptedTypes, req)
				if err != nil {
					return nil, err
				}
				out = append(out, items...)
			}
		}
	}
	return out, nil
}

func (s *Scope) overlayTypeItemsFromIndexLocked(ctx context.Context, index *ReadOverlayIndex, typeName string, acceptedTypes map[string]bool, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	if index == nil {
		return nil, nil
	}
	raw := index.ItemsByType[typeName]
	out := make([]ontology.NodeListItem, 0, len(raw))
	for _, item := range raw {
		if !acceptedTypes[item.ResolvedType] && !acceptedTypes[typeName] {
			continue
		}
		record, ok := index.RecordsByRef[nodeListItemKey(item)]
		if !ok {
			continue
		}
		record = s.enrichOverlayRecordLocked(record)
		matches, err := s.recordMatchesPredicates(ctx, record, req.Predicates)
		if err != nil {
			return nil, err
		}
		if matches {
			item.Title = record.Title
			item.UpdatedAt = record.UpdatedAt
			item.HasIssues = record.HasIssues
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Scope) overlayProjectionTypeItemsLocked(ctx context.Context, projection *ontology.NodeProjection, acceptedTypes map[string]bool, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	if projection == nil {
		return nil, nil
	}
	var out []ontology.NodeListItem
	if acceptedTypes[projection.ResolvedType] {
		record := s.nodeRecordFromProjectionLocked(ctx, projection)
		matches, err := s.recordMatchesPredicates(ctx, record, req.Predicates)
		if err != nil {
			return nil, err
		}
		if matches {
			out = append(out, ontology.NodeListItem{
				Ref:          record.Ref,
				Title:        record.Title,
				ResolvedType: record.TypeName,
				NotePath:     record.Ref.NotePath,
				UpdatedAt:    record.UpdatedAt,
				HasIssues:    record.HasIssues,
			})
		}
	}
	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, ref := range projection.Fields[name].SectionNodes {
			child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, s.service.Schema, ref)
			if err != nil || child == nil {
				continue
			}
			items, err := s.overlayProjectionTypeItemsLocked(ctx, child, acceptedTypes, req)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
	}
	return out, nil
}

func (s *Scope) overlayAcceptedTypes(typeName string) map[string]bool {
	out := map[string]bool{}
	if s.service.Schema.Types[typeName] != nil {
		out[typeName] = true
		return out
	}
	if s.service.Schema.Interfaces[typeName] != nil {
		for _, implementor := range s.interfaceImplementors(typeName) {
			out[implementor] = true
		}
	}
	return out
}

func (s *Scope) overlayTouchedPathSetLocked(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if !s.hasOverlay() {
		return out
	}
	if index, err := s.overlayIndexLocked(ctx); err == nil && index != nil {
		for path := range index.TouchedPaths {
			out[path] = true
		}
		return out
	}
	overlay := s.activeOverlay()
	for path := range overlay.Notes {
		if strings.TrimSpace(path) != "" {
			out[strings.TrimSpace(path)] = true
		}
	}
	for path := range overlay.UpdatedContentByPath {
		if strings.TrimSpace(path) != "" {
			out[strings.TrimSpace(path)] = true
		}
	}
	for _, path := range overlay.TouchedPaths {
		if strings.TrimSpace(path) != "" {
			out[strings.TrimSpace(path)] = true
		}
	}
	return out
}

func nodeListItemKey(item ontology.NodeListItem) string {
	if key := RefIdentityKey(item.Ref); key != "" {
		return key
	}
	return item.NotePath
}

// overlayListItemsLocked applies staged note records to a note-level pseudo
// listing: touched notes take their staged title and type, deleted notes drop
// out, and, when includeNew is set, notes created in the session appear.
func (s *Scope) overlayListItemsLocked(ctx context.Context, committed []ontology.NodeListItem, includeNew bool) ([]ontology.NodeListItem, error) {
	touched := s.overlayTouchedPathSetLocked(ctx)
	if len(touched) == 0 {
		return committed, nil
	}
	paths := make([]string, 0, len(touched))
	for path := range touched {
		paths = append(paths, path)
	}
	records, err := s.overlayNoteRecordsLocked(ctx, paths)
	if err != nil {
		return nil, err
	}
	staged := make(map[string]NodeRecord, len(records))
	for _, record := range records {
		staged[record.Ref.NotePath] = record
	}
	out := make([]ontology.NodeListItem, 0, len(committed)+len(staged))
	for _, item := range committed {
		if !touched[item.NotePath] {
			out = append(out, item)
			continue
		}
		record, ok := staged[item.NotePath]
		if !ok {
			// A touched note without a staged record is deleted in the session.
			continue
		}
		delete(staged, item.NotePath)
		out = append(out, allItemFromRecord(record, item))
	}
	if includeNew {
		for _, record := range staged {
			out = append(out, allItemFromRecord(record, ontology.NodeListItem{}))
		}
	}
	sortNodeListItems(out)
	return out, nil
}

func allItemFromRecord(record NodeRecord, committed ontology.NodeListItem) ontology.NodeListItem {
	resolved := record.TypeName
	if ontoschema.OntologyTypeVisibility(resolved) == ontoschema.TypeVisibilityInternal {
		resolved = ""
	}
	return ontology.NodeListItem{
		Ref:          ontology.NodeRef{NotePath: record.Ref.NotePath, Kind: ontology.NodeKindNote},
		Title:        firstNonEmptyString(record.Title, committed.Title, record.Ref.NotePath),
		ResolvedType: resolved,
		NotePath:     record.Ref.NotePath,
		UpdatedAt:    firstNonZero(record.UpdatedAt, committed.UpdatedAt),
		HasIssues:    record.HasIssues || committed.HasIssues,
	}
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
