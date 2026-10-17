package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

// Canonical catalog identities distinguish SECTION from EMBEDDED endpoints and
// supply embedded types. Host-note types apply only to NOTE targets.
func (s *Scope) filterNeighborhoodTargetsLocked(ctx context.Context, edges []NeighborhoodEdge, req NeighborhoodRequest) ([]NeighborhoodEdge, error) {
	types := stringSet(req.TargetTypes)
	kinds := nodeKindSet(req.TargetKinds)
	paths := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.Target.Kind == ontology.NodeKindNote && edge.Target.TypeName == "" && edge.TargetType == "" {
			paths = append(paths, edge.Target.NotePath)
		}
	}
	noteTypes, err := s.typesByPathsLocked(ctx, paths)
	if err != nil {
		return nil, err
	}
	out := edges[:0]
	for _, edge := range edges {
		if len(kinds) > 0 {
			if _, ok := kinds[edge.Target.Kind]; !ok {
				continue
			}
		}
		typeName := firstNonEmptyString(edge.Target.TypeName, edge.TargetType)
		if typeName == "" && edge.Target.Kind == ontology.NodeKindNote {
			typeName = noteTypes[edge.Target.NotePath].TypeName
		}
		if typeName == "" && edge.Target.Kind != ontology.NodeKindNote && (len(types) > 0 || len(req.TargetInterfaces) > 0) {
			return nil, fmt.Errorf("embedded relation endpoint %s in %s has no resolvable type", edge.Target.NodeID, edge.Target.NotePath)
		}
		if len(types) > 0 {
			if _, ok := types[typeName]; !ok {
				continue
			}
		}
		if len(req.TargetInterfaces) > 0 && !s.typeMatchesAnyInterface(typeName, req.TargetInterfaces) {
			continue
		}
		edge.TargetType = typeName
		edge.Target.TypeName = typeName
		out = append(out, edge)
	}
	return out, nil
}

func (s *Scope) typeMatchesAnyInterface(typeName string, interfaces []string) bool {
	typeName = strings.TrimSpace(typeName)
	if s == nil || s.service == nil || s.service.Schema == nil || typeName == "" {
		return false
	}
	doc := s.service.Schema.Types[typeName]
	if doc == nil {
		return false
	}
	interfaceSet := stringSet(interfaces)
	for _, name := range doc.Implements {
		if _, ok := interfaceSet[name]; ok {
			return true
		}
	}
	return false
}

// TargetFiltersForType routes schema interface names to TargetInterfaces and
// concrete type names to TargetTypes for Neighborhood and Expansion requests.
func TargetFiltersForType(schema *ontoschema.Schema, typeName string) ([]string, []string) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	if schema != nil && schema.Interfaces[typeName] != nil {
		return nil, []string{typeName}
	}
	return []string{typeName}, nil
}

func stringSet(values []string) map[string]struct{} {
	values = normalizeStrings(values)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func nodeKindSet(values []ontology.NodeKind) map[ontology.NodeKind]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[ontology.NodeKind]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func hasPath(paths map[string]struct{}, path string) bool {
	_, ok := paths[path]
	return ok
}
