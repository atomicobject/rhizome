package query

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/vektah/gqlparser/v2/ast"
)

const (
	workspaceNavigationMaxOwnerRelations          = 200
	workspaceNavigationMaxMemberRelationsPerOwner = 200
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

func (e *executor) resolveWorkspaceField(item ontology.NodeFieldState, links fieldLinkTargets, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = item.Name
		case "kind":
			out[key] = string(item.Kind)
		case "sourceKind":
			out[key] = emptyNil(string(item.SourceKind))
		case "present":
			out[key] = item.Present
		case "status":
			out[key] = e.resolveWorkspaceStatus(item.Status, field.SelectionSet, append(path, key))
		case "range":
			out[key] = e.resolveWorkspaceRange(item.Range, field.SelectionSet, append(path, key))
		case "valueRanges":
			values := make([]any, 0, len(item.ValueRanges))
			for _, value := range item.ValueRanges {
				values = append(values, e.resolveWorkspaceRange(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		case "values":
			// GraphQL declares this list non-null. Preserve that contract for
			// absent optional fields instead of letting a nil Go slice encode as
			// JSON null.
			out[key] = append([]string{}, item.Values...)
		case "links":
			out[key] = e.resolveWorkspaceFieldLinks(item, links, field.SelectionSet, append(path, key))
		case "sectionNodes":
			values := make([]any, 0, len(item.SectionNodes))
			for _, value := range item.SectionNodes {
				values = append(values, nodeRefGraphQLValue(value))
			}
			out[key] = values
		case "capability":
			out[key] = e.resolveWorkspaceFieldCapability(item.Capability, field.SelectionSet, append(path, key))
		case "issues":
			values := make([]any, 0, len(item.Issues))
			for _, value := range item.Issues {
				values = append(values, e.resolveAssessmentIssue(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		default:
			e.addError(append(path, key), "field does not exist on NodeFieldState")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceFieldCapability(item ontology.NodeFieldCapability, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		values := map[string]any{
			"ownerRef":            nodeRefGraphQLValue(item.OwnerRef),
			"ownerType":           item.OwnerType,
			"typeName":            item.TypeName,
			"valueKind":           string(item.ValueKind),
			"list":                item.List,
			"required":            item.Required,
			"enumValues":          append([]string{}, item.EnumValues...),
			"enumOptions":         nodeFieldEnumOptionsGraphQLValue(item.EnumOptions),
			"targetType":          emptyNil(item.TargetType),
			"sourceKind":          emptyNil(string(item.SourceKind)),
			"valueOrigin":         string(item.ValueOrigin),
			"identifier":          item.Identifier,
			"preferredIdentifier": item.PreferredIdentifier,
			"displayImportance":   string(item.DisplayImportance),
			"writeOperation":      emptyNil(string(item.WriteOperation)),
			"readOnlyReason":      emptyNil(item.ReadOnlyReason),
		}
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeFieldCapability")
		}
	})
	return out
}

func nodeFieldEnumOptionsGraphQLValue(options []ontology.NodeFieldEnumOption) []any {
	out := make([]any, 0, len(options))
	for _, option := range options {
		out = append(out, map[string]any{"value": option.Value, "label": option.Label, "tone": emptyNil(option.Tone), "stage": emptyNil(string(option.Stage))})
	}
	return out
}

func (e *executor) resolveWorkspaceSourceRevision(item ontology.NodeSourceRevision, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		values := map[string]any{
			"notePath":           item.NotePath,
			"contentFingerprint": item.ContentFingerprint,
			"content":            item.Content,
		}
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeSourceRevision")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceCollection(item ontology.NodeCollectionState, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = item.Name
		case "kind":
			out[key] = string(item.Kind)
		case "status":
			out[key] = e.resolveWorkspaceStatus(item.Status, field.SelectionSet, append(path, key))
		case "range":
			out[key] = e.resolveWorkspaceRange(item.Range, field.SelectionSet, append(path, key))
		case "items":
			values := make([]any, 0, len(item.Items))
			for _, value := range item.Items {
				entry := make(map[string]any)
				e.eachSelectionField(field.SelectionSet, func(child *ast.Field) {
					childKey := responseKey(child)
					switch child.Name {
					case "ref":
						entry[childKey] = nodeRefGraphQLValue(value.Ref)
					case "range":
						entry[childKey] = e.resolveWorkspaceRange(value.Range, child.SelectionSet, append(path, key, childKey))
					default:
						e.addError(append(path, key, childKey), "field does not exist on NodeCollectionItem")
					}
				})
				values = append(values, entry)
			}
			out[key] = values
		case "orderFingerprint":
			out[key] = emptyNil(item.OrderFingerprint)
		default:
			e.addError(append(path, key), "field does not exist on NodeCollectionState")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceRange(item ontology.NodeRange, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "start":
			out[key] = item.Start
		case "end":
			out[key] = item.End
		default:
			e.addError(append(path, key), "field does not exist on NodeRange")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceCapabilities(item ontology.NodeCapabilities, set ast.SelectionSet, path []string) map[string]any {
	values := map[string]bool{"canEdit": item.CanEdit, "canEditFields": item.CanEditFields, "canEditCollections": item.CanEditCollections, "canNavigateChildren": item.CanNavigateChildren, "canSubscribe": item.CanSubscribe}
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeCapabilities")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceStatus(item ontology.NodeStatus, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "dirty":
			out[key] = item.Dirty
		case "hasWarnings":
			out[key] = item.HasWarnings
		case "validation":
			out[key] = e.resolveSingleInt("issueCount", item.Validation.IssueCount, field.SelectionSet, append(path, key))
		case "freshness":
			out[key] = e.resolveSingleString("state", item.Freshness.State, field.SelectionSet, append(path, key))
		case "session":
			out[key] = e.resolveSingleString("state", item.Session.State, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), "field does not exist on NodeStatus")
		}
	})
	return out
}

func (e *executor) resolveSingleInt(name string, value int, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		if field.Name == name {
			out[key] = value
		} else {
			e.addError(append(path, key), "unknown field")
		}
	})
	return out
}
func (e *executor) resolveSingleString(name, value string, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		if field.Name == name {
			out[key] = emptyNil(value)
		} else {
			e.addError(append(path, key), "unknown field")
		}
	})
	return out
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

