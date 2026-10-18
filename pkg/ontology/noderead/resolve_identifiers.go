package noderead

import (
	"context"
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

const identifierCandidateBatchSize = 64

// preferredIdentifierCandidates caches bounded field-equality lookups, including
// misses. Two matches per type suffice to prove ambiguity without reading the
// remaining instances or their note properties.
func (s *Scope) preferredIdentifierCandidates(ctx context.Context, value string) ([]ontology.NodeRef, error) {
	value = ontoschema.NormalizeIdentifierSemanticValue(value)
	if value == "" || s.service.Schema == nil || s.service.Store == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if refs, ok := s.preferredIdentifiers[value]; ok {
		return append([]ontology.NodeRef(nil), refs...), nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	typeNames := make([]string, 0, len(s.service.Schema.Types))
	for name := range s.service.Schema.Types {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)
	refs := make([]ontology.NodeRef, 0, 2)
	seen := map[string]bool{}
	add := func(ref ontology.NodeRef) {
		if ref.Kind == ontology.NodeKindNote {
			ref = ontology.NodeRef{NotePath: ref.NotePath, Kind: ontology.NodeKindNote, TypeName: ref.TypeName}
		}
		key := RefIdentityKey(ref)
		if !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	for _, typeName := range typeNames {
		noteType := s.service.Schema.Types[typeName]
		if noteType == nil || (noteType.Role != ontoschema.TypeRoleNote && noteType.Role != ontoschema.TypeRoleEmbeddedNode) {
			continue
		}
		preferred := noteType.PreferredIdentifierField()
		if preferred == nil {
			continue
		}
		predicate := readmodel.FieldPredicate{FieldName: preferred.Name, Op: readmodel.FieldOpEq, Values: []readmodel.FieldValueRow{ontology.FieldValueRow(preferred, value)}}
		matches := 0
		for offset := 0; matches < 2; offset += identifierCandidateBatchSize {
			rows, err := s.service.Store.OntologyNodesByTypePlan(ctx, readmodel.NodeQueryPlan{TypeNames: []string{typeName}, Predicates: []readmodel.FieldPredicate{predicate}, Limit: identifierCandidateBatchSize, Offset: offset})
			if err != nil {
				return nil, fmt.Errorf("resolve preferred identifier for %s: %w", typeName, err)
			}
			for _, row := range rows {
				if index != nil {
					if _, touched := index.TouchedPaths[row.NotePath]; touched {
						continue
					}
				}
				add(nodeRefFromCatalogRow(row))
				matches++
				if matches == 2 {
					break
				}
			}
			if len(rows) < identifierCandidateBatchSize {
				break
			}
		}
		if index != nil {
			for _, record := range index.RecordsByRef {
				if record.TypeName != typeName {
					continue
				}
				for _, candidate := range s.FieldValues(record, preferred.Name) {
					if ontoschema.NormalizeIdentifierSemanticValue(candidate) == value {
						add(record.Ref)
						break
					}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool { return RefIdentityKey(refs[i]) < RefIdentityKey(refs[j]) })
	s.preferredIdentifiers[value] = append([]ontology.NodeRef(nil), refs...)
	return append([]ontology.NodeRef(nil), refs...), nil
}

// A basename match keeps precedence unless the matching note itself claims the
// identifier. This exact-path probe also detects a third claimant beyond the
// two candidates sufficient to prove ambiguity.
func (s *Scope) noteClaimsPreferredIdentifier(ctx context.Context, path, value string) (bool, error) {
	value = ontoschema.NormalizeIdentifierSemanticValue(value)
	if value == "" || s.service.Schema == nil || s.service.Store == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := path + "\x00" + value
	if claim, ok := s.preferredIdentifierClaims[key]; ok {
		return claim, nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return false, err
	}
	if index != nil {
		if _, touched := index.TouchedPaths[path]; touched {
			record := index.RecordsByRef[RefIdentityKey(ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})]
			claim := false
			if preferred := s.service.Schema.Types[record.TypeName].PreferredIdentifierField(); preferred != nil {
				for _, candidate := range s.FieldValues(record, preferred.Name) {
					claim = claim || ontoschema.NormalizeIdentifierSemanticValue(candidate) == value
				}
			}
			s.preferredIdentifierClaims[key] = claim
			return claim, nil
		}
	}
	types, err := s.typesByPathsLocked(ctx, []string{path})
	if err != nil {
		return false, err
	}
	noteType := s.service.Schema.Types[types[path].TypeName]
	preferred := noteType.PreferredIdentifierField()
	if preferred == nil {
		s.preferredIdentifierClaims[key] = false
		return false, nil
	}
	rows, err := s.service.Store.OntologyNodesByTypePlan(ctx, readmodel.NodeQueryPlan{
		TypeNames: []string{types[path].TypeName}, Limit: 1,
		Predicates: []readmodel.FieldPredicate{
			{FieldName: preferred.Name, Op: readmodel.FieldOpEq, Values: []readmodel.FieldValueRow{ontology.FieldValueRow(preferred, value)}},
			{FieldName: "notePath", Op: readmodel.FieldOpEq, Values: []readmodel.FieldValueRow{{ValueText: path, ValueNorm: path}}},
		},
	})
	if canceled := readCancellation(ctx, err); canceled != nil {
		return false, canceled
	}
	if err != nil {
		return false, err
	}
	claim := len(rows) > 0
	s.preferredIdentifierClaims[key] = claim
	return claim, nil
}
