package ontology

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var ErrAmbiguousSourceSpanIdentity = errors.New("ambiguous source span identity")

type ByteRange struct {
	Start int
	End   int
}

// Len returns the byte width of the range.
func (r ByteRange) Len() int {
	if r.End <= r.Start {
		return 0
	}
	return r.End - r.Start
}

// Valid reports whether the range fits inside content of the given length.
func (r ByteRange) Valid(max int) bool {
	return r.Start >= 0 && r.End >= r.Start && r.End <= max
}

type NodeKind string

const (
	NodeKindNote     NodeKind = "NOTE"
	NodeKindSection  NodeKind = "SECTION"
	NodeKindEmbedded NodeKind = "EMBEDDED"
)

type BindingKind string

const (
	BindingKindFrontmatterField BindingKind = "FRONTMATTER_FIELD"
	BindingKindInlineField      BindingKind = "INLINE_FIELD"
	BindingKindCheckboxField    BindingKind = "CHECKBOX_FIELD"
	BindingKindSectionField     BindingKind = "SECTION_FIELD"
	BindingKindSectionList      BindingKind = "SECTION_LIST"
)

// NodeRef identifies a note-, section-, or embedded-node projection within a markdown file.
// Docs: [[structural-node-model-and-ontology-read-path]] owns the identity contract, and [[ontology-browser-workspace#^spec-0014-us2-ac1]] is the product reason browser paths cannot collapse this back to note path alone.
type NodeRef struct {
	NotePath   string   `json:"notePath"`
	Fragment   string   `json:"fragment,omitempty"`
	NodeID     string   `json:"nodeId,omitempty"`
	TypeName   string   `json:"typeName,omitempty"`
	Kind       NodeKind `json:"kind"`
	StartByte  int      `json:"startByte,omitempty"`
	EndByte    int      `json:"endByte,omitempty"`
	ParentID   string   `json:"parentId,omitempty"`
	Structural string   `json:"structuralFingerprint,omitempty"`
}

// String returns the canonical note-or-fragment locator for the ref.
//
// It is intentionally author-facing, not a complete cache/write identity:
// callers that care about embedded nodes must also carry NodeID, Kind, and the
// structural fingerprint.
func (r NodeRef) String() string {
	if strings.TrimSpace(r.Fragment) == "" {
		return strings.TrimSpace(r.NotePath)
	}
	return strings.TrimSpace(r.NotePath) + "#" + strings.TrimPrefix(strings.TrimSpace(r.Fragment), "#")
}

// IsZero reports whether the ref points at anything.
func (r NodeRef) IsZero() bool {
	return strings.TrimSpace(r.NotePath) == ""
}

// DocumentSnapshot preserves the raw markdown plus the syntax ranges needed to
// project and minimally edit ontology-backed nodes.
type DocumentSnapshot struct {
	NotePath             string
	Content              string
	Frontmatter          map[string]any
	FrontmatterRange     ByteRange
	Sections             []*SectionNode
	SectionsByID         map[string]*SectionNode
	SourceSpans          []*MarkdownSourceSpan
	SourceSpansByID      map[string]*MarkdownSourceSpan
	ModTime              time.Time
	ContentFingerprint   string
	StructureFingerprint string
}

// InlineFieldSpan captures one Dataview-style inline property and its byte ranges.
type InlineFieldSpan struct {
	Key            string
	Value          string
	LineRange      ByteRange
	KeyRange       ByteRange
	ValueRange     ByteRange
	WholeRange     ByteRange
	PropertyKey    string
	AuthoringStyle FieldAuthoringStyle
}