type workspaceRelationItem struct {
	Ref                                                                   ontology.NodeRef
	Title, TargetTitle, RoleLabel, ResolvedType, RelationName, Provenance string
	Structural, Current                                                   bool
}
type workspaceRelationGroup struct {
	Key, Label, OwnerTitle string
	Navigation             bool
	Items                  []workspaceRelationItem
}

func (e *executor) workspaceRelationGroups(ctx context.Context, source ontology.NodeRef, path []string) []workspaceRelationGroup {
	graphSource := source
	if source.Kind == "" || source.Kind == ontology.NodeKindNote {
		graphSource = ontology.NodeRef{NotePath: source.NotePath, Kind: ontology.NodeKindNote}
	}
	graph, graphErr := e.loaders.scope.Graph(ctx, noderead.GraphRequest{
		Sources:   []ontology.NodeRef{graphSource},
		Profile:   noderead.GraphProfileCodeAware,
		NodeLimit: 201,
		EdgeLimit: 201,
	})
	if graphErr != nil {
		e.addError(path, graphErr.Error())
	}
	result, err := e.loaders.scope.Neighborhood(ctx, noderead.NeighborhoodRequest{Sources: []ontology.NodeRef{source}, Direction: noderead.TraversalDirectionBoth, IncludeStructural: true, IncludeAmbient: true, FirstTotal: 200})
	if err != nil {
		e.addError(path, err.Error())
		return nil
	}
	groups := map[bool][]workspaceRelationItem{true: {}, false: {}}
	seen := make(map[string]struct{})
	for _, edge := range result.Edges {
		key := edge.Target.String() + "\x00" + edge.RelationName + "\x00" + edge.Provenance
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		groups[edge.Structural] = append(groups[edge.Structural], workspaceRelationItem{Ref: edge.Target, Title: workspaceRefTitle(edge.Target), ResolvedType: edge.TargetType, RelationName: edge.RelationName, Provenance: edge.Provenance, Structural: edge.Structural})
	}
	var out []workspaceRelationGroup
	for _, structural := range []bool{true, false} {
		items := groups[structural]
		if len(items) == 0 {
			continue
		}
		if structural {
			out = append(out, workspaceRelationGroup{Key: "structural", Label: "Structural relations", Items: items})
		} else {
			out = append(out, workspaceRelationGroup{Key: "ambient", Label: "Ambient relations", Items: items})
		}
	}
	endpoints := make(map[string]noderead.GraphEndpoint, len(graph.Nodes))
	sourceIDs := make(map[string]struct{})
	for _, endpoint := range graph.Nodes {
		endpoints[endpoint.ID] = endpoint
		if workspaceEndpointMatchesRef(endpoint, source) {
			sourceIDs[endpoint.ID] = struct{}{}
		}
	}
	fallback := map[string][]workspaceRelationItem{
		"backlinks": {},
		"connected": {},
		"code":      {},
	}
	for _, edge := range graph.Edges {
		if edge.Kind == string(noderead.GraphEdgeKindOntology) || edge.Kind == string(noderead.GraphEdgeKindEmbeds) {
			continue
		}
		otherID := ""
		groupKey := ""
		if _, ok := sourceIDs[edge.Source]; ok {
			otherID = edge.Target
			groupKey = "connected"
		} else if _, ok := sourceIDs[edge.Target]; ok {
			otherID = edge.Source
			groupKey = "backlinks"
		} else {
			continue
		}
		other, ok := endpoints[otherID]
		if !ok {
			continue
		}
		if other.Kind == noderead.GraphEndpointCode || edge.Kind == string(noderead.GraphEdgeKindCodeRef) {
			groupKey = "code"
		}
		ref := workspaceRefFromGraphEndpoint(other)
		key := ref.String() + "\x00" + groupKey
		if _, ok := seen[key]; ok || ref.IsZero() {
			continue
		}
		seen[key] = struct{}{}
		fallback[groupKey] = append(fallback[groupKey], workspaceRelationItem{
			Ref:          ref,
			Title:        firstNonEmpty(other.Label, workspaceRefTitle(ref)),
			ResolvedType: other.TypeName,
			RelationName: edge.RelationName,
			Provenance:   firstNonEmpty(edge.Provenance, edge.Kind),
			Structural:   edge.Structural,
		})
	}
	for _, descriptor := range []struct{ key, label string }{
		{key: "backlinks", label: "Backlinks"},
		{key: "connected", label: "Connected notes"},
		{key: "code", label: "Linked code"},
	} {
		items := fallback[descriptor.key]
		if len(items) == 0 {
			continue
		}
		out = append(out, workspaceRelationGroup{Key: descriptor.key, Label: descriptor.label, Items: items})
	}
	e.applyWorkspaceRelationNodeTitles(ctx, out)
	navigation := e.workspaceNavigationGroups(ctx, source, path)
	if len(navigation) > 0 {
		promoted := workspaceNavigationRefSet(navigation)
		for groupIndex := range out {
			items := out[groupIndex].Items[:0]
			for _, item := range out[groupIndex].Items {
				if !workspaceNavigationContainsRef(promoted, item.Ref) {
					items = append(items, item)
				}
			}
			out[groupIndex].Items = items
		}
		filtered := navigation
		for _, group := range out {
			if len(group.Items) > 0 {
				filtered = append(filtered, group)
			}
		}
		out = filtered
	}
	for _, group := range out {
		if group.Navigation {
			continue
		}
		items := group.Items
		sort.Slice(items, func(i, j int) bool {
			if items[i].Title != items[j].Title {
				return items[i].Title < items[j].Title
			}
			return items[i].Ref.String() < items[j].Ref.String()
		})
	}
	return out
}

