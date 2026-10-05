package ontology

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type NodeRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type NodeDescriptor struct {
	Ref          NodeRef  `json:"ref"`
	ResolvedType string   `json:"resolvedType,omitempty"`
	NotePath     string   `json:"notePath"`
	Title        string   `json:"title,omitempty"`
	Locator      string   `json:"locator"`
	ParentRef    *NodeRef `json:"parentRef,omitempty"`
}

type NodeInlineFieldSpan struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	LineRange   NodeRange `json:"lineRange"`
	KeyRange    NodeRange `json:"keyRange"`
	ValueRange  NodeRange `json:"valueRange"`
	WholeRange  NodeRange `json:"wholeRange"`
	PropertyKey string    `json:"propertyKey,omitempty"`
}

type NodeFieldState struct {
	Name         string                `json:"name"`
	Kind         BindingKind           `json:"kind"`
	SourceKind   FieldSource           `json:"sourceKind,omitempty"`
	Present      bool                  `json:"present"`
	Status       NodeStatus            `json:"status"`
	Range        NodeRange             `json:"range"`
	ValueRanges  []NodeRange           `json:"valueRanges,omitempty"`
	Values       []string              `json:"values,omitempty"`
	InlineSpans  []NodeInlineFieldSpan `json:"inlineSpans,omitempty"`
	SectionNodes []NodeRef             `json:"sectionNodes,omitempty"`
	Capability   NodeFieldCapability   `json:"capability"`
	Issues       []ValidationIssue     `json:"issues,omitempty"`
}

type NodeSourceRevision struct {
	NotePath           string `json:"notePath"`
	ContentFingerprint string `json:"contentFingerprint"`
	Content            string `json:"content"`
}

type NodeCollectionItemState struct {
	Ref   NodeRef   `json:"ref"`
	Range NodeRange `json:"range"`
}

type NodeCollectionState struct {
	Name             string                    `json:"name"`
	Kind             BindingKind               `json:"kind"`
	Status           NodeStatus                `json:"status"`
	Range            NodeRange                 `json:"range"`
	Items            []NodeCollectionItemState `json:"items,omitempty"`
	OrderFingerprint string                    `json:"orderFingerprint,omitempty"`
}

type NodeCapabilities struct {
	CanEdit             bool `json:"canEdit"`
	CanEditFields       bool `json:"canEditFields"`
	CanEditCollections  bool `json:"canEditCollections"`
	CanNavigateChildren bool `json:"canNavigateChildren"`
	CanSubscribe        bool `json:"canSubscribe"`
}

type NodeValidationStatus struct {
	IssueCount int `json:"issueCount"`
}

type NodeFreshnessStatus struct {
	State string `json:"state,omitempty"`
}

type NodeSessionStatus struct {
	State string `json:"state,omitempty"`
}

type NodeStatus struct {
	Dirty       bool                 `json:"dirty"`
	Validation  NodeValidationStatus `json:"validation"`
	Freshness   NodeFreshnessStatus  `json:"freshness"`
	Session     NodeSessionStatus    `json:"session"`
	HasWarnings bool                 `json:"hasWarnings"`
}

type NodeContent struct {
	Markdown string `json:"markdown,omitempty"`
}

type NodeEvent struct {
	ID   string  `json:"id"`
	Kind string  `json:"kind"`
	Ref  NodeRef `json:"ref"`
	// CanonicalRef carries the node's fresh identity when the subscribed ref
	// has gone stale — an embedded item without a block ID is addressed by byte
	// offset, so an edit above it renumbers the node. Ref stays the subscribed
	// ref so delivery and pane matching still key on it; consumers reload from
	// CanonicalRef when it is present.
	CanonicalRef *NodeRef `json:"canonicalRef,omitempty"`
	Version      string   `json:"version,omitempty"`
	Cause        string   `json:"cause,omitempty"`
	Changed      []string `json:"changed,omitempty"`
}

type NodeWorkspace struct {
	Node           NodeDescriptor        `json:"node"`
	Content        NodeContent           `json:"content"`
	Fields         []NodeFieldState      `json:"fields,omitempty"`
	Collections    []NodeCollectionState `json:"collections,omitempty"`
	Capabilities   NodeCapabilities      `json:"capabilities"`
	Status         NodeStatus            `json:"status"`
	Version        string                `json:"version"`
	SourceRevision NodeSourceRevision    `json:"sourceRevision"`
}

