package web

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
)

type NodeWorkspaceResponse struct {
	RequestedRef          string                          `json:"requestedRef"`
	FocusedNodeID         string                          `json:"focusedNodeId,omitempty"`
	Node                  NodeDescriptorResponse          `json:"node"`
	Content               NodeContentResponse             `json:"content"`
	Nodes                 []WorkspaceNodeResponse         `json:"nodes,omitempty"`
	Edges                 []WorkspaceEdgeResponse         `json:"edges,omitempty"`
	Views                 *WorkspaceViewsResponse         `json:"views,omitempty"`
	Fields                []ontology.NodeFieldState       `json:"fields,omitempty"`
	Collections           []ontology.NodeCollectionState  `json:"collections,omitempty"`
	Relations             []NoteWorkspaceGroup            `json:"relations,omitempty"`
	SectionRelationGroups map[string][]NoteWorkspaceGroup `json:"sectionRelationGroups,omitempty"`
	Loaded                NodeWorkspaceLoadedResponse     `json:"loaded"`
	Capabilities          ontology.NodeCapabilities       `json:"capabilities"`
	Status                ontology.NodeStatus             `json:"status"`
	Version               string                          `json:"version"`
	SourceRevision        ontology.NodeSourceRevision     `json:"sourceRevision"`
	NodeLocator           *ontology.NodeLocator           `json:"nodeLocator,omitempty"`
	LinkTarget            *ontology.NodeLinkTarget        `json:"linkTarget,omitempty"`
	LinkFixOps            []OntologyEditOp                `json:"linkFixOps,omitempty"`
}

type NodeWorkspaceLoadedResponse struct {
	Rendered   bool `json:"rendered"`
	Assessment bool `json:"assessment"`
	Structure  bool `json:"structure"`
	Relations  bool `json:"relations"`
}

type NodeDescriptorResponse struct {
	Ref          ontology.NodeRef      `json:"ref"`
	ResolvedType string                `json:"resolvedType,omitempty"`
	NotePath     string                `json:"notePath"`
	Title        string                `json:"title,omitempty"`
	Locator      string                `json:"locator"`
	NodeLocator  *ontology.NodeLocator `json:"nodeLocator,omitempty"`
	ParentRef    *ontology.NodeRef     `json:"parentRef,omitempty"`
}

type NodeContentResponse struct {
	Path                   string                          `json:"path"`
	Title                  string                          `json:"title"`
	ResolvedType           string                          `json:"resolvedType,omitempty"`
	Markdown               string                          `json:"markdown,omitempty"`
	Format                 noteformat.FormatID             `json:"format,omitempty"`
	SourceRepresentation   ontology.SourceRepresentation   `json:"sourceRepresentation,omitempty"`
	EvidenceRepresentation ontology.EvidenceRepresentation `json:"evidenceRepresentation,omitempty"`
	SourceCapabilities     []noteformat.Capability         `json:"sourceCapabilities,omitempty"`
	Rendered               *RenderedFileResponse           `json:"rendered,omitempty"`
	Assessment             *ontology.NoteAssessment        `json:"assessment,omitempty"`
	TypeDoc                *schemadoc.TypeDoc              `json:"typeDoc,omitempty"`
	Structural             *StructuralViewResponse         `json:"structural,omitempty"`
}

type WorkspaceNodeKind string

const (
	WorkspaceNodeKindNote       WorkspaceNodeKind = "note"
	WorkspaceNodeKindSection    WorkspaceNodeKind = "section"
	WorkspaceNodeKindEmbedded   WorkspaceNodeKind = "embedded"
	WorkspaceNodeKindField      WorkspaceNodeKind = "field"
	WorkspaceNodeKindCollection WorkspaceNodeKind = "collection"
)