func workspaceNavigationRefSet(groups []workspaceRelationGroup) map[string]struct{} {
	refs := make(map[string]struct{})
	for _, group := range groups {
		for _, item := range group.Items {
			refs[noderead.RefIdentityKey(item.Ref)] = struct{}{}
		}
	}
	return refs
}

func workspaceNavigationContainsRef(refs map[string]struct{}, ref ontology.NodeRef) bool {
	_, ok := refs[noderead.RefIdentityKey(ref)]
	return ok
}

type workspaceNavigationOwner struct {
	Ref      ontology.NodeRef
	TypeName string
}

func (e *executor) workspaceNavigationGroups(ctx context.Context, source ontology.NodeRef, path []string) []workspaceRelationGroup {
	if e == nil || e.schema == nil || e.loaders == nil || e.loaders.scope == nil {
		return nil
	}
	ownerTypeNames := workspaceOwnerTypeNames(e.schema)
	if len(ownerTypeNames) == 0 {
		return nil
	}
	navigationSource := source
	if source.Fragment != "" || (source.Kind != "" && source.Kind != ontology.NodeKindNote) {
		navigationSource = ontology.NodeRef{NotePath: source.NotePath, Kind: ontology.NodeKindNote}
	}
	var ownerEdges []noderead.NeighborhoodEdge
	for _, ownerTypeName := range ownerTypeNames {
		remaining := workspaceNavigationMaxOwnerRelations - len(ownerEdges)
		ownerResult, err := e.loaders.scope.Neighborhood(ctx, noderead.NeighborhoodRequest{
			Sources:           []ontology.NodeRef{navigationSource},
			Direction:         noderead.TraversalDirectionInbound,
			RelationNames:     workspaceMemberFieldNames(e.schema.Types[ownerTypeName]),
			IncludeStructural: true,
			TargetTypes:       []string{ownerTypeName},
			FirstPerSource:    remaining + 1,
		})
		if err != nil {
			e.addError(path, err.Error())
			return nil
		}
		sourceRelations := workspaceNeighborhoodSource(ownerResult, navigationSource)
		ownerEdges = append(ownerEdges, sourceRelations.Edges...)
		if sourceRelations.Truncated || len(ownerEdges) > workspaceNavigationMaxOwnerRelations {
			e.addError(path, fmt.Sprintf("workspace navigation has more than %d inbound membership relations", workspaceNavigationMaxOwnerRelations))
			return nil
		}
	}
	owners := make([]workspaceNavigationOwner, 0)
	seenOwners := make(map[string]struct{})
	addOwner := func(ref ontology.NodeRef, typeName string) {
		noteType := e.schema.Types[typeName]
		if noteType == nil || !workspaceTypeHasMembers(noteType) {
			return
		}
		ref.Kind = ontology.NodeKindNote
		ref.Fragment, ref.NodeID, ref.Structural = "", "", ""
		ref.TypeName = typeName
		key := ref.String()
		if _, ok := seenOwners[key]; ok {
			return
		}
		seenOwners[key] = struct{}{}
		owners = append(owners, workspaceNavigationOwner{Ref: ref, TypeName: typeName})
	}

	sourceType := strings.TrimSpace(navigationSource.TypeName)
	if sourceType == "" {
		records, err := e.loaders.scope.Hydrate(ctx, []ontology.NodeRef{navigationSource}, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
		if err == nil && len(records) > 0 {
			sourceType = records[0].TypeName
		}
	}
	addOwner(navigationSource, sourceType)
	for _, edge := range ownerEdges {
		if edge.Direction != noderead.TraversalDirectionInbound || !edge.Structural {
			continue
		}
		noteType := e.schema.Types[edge.TargetType]
		if noteType == nil {
			continue
		}
		field := noteType.ByName[edge.RelationName]
		if field == nil || !field.WorkspaceMember {
			continue
		}
		addOwner(edge.Target, edge.TargetType)
	}

	sort.SliceStable(owners, func(i, j int) bool { return owners[i].Ref.String() < owners[j].Ref.String() })
	memberRelations := make(map[string]noderead.NeighborhoodSourceResult, len(owners))
	for _, batch := range workspaceOwnerBatches(e.schema, owners) {
		ownerRefs := make([]ontology.NodeRef, 0, len(batch.Owners))
		for _, owner := range batch.Owners {
			ownerRefs = append(ownerRefs, owner.Ref)
		}
		memberResult, err := e.loaders.scope.Neighborhood(ctx, noderead.NeighborhoodRequest{
			Sources:           ownerRefs,
			Direction:         noderead.TraversalDirectionOutbound,
			RelationNames:     batch.RelationNames,
			IncludeStructural: true,
			FirstPerSource:    workspaceNavigationMaxMemberRelationsPerOwner,
		})
		if err != nil {
			e.addError(path, err.Error())
			return nil
		}
		for _, item := range memberResult.Sources {
			memberRelations[noderead.RefIdentityKey(item.Source)] = item
		}
	}
	groups := make([]workspaceRelationGroup, 0, len(owners))
	for _, owner := range owners {
		ownerRelations := memberRelations[noderead.RefIdentityKey(owner.Ref)]
		if ownerRelations.Truncated {
			e.addError(path, fmt.Sprintf("workspace %s has more than %d outbound membership relations", owner.Ref.String(), workspaceNavigationMaxMemberRelationsPerOwner))
			return nil
		}
		group, ok := e.workspaceNavigationGroup(source, owner, ownerRelations.Edges)
		if ok {
			groups = append(groups, group)
		}
	}
	e.applyWorkspaceRelationNodeTitles(ctx, groups)
	for i := range groups {
		if len(groups[i].Items) > 0 {
			groups[i].OwnerTitle = groups[i].Items[0].TargetTitle
		}
	}
	return groups
}

type workspaceOwnerBatch struct {
	RelationNames []string
	Owners        []workspaceNavigationOwner
}

func workspaceOwnerTypeNames(schema *ontology.Schema) []string {
	if schema == nil {
		return nil
	}
	var names []string
	for typeName, noteType := range schema.Types {
		if workspaceTypeHasMembers(noteType) {
			names = append(names, typeName)
		}
	}
	sort.Strings(names)
	return names
}

func workspaceOwnerBatches(schema *ontology.Schema, owners []workspaceNavigationOwner) []workspaceOwnerBatch {
	byType := make(map[string]workspaceOwnerBatch)
	for _, owner := range owners {
		batch, ok := byType[owner.TypeName]
		if !ok {
			batch.RelationNames = workspaceMemberFieldNames(schema.Types[owner.TypeName])
		}
		batch.Owners = append(batch.Owners, owner)
		byType[owner.TypeName] = batch
	}
	typeNames := make([]string, 0, len(byType))
	for typeName := range byType {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)
	out := make([]workspaceOwnerBatch, 0, len(typeNames))
	for _, typeName := range typeNames {
		out = append(out, byType[typeName])
	}
	return out
}

func workspaceMemberFieldNames(noteType *ontology.NoteType) []string {
	if noteType == nil {
		return nil
	}
	var names []string
	for _, field := range noteType.Fields {
		if field != nil && field.WorkspaceMember {
			names = append(names, field.Name)
		}
	}
	sort.Strings(names)
	return names
}

func workspaceNeighborhoodSource(result noderead.NeighborhoodResult, ref ontology.NodeRef) noderead.NeighborhoodSourceResult {
	want := noderead.RefIdentityKey(ref)
	for _, source := range result.Sources {
		if noderead.RefIdentityKey(source.Source) == want {
			return source
		}
	}
	return noderead.NeighborhoodSourceResult{Source: ref}
}

func workspaceTypeHasMembers(noteType *ontology.NoteType) bool {
	if noteType == nil {
		return false
	}
	for _, field := range noteType.Fields {
		if field != nil && field.WorkspaceMember {
			return true
		}
	}
	return false
}

func (e *executor) workspaceNavigationGroup(current ontology.NodeRef, owner workspaceNavigationOwner, edges []noderead.NeighborhoodEdge) (workspaceRelationGroup, bool) {
	noteType := e.schema.Types[owner.TypeName]
	if noteType == nil {
		return workspaceRelationGroup{}, false
	}
	edgesByField := make(map[string][]noderead.NeighborhoodEdge)
	for _, edge := range edges {
		if edge.Structural {
			edgesByField[edge.RelationName] = append(edgesByField[edge.RelationName], edge)
		}
	}
	items := []workspaceRelationItem{{
		Ref:          owner.Ref,
		Title:        "Overview",
		RoleLabel:    "Overview",
		ResolvedType: owner.TypeName,
		Current:      workspaceNavigationCurrent(owner.Ref, current),
	}}
	for _, field := range noteType.Fields {
		if field == nil || !field.WorkspaceMember {
			continue
		}
		for _, edge := range edgesByField[field.Name] {
			roleLabel := ""
			if !field.List {
				roleLabel = firstNonEmpty(field.WorkspaceMemberLabel, workspaceHumanLabel(field.Name))
			}
			items = append(items, workspaceRelationItem{
				Ref:          edge.Target,
				Title:        firstNonEmpty(roleLabel, workspaceRefTitle(edge.Target)),
				RoleLabel:    roleLabel,
				ResolvedType: edge.TargetType,
				RelationName: edge.RelationName,
				Provenance:   edge.Provenance,
				Structural:   true,
				Current:      workspaceNavigationCurrent(edge.Target, current),
			})
		}
	}
	group := workspaceRelationGroup{
		Key:        "workspace:" + owner.Ref.String(),
		Label:      "In this " + workspaceOwnerLabel(e.schema, noteType),
		Navigation: true,
		Items:      items,
	}
	return group, true
}

func workspaceNavigationCurrent(item, current ontology.NodeRef) bool {
	if item.NotePath != current.NotePath {
		return false
	}
	if item.Fragment == "" && (item.Kind == "" || item.Kind == ontology.NodeKindNote) {
		return true
	}
	return strings.TrimSpace(item.Fragment) == strings.TrimSpace(current.Fragment) &&
		strings.TrimSpace(item.NodeID) == strings.TrimSpace(current.NodeID)
}

func workspaceOwnerLabel(schema *ontology.Schema, noteType *ontology.NoteType) string {
	label := "workspace"
	if noteType == nil {
		return label
	}
	for _, name := range noteType.Implements {
		if iface := schema.Interfaces[name]; iface != nil && strings.TrimSpace(iface.Label) != "" {
			label = iface.Label
			break
		}
	}
	if label == "workspace" {
		label = noteType.Label
	}
	if label == "" {
		label = noteType.Name
	}
	runes := []rune(label)
	if len(runes) > 0 {
		runes[0] = unicode.ToLower(runes[0])
	}
	return string(runes)
}

func workspaceHumanLabel(name string) string {
	var out []rune
	for i, r := range []rune(strings.TrimSpace(name)) {
		if r == '_' || r == '-' {
			if len(out) > 0 && out[len(out)-1] != ' ' {
				out = append(out, ' ')
			}
			continue
		}
		if i > 0 && unicode.IsUpper(r) && len(out) > 0 && out[len(out)-1] != ' ' {
			out = append(out, ' ')
		}
		out = append(out, unicode.ToLower(r))
	}
	if len(out) > 0 {
		out[0] = unicode.ToUpper(out[0])
	}
	return string(out)
}

// applyWorkspaceRelationNodeTitles replaces path-derived fallback titles with
// each related note or embedded node's projected title, using one batched
// summary hydration over the shared read scope.
func (e *executor) applyWorkspaceRelationNodeTitles(ctx context.Context, groups []workspaceRelationGroup) {
	if e == nil || e.loaders == nil || e.loaders.scope == nil {
		return
	}
	refs := make([]ontology.NodeRef, 0)
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Ref.IsZero() || !workspaceRelationRefHasDisplayTitle(item.Ref) {
				continue
			}
			key := noderead.RefIdentityKey(item.Ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, item.Ref)
		}
	}
	if len(refs) == 0 {
		return
	}
	records, err := e.loaders.scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return
	}
	titles := make(map[string]string, len(records)*2)
	for _, record := range records {
		title := strings.TrimSpace(record.Title)
		if title == "" {
			continue
		}
		titles[noderead.RefIdentityKey(record.Ref)] = title
		if record.Path != "" && (record.Ref.Kind == "" || record.Ref.Kind == ontology.NodeKindNote) && record.Ref.Fragment == "" {
			titles[record.Path] = title
		}
		if fragment := strings.TrimSpace(record.Ref.Fragment); fragment != "" && record.Path != "" {
			titles[record.Path+"#"+fragment] = title
		}
	}
	for _, group := range groups {
		for i := range group.Items {
			ref := group.Items[i].Ref
			if title, ok := titles[noderead.RefIdentityKey(ref)]; ok {
				applyWorkspaceRelationTitle(&group.Items[i], title)
				continue
			}
			fragment := strings.TrimSpace(ref.Fragment)
			if fragment != "" {
				if title, ok := titles[ref.NotePath+"#"+fragment]; ok {
					applyWorkspaceRelationTitle(&group.Items[i], title)
				}
				continue
			}
			if ref.Kind == "" || ref.Kind == ontology.NodeKindNote {
				if title, ok := titles[ref.NotePath]; ok {
					applyWorkspaceRelationTitle(&group.Items[i], title)
				}
			}
		}
	}
}

