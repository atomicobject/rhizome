package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"strings"
)

const overlayTypeInstancesPlanLimit = 10000

// TypeInstances lists note-root and embedded instances for one ontology type.
//
// Catalog rows are preferred for embedded nodes; projection fallback is kept
// request-scoped so broad type views do not repeatedly parse the same notes.
//
// When req.Predicates/Sort/Offset are supplied for concrete types, this routes
// through the indexed plan path so filters and sort apply before the limit cap.
// Interface predicates/sort are collapsed into a single multi-type indexed
// query only when every implementor supports the requested fields; otherwise
// callers get an explicit unsupported error for sort because NodeListItem does
// not carry enough field-value data to apply a correct global sort afterward.
// Plan requests are not cached because the result depends on the predicate set.
func (s *Scope) TypeInstances(ctx context.Context, req TypeInstancesRequest) (ontology.TypeListResult, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return ontology.TypeListResult{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	typeName := strings.TrimSpace(req.TypeName)
	if typeName == ontology.TypeScopeAll || typeName == ontology.TypeScopeIssues {
		if err := s.ensureNoteState(ctx); err != nil {
			return ontology.TypeListResult{}, err
		}
	}
	useOverlay := s.hasOverlay()
	usePlan := len(req.Predicates) > 0 || len(req.Sort) > 0 || req.Offset > 0
	cacheKey := fmt.Sprintf("%s:%d", typeName, req.Limit)
	if !usePlan && !useOverlay {
		if result, ok := s.typeInstancesByID[cacheKey]; ok {
			return cloneTypeListResult(result), nil
		}
	}
	switch typeName {
	case "":
		return ontology.TypeListResult{}, nil
	case ontology.TypeScopeAll:
		result := s.listAll()
		if useOverlay {
			items, err := s.overlayListItemsLocked(ctx, result.Items, true)
			if err != nil {
				return ontology.TypeListResult{}, err
			}
			result.Items = items
			result.Count = len(items)
			return result, nil
		}
		if !usePlan {
			s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
		}
		return result, nil
	case ontology.TypeScopeIssues:
		result := s.listIssues()
		if useOverlay {
			// Issue state comes from committed assessments, so staged notes
			// update or drop out here but new staged notes do not appear.
			items, err := s.overlayListItemsLocked(ctx, result.Items, false)
			if err != nil {
				return ontology.TypeListResult{}, err
			}
			result.Items = items
			result.Count = len(items)
			return result, nil
		}
		if !usePlan {
			s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
		}
		return result, nil
	}
	if ontoschema.OntologyTypeVisibility(typeName) == ontoschema.TypeVisibilityInternal {
		return ontology.TypeListResult{}, nil
	}
	doc, err := schemadoc.Type(s.service.Schema, typeName, identity.CurrentUserType)
	if err != nil {
		return ontology.TypeListResult{}, err
	}
	if doc == nil || s.service.Schema == nil {
		return ontology.TypeListResult{}, nil
	}
	committedReq := req
	if useOverlay {
		committedReq.Offset = 0
		committedReq.Limit = 0
		if usePlan {
			committedReq.Limit = overlayTypeInstancesPlanLimit + 1
		}
	}
	if s.service.Schema.Interfaces[typeName] != nil {
		items, err := s.interfaceItems(ctx, typeName, committedReq)
		if err != nil {
			return ontology.TypeListResult{}, err
		}
		if useOverlay {
			if usePlan && len(items) > overlayTypeInstancesPlanLimit {
				return ontology.TypeListResult{}, fmt.Errorf("overlay type listing exceeds %d committed candidates; narrow the requested filter", overlayTypeInstancesPlanLimit)
			}
			items, err = s.mergeOverlayTypeItemsLocked(ctx, typeName, items, req)
			if err != nil {
				return ontology.TypeListResult{}, err
			}
		}
		result := ontology.TypeListResult{TypeDoc: doc, Count: len(items), IssueCount: s.issueCountForItems(items), Items: items}
		if !usePlan && !useOverlay {
			s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
		}
		return result, nil
	}
	noteType := s.service.Schema.Types[typeName]
	if noteType == nil {
		return ontology.TypeListResult{}, nil
	}
	if usePlan {
		items, ok, err := s.planTypeItems(ctx, typeName, noteType, committedReq)
		if err != nil {
			return ontology.TypeListResult{}, err
		}
		if ok {
			if useOverlay {
				if len(items) > overlayTypeInstancesPlanLimit {
					return ontology.TypeListResult{}, fmt.Errorf("overlay type listing exceeds %d committed candidates; narrow the requested filter", overlayTypeInstancesPlanLimit)
				}
				items, err = s.mergeOverlayTypeItemsLocked(ctx, typeName, items, req)
				if err != nil {
					return ontology.TypeListResult{}, err
				}
			}
			return ontology.TypeListResult{TypeDoc: doc, Count: len(items), IssueCount: s.issueCountForItems(items), Items: items}, nil
		}
	}
	items, err := s.typeItems(ctx, typeName, noteType, committedReq.Limit)
	if err != nil {
		return ontology.TypeListResult{}, err
	}
	if useOverlay {
		items, err = s.mergeOverlayTypeItemsLocked(ctx, typeName, items, req)
		if err != nil {
			return ontology.TypeListResult{}, err
		}
	}
	result := ontology.TypeListResult{TypeDoc: doc, Count: len(items), IssueCount: s.issueCountForItems(items), Items: items}
	if !usePlan && !useOverlay {
		s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
	}
	return result, nil
}

// planTypeItems pushes filter/sort/page through the indexed plan when the
// catalog has rows for the type. Returns (nil, false, nil) when the store is
// not catalog-backed; callers fall back to the non-plan path.
func (s *Scope) planTypeItems(ctx context.Context, typeName string, noteType *ontoschema.NoteType, req TypeInstancesRequest) ([]ontology.NodeListItem, bool, error) {
	catalogStore := s.service.Store
	plan := readmodel.NodeQueryPlan{
		TypeNames:  []string{typeName},
		Predicates: append([]readmodel.FieldPredicate(nil), req.Predicates...),
		Sort:       append([]readmodel.FieldSort(nil), req.Sort...),
		Limit:      req.Limit,
		Offset:     req.Offset,
	}
	rows, err := catalogStore.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return nil, true, err
	}
	items, err := s.catalogListItemsLocked(ctx, rows)
	if err != nil {
		return nil, true, err
	}
	s.diagnostics.CatalogHits += len(items)
	return items, true, nil
}

