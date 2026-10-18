package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
	"unicode"
)

const (
	workspaceNavigationMaxOwnerRelations          = 200
	workspaceNavigationMaxMemberRelationsPerOwner = 200
)

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

func workspaceOwnerTypeNames(schema *ontoschema.Schema) []string {
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

func workspaceOwnerBatches(schema *ontoschema.Schema, owners []workspaceNavigationOwner) []workspaceOwnerBatch {
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

func workspaceMemberFieldNames(noteType *ontoschema.NoteType) []string {
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

func workspaceTypeHasMembers(noteType *ontoschema.NoteType) bool {
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

func workspaceOwnerLabel(schema *ontoschema.Schema, noteType *ontoschema.NoteType) string {
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
