package query

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveWorkspaceSelectionSet(ctx context.Context, ref ontology.NodeRef, set ast.SelectionSet, path []string) any {
	if e == nil || e.loaders == nil || e.loaders.scope == nil || ref.IsZero() {
		return nil
	}
	projection, err := e.loaders.scope.Projection(ctx, ref)
	if err != nil {
		e.addError(path, err.Error())
		return nil
	}
	if projection == nil {
		return nil
	}
	workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(e.schema, projection)
	if workspace == nil {
		return nil
	}
	assessment := projection.Assessment
	if assessment == nil {
		assessment = e.noteAssessment(ctx, ref.NotePath)
	}
	applyWorkspaceAssessment(workspace, assessment)

	wantsAssessment := e.selectionHasField(set, "assessment")
	wantsStructure := e.selectionHasField(set, "structure")
	wantsRelations := e.selectionHasField(set, "relationGroups")
	var relationGroups []workspaceRelationGroup
	if wantsRelations {
		relationGroups = e.workspaceRelationGroups(ctx, projection.Ref, path)
	}
	var structure []workspaceStructureNode
	if wantsStructure {
		structure = workspaceStructure(projection, workspace)
	}

	// Bodies are walked before the field loop so focus and body relation
	// fields share one link read per workspace selection.
	bodiesByField := map[*ast.Field][]workspaceBodyNode{}
	var linkOwners []workspaceFieldOwner
	if e.selectionHasFieldPath(set, "fields", "links") {
		linkOwners = append(linkOwners, workspaceFieldOwner{Projection: projection, Fields: workspace.Fields})
	}
	e.eachSelectionField(set, func(field *ast.Field) {
		if field.Name != "bodies" {
			return
		}
		bodies := e.workspaceBodyNodes(ctx, projection, workspaceBodiesLimit(e.fieldArgs(field)), append(path, responseKey(field)))
		bodiesByField[field] = bodies
		if e.selectionHasFieldPath(field.SelectionSet, "fields", "links") {
			for _, body := range bodies {
				if body.Workspace != nil {
					linkOwners = append(linkOwners, workspaceFieldOwner{Projection: body.Projection, Fields: body.Workspace.Fields})
				}
			}
		}
	})
	links := e.workspaceFieldLinks(ctx, projection.Ref.NotePath, linkOwners, path)

	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "parentRef":
			if workspace.Node.ParentRef == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*workspace.Node.ParentRef)
			}
		case "parentTitle":
			out[key] = e.workspaceParentTitle(ctx, workspace.Node.ParentRef, append(path, key))
		case "fields":
			items := make([]any, 0, len(workspace.Fields))
			for _, item := range workspace.Fields {
				items = append(items, e.resolveWorkspaceField(item, links, field.SelectionSet, append(path, key)))
			}
			out[key] = items
		case "collections":
			items := make([]any, 0, len(workspace.Collections))
			for _, item := range workspace.Collections {
				items = append(items, e.resolveWorkspaceCollection(item, field.SelectionSet, append(path, key)))
			}
			out[key] = items
		case "bodies":
			out[key] = e.resolveWorkspaceBodies(bodiesByField[field], links, field, append(path, key))
		case "sourceLinks":
			first := maxWorkspaceSourceLinks
			if args := e.fieldArgs(field); args != nil {
				first = intValue(args["first"], maxWorkspaceSourceLinks)
			}
			items := e.workspaceSourceLinks(ctx, projection, first, append(path, key))
			resolved := make([]any, 0, len(items))
			for _, item := range items {
				resolved = append(resolved, e.resolveWorkspaceSourceLink(item, field.SelectionSet, append(path, key)))
			}
			out[key] = resolved
		case "capabilities":
			out[key] = e.resolveWorkspaceCapabilities(workspace.Capabilities, field.SelectionSet, append(path, key))
		case "status":
			out[key] = e.resolveWorkspaceStatus(workspace.Status, field.SelectionSet, append(path, key))
		case "version":
			out[key] = workspace.Version
		case "sourceRevision":
			out[key] = e.resolveWorkspaceSourceRevision(workspace.SourceRevision, field.SelectionSet, append(path, key))
		case "assessment":
			if assessment == nil {
				out[key] = nil
			} else {
				out[key] = e.resolveWorkspaceAssessment(*assessment, field.SelectionSet, append(path, key))
			}
		case "structure":
			items := make([]any, 0, len(structure))
			for _, item := range structure {
				items = append(items, e.resolveWorkspaceStructure(item, field.SelectionSet, append(path, key)))
			}
			out[key] = items
		case "relationGroups":
			items := make([]any, 0, len(relationGroups))
			for _, item := range relationGroups {
				items = append(items, e.resolveWorkspaceRelationGroup(item, field.SelectionSet, append(path, key)))
			}
			out[key] = items
		case "loaded":
			loaded := map[string]bool{
				"rendered":   true,
				"assessment": wantsAssessment,
				"structure":  wantsStructure,
				"relations":  wantsRelations,
			}
			value := make(map[string]any)
			e.eachSelectionField(field.SelectionSet, func(child *ast.Field) {
				childKey := responseKey(child)
				if loadedValue, ok := loaded[child.Name]; ok {
					value[childKey] = loadedValue
				} else {
					e.addError(append(path, key, childKey), "field does not exist on NodeLoadedDomains")
				}
			})
			out[key] = value
		default:
			e.addError(append(path, key), "field does not exist on NodeWorkspaceProjection")
		}
	})
	return out
}

