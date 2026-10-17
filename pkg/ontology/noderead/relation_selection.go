package noderead

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// RelationSelectionForField compiles an authored neighbor field into the
// relation read used by list and count consumers.
func RelationSelectionForField(schema *ontoschema.Schema, field *ontoschema.Field) (RelationSelection, bool) {
	if field == nil || (field.Kind != ontoschema.FieldKindNeighbor && field.Kind != ontoschema.FieldKindReverse) || !field.List {
		return RelationSelection{}, false
	}
	targetTypes, targetInterfaces := TargetFiltersForType(schema, field.TypeName)
	selection := RelationSelection{
		Name:             field.Name,
		TargetTypes:      targetTypes,
		TargetInterfaces: targetInterfaces,
		NeighborScope:    field.Scope,
	}
	if field.Kind == ontoschema.FieldKindReverse {
		selection.RelationNames = []string{field.ReverseField}
		selection.Direction = TraversalDirectionInbound
		selection.IncludeStructural = true
		return selection, true
	}
	if field.Scope == ontoschema.NeighborScopeSubtree {
		selection.RelationNames = []string{field.Name}
		selection.Direction = traversalDirectionForNeighbor(field.Direction)
		selection.IncludeStructural = true
		return selection, true
	}
	selection.Direction = TraversalDirectionOutbound
	selection.Provenance = neighborDirectionProvenance(field.Direction)
	selection.IncludeAmbient = true
	return selection, true
}

func traversalDirectionForNeighbor(direction ontoschema.NeighborDirection) TraversalDirection {
	switch direction {
	case ontoschema.NeighborDirectionInbound:
		return TraversalDirectionInbound
	case ontoschema.NeighborDirectionBoth:
		return TraversalDirectionBoth
	default:
		return TraversalDirectionOutbound
	}
}

func neighborDirectionProvenance(direction ontoschema.NeighborDirection) map[string]struct{} {
	switch direction {
	case ontoschema.NeighborDirectionOutbound:
		return map[string]struct{}{"body_link": {}}
	case ontoschema.NeighborDirectionInbound:
		return map[string]struct{}{"backlink": {}}
	default:
		return map[string]struct{}{"body_link": {}, "backlink": {}}
	}
}

// RelationCountSelectionsForField partitions sources by concrete source type
// while preserving one compiled relation shape per type.
func RelationCountSelectionsForField(schema *ontoschema.Schema, sources []ontology.NodeRef, fieldName string) []RelationSelection {
	if schema == nil || strings.TrimSpace(fieldName) == "" {
		return nil
	}
	byType := map[string][]ontology.NodeRef{}
	for _, source := range sources {
		if source.TypeName != "" {
			byType[source.TypeName] = append(byType[source.TypeName], source)
		}
	}
	out := make([]RelationSelection, 0, len(byType))
	typeNames := make([]string, 0, len(byType))
	for typeName := range byType {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)
	for _, typeName := range typeNames {
		refs := byType[typeName]
		noteType := schema.Types[typeName]
		if noteType == nil {
			continue
		}
		selection, ok := RelationSelectionForField(schema, noteType.ByName[fieldName])
		if !ok {
			continue
		}
		selection.SourceTypes = []string{typeName}
		selection.Sources = refs
		out = append(out, selection)
	}
	return out
}
