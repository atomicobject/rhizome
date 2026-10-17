package web

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"sort"
	"strings"
)

func (b *workspaceGraphBuilder) addRelationGroups(focusedID string) []WorkspaceRelationGroupsViewResponse {
	var out []WorkspaceRelationGroupsViewResponse
	if len(b.resp.Relations) > 0 {
		out = append(out, WorkspaceRelationGroupsViewResponse{
			ScopeNodeID: focusedID,
			Groups:      cloneNoteWorkspaceGroups(b.resp.Relations),
		})
		b.addRelationEdges(focusedID, b.resp.Relations)
	}
	if len(b.resp.SectionRelationGroups) == 0 {
		return out
	}
	keys := make([]string, 0, len(b.resp.SectionRelationGroups))
	for key := range b.resp.SectionRelationGroups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, sectionID := range keys {
		scopeRef := refFromSectionID(sectionID)
		scopeNodeID := workspaceNodeIDForRef(scopeRef)
		if _, ok := b.nodes[scopeNodeID]; !ok {
			b.ensureRefNode(scopeRef, "")
		}
		groups := cloneNoteWorkspaceGroups(b.resp.SectionRelationGroups[sectionID])
		out = append(out, WorkspaceRelationGroupsViewResponse{
			ScopeNodeID: scopeNodeID,
			Groups:      groups,
		})
		b.addRelationEdges(scopeNodeID, groups)
	}
	return out
}

func (b *workspaceGraphBuilder) addRelationEdges(scopeNodeID string, groups []NoteWorkspaceGroup) {
	for _, group := range groups {
		for _, item := range group.Items {
			targetID := b.ensureRelationTargetNode(item)
			if targetID == "" {
				continue
			}
			b.edges = append(b.edges, WorkspaceEdgeResponse{
				Kind:          WorkspaceEdgeKindRelatesTo,
				FromID:        scopeNodeID,
				ToID:          targetID,
				RelationKey:   group.Key,
				RelationLabel: group.Label,
				ScopeNodeID:   scopeNodeID,
			})
		}
	}
}

func (b *workspaceGraphBuilder) ensureRelationTargetNode(item NoteWorkspaceLink) string {
	ref := ontology.NodeRef{
		NotePath: item.Path,
		Kind:     ontology.NodeKindNote,
	}
	if anchor := strings.TrimSpace(item.Anchor); anchor != "" {
		ref.Fragment = anchor
		ref.NodeID = item.Path + "#" + anchor
		if strings.HasPrefix(anchor, "^") {
			ref.Kind = ontology.NodeKindEmbedded
		} else {
			ref.Kind = ontology.NodeKindSection
		}
	}
	id := b.ensureRefNode(ref, item.Title)
	if node, ok := b.nodes[id]; ok {
		node.Data = ensureNodeData(node.Data)
		node.Data.Title = firstNonEmpty(node.Data.Title, item.Title)
		node.Data.ResolvedType = firstNonEmpty(node.Data.ResolvedType, item.ResolvedType)
		node.Version = workspaceGraphNodeVersion(node)
	}
	return id
}

// ensureRefNode creates a bare workspace node for a ref if one doesn't yet
// exist. The kind is set once (schema-derived when possible), and the Data
// payload is allocated with a default locator so later merges don't have to
// branch by kind. Identity is purely location-based, so concurrent calls
// from different builder passes (collection / rendered / structural) all
// converge on the same node.
func (b *workspaceGraphBuilder) ensureRefNode(ref ontology.NodeRef, title string) string {
	id := workspaceNodeIDForRef(ref)
	if _, ok := b.nodes[id]; ok {
		return id
	}
	kind := workspaceNodeKindForRef(ref)
	node := &WorkspaceNodeResponse{
		ID:           id,
		Kind:         kind,
		Ref:          ref,
		NotePath:     ref.NotePath,
		Status:       ontology.NodeStatus{},
		Capabilities: ontology.NodeCapabilities{},
		Data: &WorkspaceNodeData{
			Title:    title,
			Locator:  defaultLocatorForKind(kind),
			Fragment: ref.Fragment,
		},
	}
	node.Version = workspaceGraphNodeVersion(node)
	b.nodes[id] = node
	return id
}

func defaultLocatorForKind(kind WorkspaceNodeKind) string {
	switch kind {
	case WorkspaceNodeKindNote:
		return "FILE"
	case WorkspaceNodeKindEmbedded:
		return "EMBEDDED"
	case WorkspaceNodeKindSection:
		return "SECTION"
	default:
		return ""
	}
}

// ensureNodeData allocates Data lazily so merge callers always have a target
// pointer without having to initialize in every branch.
func ensureNodeData(data *WorkspaceNodeData) *WorkspaceNodeData {
	if data != nil {
		return data
	}
	return &WorkspaceNodeData{}
}

func (b *workspaceGraphBuilder) ensureSectionLikeNode(id string, ref ontology.NodeRef, section RenderedSection, focusedID string) *WorkspaceNodeResponse {
	node, ok := b.nodes[id]
	if !ok {
		node = &WorkspaceNodeResponse{
			ID:           id,
			Kind:         workspaceNodeKindForRef(ref),
			Ref:          ref,
			NotePath:     ref.NotePath,
			Status:       ontology.NodeStatus{},
			Capabilities: ontology.NodeCapabilities{},
			Data:         &WorkspaceNodeData{},
		}
		b.nodes[id] = node
	}
	node.Data = mergeNodeDataFromRenderedSection(node.Data, section)
	node.NotePath = ref.NotePath
	node.Ref = ref
	if id != focusedID {
		node.Version = workspaceGraphNodeVersion(node)
	}
	return node
}

func (b *workspaceGraphBuilder) ensureStructuralNode(id string, ref ontology.NodeRef, structural StructuralNodeResponse, focusedID string) *WorkspaceNodeResponse {
	node, ok := b.nodes[id]
	if !ok {
		node = &WorkspaceNodeResponse{
			ID:           id,
			Kind:         workspaceNodeKindForRef(ref),
			Ref:          ref,
			NotePath:     ref.NotePath,
			Status:       ontology.NodeStatus{},
			Capabilities: ontology.NodeCapabilities{},
			Data:         &WorkspaceNodeData{},
		}
		b.nodes[id] = node
	}
	node.Data = mergeNodeDataFromStructuralNode(node.Data, structural)
	if id != focusedID {
		node.Version = workspaceGraphNodeVersion(node)
	}
	return node
}
