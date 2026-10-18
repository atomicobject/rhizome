package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

// TypeSummaries counts public schema types and interfaces from one catalog
// read. Items contains at most previewLimit examples; zero requests counts only.
func (s *Scope) TypeSummaries(ctx context.Context, typeNames []string, previewLimit int) (map[string]ontology.TypeListResult, error) {
	results := make(map[string]ontology.TypeListResult, len(typeNames))
	if s == nil || s.service == nil || s.service.Store == nil || s.service.Schema == nil {
		return results, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if previewLimit < 0 {
		previewLimit = 0
	}
	accepted := make(map[string][]string, len(typeNames))
	wanted := map[string]bool{}
	for _, name := range normalizeStrings(typeNames) {
		if ontoschema.OntologyTypeVisibility(name) == ontoschema.TypeVisibilityInternal {
			continue
		}
		doc, err := schemadoc.Type(s.service.Schema, name, identity.CurrentUserType)
		if err != nil {
			return nil, err
		}
		if doc == nil {
			continue
		}
		results[name] = ontology.TypeListResult{TypeDoc: doc}
		types := []string{name}
		if s.service.Schema.Interfaces[name] != nil {
			types = s.interfaceImplementors(name)
		}
		accepted[name] = types
		for _, typeName := range types {
			wanted[typeName] = true
		}
	}
	if len(accepted) == 0 {
		return results, nil
	}
	rows, err := s.service.Store.AllOntologyNodes(ctx)
	if err != nil {
		return nil, err
	}
	selected := make([]readmodel.NodeRow, 0, len(rows))
	rootPaths := make([]string, 0)
	hostPaths := make([]string, 0)
	for _, row := range rows {
		if !wanted[row.TypeName] || (row.NodeKind != string(ontology.NodeKindNote) && row.NodeKind != string(ontology.NodeKindEmbedded)) {
			continue
		}
		selected = append(selected, row)
		hostPaths = append(hostPaths, row.NotePath)
		if row.NodeKind == string(ontology.NodeKindNote) {
			rootPaths = append(rootPaths, row.NotePath)
		}
	}
	if s.hasOverlay() {
		for path := range s.overlayTouchedPathSetLocked(ctx) {
			hostPaths = append(hostPaths, path)
			rootPaths = append(rootPaths, path)
		}
	}
	if err := s.loadPathMetadataLocked(ctx, rootPaths); err != nil {
		return nil, err
	}
	if err := s.ensureIssueFlagsLocked(ctx, hostPaths); err != nil {
		return nil, err
	}
	items, err := s.catalogListItemsLocked(ctx, selected)
	if err != nil {
		return nil, err
	}
	byType := make(map[string][]ontology.NodeListItem, len(wanted))
	for i, item := range items {
		if selected[i].NodeKind == string(ontology.NodeKindNote) {
			item = s.noteRootItem(item.NotePath, item.ResolvedType)
		}
		byType[item.ResolvedType] = append(byType[item.ResolvedType], item)
	}
	for name, types := range accepted {
		combined := make([]ontology.NodeListItem, 0)
		seen := map[string]bool{}
		for _, typeName := range types {
			for _, item := range byType[typeName] {
				key := RefIdentityKey(item.Ref)
				if !seen[key] {
					seen[key] = true
					combined = append(combined, item)
				}
			}
		}
		if s.hasOverlay() {
			combined, err = s.mergeOverlayTypeItemsLocked(ctx, name, combined, TypeInstancesRequest{TypeName: name})
			if err != nil {
				return nil, err
			}
		} else {
			sortNodeListItems(combined)
		}
		result := results[name]
		result.Count = len(combined)
		result.IssueCount = s.issueCountForItems(combined)
		result.Items = append([]ontology.NodeListItem(nil), combined[:min(previewLimit, len(combined))]...)
		results[name] = result
	}
	return results, ctx.Err()
}
