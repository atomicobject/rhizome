package ontology

import (
	"sort"
	"strings"
)

// NodeBodyBlockKind identifies a body block's role in a node's source-preserving
// body projection.
type NodeBodyBlockKind string

const (
	// NodeBodyBlockKindNarrative is a contiguous run of prose inside the node's
	// content range, with no inline fields or child sections in it.
	NodeBodyBlockKindNarrative NodeBodyBlockKind = "narrative"
	// NodeBodyBlockKindInlineField marks the source position of a Dataview-style
	// inline property span. Widgets render these; the block carries the field
	// name + raw key so the walker can pick the right editor.
	NodeBodyBlockKindInlineField NodeBodyBlockKind = "inline_field"
	// NodeBodyBlockKindChildSection is a single non-list child section or
	// embedded node. SectionDisplay (from the parent's @contains binding)
	// tells the walker whether the section reads open (INLINE) or as a
	// collapsed disclosure (PANE); both render their body in place.
	NodeBodyBlockKindChildSection NodeBodyBlockKind = "child_section"
	// NodeBodyBlockKindCollection groups consecutive list-bound siblings under
	// one block so the UI can render them as one typed collection (searchable
	// table, etc.) rather than a sequence of unrelated child sections.
	NodeBodyBlockKindCollection NodeBodyBlockKind = "collection"
)

// NodeBodyBlock is one position in a node's body projection. Each block has
// a byte range in the note that unambiguously identifies what's being
// rendered; editing a block targets exactly that range without mangling
// surrounding structure.
type NodeBodyBlock struct {
	Kind           NodeBodyBlockKind `json:"kind"`
	Range          NodeRange         `json:"range"`
	Markdown       string            `json:"markdown,omitempty"`       // narrative
	FieldName      string            `json:"fieldName,omitempty"`      // inline_field | child_section | collection
	RawKey         string            `json:"rawKey,omitempty"`         // inline_field: key as authored
	ChildRef       *NodeRef          `json:"childRef,omitempty"`       // child_section
	ChildRefs      []NodeRef         `json:"childRefs,omitempty"`      // collection
	SectionDisplay SectionDisplay    `json:"sectionDisplay,omitempty"` // child_section | collection
}

// Undeclared reports whether a child_section block is a heading that no
// schema field declares: ordinary document structure owned by its parent.
func (b NodeBodyBlock) Undeclared() bool {
	return b.Kind == NodeBodyBlockKindChildSection && b.ChildRef != nil && b.FieldName == ""
}

// RendersInline reports whether a child_section block's body belongs inside
// its parent's body. Every child section does: INLINE sections and undeclared
// headings read open, and PANE sections read as collapsed disclosures that
// still edit in place. Collection items stay rows that open as their own node.
func (b NodeBodyBlock) RendersInline() bool {
	return b.Kind == NodeBodyBlockKindChildSection && b.ChildRef != nil
}