type WorkspaceNodeResponse struct {
	ID           string                    `json:"id"`
	Kind         WorkspaceNodeKind         `json:"kind"`
	Ref          ontology.NodeRef          `json:"ref"`
	NotePath     string                    `json:"notePath"`
	ParentID     string                    `json:"parentId,omitempty"`
	ChildIDs     []string                  `json:"childIds,omitempty"`
	Status       ontology.NodeStatus       `json:"status"`
	Capabilities ontology.NodeCapabilities `json:"capabilities"`
	Version      string                    `json:"version,omitempty"`
	NodeLocator  *ontology.NodeLocator     `json:"nodeLocator,omitempty"`
	// Body is the ordered list of body blocks (narrative / inline_field /
	// child_section / collection) that make up this node's rendered body.
	// Populated for note/section/embedded nodes; empty for field/collection
	// synthetic nodes. Field and child-section blocks reference other nodes
	// via their canonical NodeRef, resolved against workspace.nodes[].
	Body []ontology.NodeBodyBlock `json:"body,omitempty"`
	// Data carries the uniform content payload for note/section/embedded
	// nodes. The three were previously modeled as separate types, but they
	// share the same shape (title + level + locator + blockId + fragment +
	// markdown + binding) and should be rendered uniformly on the client.
	// `Kind` stays on the response as an informational tag for callers that
	// route edit ops differently (e.g. block-id generation for embedded
	// types), but rendering must never branch on it.
	Data       *WorkspaceNodeData           `json:"data,omitempty"`
	Field      *WorkspaceFieldNodeData      `json:"field,omitempty"`
	Collection *WorkspaceCollectionNodeData `json:"collection,omitempty"`
}

// WorkspaceNodeData is the uniform payload for note/section/embedded nodes.
// All fields are optional — NOTE nodes omit Level/BlockID/Fragment/Binding;
// section/embedded nodes populate them as the source provides. Consumers
// should read these fields without switching on Kind.
type WorkspaceNodeData struct {
	Title        string                       `json:"title,omitempty"`
	ResolvedType string                       `json:"resolvedType,omitempty"`
	Markdown     string                       `json:"markdown,omitempty"`
	Locator      string                       `json:"locator,omitempty"`
	Level        ontoschema.SectionLevel      `json:"level,omitempty"`
	BlockID      string                       `json:"blockId,omitempty"`
	Fragment     string                       `json:"fragment,omitempty"`
	Binding      *WorkspaceSectionBindingData `json:"binding,omitempty"`
}

type WorkspaceSectionBindingData struct {
	TypeName        string                    `json:"typeName,omitempty"`
	FieldName       string                    `json:"fieldName,omitempty"`
	FieldPath       string                    `json:"fieldPath,omitempty"`
	FieldList       bool                      `json:"fieldList,omitempty"`
	SectionDisplay  ontoschema.SectionDisplay `json:"sectionDisplay,omitempty"`
	Properties      map[string]string         `json:"properties,omitempty"`
	IdentifierField string                    `json:"identifierField,omitempty"`
	PreviewTemplate string                    `json:"previewTemplate,omitempty"`
	Collapsed       bool                      `json:"collapsed,omitempty"`
}

type WorkspaceFieldNodeData struct {
	Name string `json:"name"`
	// ValueKind is a coarse shape hint (scalar/list/section-ref).
	ValueKind string `json:"valueKind"`
	// TypeName is the schema-declared type name for the field (e.g. "String",
	// "SpecStatus", "Date"). Empty when the owner note has no resolved type or
	// the field is not declared in the ontology.
	TypeName string `json:"typeName,omitempty"`
	// EnumValues is the ordered set of admitted values when TypeName names an
	// ontology enum. Empty for non-enum fields.
	EnumValues          []string                       `json:"enumValues,omitempty"`
	Identifier          bool                           `json:"identifier,omitempty"`
	PreferredIdentifier bool                           `json:"preferredIdentifier,omitempty"`
	Present             bool                           `json:"present"`
	Values              []string                       `json:"values,omitempty"`
	Range               ontology.ByteRange             `json:"range"`
	ValueRanges         []ontology.ByteRange           `json:"valueRanges,omitempty"`
	InlineSpans         []ontology.NodeInlineFieldSpan `json:"inlineSpans,omitempty"`
	SectionRefs         []ontology.NodeRef             `json:"sectionRefs,omitempty"`
}

type WorkspaceCollectionNodeData struct {
	Name             string                             `json:"name"`
	ItemRefs         []ontology.NodeCollectionItemState `json:"itemRefs,omitempty"`
	OrderFingerprint string                             `json:"orderFingerprint,omitempty"`
	Range            ontology.ByteRange                 `json:"range"`
}

