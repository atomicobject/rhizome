package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

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

func firstNonEmptySectionLevel(values ...ontoschema.SectionLevel) ontoschema.SectionLevel {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptySectionDisplay(values ...ontoschema.SectionDisplay) ontoschema.SectionDisplay {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