// BuildNodeWorkspace resolves the focused ref into the canonical browser pane
// model. Docs: [[ontology-browser-workspace#^spec-0014-us2-ac1]] requires note,
// section, and embedded-node panes to share this identity/status/capability shape.
func BuildNodeWorkspace(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, schema *Schema, ref NodeRef) (*NodeWorkspace, error) {
	projection, err := ProjectNode(ctx, vaultDef, noteMgr, schema, ref)
	if err != nil {
		return nil, err
	}
	return BuildNodeWorkspaceFromProjectionWithSchema(schema, projection), nil
}

func BuildNodeWorkspaceFromProjection(projection *NodeProjection) *NodeWorkspace {
	return BuildNodeWorkspaceFromProjectionWithSchema(nil, projection)
}

// BuildNodeWorkspaceFromProjectionWithSchema builds a workspace descriptor with
// schema-aware parent refs that skip structural wrapper sections.
// Docs: [[ontology-browser-workspace#^spec-0014-us2-ac3]] keeps derived views
// downstream of this snapshot instead of letting UI payloads invent identities.
func BuildNodeWorkspaceFromProjectionWithSchema(schema *Schema, projection *NodeProjection) *NodeWorkspace {
	if projection == nil {
		return nil
	}
	workspace := &NodeWorkspace{
		Node:           buildNodeDescriptor(schema, projection),
		Content:        NodeContent{Markdown: nodeProjectionMarkdown(projection)},
		Fields:         buildNodeFieldStates(schema, projection),
		Collections:    buildNodeCollectionStates(projection),
		Capabilities:   buildNodeCapabilities(projection),
		Status:         NodeStatus{},
		SourceRevision: buildNodeSourceRevision(projection),
	}
	workspace.Version = buildNodeVersion(workspace)
	return workspace
}

func buildNodeDescriptor(schema *Schema, projection *NodeProjection) NodeDescriptor {
	ref := projection.Ref
	desc := NodeDescriptor{
		Ref:          ref,
		ResolvedType: firstNonEmptyNodeValue(ref.TypeName, projection.ResolvedType),
		NotePath:     ref.NotePath,
		Title:        nodeProjectionTitle(projection),
		Locator:      string(ref.Kind),
	}
	if schema != nil && ref.Kind != NodeKindNote {
		if parent, ok, err := SemanticParentRef(projection.Snapshot, schema, ref); err == nil && ok {
			desc.ParentRef = &parent
			return desc
		}
	}
	if strings.TrimSpace(ref.ParentID) != "" {
		parent := NodeRef{
			NotePath: ref.NotePath,
			NodeID:   ref.ParentID,
			Kind:     NodeKindSection,
		}
		desc.ParentRef = &parent
	}
	return desc
}

func buildNodeFieldStates(schema *Schema, projection *NodeProjection) []NodeFieldState {
	if projection == nil || len(projection.Fields) == 0 {
		return nil
	}
	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]NodeFieldState, 0, len(names))
	for _, name := range names {
		binding := projection.Fields[name]
		state := NodeFieldState{
			Name:        name,
			Kind:        binding.Kind,
			SourceKind:  binding.SourceKind,
			Present:     binding.Present,
			Range:       nodeRange(binding.Range),
			ValueRanges: make([]NodeRange, 0, len(binding.ValueRanges)),
			Values:      append([]string(nil), binding.Values...),
			InlineSpans: make([]NodeInlineFieldSpan, 0, len(binding.InlineSpans)),
		}
		var field *Field
		if projection.Type != nil {
			field = projection.Type.ByName[name]
		}
		state.Capability = buildNodeFieldCapability(schema, projection, field, binding)
		// NoteAssessment is file-root scoped. Reusing it for an embedded or
		// section owner can attach a root field's diagnostics to a same-named
		// nested field, so nested diagnostics stay empty until projection owns
		// an owner-specific assessment.
		if projection.Ref.Kind == NodeKindNote && projection.Assessment != nil {
			if assessed, ok := projection.Assessment.Field(name); ok {
				state.Issues = append([]ValidationIssue(nil), assessed.Issues...)
			}
		}
		for _, r := range binding.ValueRanges {
			state.ValueRanges = append(state.ValueRanges, nodeRange(r))
		}
		for _, span := range binding.InlineSpans {
			state.InlineSpans = append(state.InlineSpans, NodeInlineFieldSpan{
				Key:         span.Key,
				Value:       span.Value,
				LineRange:   nodeRange(span.LineRange),
				KeyRange:    nodeRange(span.KeyRange),
				ValueRange:  nodeRange(span.ValueRange),
				WholeRange:  nodeRange(span.WholeRange),
				PropertyKey: span.PropertyKey,
			})
		}
		state.SectionNodes = append([]NodeRef(nil), binding.SectionNodes...)
		out = append(out, state)
	}
	return out
}