// BuildNodeBody emits the ordered sequence of body blocks for a projection.
// The projection must be source-preserving (snapshot + schema); the blocks
// together cover the node's content range exactly, in byte order.
//
// `schema` is optional but recommended: when present, child section refs
// get the correct `NodeKindEmbedded` for types declared `@node(locator:
// EMBEDDED)`, so the emitted refs match the projection's canonical refs
// used elsewhere in the workspace graph.
func BuildNodeBody(projection *NodeProjection, schema *Schema) []NodeBodyBlock {
	if projection == nil || projection.Snapshot == nil {
		return nil
	}
	contentRange, directChildren, ok := nodeBodyContentRange(projection)
	if !ok || contentRange.End <= contentRange.Start {
		return nil
	}
	content := projection.Snapshot.Content

	bindings := childBindingsForProjection(projection, schema)

	// Inline field spans whose WholeRange sits inside the node's content range.
	// For a NOTE these include pre-heading `key:: value` lines; for an EMBEDDED
	// they include the inline-fields line right under the heading.
	type inlineEntry struct {
		Range     ByteRange
		FieldName string
		RawKey    string
	}
	inlineEntries := make([]inlineEntry, 0)
	for _, binding := range projection.Fields {
		for _, span := range binding.InlineSpans {
			if !rangeWithin(span.WholeRange, contentRange) {
				continue
			}
			inlineEntries = append(inlineEntries, inlineEntry{
				Range:     span.WholeRange,
				FieldName: binding.FieldName,
				RawKey:    span.Key,
			})
		}
	}
	if projection.Ref.Kind == NodeKindEmbedded {
		if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
			if blockRange, ok := sectionBlockIDRange(projection.Snapshot.Content, section); ok && rangeWithin(blockRange, contentRange) {
				inlineEntries = append(inlineEntries, inlineEntry{
					Range:     blockRange,
					FieldName: "locator",
					RawKey:    "locator",
				})
			}
		}
	}
	sort.SliceStable(inlineEntries, func(i, j int) bool {
		return inlineEntries[i].Range.Start < inlineEntries[j].Range.Start
	})

	// Direct children in byte order, filtered to those inside the content range.
	children := make([]*SectionNode, 0, len(directChildren))
	for _, child := range directChildren {
		if child == nil {
			continue
		}
		if child.StartByte >= contentRange.Start && child.EndByte <= contentRange.End {
			children = append(children, child)
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].StartByte < children[j].StartByte
	})

	blocks := make([]NodeBodyBlock, 0, len(children)+len(inlineEntries)+1)
	cursor := contentRange.Start
	inlineIdx := 0
	childIdx := 0

	emitNarrative := func(start, end int) {
		if end <= start {
			return
		}
		chunk := content[start:end]
		if strings.TrimSpace(chunk) == "" {
			return
		}
		blocks = append(blocks, NodeBodyBlock{
			Kind:     NodeBodyBlockKindNarrative,
			Range:    NodeRange{Start: start, End: end},
			Markdown: strings.TrimRight(chunk, " \t\n"),
		})
	}

	// Notes commonly use a single H1 heading as a transparent wrapper. Keep any
	// prose authored before that H1, but skip the heading line itself so the
	// note title doesn't duplicate in the structural body.
	if transparent := transparentNoteWrapper(projection); transparent != nil {
		emitNarrative(contentRange.Start, transparent.StartByte)
		cursor = advancePastHeadingLine(
			content,
			transparent.StartByte,
			transparent.EndByte,
		)
	}

	for cursor < contentRange.End {
		for inlineIdx < len(inlineEntries) && inlineEntries[inlineIdx].Range.End <= cursor {
			inlineIdx++
		}
		nextInlineStart := contentRange.End + 1
		nextChildStart := contentRange.End + 1
		if inlineIdx < len(inlineEntries) {
			nextInlineStart = inlineEntries[inlineIdx].Range.Start
		}
		if childIdx < len(children) {
			nextChildStart = children[childIdx].StartByte
		}
		nextStart := nextInlineStart
		if nextChildStart < nextStart {
			nextStart = nextChildStart
		}
		if nextStart > contentRange.End {
			emitNarrative(cursor, contentRange.End)
			break
		}
		if nextStart > cursor {
			emitNarrative(cursor, nextStart)
		}

		if nextInlineStart <= nextChildStart {
			span := inlineEntries[inlineIdx]
			blocks = append(blocks, NodeBodyBlock{
				Kind:      NodeBodyBlockKindInlineField,
				Range:     toNodeRange(span.Range),
				FieldName: span.FieldName,
				RawKey:    span.RawKey,
			})
			cursor = span.Range.End
			inlineIdx++
			continue
		}

		// Child section — possibly the start of a collection run.
		firstChild := children[childIdx]
		binding, hasBinding := bindings[firstChild.ID]
		if hasBinding && binding.fieldList {
			groupEnd := firstChild.EndByte
			refs := []NodeRef{sectionNodeRef(firstChild, binding)}
			j := childIdx + 1
			for j < len(children) {
				peer := children[j]
				peerBinding, ok := bindings[peer.ID]
				if !ok || !peerBinding.fieldList || peerBinding.fieldName != binding.fieldName {
					break
				}
				refs = append(refs, sectionNodeRef(peer, peerBinding))
				groupEnd = peer.EndByte
				j++
			}
			blocks = append(blocks, NodeBodyBlock{
				Kind:           NodeBodyBlockKindCollection,
				Range:          NodeRange{Start: firstChild.StartByte, End: groupEnd},
				FieldName:      binding.fieldName,
				ChildRefs:      refs,
				SectionDisplay: binding.sectionDisplay,
			})
			cursor = groupEnd
			childIdx = j
			continue
		}

		ref := sectionNodeRef(firstChild, binding)
		block := NodeBodyBlock{
			Kind:     NodeBodyBlockKindChildSection,
			Range:    NodeRange{Start: firstChild.StartByte, End: firstChild.EndByte},
			ChildRef: &ref,
		}
		if hasBinding {
			block.FieldName = binding.fieldName
			block.SectionDisplay = binding.sectionDisplay
		}
		blocks = append(blocks, block)
		cursor = firstChild.EndByte
		childIdx++
	}

	return blocks
}

