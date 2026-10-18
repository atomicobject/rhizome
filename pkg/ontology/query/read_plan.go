package query

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) executeNodeReadPlan(ctx context.Context, records []*noteRecord, set ast.SelectionSet) {
	if e == nil || len(records) == 0 || len(set) == 0 {
		return
	}
	if e.loaders == nil || e.loaders.scope == nil {
		return
	}
	roots := nodeReadRootsForRecords(records)
	if len(roots) == 0 {
		return
	}
	sourceTypes := possibleTypesForRecords(records)
	rootRefs := make([]ontology.NodeRef, 0, len(roots))
	for _, root := range roots {
		rootRefs = append(rootRefs, root.Ref)
	}
	e.prefetchRelationCounts(ctx, rootRefs, set)
	relations := e.compileNodeRelationSelections(set, sourceTypes)
	fields := e.compileNodeFieldSelection(set, sourceTypes)
	if len(relations) == 0 && len(fields.Names) == 0 && !fields.IncludeBuiltin && !fields.IncludeContent && !fields.IncludeIssues && !fields.IncludeLocators {
		return
	}
	result, err := e.loaders.scope.Execute(ctx, noderead.NodeReadPlan{
		Roots:       roots,
		Fields:      fields,
		Relations:   relations,
		Diagnostics: false,
	})
	if err != nil {
		e.addError(nil, err.Error())
		return
	}
	e.loaders.rememberNodeViews(result.Roots)
	e.loaders.rememberNodeViews(result.Nodes)
	for _, group := range result.Groups {
		noteType := e.schema.Types[group.Source.TypeName]
		if noteType == nil {
			continue
		}
		fieldName, _, _ := strings.Cut(group.Name, relationCacheSeparator)
		field := noteType.ByName[fieldName]
		if field == nil || field.Kind != ontoschema.FieldKindReverse {
			continue
		}
		key := noderead.RefIdentityKey(group.Source)
		if e.reverseRelations == nil {
			e.reverseRelations = map[string]map[string][]ontology.NodeRef{}
		}
		if e.reverseRelations[key] == nil {
			e.reverseRelations[key] = map[string][]ontology.NodeRef{}
		}
		refs := make([]ontology.NodeRef, 0, len(group.Edges))
		for _, edge := range group.Edges {
			refs = append(refs, edge.Target)
		}
		e.reverseRelations[key][group.Name] = refs
	}
}

func (e *executor) compileNodeRelationSelections(set ast.SelectionSet, sourceTypes []string) []noderead.RelationSelection {
	if e == nil || len(set) == 0 {
		return nil
	}
	out := make([]noderead.RelationSelection, 0)
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			out = append(out, e.compileNodeRelationField(current, sourceTypes)...)
		case *ast.InlineFragment:
			fragmentTypes := sourceTypes
			if current.TypeCondition != "" && current.TypeCondition != "NoteNode" {
				fragmentTypes = intersectStrings(sourceTypes, e.possibleConcreteTypes(current.TypeCondition))
			}
			out = append(out, e.compileNodeRelationSelections(current.SelectionSet, fragmentTypes)...)
		}
	}
	return out
}

func (e *executor) prefetchRelationCounts(ctx context.Context, refs []ontology.NodeRef, set ast.SelectionSet) {
	if e == nil || e.loaders == nil || e.loaders.scope == nil || len(refs) == 0 {
		return
	}
	fields := e.selectedRelationCountFields(set, refs)
	for _, fieldName := range fields {
		selections := noderead.RelationCountSelectionsForField(e.schema, refs, fieldName)
		if len(selections) == 0 {
			continue
		}
		result, err := e.loaders.scope.RelationCounts(ctx, noderead.RelationCountsRequest{Sources: refs, Selections: selections})
		if err != nil {
			e.addError(nil, err.Error())
			continue
		}
		for _, item := range result.Sources {
			key := noderead.RefIdentityKey(item.Source)
			if e.relationCounts[key] == nil {
				e.relationCounts[key] = map[string]int{}
			}
			e.relationCounts[key][fieldName] = item.Count
		}
	}
}