// FieldBinding describes where an ontology field is authored inside a snapshot.
type FieldBinding struct {
	FieldName   string
	Kind        BindingKind
	SourceKind  FieldSource
	Present     bool
	Range       ByteRange
	ValueRanges []ByteRange
	// ValueRangesExact reports that ValueRanges maps 1:1 to Values in authored
	// order. False means the semantic values remain readable, but their YAML or
	// Markdown spelling is not safe for source patching.
	ValueRangesExact bool
	Values           []string
	InlineSpans      []InlineFieldSpan
	SectionNodes     []NodeRef
	// Derived is true when Values were synthesized from structure rather than
	// authored in source — currently only set for derivable preferred-identifier
	// fields (SPEC-0023.US8).
	Derived bool
}

// CollectionItem identifies one ordered member of a projected collection.
type CollectionItem struct {
	Ref   NodeRef
	Range ByteRange
}

// CollectionBinding captures the authored order and text span for a list-like field.
type CollectionBinding struct {
	FieldName        string
	Kind             BindingKind
	Range            ByteRange
	Items            []CollectionItem
	OrderFingerprint string
}

// NodeProjection is the source-preserving ontology view over a single node ref.
type NodeProjection struct {
	Ref      NodeRef
	Snapshot *DocumentSnapshot
	// RootSnapshot is present for every provider-backed note root. Structural
	// projections retain Snapshot; root-only providers deliberately do not.
	RootSnapshot *RootDocumentSnapshot
	ResolvedType string
	Type         *NoteType
	Assessment   *NoteAssessment
	PropertyCase PropertyCase
	Fields       map[string]FieldBinding
	Collections  map[string]CollectionBinding
}

type projectionResolver struct {
	snapshot         *DocumentSnapshot
	schema           *Schema
	noteDoc          *noteDoc
	assessment       *NoteAssessment
	noteType         *NoteType
	sectionTypeByID  map[string]string
	parentIDByID     map[string]string
	propertyCaseByID map[string]PropertyCase
	// sourceSpanFingerprintByID keeps recursive parent identity lookup linear.
	sourceSpanFingerprintByID map[string]string
	// sourceSpansByStructural makes relocation and ambiguity checks linear per snapshot.
	sourceSpansByStructural map[string][]*MarkdownSourceSpan
}

// LoadDocumentSnapshot reads and parses one markdown file into a reusable syntax snapshot.
func LoadDocumentSnapshot(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, notePath string) (*DocumentSnapshot, error) {
	if noteMgr == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	content, err := noteMgr.GetContents(vaultDef, notePath)
	if err != nil {
		return nil, err
	}
	modTime, err := noteMgr.GetModTime(vaultDef, notePath)
	if err != nil {
		modTime = time.Time{}
	}
	return BuildDocumentSnapshot(notePath, content, modTime)
}

// BuildDocumentSnapshot parses already-loaded markdown into a syntax snapshot.
func BuildDocumentSnapshot(notePath, content string, modTime time.Time) (*DocumentSnapshot, error) {
	fm, err := obsidian.ExtractFrontmatter(content)
	if err != nil {
		// Malformed YAML should not make ontology projection unavailable for the
		// whole document. Treat invalid frontmatter as absent and continue
		// projecting headings/embedded nodes from the markdown body.
		fm = nil
	}
	sections := ParseSections(notePath, content)
	sectionsByID := make(map[string]*SectionNode, len(sections))
	var walk func(nodes []*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			sectionsByID[node.ID] = node
			walk(node.Children)
		}
	}
	walk(sections)
	sourceSpans := append(sectionMarkdownSourceSpans(sections), ParseMarkdownListSourceSpans(notePath, content)...)
	linkListSourceSpansToSections(sourceSpans)
	sourceSpansByID := make(map[string]*MarkdownSourceSpan, len(sourceSpans))
	for _, span := range sourceSpans {
		if span == nil || strings.TrimSpace(span.ID) == "" {
			continue
		}
		sourceSpansByID[span.ID] = span
	}
	return &DocumentSnapshot{
		NotePath:             notePath,
		Content:              content,
		Frontmatter:          cloneAnyMap(fm),
		FrontmatterRange:     frontmatterRange(content),
		Sections:             sections,
		SectionsByID:         sectionsByID,
		SourceSpans:          sourceSpans,
		SourceSpansByID:      sourceSpansByID,
		ModTime:              modTime,
		ContentFingerprint:   hashText(content),
		StructureFingerprint: hashText(structureFingerprintText(notePath, sections)),
	}, nil
}