// workspaceParentTitle titles the semantic parent with one summary
// hydration, which reads indexed metadata and shares the request scope.
func (e *executor) workspaceParentTitle(ctx context.Context, parent *ontology.NodeRef, path []string) any {
	if parent == nil || parent.IsZero() {
		return nil
	}
	records, err := e.loaders.scope.Hydrate(ctx, []ontology.NodeRef{*parent}, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		e.addError(path, err.Error())
		return nil
	}
	if len(records) == 0 {
		return nil
	}
	return emptyNil(records[0].Title)
}

func (e *executor) eachSelectionField(set ast.SelectionSet, visit func(*ast.Field)) {
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			visit(current)
		case *ast.InlineFragment:
			e.eachSelectionField(current.SelectionSet, visit)
		case *ast.FragmentSpread:
			if fragment := e.fragments.ForName(current.Name); fragment != nil {
				e.eachSelectionField(fragment.SelectionSet, visit)
			}
		}
	}
}

func (e *executor) selectionHasField(set ast.SelectionSet, name string) bool {
	found := false
	e.eachSelectionField(set, func(field *ast.Field) {
		if field.Name == name {
			found = true
		}
	})
	return found
}

// selectionHasFieldPath reports whether set selects name, then each nested
// name in turn, e.g. fields { links }.
func (e *executor) selectionHasFieldPath(set ast.SelectionSet, name string, nested ...string) bool {
	found := false
	e.eachSelectionField(set, func(field *ast.Field) {
		if field.Name != name || found {
			return
		}
		found = len(nested) == 0 || e.selectionHasFieldPath(field.SelectionSet, nested[0], nested[1:]...)
	})
	return found
}

func applyWorkspaceAssessment(workspace *ontology.NodeWorkspace, assessment *ontology.NoteAssessment) {
	if workspace == nil || assessment == nil {
		return
	}
	issueCount := len(assessment.Issues)
	for _, field := range assessment.Fields {
		issueCount += len(field.Issues)
	}
	for _, relation := range assessment.Relations {
		issueCount += len(relation.Issues)
	}
	workspace.Status.Validation.IssueCount = issueCount
	workspace.Status.HasWarnings = issueCount > 0
	for i := range workspace.Fields {
		if field, ok := assessment.Field(workspace.Fields[i].Name); ok {
			workspace.Fields[i].Status.Validation.IssueCount = len(field.Issues)
			workspace.Fields[i].Status.HasWarnings = len(field.Issues) > 0
		}
	}
	for i := range workspace.Collections {
		if field, ok := assessment.Field(workspace.Collections[i].Name); ok {
			workspace.Collections[i].Status.Validation.IssueCount = len(field.Issues)
			workspace.Collections[i].Status.HasWarnings = len(field.Issues) > 0
		}
	}
}

type workspaceStructureNode struct {
	Ref                   ontology.NodeRef
	ParentRef             *ontology.NodeRef
	Title, Level, Content string
}

func workspaceStructure(projection *ontology.NodeProjection, workspace *ontology.NodeWorkspace) []workspaceStructureNode {
	if projection == nil || projection.Snapshot == nil {
		return nil
	}
	known := make(map[string]ontology.NodeRef)
	if workspace != nil {
		for _, field := range workspace.Fields {
			for _, ref := range field.SectionNodes {
				known[ref.NodeID] = ref
			}
		}
		for _, collection := range workspace.Collections {
			for _, item := range collection.Items {
				known[item.Ref.NodeID] = item.Ref
			}
		}
	}
	var out []workspaceStructureNode
	var walk func([]*ontology.SectionNode, *ontology.NodeRef)
	walk = func(nodes []*ontology.SectionNode, parent *ontology.NodeRef) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			ref, ok := known[node.ID]
			if !ok {
				ref = ontology.NodeRef{NotePath: node.NotePath, NodeID: node.ID, Kind: ontology.NodeKindSection, Fragment: queryFragmentFromSectionID(node.NotePath, node.ID)}
			}
			current := ref
			out = append(out, workspaceStructureNode{Ref: ref, ParentRef: parent, Title: node.Title, Level: string(node.Level), Content: ontology.SectionBody(node)})
			walk(node.Children, &current)
		}
	}
	walk(projection.Snapshot.Sections, nil)
	return out
}

func (e *executor) resolveWorkspaceStructure(item workspaceStructureNode, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "ref":
			out[key] = nodeRefGraphQLValue(item.Ref)
		case "parentRef":
			if item.ParentRef == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*item.ParentRef)
			}
		case "title":
			out[key] = item.Title
		case "level":
			out[key] = emptyNil(item.Level)
		case "content":
			out[key] = emptyNil(item.Content)
		default:
			e.addError(append(path, key), "field does not exist on NodeStructure")
		}
	})
	return out
}
