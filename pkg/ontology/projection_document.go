package ontology

import (
	"fmt"
	"sort"
	"strings"
)

// ProjectDocumentNodesFromSnapshot returns the complete source-node inventory
// for one parsed document. The note root is first; section, embedded, list, and
// checkbox nodes then follow authored byte order. Identity dedupe uses the full
// NodeRef rather than its author-facing String representation.
func ProjectDocumentNodesFromSnapshot(snapshot *DocumentSnapshot, schema *Schema) ([]*NodeProjection, error) {
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
	return projectDocumentNodesWithResolver(snapshot, schema, resolver)
}

// ProjectDocumentNodesFromSnapshotAsSelectorType projects a document whose
// schema selectors identify exactly the requested type even though its current
// preferred identifier fails that type's grammar. This narrow recovery path is
// intended for whole-pool identifier strategy migration; ordinary resolution
// remains gated by the declared identifier strategy.
func ProjectDocumentNodesFromSnapshotAsSelectorType(snapshot *DocumentSnapshot, schema *Schema, typeName string) ([]*NodeProjection, error) {
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
	typeName = strings.TrimSpace(typeName)
	if resolver.assessment == nil || resolver.assessment.ResolvedType != "" || len(resolver.assessment.SelectorCandidateTypes) != 1 || resolver.assessment.SelectorCandidateTypes[0] != typeName {
		return nil, fmt.Errorf("document %s does not have exactly one unresolved selector candidate %q", snapshot.NotePath, typeName)
	}
	noteType := schema.Types[typeName]
	if noteType == nil || noteType.Role != TypeRoleNote {
		return nil, fmt.Errorf("selector candidate %q is not a note type", typeName)
	}
	resolver.noteType = noteType
	resolver.noteDoc.TypeName = typeName
	resolver.indexSectionTypesForNote(noteType)
	return projectDocumentNodesWithResolver(snapshot, schema, resolver)
}

// ProjectNodeFromSnapshotWithSelectorRecovery projects one requested note or
// embedded ref after recovering the document's sole unresolved selector type.
// It preserves ordinary identifier-gated resolution and is only appropriate
// for a reviewed repair that is changing the identifier grammar itself.
func ProjectNodeFromSnapshotWithSelectorRecovery(snapshot *DocumentSnapshot, schema *Schema, ref NodeRef) (*NodeProjection, error) {
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
	if resolver.assessment == nil || resolver.assessment.ResolvedType != "" || len(resolver.assessment.SelectorCandidateTypes) != 1 {
		return nil, fmt.Errorf("document %s does not have exactly one unresolved selector candidate", snapshot.NotePath)
	}
	typeName := resolver.assessment.SelectorCandidateTypes[0]
	noteType := schema.Types[typeName]
	if noteType == nil || noteType.Role != TypeRoleNote {
		return nil, fmt.Errorf("selector candidate %q is not a note type", typeName)
	}
	resolver.noteType = noteType
	resolver.noteDoc.TypeName = typeName
	resolver.indexSectionTypesForNote(noteType)
	projection, err := resolver.project(ref)
	if err != nil {
		return nil, err
	}
	if ref.TypeName != "" && projection.Type != nil && projection.Type.Name != ref.TypeName {
		return nil, fmt.Errorf("selector recovery resolved %s as %s, not %s", ref.String(), projection.Type.Name, ref.TypeName)
	}
	return projection, nil
}

func projectDocumentNodesWithResolver(snapshot *DocumentSnapshot, schema *Schema, resolver *projectionResolver) ([]*NodeProjection, error) {

	root := resolver.projectNote().Ref
	candidates := make([]NodeRef, 0, 1+len(snapshot.Sections)+len(snapshot.SourceSpans))
	seen := map[NodeRef]struct{}{root: {}}
	appendRef := func(ref NodeRef) {
		if _, exists := seen[ref]; exists {
			return
		}
		seen[ref] = struct{}{}
		candidates = append(candidates, ref)
	}
	var appendSections func([]*SectionNode)
	appendSections = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			appendRef(resolver.sectionNodeRef(node))
			appendSections(node.Children)
		}
	}
	appendSections(snapshot.Sections)

	globalRefs, err := GlobalSourceNodeRefsFromSnapshot(snapshot, schema)
	if err != nil {
		return nil, err
	}
	for _, ref := range globalRefs {
		appendRef(ref)
	}
	for _, span := range snapshot.SourceSpans {
		if span == nil || span.Shape == EmbeddedSourceShapeSection {
			continue
		}
		appendRef(resolver.sourceSpanNodeRef(span))
	}

	sort.Slice(candidates, func(i, j int) bool { return nodeRefSourceLess(candidates[i], candidates[j]) })
	refs := make([]NodeRef, 0, 1+len(candidates))
	refs = append(refs, root)
	refs = append(refs, candidates...)
	out := make([]*NodeProjection, 0, len(refs))
	for _, ref := range refs {
		projection, err := resolver.projectCurrent(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, projection)
	}
	return out, nil
}

func nodeRefSourceLess(left, right NodeRef) bool {
	if left.StartByte != right.StartByte {
		return left.StartByte < right.StartByte
	}
	if left.EndByte != right.EndByte {
		return left.EndByte < right.EndByte
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.NodeID != right.NodeID {
		return left.NodeID < right.NodeID
	}
	if left.TypeName != right.TypeName {
		return left.TypeName < right.TypeName
	}
	if left.ParentID != right.ParentID {
		return left.ParentID < right.ParentID
	}
	if left.Structural != right.Structural {
		return left.Structural < right.Structural
	}
	if left.Fragment != right.Fragment {
		return left.Fragment < right.Fragment
	}
	return left.NotePath < right.NotePath
}
