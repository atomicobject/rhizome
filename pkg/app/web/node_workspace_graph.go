package web

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/paths"
	"sort"
	"strings"
)

type workspaceGraphBuilder struct {
	resp     *NodeWorkspaceResponse
	snapshot *ontology.DocumentSnapshot
	schema   *ontoschema.Schema
	nodes    map[string]*WorkspaceNodeResponse
	edges    []WorkspaceEdgeResponse
	// bodyCache memoizes per-node body projections keyed by canonical node id
	// so callers that reproject (e.g. ensureRefNode + addFocusedNode overlap)
	// only pay the projection cost once per response.
	bodyCache map[string][]ontology.NodeBodyBlock
}

func buildWorkspaceGraph(resp *NodeWorkspaceResponse, snapshot *ontology.DocumentSnapshot, schema *ontoschema.Schema) (string, []WorkspaceNodeResponse, []WorkspaceEdgeResponse, *WorkspaceViewsResponse) {
	if resp == nil {
		return "", nil, nil, nil
	}
	// Docs: [[ontology-browser-workspace#^spec-0014-us2-ac3]]: graph nodes,
	// outlines, fields, collections, and relation groups are derived views over
	// the canonical workspace, not a second browser identity model.
	builder := &workspaceGraphBuilder{
		resp:      resp,
		snapshot:  snapshot,
		schema:    schema,
		nodes:     map[string]*WorkspaceNodeResponse{},
		bodyCache: map[string][]ontology.NodeBodyBlock{},
	}

	focusedID := builder.addFocusedNode()
	builder.addFieldNodes(focusedID)
	builder.addCollectionNodes(focusedID)
	renderedRoots := builder.addRenderedOutline(focusedID)
	structuralView := builder.addStructuralOutline(focusedID)
	relationViews := builder.addRelationGroups(focusedID)
	builder.addLocatorFieldNodes()

	// Body projection runs last, after every note/section/embedded node has
	// been created. Each body references other nodes via canonical NodeRefs,
	// so having the full node map in place lets the client walk cross-refs
	// without a second round-trip.
	builder.attachFocusedBody(focusedID)

	nodes := make([]WorkspaceNodeResponse, 0, len(builder.nodes))
	for _, node := range builder.nodes {
		nodes = append(nodes, *node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	sort.Slice(builder.edges, func(i, j int) bool {
		left := builder.edges[i]
		right := builder.edges[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.FromID != right.FromID {
			return left.FromID < right.FromID
		}
		if left.ToID != right.ToID {
			return left.ToID < right.ToID
		}
		if left.ScopeNodeID != right.ScopeNodeID {
			return left.ScopeNodeID < right.ScopeNodeID
		}
		return left.RelationKey < right.RelationKey
	})
	for i := range builder.edges {
		builder.edges[i].Index = i
	}

	var views *WorkspaceViewsResponse
	if len(renderedRoots) > 0 || structuralView != nil || len(relationViews) > 0 {
		views = &WorkspaceViewsResponse{
			RelationGroups: relationViews,
		}
		if len(renderedRoots) > 0 {
			views.RenderedOutline = &WorkspaceRenderedOutlineViewResponse{RootIDs: renderedRoots}
		}
		if structuralView != nil {
			views.StructuralOutline = structuralView
		}
	}
	return focusedID, nodes, builder.edges, views
}

func (b *workspaceGraphBuilder) addFocusedNode() string {
	ref := b.resp.Node.Ref
	id := workspaceNodeIDForRef(ref)
	kind := workspaceNodeKindForRef(ref)
	node := &WorkspaceNodeResponse{
		ID:           id,
		Kind:         kind,
		Ref:          ref,
		NotePath:     b.resp.Node.NotePath,
		Status:       b.resp.Status,
		Capabilities: b.resp.Capabilities,
		Version:      b.resp.Version,
	}
	locator := b.resp.Node.Locator
	if kind == WorkspaceNodeKindNote {
		locator = "FILE"
	}
	node.Data = &WorkspaceNodeData{
		Title:        b.resp.Node.Title,
		ResolvedType: b.resp.Node.ResolvedType,
		Markdown:     b.resp.Content.Markdown,
		Locator:      locator,
		Fragment:     ref.Fragment,
	}
	node.Version = workspaceGraphNodeVersion(node)
	b.nodes[id] = node
	if parentRef := b.resp.Node.ParentRef; parentRef != nil && !parentRef.IsZero() {
		parentID := b.ensureRefNode(*parentRef, paths.NoteTitle(parentRef.NotePath))
		if parentID != "" && parentID != id {
			node.ParentID = parentID
			b.addChild(parentID, id)
			node.Version = workspaceGraphNodeVersion(node)
		}
	}
	return id
}

func (b *workspaceGraphBuilder) addFieldNodes(ownerID string) {
	fieldDocs := fieldDocsByName(b.resp.Content.TypeDoc)
	for _, field := range b.resp.Fields {
		id := workspaceSyntheticNodeID("field", ownerID, field.Name)
		data := &WorkspaceFieldNodeData{
			Name:        field.Name,
			ValueKind:   workspaceFieldValueKind(field),
			Present:     field.Present,
			Values:      append([]string(nil), field.Values...),
			Range:       field.Range,
			ValueRanges: append([]ontology.ByteRange(nil), field.ValueRanges...),
			InlineSpans: append([]ontology.NodeInlineFieldSpan(nil), field.InlineSpans...),
			SectionRefs: append([]ontology.NodeRef(nil), field.SectionNodes...),
		}
		if doc, ok := fieldDocs[field.Name]; ok {
			data.TypeName = doc.TypeName
			if len(doc.EnumValues) > 0 {
				data.EnumValues = append([]string(nil), doc.EnumValues...)
			}
			data.Identifier = doc.Identifier
			data.PreferredIdentifier = doc.PreferredIdentifier
		}
		node := &WorkspaceNodeResponse{
			ID:           id,
			Kind:         WorkspaceNodeKindField,
			Ref:          b.resp.Node.Ref,
			NotePath:     b.resp.Node.NotePath,
			ParentID:     ownerID,
			Status:       field.Status,
			Capabilities: ontology.NodeCapabilities{},
			Field:        data,
		}
		node.Version = workspaceGraphNodeVersion(node)
		b.nodes[id] = node
		b.addChild(ownerID, id)
		b.edges = append(b.edges, WorkspaceEdgeResponse{
			Kind:      WorkspaceEdgeKindBindsField,
			FromID:    ownerID,
			ToID:      id,
			FieldName: field.Name,
		})
	}
}

func (b *workspaceGraphBuilder) addLocatorFieldNodes() {
	nodes := make([]*WorkspaceNodeResponse, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	for _, owner := range nodes {
		if owner == nil || owner.Kind != WorkspaceNodeKindEmbedded || owner.Data == nil {
			continue
		}
		blockID := strings.TrimSpace(strings.TrimPrefix(owner.Data.BlockID, "^"))
		if blockID == "" {
			continue
		}
		if b.hasIdentifierFieldForBlockID(owner, blockID) {
			continue
		}
		id := workspaceSyntheticNodeID("field", owner.ID, "locator")
		if _, exists := b.nodes[id]; exists {
			continue
		}
		field := &WorkspaceNodeResponse{
			ID:           id,
			Kind:         WorkspaceNodeKindField,
			Ref:          owner.Ref,
			NotePath:     owner.NotePath,
			ParentID:     owner.ID,
			Status:       ontology.NodeStatus{},
			Capabilities: ontology.NodeCapabilities{},
			Field: &WorkspaceFieldNodeData{
				Name:      "locator",
				ValueKind: "scalar",
				TypeName:  "String",
				Present:   true,
				Values:    []string{blockID},
			},
		}
		field.Version = workspaceGraphNodeVersion(field)
		b.nodes[id] = field
		b.addChild(owner.ID, id)
		b.edges = append(b.edges, WorkspaceEdgeResponse{
			Kind:      WorkspaceEdgeKindBindsField,
			FromID:    owner.ID,
			ToID:      id,
			FieldName: "locator",
		})
	}
}

func (b *workspaceGraphBuilder) hasIdentifierFieldForBlockID(owner *WorkspaceNodeResponse, blockID string) bool {
	if owner == nil {
		return false
	}
	if owner.Data != nil && owner.Data.Binding != nil {
		identifierField := strings.TrimSpace(owner.Data.Binding.IdentifierField)
		if identifierField != "" {
			if value := owner.Data.Binding.Properties[identifierField]; workspaceIdentifierMatchesBlockID(value, blockID) {
				return true
			}
		}
	}
	for _, childID := range owner.ChildIDs {
		child := b.nodes[childID]
		if child == nil || child.Field == nil || !child.Field.Identifier {
			continue
		}
		for _, value := range child.Field.Values {
			if workspaceIdentifierMatchesBlockID(value, blockID) {
				return true
			}
		}
	}
	return false
}

func workspaceIdentifierMatchesBlockID(value, blockID string) bool {
	value = strings.TrimSpace(strings.TrimPrefix(value, "^"))
	blockID = strings.TrimSpace(strings.TrimPrefix(blockID, "^"))
	if value == "" || blockID == "" {
		return false
	}
	return value == blockID || ontology.BlockSafeIdentifier(value) == blockID
}

// fieldDocsByName indexes a typeDoc's FieldDoc entries so the graph builder
// can attach ontology type metadata (declared type name, enum values) to each
// field node without re-walking the typedoc per field.
func fieldDocsByName(typeDoc *schemadoc.TypeDoc) map[string]schemadoc.FieldDoc {
	if typeDoc == nil || len(typeDoc.Fields) == 0 {
		return nil
	}
	out := make(map[string]schemadoc.FieldDoc, len(typeDoc.Fields))
	for _, field := range typeDoc.Fields {
		out[field.Name] = field
	}
	return out
}

// attachFocusedBody projects the body block list for the node the pane is
// currently rendering plus every child-section descendant (INLINE sections
// and undeclared headings read open, PANE sections as collapsed disclosures).
// Compact collection rows do not need bodies, but the section renderers
// recurse through BodyWalker and therefore need the child node's Body.
func (b *workspaceGraphBuilder) attachFocusedBody(focusedID string) {
	if b.snapshot == nil || b.schema == nil {
		return
	}
	b.attachBodyTree(focusedID, map[string]struct{}{})
}

func (b *workspaceGraphBuilder) attachBodyTree(nodeID string, seen map[string]struct{}) {
	if nodeID == "" {
		return
	}
	if _, ok := seen[nodeID]; ok {
		return
	}
	seen[nodeID] = struct{}{}
	node := b.nodes[nodeID]
	if node == nil {
		return
	}
	var blocks []ontology.NodeBodyBlock
	switch node.Kind {
	case WorkspaceNodeKindNote, WorkspaceNodeKindSection, WorkspaceNodeKindEmbedded:
		blocks = b.bodyForRef(node.Ref)
		node.Body = blocks
	default:
		return
	}
	for _, block := range blocks {
		if !block.RendersInline() {
			continue
		}
		childID := b.ensureRefNode(*block.ChildRef, "")
		b.attachBodyTree(childID, seen)
	}
}

// bodyForRef reprojects `ref` against the shared snapshot to compute its body
// block list. Results are memoized so repeated calls (e.g. a node that also
// lives in a collection's ChildRefs) don't re-project.
func (b *workspaceGraphBuilder) bodyForRef(ref ontology.NodeRef) []ontology.NodeBodyBlock {
	if b.snapshot == nil || b.schema == nil {
		return nil
	}
	key := canonicalNodeRefKey(ref)
	if cached, ok := b.bodyCache[key]; ok {
		return cached
	}
	projection, err := ontology.ProjectBoundNodeFromSnapshot(b.snapshot, b.schema, ref)
	if err != nil {
		b.bodyCache[key] = nil
		return nil
	}
	blocks := ontology.BuildNodeBody(projection, b.schema)
	b.bodyCache[key] = blocks
	return blocks
}

func (b *workspaceGraphBuilder) addCollectionNodes(ownerID string) {
	for _, collection := range b.resp.Collections {
		id := workspaceSyntheticNodeID("collection", ownerID, collection.Name)
		node := &WorkspaceNodeResponse{
			ID:           id,
			Kind:         WorkspaceNodeKindCollection,
			Ref:          b.resp.Node.Ref,
			NotePath:     b.resp.Node.NotePath,
			ParentID:     ownerID,
			Status:       collection.Status,
			Capabilities: ontology.NodeCapabilities{},
			Collection: &WorkspaceCollectionNodeData{
				Name:             collection.Name,
				ItemRefs:         append([]ontology.NodeCollectionItemState(nil), collection.Items...),
				OrderFingerprint: collection.OrderFingerprint,
				Range:            collection.Range,
			},
		}
		for _, item := range collection.Items {
			targetID := b.ensureRefNode(item.Ref, "")
			if targetID == "" {
				continue
			}
			node.ChildIDs = appendUniqueChildID(node.ChildIDs, targetID)
			b.edges = append(b.edges, WorkspaceEdgeResponse{
				Kind:           WorkspaceEdgeKindCollectionItem,
				FromID:         id,
				ToID:           targetID,
				CollectionName: collection.Name,
			})
		}
		node.Version = workspaceGraphNodeVersion(node)
		b.nodes[id] = node
		b.addChild(ownerID, id)
	}
}

func (b *workspaceGraphBuilder) addRenderedOutline(focusedID string) []string {
	rendered := b.resp.Content.Rendered
	if rendered == nil || len(rendered.Sections) == 0 {
		return nil
	}
	rootIDs := make([]string, 0, len(rendered.Sections))
	parentID := focusedID
	if b.resp.Node.Ref.Kind != ontology.NodeKindNote {
		parentID = ""
	}
	for _, section := range rendered.Sections {
		rootID := b.addRenderedSectionNode(section, parentID, focusedID)
		if rootID != "" {
			rootIDs = append(rootIDs, rootID)
		}
	}
	return uniqueStrings(rootIDs)
}

func (b *workspaceGraphBuilder) addRenderedSectionNode(section RenderedSection, parentID, focusedID string) string {
	ref := refFromRenderedSection(section)
	id := workspaceNodeIDForRef(ref)
	node := b.ensureSectionLikeNode(id, ref, section, focusedID)
	if parentID != "" && parentID != id {
		node.ParentID = parentID
		b.addChild(parentID, id)
	}
	for _, child := range section.Children {
		_ = b.addRenderedSectionNode(child, id, focusedID)
	}
	node.Version = workspaceGraphNodeVersion(node)
	return id
}

func (b *workspaceGraphBuilder) addStructuralOutline(focusedID string) *WorkspaceStructuralOutlineViewResponse {
	structural := b.resp.Content.Structural
	if structural == nil {
		return nil
	}
	rootID := b.addStructuralNode(structural.Root, focusedID)
	view := &WorkspaceStructuralOutlineViewResponse{
		RootID:      rootID,
		DefaultView: structural.DefaultView,
		Tabs:        make([]WorkspaceStructuralTabViewResponse, 0, len(structural.Tabs)),
	}
	for _, tab := range structural.Tabs {
		tabIDs := make([]string, 0, len(tab.Nodes))
		for _, node := range tab.Nodes {
			tabID := b.addStructuralNode(node, focusedID)
			if tabID != "" {
				tabIDs = append(tabIDs, tabID)
			}
		}
		view.Tabs = append(view.Tabs, WorkspaceStructuralTabViewResponse{
			Key:       tab.Key,
			Label:     tab.Label,
			FieldName: tab.FieldName,
			Count:     tab.Count,
			NodeIDs:   uniqueStrings(tabIDs),
		})
	}
	return view
}

func (b *workspaceGraphBuilder) addStructuralNode(node StructuralNodeResponse, focusedID string) string {
	ref := refFromStructuralNode(node)
	id := workspaceNodeIDForRef(ref)
	if node.Locator == "FILE" {
		id = focusedID
	}
	entry := b.ensureStructuralNode(id, ref, node, focusedID)
	for _, child := range node.Children {
		childID := b.addStructuralNode(child, focusedID)
		if childID != "" && childID != id {
			b.addChild(id, childID)
			if childNode, ok := b.nodes[childID]; ok && childNode.ParentID == "" {
				childNode.ParentID = id
			}
		}
	}
	entry.Version = workspaceGraphNodeVersion(entry)
	return id
}
