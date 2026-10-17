package ontology

import (
	"path/filepath"

	"strings"
)

func noteLinkTarget(ref NodeRef, renderer wikilinkRenderer) NodeLinkTarget {
	base := renderer.target(ref.NotePath)
	label := strings.TrimSuffix(filepath.Base(ref.NotePath), filepath.Ext(ref.NotePath))
	return NodeLinkTarget{
		Ref:          ref,
		Markdown:     ref.NotePath,
		Wikilink:     "[[" + base + "]]",
		DisplayLabel: label,
		Exists:       true,
	}
}

func embeddedLinkTarget(snapshot *DocumentSnapshot, projection *NodeProjection, renderer wikilinkRenderer) (NodeLinkTarget, *NodeLinkFixAction, []NodeLinkDiagnostic) {
	ref := projection.Ref
	node := snapshot.SectionsByID[ref.NodeID]
	var label string
	var blockID string
	if node != nil {
		label = node.Title
		blockID = node.BlockID
	} else if span := snapshot.SourceSpansByID[ref.NodeID]; span != nil {
		label = span.Title
		blockID = span.BlockID
	}
	if label == "" {
		label = ref.TypeName
	}
	if node == nil && snapshot.SourceSpansByID[ref.NodeID] == nil {
		return NodeLinkTarget{}, nil, []NodeLinkDiagnostic{{
			Code:     "node_missing_source",
			NotePath: ref.NotePath,
			Ref:      ref,
			Message:  "embedded node source range is unavailable",
		}}
	}
	if strings.TrimSpace(blockID) != "" {
		target := nodeLinkTargetWithBlock(ref, renderer, label, blockID, true)
		return target, nil, nil
	}
	blockID = GenerateBlockIDForProjection(snapshot, projection)
	target := nodeLinkTargetWithBlock(ref, renderer, label, blockID, false)
	target.RequiresFix = true
	return target, &NodeLinkFixAction{Ref: ref, BlockID: blockID}, []NodeLinkDiagnostic{{
		Code:     "missing_embedded_block_id",
		NotePath: ref.NotePath,
		Ref:      ref,
		BlockID:  blockID,
		Message:  "embedded ontology node needs a block ID before its link target is durable",
	}}
}

func nodeLinkTargetWithBlock(ref NodeRef, renderer wikilinkRenderer, label, blockID string, exists bool) NodeLinkTarget {
	blockID = strings.TrimPrefix(strings.TrimSpace(blockID), "^")
	markdown := ref.NotePath + "#^" + blockID
	wikiTarget := renderer.target(ref.NotePath) + "#^" + blockID
	return NodeLinkTarget{
		Ref:          ref,
		Markdown:     markdown,
		Wikilink:     "[[" + wikiTarget + "]]",
		DisplayLabel: label,
		Exists:       exists,
		BlockID:      blockID,
	}
}

type wikilinkRenderer struct {
	uniqueStem map[string]bool
}

func newWikilinkRenderer(allNotes []string) wikilinkRenderer {
	counts := map[string]int{}
	for _, note := range allNotes {
		stem := strings.TrimSuffix(filepath.Base(note), filepath.Ext(note))
		if stem != "" {
			counts[stem]++
		}
	}
	unique := map[string]bool{}
	for stem, count := range counts {
		unique[stem] = count == 1
	}
	return wikilinkRenderer{uniqueStem: unique}
}

func (r wikilinkRenderer) target(notePath string) string {
	stem := strings.TrimSuffix(filepath.Base(notePath), filepath.Ext(notePath))
	if stem != "" && r.uniqueStem[stem] {
		return stem
	}
	return strings.TrimSuffix(notePath, filepath.Ext(notePath))
}

func normalizeLinkNodeRef(ref NodeRef) NodeRef {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.TypeName = strings.TrimSpace(ref.TypeName)
	ref.ParentID = strings.TrimSpace(ref.ParentID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	return ref
}

func nodeLocatorKind(ref NodeRef) NodeKind {
	if ref.Kind == "" {
		return NodeKindNote
	}
	return ref.Kind
}

func nodeLocatorMapKey(ref NodeRef) string {
	if locator := nodeSourceLocator(ref); locator != "" {
		return locator
	}
	return ref.String()
}

func nodeSourceLocator(ref NodeRef) string {
	ref = normalizeLinkNodeRef(ref)
	if ref.Kind == NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if fragment := normalizeSectionFragment(ref.NodeID, ref.NotePath); fragment != "" {
			return ref.NotePath + "#" + fragment
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func locatorStatusFromDiagnostics(diagnostics []NodeLinkDiagnostic) NodeLocatorStatus {
	for _, diagnostic := range diagnostics {
		switch diagnostic.Code {
		case "unsupported_node_kind":
			return NodeLocatorUnsupported
		case "node_unresolved", "node_missing_source", "snapshot_load_failed":
			return NodeLocatorUnresolved
		}
	}
	return NodeLocatorUnresolved
}

func nodeLocatorFixActions(ref NodeRef, keyed []NodeLinkFixAction, plan *NodeLinkFixPlan) []NodeLinkFixAction {
	if len(keyed) > 0 || plan == nil {
		return keyed
	}
	for _, action := range plan.Actions {
		if nodeLocatorRefsMatch(ref, action.Ref) {
			return []NodeLinkFixAction{action}
		}
	}
	return nil
}

func nodeLocatorRefsMatch(a, b NodeRef) bool {
	a = normalizeLinkNodeRef(a)
	b = normalizeLinkNodeRef(b)
	if a.NotePath == "" || a.NotePath != b.NotePath {
		return false
	}
	if a.NodeID != "" && b.NodeID != "" {
		return a.NodeID == b.NodeID
	}
	if a.Structural != "" && b.Structural != "" {
		return a.Structural == b.Structural
	}
	if a.Fragment != "" && b.Fragment != "" {
		return a.Fragment == b.Fragment
	}
	return false
}
