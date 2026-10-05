package noderead

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// RelationSelectionForField compiles an authored neighbor field into the
// relation read used by list and count consumers.
func RelationSelectionForField(schema *ontology.Schema, field *ontology.Field) (RelationSelection, bool) {
	if field == nil || (field.Kind != ontology.FieldKindNeighbor && field.Kind != ontology.FieldKindReverse) || !field.List {
		return RelationSelection{}, false
	}
	targetTypes, targetInterfaces := TargetFiltersForType(schema, field.TypeName)
	selection := RelationSelection{
		Name:             field.Name,
		TargetTypes:      targetTypes,
		TargetInterfaces: targetInterfaces,
		NeighborScope:    field.Scope,
	}
	if field.Kind == ontology.FieldKindReverse {
		selection.RelationNames = []string{field.ReverseField}
		selection.Direction = TraversalDirectionInbound
		selection.IncludeStructural = true
		return selection, true
	}
	if field.Scope == ontology.NeighborScopeSubtree {
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

func traversalDirectionForNeighbor(direction ontology.NeighborDirection) TraversalDirection {
	switch direction {
	case ontology.NeighborDirectionInbound:
		return TraversalDirectionInbound
	case ontology.NeighborDirectionBoth:
		return TraversalDirectionBoth
	default:
		return TraversalDirectionOutbound
	}
}

func neighborDirectionProvenance(direction ontology.NeighborDirection) map[string]struct{} {
	switch direction {
	case ontology.NeighborDirectionOutbound:
		return map[string]struct{}{"body_link": {}}
	case ontology.NeighborDirectionInbound:
		return map[string]struct{}{"backlink": {}}
	default:
		return map[string]struct{}{"body_link": {}, "backlink": {}}
	}
}

// RelationCountSelectionsForField partitions sources by concrete source type
// while preserving one compiled relation shape per type.
func RelationCountSelectionsForField(schema *ontology.Schema, sources []ontology.NodeRef, fieldName string) []RelationSelection {
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
