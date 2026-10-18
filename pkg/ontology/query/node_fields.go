package query

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveNodeSelectionSet(ctx context.Context, node *noteRecord, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported selection on NoteNode", func(condition string) bool {
		return e.noteMatchesTypeCondition(node, condition)
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveNodeField(ctx, node, field, fieldPath)
	})
}

func (e *executor) resolveNodeField(ctx context.Context, node *noteRecord, field *ast.Field, path []string) (any, bool) {
	switch field.Name {
	case "ref":
		return nodeRefGraphQLValue(ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}), true
	case "nodeId":
		return "note:" + node.Path, true
	case "nodeKind":
		return "NOTE", true
	case "path":
		return node.Path, true
	case "title":
		return node.Title, true
	case "resolvedType":
		return node.TypeName, true
	case "content":
		if node.Content == "" {
			if record, err := e.loaders.hydrateContentForPath(ctx, node.Path); err == nil && record != nil {
				node.Content = record.Content
				node.InlineProps = cloneInlineProps(record.InlineProps)
			}
		}
		return node.Content, true
	case "format":
		return string(node.Format), true
	case "sourceRepresentation":
		return string(node.SourceRepresentation), true
	case "evidenceRepresentation":
		return string(node.EvidenceRepresentation), true
	case "sourceCapabilities":
		values := make([]string, 0, len(node.Capabilities))
		for _, capability := range node.Capabilities {
			values = append(values, string(capability))
		}
		return values, true
	case "frontmatter":
		return node.cloneFrontmatter(), true
	case "tags":
		return node.cloneTags(), true
	case "score":
		if node.Score == nil {
			return nil, true
		}
		return *node.Score, true
	case "locator":
		ref := ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote}
		return e.resolveLocator(ctx, ref), true
	case "workspace":
		return e.resolveWorkspaceSelectionSet(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, field.SelectionSet, path), true
	case "neighborhood":
		return e.resolveNeighborhoodSelectionSet(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, field, path), true
	case "localGraph":
		return e.resolveLocalGraphSelectionSet(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, field, path), true
	case "linked", "backlinked", "connected":
		return e.resolveAmbientBuiltin(ctx, node, field, path)
	}

	schemaField := e.noteFieldForNode(node, field.Name)
	if schemaField == nil {
		if count, ok := e.relationCount(ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, node.TypeName, field.Name); ok {
			return count, true
		}
		switch field.Name {
		case recordFieldUpdatedAt:
			return e.recordUpdatedAt(node.Path), true
		case recordFieldIssueCount:
			return e.recordIssueCount(ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote}), true
		}
		typeName := strings.TrimSpace(node.TypeName)
		if typeName == "" {
			typeName = "NoteNode"
		}
		e.addError(path, fmt.Sprintf("field %q does not exist on %s", field.Name, typeName))
		return nil, false
	}

	switch schemaField.Kind {
	case ontoschema.FieldKindScalar, ontoschema.FieldKindEnum:
		return e.resolveScalarField(ctx, node, schemaField, path)
	case ontoschema.FieldKindLink:
		return e.resolveStructuralField(ctx, node, schemaField, field.SelectionSet, path)
	case ontoschema.FieldKindNeighbor:
		return e.resolveNeighborField(ctx, node, schemaField, field.SelectionSet, path, e.relationFirst(field))
	case ontoschema.FieldKindReverse:
		return e.resolveReverseField(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, schemaField, field.SelectionSet, path, e.relationFirst(field))
	case ontoschema.FieldKindSection:
		return e.resolveSectionField(ctx, node, schemaField, field, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported field kind %q", schemaField.Kind))
		return nil, false
	}
}

func (e *executor) noteFieldForNode(node *noteRecord, fieldName string) *ontoschema.Field {
	if e == nil || e.schema == nil || node == nil {
		return nil
	}
	if noteType := e.schema.Types[node.TypeName]; noteType != nil {
		if field := noteType.ByName[fieldName]; field != nil {
			return field
		}
	}
	if e.schema.NoteInterfaceAuthored {
		if iface := e.schema.Interfaces["Note"]; iface != nil {
			if field := iface.ByName[fieldName]; field != nil {
				return field
			}
		}
	}
	return nil
}