type WorkspaceEdgeKind string

const (
	WorkspaceEdgeKindContains       WorkspaceEdgeKind = "contains"
	WorkspaceEdgeKindBindsField     WorkspaceEdgeKind = "binds_field"
	WorkspaceEdgeKindCollectionItem WorkspaceEdgeKind = "collection_item"
	WorkspaceEdgeKindRelatesTo      WorkspaceEdgeKind = "relates_to"
)

type WorkspaceEdgeResponse struct {
	Kind           WorkspaceEdgeKind `json:"kind"`
	FromID         string            `json:"fromId"`
	ToID           string            `json:"toId"`
	FieldName      string            `json:"fieldName,omitempty"`
	CollectionName string            `json:"collectionName,omitempty"`
	RelationKey    string            `json:"relationKey,omitempty"`
	RelationLabel  string            `json:"relationLabel,omitempty"`
	ScopeNodeID    string            `json:"scopeNodeId,omitempty"`
	Index          int               `json:"index,omitempty"`
}

type WorkspaceViewsResponse struct {
	RenderedOutline   *WorkspaceRenderedOutlineViewResponse   `json:"renderedOutline,omitempty"`
	StructuralOutline *WorkspaceStructuralOutlineViewResponse `json:"structuralOutline,omitempty"`
	RelationGroups    []WorkspaceRelationGroupsViewResponse   `json:"relationGroups,omitempty"`
}

type WorkspaceRenderedOutlineViewResponse struct {
	RootIDs []string `json:"rootIds,omitempty"`
}

type WorkspaceStructuralOutlineViewResponse struct {
	RootID      string                               `json:"rootId,omitempty"`
	DefaultView string                               `json:"defaultView,omitempty"`
	Tabs        []WorkspaceStructuralTabViewResponse `json:"tabs,omitempty"`
}

type WorkspaceStructuralTabViewResponse struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	FieldName string   `json:"fieldName,omitempty"`
	Count     int      `json:"count"`
	NodeIDs   []string `json:"nodeIds,omitempty"`
}

type WorkspaceRelationGroupsViewResponse struct {
	ScopeNodeID string               `json:"scopeNodeId,omitempty"`
	Groups      []NoteWorkspaceGroup `json:"groups,omitempty"`
}

type NoteWorkspaceGroup struct {
	Key        string              `json:"key"`
	Label      string              `json:"label"`
	OwnerTitle string              `json:"ownerTitle,omitempty"`
	Navigation bool                `json:"navigation,omitempty"`
	Items      []NoteWorkspaceLink `json:"items,omitempty"`
}

type NoteWorkspaceLink struct {
	Path           string                  `json:"path"`
	Title          string                  `json:"title"`
	TargetTitle    string                  `json:"targetTitle,omitempty"`
	Kind           string                  `json:"kind"`
	ResolvedType   string                  `json:"resolvedType,omitempty"`
	Anchor         string                  `json:"anchor,omitempty"`
	StructuralNode *StructuralNodeResponse `json:"structuralNode,omitempty"`
	RelationName   string                  `json:"relationName,omitempty"`
	Provenance     string                  `json:"provenance,omitempty"`
	Direction      string                  `json:"direction,omitempty"`
	Structural     bool                    `json:"structural,omitempty"`
	Current        bool                    `json:"current,omitempty"`
	Score          float64                 `json:"score,omitempty"`
	Summary        string                  `json:"summary,omitempty"`
}

// Direction values for NoteWorkspaceLink: whether the focused note points at
// the target or the target points at the focused note.
const (
	linkDirectionOutgoing = "outgoing"
	linkDirectionIncoming = "incoming"
)

// workspaceLinkDirection reports which way the authored link runs given the
// edge's orientation relative to the focused note. The index mirrors every
// body link A→B with a synthetic B→A edge whose provenance is "backlink".
func workspaceLinkDirection(inbound bool, provenance string) string {
	if inbound != (provenance == "backlink") {
		return linkDirectionIncoming
	}
	return linkDirectionOutgoing
}