type childBinding struct {
	fieldName      string
	fieldList      bool
	sectionDisplay SectionDisplay
	// childTypeRole is the schema Role of the declared target type (e.g.
	// EMBEDDED_NODE for UserStory). Resolved up-front so ref construction
	// doesn't re-walk the schema.
	childTypeRole TypeRole
}

// nodeBodyContentRange computes the byte range covered by a node's body in
// the document, plus the direct child sections that live inside that range.
// For NOTE refs the body is everything after the frontmatter. For SECTION /
// EMBEDDED refs it's the section range minus the heading line so the walker
// doesn't emit the heading itself as narrative.
func nodeBodyContentRange(projection *NodeProjection) (ByteRange, []*SectionNode, bool) {
	snapshot := projection.Snapshot
	ref := projection.Ref
	switch ref.Kind {
	case NodeKindNote:
		start := snapshot.FrontmatterRange.End
		children := snapshot.Sections
		// Treat a single H1 wrapper (the conventional `# Note Title`) as
		// transparent: advance the content range past its heading line so the
		// title isn't duplicated as narrative, and use its H2 children as the
		// note's direct body children so they can bind to the note's typed
		// fields (@contains level: H2).
		if wrapper := transparentNoteWrapper(projection); wrapper != nil {
			children = wrapper.Children
		}
		return ByteRange{Start: start, End: len(snapshot.Content)}, children, true
	case NodeKindSection, NodeKindEmbedded:
		section := snapshot.SectionsByID[ref.NodeID]
		if section == nil {
			return ByteRange{}, nil, false
		}
		headingEnd := advancePastHeadingLine(snapshot.Content, section.StartByte, section.EndByte)
		return ByteRange{Start: headingEnd, End: section.EndByte}, section.Children, true
	}
	return ByteRange{}, nil, false
}

func transparentNoteWrapper(projection *NodeProjection) *SectionNode {
	if projection == nil || projection.Snapshot == nil || projection.Ref.Kind != NodeKindNote {
		return nil
	}
	children := projection.Snapshot.Sections
	if len(children) != 1 || children[0] == nil {
		return nil
	}
	wrapper := children[0]
	if wrapper.Level != SectionLevelH1 || len(wrapper.Children) == 0 {
		return nil
	}
	return wrapper
}

// advancePastHeadingLine returns the byte offset just after the newline that
// terminates the heading line at `start`. If the heading is the last line of
// the section (no newline), returns `cap`. Used to exclude the heading text
// itself from a section's body projection — the heading is already surfaced
// as the node's title.
func advancePastHeadingLine(content string, start, cap int) int {
	i := start
	for i < cap && i < len(content) && content[i] != '\n' {
		i++
	}
	if i < cap && i < len(content) {
		i++
	}
	return i
}