func (e *executor) resolveSectionField(ctx context.Context, node *noteRecord, field *ontoschema.Field, gqlField *ast.Field, path []string) (any, bool) {
	if node == nil {
		return nil, true
	}
	sections, err := e.matchSectionsForField(ctx, node, field)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	if field.SectionRequired && len(sections) == 0 {
		e.addError(path, fmt.Sprintf("required section %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	if !field.List && len(sections) > 1 {
		e.addError(path, fmt.Sprintf("section %s.%s must match a single heading", node.TypeName, field.Name))
		return nil, false
	}
	if field.List {
		out := make([]any, 0, len(sections))
		for _, section := range sections {
			out = append(out, e.resolveSectionSelectionSet(ctx, section, gqlField.SelectionSet, path))
		}
		return out, true
	}
	if len(sections) == 0 {
		return nil, true
	}
	return e.resolveSectionSelectionSet(ctx, sections[0], gqlField.SelectionSet, path), true
}

func (e *executor) resolveSectionSelectionSet(ctx context.Context, node *sectionRecord, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported selection on Section", func(condition string) bool {
		return e.sectionMatchesTypeCondition(node, condition)
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveSectionNodeField(ctx, node, field, fieldPath)
	})
}

func (e *executor) resolveAnyNodeSelectionSet(ctx context.Context, node *resolvedNode, set ast.SelectionSet, path []string) map[string]any {
	if node == nil {
		return nil
	}
	switch {
	case node.Note != nil:
		return e.resolveNodeSelectionSet(ctx, node.Note, set, path)
	case node.Section != nil:
		return e.resolveSectionSelectionSet(ctx, node.Section, set, path)
	case node.Code != nil:
		return e.resolveCodeSelectionSet(ctx, node.Code, set, path)
	default:
		return nil
	}
}