func applyWorkspaceRelationTitle(item *workspaceRelationItem, targetTitle string) {
	if item == nil || strings.TrimSpace(targetTitle) == "" {
		return
	}
	item.TargetTitle = targetTitle
	item.Title = firstNonEmpty(item.RoleLabel, targetTitle)
}

func workspaceRelationRefHasDisplayTitle(ref ontology.NodeRef) bool {
	switch ref.Kind {
	case "", ontology.NodeKindNote, ontology.NodeKindSection, ontology.NodeKindEmbedded:
		return true
	default:
		return false
	}
}

func workspaceEndpointMatchesRef(endpoint noderead.GraphEndpoint, ref ontology.NodeRef) bool {
	if !endpoint.Ref.IsZero() && endpoint.Ref.String() == ref.String() {
		return true
	}
	return endpoint.Kind == noderead.GraphEndpointNote && endpoint.NotePath == ref.NotePath && ref.Fragment == "" && ref.Kind != ontology.NodeKindEmbedded && ref.Kind != ontology.NodeKindSection
}

func workspaceRefFromGraphEndpoint(endpoint noderead.GraphEndpoint) ontology.NodeRef {
	if !endpoint.Ref.IsZero() {
		return endpoint.Ref
	}
	if endpoint.Kind == noderead.GraphEndpointCode && strings.TrimSpace(endpoint.Path) != "" {
		return ontology.NodeRef{NotePath: endpoint.Path, Kind: ontology.NodeKind("CODE_FILE")}
	}
	return ontology.NodeRef{}
}

