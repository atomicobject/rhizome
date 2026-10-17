package web

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"path/filepath"
	"strings"
)

func (s *Server) applyFastWorkspaceNodeLocators(ctx context.Context, resp *NodeWorkspaceResponse) {
	if resp == nil {
		return
	}
	for i := range resp.Nodes {
		isFocused := resp.FocusedNodeID != "" && resp.Nodes[i].ID == resp.FocusedNodeID
		if isFocused && resp.NodeLocator != nil {
			resp.Nodes[i].NodeLocator = resp.NodeLocator
			continue
		}
		label := ""
		if resp.Nodes[i].Data != nil {
			label = resp.Nodes[i].Data.Title
		}
		locator, ok := s.fastNodeWorkspaceLocator(ctx, resp.Nodes[i].Ref, label)
		if !ok {
			continue
		}
		resp.Nodes[i].NodeLocator = locator
		if isFocused {
			resp.Node.NodeLocator = locator
			resp.NodeLocator = locator
			resp.LinkTarget = locator.LinkTarget
		}
	}
}

func (s *Server) fastNodeWorkspaceLocator(ctx context.Context, ref ontology.NodeRef, label string) (*ontology.NodeLocator, bool) {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	if ref.NotePath == "" {
		return nil, false
	}
	switch ref.Kind {
	case "", ontology.NodeKindNote:
		target := ontology.NodeLinkTarget{
			Ref:          ref,
			Markdown:     ref.NotePath,
			Wikilink:     "[[" + s.fastWikilinkTarget(ctx, ref.NotePath) + "]]",
			DisplayLabel: firstNonEmptyString(label, paths.NoteTitle(ref.NotePath)),
			Exists:       true,
		}
		return &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ontology.NodeKindNote,
			SourceLocator: ref.NotePath,
			LinkTarget:    &target,
			Status:        ontology.NodeLocatorLinkable,
		}, true
	case ontology.NodeKindEmbedded:
		if !strings.HasPrefix(ref.Fragment, "^") {
			return nil, false
		}
		blockID := strings.TrimPrefix(ref.Fragment, "^")
		target := ontology.NodeLinkTarget{
			Ref:          ref,
			Markdown:     ref.NotePath + "#^" + blockID,
			Wikilink:     "[[" + s.fastWikilinkTarget(ctx, ref.NotePath) + "#^" + blockID + "]]",
			DisplayLabel: firstNonEmptyString(label, ref.TypeName, blockID),
			Exists:       true,
			BlockID:      blockID,
		}
		return &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ontology.NodeKindEmbedded,
			SourceLocator: ref.NotePath + "#^" + blockID,
			LinkTarget:    &target,
			Status:        ontology.NodeLocatorLinkable,
		}, true
	case ontology.NodeKindSection:
		sourceLocator := ref.NotePath
		if ref.Fragment != "" {
			sourceLocator += "#" + ref.Fragment
		} else if ref.NodeID != "" {
			sourceLocator = ref.NodeID
		} else if ref.Structural != "" {
			sourceLocator += "#struct:" + ref.Structural
		}
		return &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ontology.NodeKindSection,
			SourceLocator: sourceLocator,
			Status:        ontology.NodeLocatorUnsupported,
			Diagnostics: []ontology.NodeLinkDiagnostic{{
				Code:     "unsupported_node_kind",
				NotePath: ref.NotePath,
				Ref:      ref,
				Message:  "only embedded ontology nodes are linkable with block identifiers",
			}},
		}, true
	default:
		return nil, false
	}
}

func (s *Server) fastWikilinkTarget(ctx context.Context, notePath string) string {
	stem := strings.TrimSuffix(filepath.Base(notePath), filepath.Ext(notePath))
	if stem != "" {
		if resolved, ok := s.notePathCache(ctx).ResolveNote(stem); ok && resolved == notePath {
			return stem
		}
	}
	return strings.TrimSuffix(notePath, filepath.Ext(notePath))
}

func (s *Server) nodeWorkspaceLocator(ctx context.Context, defs *ontologyDefinitions, scope *noderead.Scope, ref ontology.NodeRef, ensure ontology.EnsureLinkTargetMode) (*ontology.NodeLocator, []OntologyEditOp, error) {
	if defs == nil || defs.schema == nil {
		return nil, nil, nil
	}
	if scope == nil {
		scope = s.nodeReadScope(ctx, defs)
	}
	locators, err := scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ensure)
	if err != nil {
		return nil, nil, err
	}
	locator, ok := locators[nodeWorkspaceLocatorKey(ref)]
	if !ok {
		return nil, nil, nil
	}
	return &locator, nodeLinkFixActionsToEditOps(locator.FixActions), nil
}

func nodeWorkspaceLocatorKey(ref ontology.NodeRef) string {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	if ref.Kind == ontology.NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if strings.HasPrefix(ref.NodeID, ref.NotePath+"#") {
			return ref.NodeID
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func nodeLinkFixActionsToEditOps(actions []ontology.NodeLinkFixAction) []OntologyEditOp {
	if len(actions) == 0 {
		return nil
	}
	fixOps := make([]OntologyEditOp, 0, len(actions))
	for _, action := range actions {
		fixOps = append(fixOps, OntologyEditOp{
			Kind:       "ensureBlockID",
			Path:       action.Ref.NotePath,
			NodeID:     action.Ref.NodeID,
			Structural: action.Ref.Structural,
			BlockID:    action.BlockID,
		})
	}
	return fixOps
}
