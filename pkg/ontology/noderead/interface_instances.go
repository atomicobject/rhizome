package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
)

func (s *Scope) interfaceItems(ctx context.Context, typeName string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	implementors := s.interfaceImplementors(typeName)
	if len(implementors) == 0 {
		return nil, nil
	}
	// Collapsed multi-type plan: when every implementor has the predicates'
	// fields with compatible capability, a single n.type_name IN (...) query
	// yields a globally-paged result. Falls back to the per-implementor union
	// when any implementor lacks a predicate field.
	if len(req.Sort) > 0 {
		if !s.allImplementorsSupportSorts(implementors, req.Sort) {
			return nil, fmt.Errorf("interface type %s does not support sorted TypeInstances requests for non-common or non-indexed sort fields", typeName)
		}
		if req.Limit <= 0 {
			return nil, fmt.Errorf("interface type %s sorted TypeInstances requests require a positive limit", typeName)
		}
		if len(req.Predicates) > 0 && !s.allImplementorsSupportPredicates(implementors, req.Predicates) {
			return nil, fmt.Errorf("interface type %s does not support indexed TypeInstances predicates for all implementors", typeName)
		}
		return s.interfaceItemsCollapsed(ctx, implementors, req)
	}
	if len(req.Predicates) > 0 && s.allImplementorsSupportPredicates(implementors, req.Predicates) {
		return s.interfaceItemsCollapsed(ctx, implementors, req)
	}
	return s.interfaceItemsPerImplementor(ctx, implementors, req)
}

// interfaceImplementors returns implementor types in deterministic order.
func (s *Scope) interfaceImplementors(typeName string) []string {
	implementors := make([]string, 0)
	for name, noteType := range s.service.Schema.Types {
		if noteType == nil {
			continue
		}
		for _, iface := range noteType.Implements {
			if iface == typeName {
				implementors = append(implementors, name)
				break
			}
		}
	}
	sort.Strings(implementors)
	return implementors
}

// allImplementorsSupportPredicates reports whether every implementor type has
// the predicate fields with capability-compatible operators. This is the gate
// for a single multi-type plan.
func (s *Scope) allImplementorsSupportPredicates(typeNames []string, predicates []readmodel.FieldPredicate) bool {
	if len(predicates) == 0 || s.service.Schema == nil {
		return false
	}
	for _, predicate := range predicates {
		fieldName := strings.TrimSpace(predicate.FieldName)
		if fieldName == "" {
			continue
		}
		// Builtin path/notePath predicates always work across implementors.
		if fieldName == "notePath" || fieldName == "path" {
			continue
		}
		op := strings.ToLower(strings.TrimSpace(string(predicate.Op)))
		if op == "" {
			op = "eq"
		}
		for _, typeName := range typeNames {
			noteType := s.service.Schema.Types[typeName]
			if noteType == nil {
				return false
			}
			field := noteType.ByName[fieldName]
			if field == nil {
				return false
			}
			capability, ok := ontoschema.FieldQueryCapabilityForField(s.service.Schema, field)
			if !ok {
				return false
			}
			supported := false
			for _, allowed := range capability.IndexedFilterOps {
				if strings.EqualFold(allowed, op) {
					supported = true
					break
				}
			}
			if !supported {
				return false
			}
		}
	}
	return true
}

// allImplementorsSupportSorts reports whether every implementor can satisfy
// the requested sort keys through the indexed catalog with compatible value
// kinds. That proof is required before using a collapsed interface plan,
// because per-implementor merging cannot preserve field-order semantics.
func (s *Scope) allImplementorsSupportSorts(typeNames []string, sorts []readmodel.FieldSort) bool {
	if len(sorts) == 0 || s.service.Schema == nil {
		return false
	}
	for _, sortSpec := range sorts {
		fieldName := strings.TrimSpace(sortSpec.FieldName)
		if fieldName == "" {
			continue
		}
		if strings.EqualFold(fieldName, "notePath") || strings.EqualFold(fieldName, "path") {
			continue
		}
		expectedKind := strings.ToLower(strings.TrimSpace(sortSpec.ValueKind))
		for _, typeName := range typeNames {
			noteType := s.service.Schema.Types[typeName]
			if noteType == nil {
				return false
			}
			field := noteType.ByName[fieldName]
			if field == nil {
				return false
			}
			capability, ok := ontoschema.FieldQueryCapabilityForField(s.service.Schema, field)
			if !ok || !capability.Sortable {
				return false
			}
			if expectedKind != "" && expectedKind != "builtin" && !strings.EqualFold(expectedKind, capability.ValueKind) {
				return false
			}
		}
	}
	return true
}

func (s *Scope) interfaceItemsCollapsed(ctx context.Context, implementors []string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	catalogStore := s.service.Store
	limit := req.Limit
	offset := req.Offset
	if limit <= 0 {
		limit = 0 // Plan requires positive limit; fall back below.
	}
	if limit == 0 {
		// No limit means we cannot use the SQL plan (it requires positive Limit);
		// fall back to per-implementor.
		return s.interfaceItemsPerImplementor(ctx, implementors, req)
	}
	plan := readmodel.NodeQueryPlan{
		TypeNames:  append([]string(nil), implementors...),
		Predicates: append([]readmodel.FieldPredicate(nil), req.Predicates...),
		Sort:       append([]readmodel.FieldSort(nil), req.Sort...),
		Limit:      limit,
		Offset:     offset,
	}
	rows, err := catalogStore.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	out, err := s.catalogListItemsLocked(ctx, rows)
	if err != nil {
		return nil, err
	}
	s.diagnostics.CatalogHits += len(out)
	return out, nil
}

func (s *Scope) interfaceItemsPerImplementor(ctx context.Context, implementors []string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	out := make([]ontology.NodeListItem, 0)
	seen := map[string]struct{}{}
	for _, name := range implementors {
		noteType := s.service.Schema.Types[name]
		if noteType == nil {
			continue
		}
		var items []ontology.NodeListItem
		var err error
		if len(req.Predicates) > 0 || len(req.Sort) > 0 || req.Offset > 0 {
			var ok bool
			limit := req.Limit
			if limit > 0 && req.Offset > 0 {
				limit += req.Offset
			}
			items, ok, err = s.planTypeItems(ctx, name, noteType, TypeInstancesRequest{
				TypeName:   name,
				Limit:      limit,
				Predicates: req.Predicates,
				Sort:       req.Sort,
			})
			if err != nil {
				return nil, err
			}
			if !ok {
				items, err = s.typeItems(ctx, name, noteType, req.Limit)
			}
		} else {
			items, err = s.typeItems(ctx, name, noteType, req.Limit)
		}
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key := item.Ref.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	sortNodeListItems(out)
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