func workspaceRefTitle(ref ontology.NodeRef) string {
	if ref.Fragment != "" {
		return strings.TrimPrefix(ref.Fragment, "^")
	}
	base := filepath.Base(ref.NotePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func (e *executor) resolveWorkspaceRelationGroup(item workspaceRelationGroup, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "key":
			out[key] = item.Key
		case "label":
			out[key] = item.Label
		case "ownerTitle":
			out[key] = emptyNil(item.OwnerTitle)
		case "navigation":
			out[key] = item.Navigation
		case "items":
			values := make([]any, 0, len(item.Items))
			for _, value := range item.Items {
				entry := make(map[string]any)
				e.eachSelectionField(field.SelectionSet, func(child *ast.Field) {
					childKey := responseKey(child)
					switch child.Name {
					case "ref":
						entry[childKey] = nodeRefGraphQLValue(value.Ref)
					case "title":
						entry[childKey] = value.Title
					case "targetTitle":
						entry[childKey] = emptyNil(value.TargetTitle)
					case "resolvedType":
						entry[childKey] = emptyNil(value.ResolvedType)
					case "relationName":
						entry[childKey] = emptyNil(value.RelationName)
					case "provenance":
						entry[childKey] = emptyNil(value.Provenance)
					case "structural":
						entry[childKey] = value.Structural
					case "current":
						entry[childKey] = value.Current
					default:
						e.addError(append(path, key, childKey), "field does not exist on NodeRelationItem")
					}
				})
				values = append(values, entry)
			}
			out[key] = values
		default:
			e.addError(append(path, key), "field does not exist on NodeRelationGroup")
		}
	})
	return out
}