func (e *executor) selectedRelationCountFields(set ast.SelectionSet, refs []ontology.NodeRef) []string {
	types := map[string]struct{}{}
	for _, ref := range refs {
		if ref.TypeName != "" {
			types[ref.TypeName] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	var visit func(ast.SelectionSet)
	visit = func(current ast.SelectionSet) {
		for _, selection := range current {
			switch value := selection.(type) {
			case *ast.Field:
				for typeName := range types {
					if relationField, ok := e.relationFieldForCount(typeName, value.Name); ok {
						seen[relationField.Name] = struct{}{}
					}
				}
			case *ast.InlineFragment:
				visit(value.SelectionSet)
			case *ast.FragmentSpread:
				if fragment := e.fragments.ForName(value.Name); fragment != nil {
					visit(fragment.SelectionSet)
				}
			}
		}
	}
	visit(set)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (e *executor) relationFieldForCount(typeName, countName string) (*ontoschema.Field, bool) {
	if e == nil || e.schema == nil || !strings.HasSuffix(countName, "Count") {
		return nil, false
	}
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.ByName[countName] != nil {
		return nil, false
	}
	field := noteType.ByName[strings.TrimSuffix(countName, "Count")]
	return field, field != nil && (field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse) && field.List
}

func (e *executor) relationCount(ref ontology.NodeRef, typeName, countName string) (int, bool) {
	field, ok := e.relationFieldForCount(typeName, countName)
	if !ok {
		return 0, false
	}
	counts := e.relationCounts[noderead.RefIdentityKey(ref)]
	if counts == nil {
		return 0, true
	}
	return counts[field.Name], true
}

func (e *executor) compileNodeRelationField(field *ast.Field, sourceTypes []string) []noderead.RelationSelection {
	if field == nil {
		return nil
	}
	switch field.Name {
	case "linked", "backlinked", "connected":
		args := e.fieldArgs(field)
		typeFilter := strings.TrimSpace(stringValue(args["type"]))
		first := intValue(args["first"], 20)
		if first <= 0 {
			first = 20
		}
		loadType := typeFilter
		if _, ok := e.schema.Interfaces[typeFilter]; ok {
			loadType = ""
		}
		targetTypes := e.possibleConcreteTypes(typeFilter)
		return []noderead.RelationSelection{{
			Name:           field.Name,
			SourceTypes:    cloneStrings(sourceTypes),
			Provenance:     builtinProvenance(field.Name),
			IncludeAmbient: true,
			TargetTypes:    optionalSingle(loadType),
			FirstPerSource: first,
			Fields:         e.compileNodeFieldSelection(field.SelectionSet, targetTypes),
			Children:       e.compileNodeRelationSelections(field.SelectionSet, targetTypes),
		}}
	}
	out := make([]noderead.RelationSelection, 0)
	for _, typeName := range sourceTypes {
		noteType := e.schema.Types[typeName]
		if noteType == nil {
			continue
		}
		schemaField := noteType.ByName[field.Name]
		if schemaField == nil {
			continue
		}
		switch schemaField.Kind {
		case ontoschema.FieldKindLink:
			out = append(out, noderead.RelationSelection{
				Name:              schemaField.Name,
				SourceTypes:       []string{typeName},
				RelationNames:     []string{schemaField.Name},
				IncludeStructural: true,
				Fields:            e.compileNodeFieldSelection(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName)),
				Children:          e.compileNodeRelationSelections(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName)),
			})
		case ontoschema.FieldKindNeighbor, ontoschema.FieldKindReverse:
			selection, ok := noderead.RelationSelectionForField(e.schema, schemaField)
			if !ok {
				continue
			}
			first := e.relationFirst(field)
			selection.SourceTypes = []string{typeName}
			selection.FirstPerSource = first
			selection.Name = relationCacheName(schemaField.Name, first)
			selection.Fields = e.compileNodeFieldSelection(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName))
			selection.Children = e.compileNodeRelationSelections(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName))
			out = append(out, selection)
		}
	}
	return out
}