func sectionBlockIDRange(content string, node *SectionNode) (ByteRange, bool) {
	if node == nil || strings.TrimSpace(node.BlockID) == "" {
		return ByteRange{}, false
	}
	blockID := "^" + strings.TrimPrefix(strings.TrimSpace(node.BlockID), "^")
	line := "\n" + blockID
	if idx := strings.LastIndex(node.Content, line); idx >= 0 {
		start := node.StartByte + idx + 1
		end := start + len(blockID)
		if end == len(content) || content[end] == '\n' || content[end] == '\r' {
			return ByteRange{Start: start, End: end}, true
		}
	}
	bodyStart := advancePastHeadingLine(content, node.StartByte, node.EndByte)
	if bodyStart+len(blockID) <= node.EndByte && strings.HasPrefix(content[bodyStart:], blockID) {
		end := bodyStart + len(blockID)
		if end != len(content) && content[end] != '\n' && content[end] != '\r' {
			return ByteRange{}, false
		}
		return ByteRange{Start: bodyStart, End: bodyStart + len(blockID)}, true
	}
	return ByteRange{}, false
}

// childBindingsForProjection maps a projection's direct child section IDs to
// the parent field that binds them. Used to decide which children participate
// in a collection block, which display mode the parent declared, and — when
// schema is present — the declared child type's Role so emitted child refs
// match the projection's canonical Kind (EMBEDDED vs SECTION).
func childBindingsForProjection(
	projection *NodeProjection,
	schema *Schema,
) map[string]childBinding {
	out := map[string]childBinding{}
	if projection == nil || projection.Type == nil {
		return out
	}
	for _, field := range projection.Type.Fields {
		if field == nil || field.Kind != FieldKindSection {
			continue
		}
		var childRole TypeRole
		if schema != nil {
			if childType := schema.Types[field.TypeName]; childType != nil {
				childRole = childType.Role
			}
		}
		binding, hasBinding := projection.Fields[field.Name]
		if hasBinding {
			for _, ref := range binding.SectionNodes {
				out[ref.NodeID] = childBinding{
					fieldName:      field.Name,
					fieldList:      field.List,
					sectionDisplay: field.SectionDisplay,
					childTypeRole:  childRole,
				}
			}
		}
		if field.List {
			if coll, ok := projection.Collections[field.Name]; ok {
				for _, item := range coll.Items {
					out[item.Ref.NodeID] = childBinding{
						fieldName:      field.Name,
						fieldList:      true,
						sectionDisplay: field.SectionDisplay,
						childTypeRole:  childRole,
					}
				}
			}
		}
	}
	return out
}

// sectionNodeRef lifts a snapshot SectionNode into the ref shape the
// workspace graph uses. Kind comes from schema role only:
//   - Binding declares the child type as `@node(locator: EMBEDDED)` → EMBEDDED
//   - Otherwise → SECTION
//
// WHY: presence of a `^block-id` no longer reclassifies a section. Per
// SPEC-0023's usage-driven block-id lifecycle, block IDs are author-facing
// addresses driven by external citation; promoting a structural section to
// EMBEDDED just because it carries an anchor created a self-reinforcing loop
// (anchor → embedded → must keep anchor). Stray anchors on non-embedded
// sections now surface through a separate diagnostic
// (stray_block_id_on_non_embedded_section) rather than silently changing the
// node's identity. Coderefs:
// [[linkable-embedded-node-identifiers#^spec-0023-us5]]
func sectionNodeRef(node *SectionNode, binding childBinding) NodeRef {
	fragment := sectionFragmentFromID(node.ID)
	kind := NodeKindSection
	if binding.childTypeRole == TypeRoleEmbeddedNode {
		kind = NodeKindEmbedded
	}
	return NodeRef{
		NotePath: node.NotePath,
		NodeID:   node.ID,
		Fragment: fragment,
		Kind:     kind,
	}
}

func sectionFragmentFromID(id string) string {
	idx := strings.Index(id, "#")
	if idx < 0 {
		return ""
	}
	return id[idx+1:]
}

func rangeWithin(inner, outer ByteRange) bool {
	return inner.Start >= outer.Start && inner.End <= outer.End
}

func toNodeRange(r ByteRange) NodeRange {
	return NodeRange{Start: r.Start, End: r.End}
}