// ProjectNote resolves the file-backed note node for the given path.
func ProjectNote(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, schema *Schema, notePath string) (*NodeProjection, error) {
	return ProjectNode(ctx, vaultDef, noteMgr, schema, NodeRef{NotePath: notePath, Kind: NodeKindNote})
}

// ProjectNode resolves a note, section, or embedded-node ref against the current file contents.
// Docs: [[ontology-browser-workspace#^spec-0014-us2-ac1]] and [[linkable-embedded-node-identifiers#^spec-0023-us1-ac2]] explain why callers should preserve the resolved ref and locator-capable identity.
func ProjectNode(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, schema *Schema, ref NodeRef) (*NodeProjection, error) {
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	snapshot, err := LoadDocumentSnapshot(ctx, vaultDef, noteMgr, ref.NotePath)
	if err != nil {
		return nil, err
	}
	return ProjectNodeFromSnapshot(snapshot, schema, ref)
}

// ProjectNodeFromSnapshot resolves a node ref against an existing document snapshot.
//
// Prefer this in batch/read paths that already loaded markdown. It avoids a
// second filesystem read and guarantees parent/child projections share the same
// content and structural fingerprint.
func ProjectNodeFromSnapshot(snapshot *DocumentSnapshot, schema *Schema, ref NodeRef) (*NodeProjection, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("document snapshot is required")
	}
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil, err
	}
	return resolver.project(ref)
}

// ProjectBoundNodeFromSnapshot projects a ref emitted by a projection over the
// same snapshot. The caller must prove that provenance. Exact byte ranges and
// structural identity are both verified; callers must use ProjectNodeFromSnapshot
// for catalog results, external input, or replayed refs.
func ProjectBoundNodeFromSnapshot(snapshot *DocumentSnapshot, schema *Schema, ref NodeRef) (*NodeProjection, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("document snapshot is required")
	}
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil, err
	}
	return resolver.projectCurrent(ref)
}

// ProjectNodesFromSnapshot resolves multiple refs against one parsed document
// snapshot and one projection resolver.
func ProjectNodesFromSnapshot(snapshot *DocumentSnapshot, schema *Schema, refs []NodeRef) ([]*NodeProjection, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("document snapshot is required")
	}
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil, err
	}
	out := make([]*NodeProjection, 0, len(refs))
	for _, ref := range refs {
		projection, err := resolver.project(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, projection)
	}
	return out, nil
}

// GlobalSourceNodeRefsFromSnapshot returns embedded-node refs declared by
// object-level @source directives, independent of any containing note field.
func GlobalSourceNodeRefsFromSnapshot(snapshot *DocumentSnapshot, schema *Schema) ([]NodeRef, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("document snapshot is required")
	}
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil, err
	}
	return resolver.globalSourceNodeRefs(), nil
}

func (r *projectionResolver) globalSourceNodeRefs() []NodeRef {
	out := make([]NodeRef, 0)
	for _, span := range r.snapshot.SourceSpans {
		if span == nil || span.Shape == EmbeddedSourceShapeSection {
			continue
		}
		typeName := r.sectionTypeByID[span.ID]
		noteType := r.schema.Types[typeName]
		if noteType == nil || noteType.SourceShape == "" {
			continue
		}
		out = append(out, r.sourceSpanNodeRef(span))
	}
	return out
}

