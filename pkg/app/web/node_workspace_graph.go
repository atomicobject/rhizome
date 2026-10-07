package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type workspaceGraphBuilder struct {
	resp     *NodeWorkspaceResponse
	snapshot *ontology.DocumentSnapshot
	schema   *ontology.Schema
	nodes    map[string]*WorkspaceNodeResponse
	edges    []WorkspaceEdgeResponse
	// bodyCache memoizes per-node body projections keyed by canonical node id
	// so callers that reproject (e.g. ensureRefNode + addFocusedNode overlap)
	// only pay the projection cost once per response.
	bodyCache map[string][]ontology.NodeBodyBlock
}

func buildWorkspaceGraph(resp *NodeWorkspaceResponse, snapshot *ontology.DocumentSnapshot, schema *ontology.Schema) (string, []WorkspaceNodeResponse, []WorkspaceEdgeResponse, *WorkspaceViewsResponse) {
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
		parentID := b.ensureRefNode(*parentRef, titleFromPath(parentRef.NotePath))
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
			ValueRanges: append([]ontology.NodeRange(nil), field.ValueRanges...),
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
func fieldDocsByName(typeDoc *ontology.TypeDoc) map[string]ontology.FieldDoc {
	if typeDoc == nil || len(typeDoc.Fields) == 0 {
		return nil
	}
	out := make(map[string]ontology.FieldDoc, len(typeDoc.Fields))
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

func (b *workspaceGraphBuilder) addChild(parentID, childID string) {
	if parentID == "" || childID == "" || parentID == childID {
		return
	}
	parent := b.nodes[parentID]
	if parent == nil {
		return
	}
	parent.ChildIDs = appendUniqueChildID(parent.ChildIDs, childID)
	parent.Version = workspaceGraphNodeVersion(parent)
	for _, edge := range b.edges {
		if edge.Kind == WorkspaceEdgeKindContains && edge.FromID == parentID && edge.ToID == childID {
			return
		}
	}
	b.edges = append(b.edges, WorkspaceEdgeResponse{
		Kind:   WorkspaceEdgeKindContains,
		FromID: parentID,
		ToID:   childID,
	})
}

func workspaceNodeKindForRef(ref ontology.NodeRef) WorkspaceNodeKind {
	switch ref.Kind {
	case ontology.NodeKindEmbedded:
		return WorkspaceNodeKindEmbedded
	case ontology.NodeKindSection:
		return WorkspaceNodeKindSection
	default:
		return WorkspaceNodeKindNote
	}
}

// workspaceNodeIDForRef keys workspace nodes by pure location identity:
// (notePath, fragment, nodeId). Kind is intentionally excluded — a note/
// section/embedded node IS uniquely identified by its location in the
// vault, and Kind is a descriptive attribute (set from the schema's
// declared locator) rather than an identity dimension. Including Kind
// would fragment the graph when different call paths use different Kind
// detection strategies (e.g. fragment-prefix vs schema-aware), which is
// the bug that motivated this change.
func workspaceNodeIDForRef(ref ontology.NodeRef) string {
	return "node|" + workspaceNodeIdentityKey(ref)
}

func workspaceNodeIdentityKey(ref ontology.NodeRef) string {
	return strings.Join([]string{
		strings.TrimSpace(ref.NotePath),
		strings.TrimSpace(ref.Fragment),
		strings.TrimSpace(ref.NodeID),
	}, "|")
}

func workspaceSyntheticNodeID(prefix, parentID, name string) string {
	return prefix + "|" + parentID + "|" + strings.TrimSpace(name)
}

func workspaceFieldValueKind(field ontology.NodeFieldState) string {
	switch {
	case len(field.SectionNodes) > 0:
		return "section-ref"
	case len(field.Values) > 1:
		return "list"
	default:
		return "scalar"
	}
}

func refFromRenderedSection(section RenderedSection) ontology.NodeRef {
	return refFromSectionID(section.ID)
}

func refFromSectionID(id string) ontology.NodeRef {
	notePath, fragment, _ := strings.Cut(id, "#")
	ref := ontology.NodeRef{
		NotePath: notePath,
		Fragment: fragment,
		Kind:     ontology.NodeKindSection,
		NodeID:   id,
	}
	if strings.HasPrefix(fragment, "^") {
		ref.Kind = ontology.NodeKindEmbedded
	}
	return ref
}

func refFromStructuralNode(node StructuralNodeResponse) ontology.NodeRef {
	ref := ontology.NodeRef{
		NotePath: node.NotePath,
		Fragment: node.Fragment,
		NodeID:   node.NodeID,
		Kind:     ontology.NodeKindSection,
	}
	switch node.Locator {
	case "FILE":
		ref.Kind = ontology.NodeKindNote
		ref.NodeID = ""
	case "EMBEDDED":
		ref.Kind = ontology.NodeKindEmbedded
	case "SECTION":
		ref.Kind = ontology.NodeKindSection
	}
	return ref
}

// mergeNodeDataFromRenderedSection populates the uniform node data payload
// from a rendered section. Unknown locators default based on the structure
// of the section's ID (block-ID → EMBEDDED, otherwise SECTION); callers
// that know the kind authoritatively (schema-aware paths) should have set
// Locator on the node already so this merge preserves it.
func mergeNodeDataFromRenderedSection(current *WorkspaceNodeData, section RenderedSection) *WorkspaceNodeData {
	data := ensureNodeData(current)
	data.Title = firstNonEmpty(data.Title, section.Title)
	data.Level = firstNonEmptySectionLevel(data.Level, section.Level)
	defaultLocator := "SECTION"
	if strings.HasPrefix(strings.TrimSpace(section.BlockID), "") && section.BlockID != "" {
		defaultLocator = "EMBEDDED"
	}
	data.Locator = firstNonEmpty(data.Locator, firstNonEmpty(section.Locator, defaultLocator))
	data.BlockID = firstNonEmpty(data.BlockID, strings.TrimPrefix(section.BlockID, "^"))
	data.Fragment = firstNonEmpty(data.Fragment, renderedSectionFragment(section))
	data.Markdown = firstNonEmpty(data.Markdown, section.Content)
	if binding := bindingFromRenderedSection(section); binding != nil {
		data.Binding = mergeSectionBinding(data.Binding, binding)
	}
	return data
}

// mergeNodeDataFromStructuralNode populates the uniform node data payload
// from a structural-view node. Behaves symmetrically to the rendered-section
// version; together they cover the two places where section/embedded nodes
// are authored into the graph.
func mergeNodeDataFromStructuralNode(current *WorkspaceNodeData, node StructuralNodeResponse) *WorkspaceNodeData {
	data := ensureNodeData(current)
	data.Title = firstNonEmpty(data.Title, node.Title)
	data.ResolvedType = firstNonEmpty(data.ResolvedType, node.TypeName)
	data.Level = firstNonEmptySectionLevel(data.Level, node.Level)
	data.Locator = firstNonEmpty(data.Locator, node.Locator)
	data.BlockID = firstNonEmpty(data.BlockID, strings.TrimPrefix(node.Fragment, "^"))
	data.Fragment = firstNonEmpty(data.Fragment, node.Fragment)
	data.Markdown = firstNonEmpty(data.Markdown, node.Content)
	if binding := bindingFromStructuralNode(node); binding != nil {
		data.Binding = mergeSectionBinding(data.Binding, binding)
	}
	return data
}

func bindingFromRenderedSection(section RenderedSection) *WorkspaceSectionBindingData {
	if strings.TrimSpace(section.TypeName) == "" &&
		strings.TrimSpace(section.FieldName) == "" &&
		strings.TrimSpace(section.FieldPath) == "" &&
		len(section.Properties) == 0 &&
		strings.TrimSpace(section.PreviewTemplate) == "" &&
		!section.FieldList &&
		!section.Collapsed &&
		section.SectionDisplay == "" {
		return nil
	}
	return &WorkspaceSectionBindingData{
		TypeName:        section.TypeName,
		FieldName:       section.FieldName,
		FieldPath:       section.FieldPath,
		FieldList:       section.FieldList,
		SectionDisplay:  section.SectionDisplay,
		Properties:      cloneStringMap(section.Properties),
		IdentifierField: section.IdentifierField,
		PreviewTemplate: section.PreviewTemplate,
		Collapsed:       section.Collapsed,
	}
}

func bindingFromStructuralNode(node StructuralNodeResponse) *WorkspaceSectionBindingData {
	if strings.TrimSpace(node.TypeName) == "" &&
		strings.TrimSpace(node.FieldName) == "" &&
		strings.TrimSpace(node.FieldPath) == "" &&
		len(node.Properties) == 0 &&
		strings.TrimSpace(node.PreviewTemplate) == "" &&
		!node.FieldList &&
		!node.Collapsed &&
		node.SectionDisplay == "" {
		return nil
	}
	return &WorkspaceSectionBindingData{
		TypeName:        node.TypeName,
		FieldName:       node.FieldName,
		FieldPath:       node.FieldPath,
		FieldList:       node.FieldList,
		SectionDisplay:  node.SectionDisplay,
		Properties:      cloneStringMap(node.Properties),
		IdentifierField: node.IdentifierField,
		PreviewTemplate: node.PreviewTemplate,
		Collapsed:       node.Collapsed,
	}
}

func mergeSectionBinding(current, incoming *WorkspaceSectionBindingData) *WorkspaceSectionBindingData {
	if incoming == nil {
		return current
	}
	if current == nil {
		return incoming
	}
	current.TypeName = firstNonEmpty(current.TypeName, incoming.TypeName)
	current.FieldName = firstNonEmpty(current.FieldName, incoming.FieldName)
	current.FieldPath = firstNonEmpty(current.FieldPath, incoming.FieldPath)
	current.FieldList = current.FieldList || incoming.FieldList
	current.SectionDisplay = firstNonEmptySectionDisplay(current.SectionDisplay, incoming.SectionDisplay)
	if len(current.Properties) == 0 && len(incoming.Properties) > 0 {
		current.Properties = cloneStringMap(incoming.Properties)
	}
	current.IdentifierField = firstNonEmpty(current.IdentifierField, incoming.IdentifierField)
	current.PreviewTemplate = firstNonEmpty(current.PreviewTemplate, incoming.PreviewTemplate)
	current.Collapsed = current.Collapsed || incoming.Collapsed
	return current
}

func workspaceGraphNodeVersion(node *WorkspaceNodeResponse) string {
	if node == nil {
		return ""
	}
	clone := *node
	clone.Version = ""
	data, err := json.Marshal(clone)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func appendUniqueChildID(ids []string, value string) []string {
	for _, existing := range ids {
		if existing == value {
			return ids
		}
	}
	return append(ids, value)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneNoteWorkspaceGroups(groups []NoteWorkspaceGroup) []NoteWorkspaceGroup {
	if len(groups) == 0 {
		return nil
	}
	out := make([]NoteWorkspaceGroup, 0, len(groups))
	for _, group := range groups {
		cloned := NoteWorkspaceGroup{
			Key:   group.Key,
			Label: group.Label,
		}
		if len(group.Items) > 0 {
			cloned.Items = append([]NoteWorkspaceLink(nil), group.Items...)
		}
		out = append(out, cloned)
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmptySectionLevel(values ...ontology.SectionLevel) ontology.SectionLevel {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptySectionDisplay(values ...ontology.SectionDisplay) ontology.SectionDisplay {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