func (e *executor) compileNodeFieldSelection(set ast.SelectionSet, sourceTypes []string) noderead.FieldSelection {
	fields := noderead.FieldSelection{}
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			fields.Names = append(fields.Names, current.Name)
			switch current.Name {
			case "path", "title", "frontmatter", "tags", "format", "sourceRepresentation", "evidenceRepresentation", "sourceCapabilities":
				fields.IncludeBuiltin = true
			case "content":
				fields.IncludeBuiltin = true
				fields.IncludeContent = true
			case "locator":
				fields.IncludeBuiltin = true
				fields.IncludeLocators = true
			case "linked", "backlinked", "connected":
				continue
			default:
				for _, sourceType := range sourceTypes {
					noteType := e.schema.Types[sourceType]
					if noteType == nil {
						continue
					}
					schemaField := noteType.ByName[current.Name]
					if schemaField == nil {
						continue
					}
					fields.IncludeBuiltin = true
					if schemaField.Kind == ontoschema.FieldKindSection {
						fields.IncludeContent = true
					}
				}
			}
		case *ast.InlineFragment:
			fragmentTypes := sourceTypes
			if current.TypeCondition != "" && current.TypeCondition != "NoteNode" {
				fragmentTypes = intersectStrings(sourceTypes, e.possibleConcreteTypes(current.TypeCondition))
			}
			fields = mergeFieldSelections(fields, e.compileNodeFieldSelection(current.SelectionSet, fragmentTypes))
		}
	}
	fields.Names = normalizeStrings(fields.Names)
	return fields
}

func mergeFieldSelections(a, b noderead.FieldSelection) noderead.FieldSelection {
	a.Names = append(a.Names, b.Names...)
	a.Names = normalizeStrings(a.Names)
	a.IncludeBuiltin = a.IncludeBuiltin || b.IncludeBuiltin
	a.IncludeIssues = a.IncludeIssues || b.IncludeIssues
	a.IncludeContent = a.IncludeContent || b.IncludeContent
	a.IncludeLocators = a.IncludeLocators || b.IncludeLocators
	return a
}

func nodeReadRootsForRecords(records []*noteRecord) []noderead.NodeReadRoot {
	roots := make([]noderead.NodeReadRoot, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.Path) == "" {
			continue
		}
		ref := ontology.NodeRef{NotePath: record.Path, Kind: ontology.NodeKindNote, TypeName: record.TypeName}
		key := ref.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, noderead.NodeReadRoot{Ref: ref, TypeName: record.TypeName})
	}
	return roots
}

func possibleTypesForRecords(records []*noteRecord) []string {
	seen := make(map[string]struct{}, len(records))
	out := make([]string, 0, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.TypeName) == "" {
			continue
		}
		if _, ok := seen[record.TypeName]; ok {
			continue
		}
		seen[record.TypeName] = struct{}{}
		out = append(out, record.TypeName)
	}
	sort.Strings(out)
	return out
}

func (e *executor) possibleConcreteTypes(typeName string) []string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || typeName == "NoteNode" {
		out := make([]string, 0, len(e.schema.Types))
		for name, typ := range e.schema.Types {
			if typ != nil && typ.Role != ontoschema.TypeRoleEmbeddedNode {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	if _, ok := e.schema.Interfaces[typeName]; ok {
		out := make([]string, 0)
		for name := range e.schema.Types {
			if e.typeMatchesOrImplements(name, typeName) {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	return []string{typeName}
}

func intersectStrings(a, b []string) []string {
	if len(a) == 0 {
		return cloneStrings(b)
	}
	allowed := make(map[string]struct{}, len(b))
	for _, value := range b {
		allowed[value] = struct{}{}
	}
	out := make([]string, 0)
	for _, value := range a {
		if _, ok := allowed[value]; ok {
			out = append(out, value)
		}
	}
	return out
}

func optionalSingle(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}