// SemanticParentRef returns the nearest authored ontology-node parent for a ref.
// Wrapper sections are structural containers, not semantic parents: embedded
// nodes belong to the nearest enclosing embedded node, or the note root when no
// enclosing embedded node exists.
func SemanticParentRef(snapshot *DocumentSnapshot, schema *Schema, ref NodeRef) (NodeRef, bool, error) {
	if snapshot == nil {
		return NodeRef{}, false, fmt.Errorf("document snapshot is required")
	}
	if schema == nil {
		return NodeRef{}, false, fmt.Errorf("ontology schema is required")
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return NodeRef{}, false, err
	}
	if span, _, ok, resolveErr := resolver.resolveSourceSpanRef(ref); resolveErr != nil {
		return NodeRef{}, false, resolveErr
	} else if ok && span.Shape != EmbeddedSourceShapeSection {
		for parentID := strings.TrimSpace(resolver.parentIDByID[span.ID]); parentID != ""; parentID = strings.TrimSpace(resolver.parentIDByID[parentID]) {
			parentSpan := snapshot.SourceSpansByID[parentID]
			if parentSpan == nil {
				continue
			}
			parentTypeName := resolver.sectionTypeByID[parentID]
			parentType := schema.Types[parentTypeName]
			if parentType != nil && parentType.Role == TypeRoleEmbeddedNode {
				return resolver.sourceSpanNodeRef(parentSpan), true, nil
			}
		}
		if resolver.noteType == nil {
			return NodeRef{}, false, nil
		}
		return NodeRef{
			NotePath:   snapshot.NotePath,
			Kind:       NodeKindNote,
			TypeName:   resolver.noteType.Name,
			StartByte:  0,
			EndByte:    len(snapshot.Content),
			Structural: snapshot.StructureFingerprint,
		}, true, nil
	}
	node, _, err := resolver.resolveSectionRef(ref)
	if err != nil {
		return NodeRef{}, false, err
	}
	for parentID := strings.TrimSpace(resolver.parentIDByID[node.ID]); parentID != ""; parentID = strings.TrimSpace(resolver.parentIDByID[parentID]) {
		parentNode := snapshot.SectionsByID[parentID]
		if parentNode == nil {
			continue
		}
		parentTypeName := resolver.sectionTypeByID[parentID]
		parentType := schema.Types[parentTypeName]
		if parentType != nil && parentType.Role == TypeRoleEmbeddedNode {
			return resolver.sectionNodeRef(parentNode), true, nil
		}
	}
	if resolver.noteType == nil {
		return NodeRef{}, false, nil
	}
	return NodeRef{
		NotePath:   snapshot.NotePath,
		Kind:       NodeKindNote,
		TypeName:   resolver.noteType.Name,
		StartByte:  0,
		EndByte:    len(snapshot.Content),
		Structural: snapshot.StructureFingerprint,
	}, true, nil
}

func newProjectionResolver(snapshot *DocumentSnapshot, schema *Schema) (*projectionResolver, error) {
	doc := &noteDoc{
		Path:        snapshot.NotePath,
		Title:       projectionNoteTitle(snapshot.NotePath, snapshot.Frontmatter, snapshot.Sections),
		Content:     snapshot.Content,
		Frontmatter: cloneAnyMap(snapshot.Frontmatter),
		Inline:      obsidian.ExtractInlineProperties(snapshot.Content),
		TypeName:    stringValue(snapshot.Frontmatter[TypeFieldName()]),
	}
	assessment := resolveNoteAssessment(doc, schema)
	var noteType *NoteType
	if assessment != nil && assessment.ResolvedType != "" {
		noteType = schema.Types[assessment.ResolvedType]
		doc.TypeName = assessment.ResolvedType
	}
	resolver := &projectionResolver{
		snapshot:         snapshot,
		schema:           schema,
		noteDoc:          doc,
		assessment:       assessment,
		noteType:         noteType,
		sectionTypeByID:  map[string]string{},
		parentIDByID:     map[string]string{},
		propertyCaseByID: map[string]PropertyCase{},
	}
	if noteType != nil {
		resolver.indexSectionTypesForNote(noteType)
	}
	resolver.indexGlobalSourceTypes()
	return resolver, nil
}
