package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/vektah/gqlparser/v2/ast"
	"path/filepath"
	"sort"
	"strings"
)

type workspaceRelationItem struct {
	Ref                                                                   ontology.NodeRef
	Title, TargetTitle, RoleLabel, ResolvedType, RelationName, Provenance string
	// Direction is relative to the workspace source; empty for navigation.
	Direction           noderead.TraversalDirection
	Structural, Current bool
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
		direction := workspaceLinkDirection(edge.Direction, edge.Provenance)
		key := edge.Target.String() + "\x00" + edge.RelationName + "\x00" + edge.Provenance + "\x00" + string(direction)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		groups[edge.Structural] = append(groups[edge.Structural], workspaceRelationItem{Ref: edge.Target, Title: workspaceRefTitle(edge.Target), ResolvedType: edge.TargetType, RelationName: edge.RelationName, Provenance: edge.Provenance, Direction: direction, Structural: edge.Structural})
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
		direction := noderead.TraversalDirectionOutbound
		if _, ok := sourceIDs[edge.Source]; ok {
			otherID = edge.Target
			groupKey = "connected"
		} else if _, ok := sourceIDs[edge.Target]; ok {
			otherID = edge.Source
			groupKey = "backlinks"
			direction = noderead.TraversalDirectionInbound
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
			Direction:    direction,
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

// workspaceLinkDirection reports which way the authored link runs. The index
// mirrors every body link A→B with a synthetic B→A edge whose provenance is
// "backlink", so that edge's traversal direction is the reverse of the link's.
func workspaceLinkDirection(traversal noderead.TraversalDirection, provenance string) noderead.TraversalDirection {
	if provenance != "backlink" {
		return traversal
	}
	switch traversal {
	case noderead.TraversalDirectionOutbound:
		return noderead.TraversalDirectionInbound
	case noderead.TraversalDirectionInbound:
		return noderead.TraversalDirectionOutbound
	default:
		return traversal
	}
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
					case "direction":
						if value.Direction == "" {
							entry[childKey] = nil
						} else {
							entry[childKey] = traversalDirectionGraphQLValue(value.Direction)
						}
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