func buildNodeSourceRevision(projection *NodeProjection) NodeSourceRevision {
	if projection == nil {
		return NodeSourceRevision{}
	}
	if projection.RootSnapshot != nil {
		return NodeSourceRevision{
			NotePath:           projection.RootSnapshot.NotePath.String(),
			ContentFingerprint: projection.RootSnapshot.ContentHash,
			Content:            string(projection.RootSnapshot.RawSource),
		}
	}
	if projection.Snapshot == nil {
		return NodeSourceRevision{}
	}
	return NodeSourceRevision{
		NotePath:           projection.Snapshot.NotePath,
		ContentFingerprint: projection.Snapshot.ContentFingerprint,
		Content:            projection.Snapshot.Content,
	}
}

func buildNodeCollectionStates(projection *NodeProjection) []NodeCollectionState {
	if projection == nil || len(projection.Collections) == 0 {
		return nil
	}
	names := make([]string, 0, len(projection.Collections))
	for name := range projection.Collections {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]NodeCollectionState, 0, len(names))
	for _, name := range names {
		binding := projection.Collections[name]
		state := NodeCollectionState{
			Name:             name,
			Kind:             binding.Kind,
			Range:            nodeRange(binding.Range),
			OrderFingerprint: binding.OrderFingerprint,
			Items:            make([]NodeCollectionItemState, 0, len(binding.Items)),
		}
		for _, item := range binding.Items {
			state.Items = append(state.Items, NodeCollectionItemState{
				Ref:   item.Ref,
				Range: nodeRange(item.Range),
			})
		}
		out = append(out, state)
	}
	return out
}

func buildNodeCapabilities(projection *NodeProjection) NodeCapabilities {
	canEditFields := len(projection.Fields) > 0
	canEditCollections := len(projection.Collections) > 0
	canNavigateChildren := canEditCollections
	if !canNavigateChildren {
		for _, field := range projection.Fields {
			if len(field.SectionNodes) > 0 {
				canNavigateChildren = true
				break
			}
		}
	}
	return NodeCapabilities{
		CanEdit:             canEditFields || canEditCollections,
		CanEditFields:       canEditFields,
		CanEditCollections:  canEditCollections,
		CanNavigateChildren: canNavigateChildren,
		CanSubscribe:        true,
	}
}

// buildNodeVersion computes a content-addressable version hash from the
// rendered workspace. SourceRevision is intentionally excluded: it captures
// the whole file for safe first-input editing, while a nested node version
// remains stable when an unrelated sibling changes.
func buildNodeVersion(workspace *NodeWorkspace) string {
	if workspace == nil {
		return ""
	}
	versioned := *workspace
	versioned.SourceRevision = NodeSourceRevision{}
	data, err := json.Marshal(versioned)
	if err != nil {
		return ""
	}
	return hashText(string(data))
}

func nodeProjectionTitle(projection *NodeProjection) string {
	if projection == nil {
		return ""
	}
	switch projection.Ref.Kind {
	case NodeKindNote:
		if projection.Snapshot == nil {
			if projection.RootSnapshot != nil {
				return strings.TrimSpace(projection.RootSnapshot.Title)
			}
			return ""
		}
		return projectionNoteTitle(
			projection.Ref.NotePath,
			projection.Snapshot.Frontmatter,
			projection.Snapshot.Sections,
		)
	case NodeKindSection, NodeKindEmbedded:
		if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
			return strings.TrimSpace(node.Title)
		}
		// Non-section embedded shapes (CHECKBOX_ITEM, LIST_ITEM) live in
		// SourceSpansByID rather than SectionsByID. Their first-line title is
		// already extracted by the parser; surface it here so views and
		// workspaces don't fall through to the host file's basename.
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			return strings.TrimSpace(span.Title)
		}
	}
	return ""
}

func nodeProjectionMarkdown(projection *NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	switch projection.Ref.Kind {
	case NodeKindNote:
		return strings.TrimSpace(projection.Snapshot.Content)
	case NodeKindSection, NodeKindEmbedded:
		if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
			return strings.TrimSpace(node.Content)
		}
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			return strings.TrimSpace(span.Content)
		}
	}
	return ""
}

func nodeRange(r ByteRange) NodeRange {
	return NodeRange{Start: r.Start, End: r.End}
}

func firstNonEmptyNodeValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