func (e *executor) resolveSectionNodeField(ctx context.Context, node *sectionRecord, field *ast.Field, path []string) (any, bool) {
	if node == nil || node.Node == nil {
		return nil, true
	}
	resolveBuiltin := func() (any, bool, bool) {
		switch field.Name {
		case "ref":
			ref, err := e.publicSectionRef(ctx, node)
			if err != nil {
				e.addError(path, err.Error())
				return nil, true, true
			}
			return nodeRefGraphQLValue(ref), true, true
		case "nodeId":
			return "section:" + node.Node.NotePath + "#" + node.Node.ID, true, true
		case "nodeKind":
			if ref := e.sectionNodeRef(node); ref.Kind == ontology.NodeKindEmbedded {
				return "EMBEDDED", true, true
			}
			return "SECTION", true, true
		case "id":
			if node.Projection != nil {
				return node.Projection.Ref.NodeID, true, true
			}
			return node.Node.ID, true, true
		case "path":
			if node.Projection != nil {
				return node.Projection.Ref.String(), true, true
			}
			return node.Node.NotePath + "#" + queryFragmentFromSectionID(node.Node.NotePath, node.Node.ID), true, true
		case "notePath":
			return node.Node.NotePath, true, true
		case "title":
			if node.Projection != nil {
				return nodeCatalogTitleFromProjection(node.Projection), true, true
			}
			return node.Node.Title, true, true
		case "resolvedType":
			return node.TypeName, true, true
		case recordFieldUpdatedAt:
			return e.recordUpdatedAt(node.Node.NotePath), true, true
		case recordFieldIssueCount:
			return e.recordIssueCount(e.sectionNodeRef(node)), true, true
		case "level":
			if node.Projection != nil && node.Node.Level == "" {
				return "", true, true
			}
			return string(node.Node.Level), true, true
		case "content":
			if node.Projection != nil {
				return projectionRecordContent(node.Projection), true, true
			}
			return ontology.SectionBody(node.Node), true, true
		case "locator":
			return e.resolveLocator(ctx, e.sectionNodeRef(node)), true, true
		case "workspace":
			return e.resolveWorkspaceSelectionSet(ctx, e.sectionNodeRef(node), field.SelectionSet, path), true, true
		case "neighborhood":
			return e.resolveNeighborhoodSelectionSet(ctx, e.sectionNodeRef(node), field, path), true, true
		case "localGraph":
			return e.resolveLocalGraphSelectionSet(ctx, e.sectionNodeRef(node), field, path), true, true
		case "children":
			if node.Projection != nil {
				children := e.sectionRecordsFromProjectionField(node.Projection, node.Note, &ontoschema.Field{Name: "children"})
				first := 20
				if args := e.fieldArgs(field); args != nil {
					first = intValue(args["first"], 20)
				}
				if first > 0 && len(children) > first {
					children = children[:first]
				}
				out := make([]any, 0, len(children))
				for _, child := range children {
					out = append(out, e.resolveSectionSelectionSet(ctx, child, field.SelectionSet, path))
				}
				return out, true, true
			}
			first := 20
			if args := e.fieldArgs(field); args != nil {
				first = intValue(args["first"], 20)
			}
			children := node.Node.Children
			if first > 0 && len(children) > first {
				children = children[:first]
			}
			out := make([]any, 0, len(children))
			for _, child := range children {
				out = append(out, e.resolveSectionSelectionSet(ctx, &sectionRecord{
					Node:         child,
					Note:         node.Note,
					TypeName:     e.childSectionType(node, child),
					PropertyCase: node.PropertyCase,
				}, field.SelectionSet, path))
			}
			return out, true, true
		default:
			return nil, false, false
		}
	}

	noteType := e.schema.Types[node.TypeName]
	if noteType == nil {
		if value, ok, handled := resolveBuiltin(); handled {
			return value, ok
		}
		e.addError(path, fmt.Sprintf("unknown ontology section type %q", node.TypeName))
		return nil, false
	}
	schemaField := noteType.ByName[field.Name]
	if schemaField == nil {
		if count, ok := e.relationCount(e.sectionNodeRef(node), node.TypeName, field.Name); ok {
			return count, true
		}
		if value, ok, handled := resolveBuiltin(); handled {
			return value, ok
		}
		e.addError(path, fmt.Sprintf("field %q does not exist on %s", field.Name, node.TypeName))
		return nil, false
	}
	switch schemaField.Kind {
	case ontoschema.FieldKindSection:
		matches, err := e.matchNestedSections(ctx, node, schemaField)
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if schemaField.SectionRequired && len(matches) == 0 {
			e.addError(path, fmt.Sprintf("required section %s.%s is missing", node.TypeName, field.Name))
			return nil, false
		}
		if !schemaField.List && len(matches) > 1 {
			e.addError(path, fmt.Sprintf("section %s.%s must match a single heading", node.TypeName, field.Name))
			return nil, false
		}
		if schemaField.List {
			out := make([]any, 0, len(matches))
			for _, match := range matches {
				out = append(out, e.resolveSectionSelectionSet(ctx, match, field.SelectionSet, path))
			}
			return out, true
		}
		if len(matches) == 0 {
			return nil, true
		}
		return e.resolveSectionSelectionSet(ctx, matches[0], field.SelectionSet, path), true
	case ontoschema.FieldKindLink:
		return e.resolveSectionStructuralField(ctx, node, schemaField, field.SelectionSet, path)
	case ontoschema.FieldKindScalar, ontoschema.FieldKindEnum:
		return e.resolveSectionScalarField(ctx, node, schemaField, path)
	case ontoschema.FieldKindNeighbor:
		return e.resolveSectionNeighborField(ctx, node, schemaField, field.SelectionSet, path, e.relationFirst(field))
	default:
		e.addError(path, fmt.Sprintf("unsupported field kind %q on section type", schemaField.Kind))
		return nil, false
	}
}