func (s *Scope) typeItems(ctx context.Context, typeName string, noteType *ontoschema.NoteType, limit int) ([]ontology.NodeListItem, error) {
	if noteType != nil && noteType.Role == ontoschema.TypeRoleEmbeddedNode {
		items, _, err := s.catalogTypeItems(ctx, typeName, limit)
		return items, err
	}
	pathsList, err := s.service.Store.OntologyPathsByType(ctx, typeName, limit)
	if err != nil {
		return nil, err
	}
	if err := s.loadPathMetadataLocked(ctx, pathsList); err != nil {
		return nil, err
	}
	if err := s.ensureIssueFlagsLocked(ctx, pathsList); err != nil {
		return nil, err
	}
	items := make([]ontology.NodeListItem, 0, len(pathsList))
	for _, path := range pathsList {
		items = append(items, s.noteRootItem(path, typeName))
	}
	sortNodeListItems(items)
	return items, nil
}

func (s *Scope) catalogTypeItems(ctx context.Context, typeName string, limit int) ([]ontology.NodeListItem, bool, error) {
	catalogStore := s.service.Store
	var rows []readmodel.NodeRow
	var err error
	if limit > 0 {
		rows, err = catalogStore.OntologyNodesByTypePlan(ctx, readmodel.NodeQueryPlan{TypeNames: []string{typeName}, Limit: limit})
	} else {
		rows, err = catalogStore.OntologyNodesByType(ctx, typeName)
	}
	if err != nil {
		return nil, true, err
	}
	if len(rows) == 0 {
		return nil, true, nil
	}
	items, err := s.catalogListItemsLocked(ctx, rows)
	if err != nil {
		return nil, true, err
	}
	sortNodeListItems(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	s.diagnostics.CatalogHits += len(items)
	return items, true, nil
}
