package query

import (
	"cmp"
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveSectionNeighborField(ctx context.Context, node *sectionRecord, field *ontoschema.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	if node == nil || node.Note == nil || node.Node == nil {
		return []any{}, true
	}
	if field.Scope != ontoschema.NeighborScopeSubtree {
		rows, ok := e.resolveAmbientRows(ctx, node.Note.Path, "", directionProvenance(field.Direction), field.TypeName, 0, path)
		if !ok {
			return nil, false
		}
		records, err := e.edgeRecords(ctx, e.firstEdgeRows(rows, field.TypeName, first))
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		return e.resolveNodeList(ctx, firstRecords(records, first), set, path), true
	}
	records, err := e.subtreeNeighborRecords(ctx, node, field, first)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	return e.resolveNodeList(ctx, records, set, path), true
}

func (e *executor) subtreeNeighborRecords(ctx context.Context, node *sectionRecord, field *ontoschema.Field, first int) ([]*noteRecord, error) {
	cache, err := e.notePathCacheForSections(ctx)
	if err != nil {
		return nil, err
	}
	pathsSeen := make(map[string]struct{})
	pathsList := make([]string, 0)
	if field.Direction != ontoschema.NeighborDirectionInbound {
		for _, scanned := range ontology.ScanMarkdownBodyLinks(ontology.SectionBody(node.Node), markdownsyntax.DefaultWikilinkOptions, markdownsyntax.DefaultMdLinkOptions) {
			target := scanned.Detail
			var (
				resolved string
				ok       bool
			)
			switch target.LinkType {
			case "mdlink":
				resolved, ok = cache.ResolveMdLink(target.Target, node.Note.Path)
			default:
				resolved, ok = cache.ResolveNote(target.Target)
			}
			if !ok {
				continue
			}
			if _, seen := pathsSeen[resolved]; seen {
				continue
			}
			row, ok, err := e.resolvedType(ctx, resolved)
			if err != nil || !ok {
				continue
			}
			if !e.typeMatchesOrImplements(row.TypeName, field.TypeName) {
				continue
			}
			pathsSeen[resolved] = struct{}{}
			pathsList = append(pathsList, resolved)
		}
	}
	if field.Direction == ontoschema.NeighborDirectionInbound || field.Direction == ontoschema.NeighborDirectionBoth {
		inbound, err := e.inboundSectionNeighborIndex(ctx, node.Note.Path)
		if err != nil {
			return nil, err
		}
		targetID := inboundSectionNeighborKey(node.Note.Path, sectionAnchorMatchKey(node.Node))
		targetIDs := []string{targetID}
		nodeIDTarget := inboundSectionNeighborKey(node.Note.Path, node.Node.ID)
		if nodeIDTarget != targetID {
			targetIDs = append(targetIDs, nodeIDTarget)
		}
		for _, targetID := range targetIDs {
			for _, referrerPath := range inbound[targetID] {
				if _, seen := pathsSeen[referrerPath]; seen {
					continue
				}
				row, ok, err := e.resolvedType(ctx, referrerPath)
				if err != nil || !ok {
					continue
				}
				if !e.typeMatchesOrImplements(row.TypeName, field.TypeName) {
					continue
				}
				pathsSeen[referrerPath] = struct{}{}
				pathsList = append(pathsList, referrerPath)
			}
		}
	}
	return e.filterTypedRecords(ctx, pathsList, field.TypeName, first)
}

// shortcut: incoming sources still need provider projection; persist resolved fragment endpoints to remove these reads.
func (e *executor) inboundSectionNeighborIndex(ctx context.Context, targetPath string) (map[string][]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cached, ok := e.inboundSectionNeighbors[targetPath]; ok {
		return cached, nil
	}
	cache, err := e.notePathCacheForSections(ctx)
	if err != nil {
		return nil, err
	}
	ambient, err := e.deps.Store.OntologyAmbientEdgesByTargets(ctx, []string{targetPath}, "", 0)
	if err != nil {
		return nil, err
	}
	structural, err := e.deps.Store.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{Paths: []string{targetPath}})
	if err != nil {
		return nil, err
	}
	candidates := make(map[string]struct{})
	linkedSources, err := e.deps.Store.GraphDocNoteLinkSourcesByTargets(ctx, []string{targetPath})
	if err != nil {
		return nil, err
	}
	for _, source := range linkedSources {
		candidates[source] = struct{}{}
	}
	for _, edge := range ambient {
		candidates[edge.SrcPath] = struct{}{}
	}
	for _, edge := range structural {
		if edge.DstPath == targetPath {
			candidates[edge.SrcPath] = struct{}{}
		}
	}
	if overlay := e.deps.ReadOverlay; overlay != nil {
		for path := range overlay.Notes {
			candidates[path] = struct{}{}
		}
		for path := range overlay.UpdatedContentByPath {
			candidates[path] = struct{}{}
		}
		for _, path := range overlay.TouchedPaths {
			candidates[path] = struct{}{}
		}
		if overlay.Index != nil {
			for path := range overlay.Index.TouchedPaths {
				candidates[path] = struct{}{}
			}
		}
	}
	index := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	add := func(key, source string) {
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		if !seen[key][source] {
			seen[key][source] = true
			index[key] = append(index[key], source)
		}
	}
	if err := e.addProviderInboundSectionNeighbors(ctx, cache, nil, candidates, add); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(structural))
	for _, edge := range structural {
		if edge.DstPath == targetPath && edge.DstNodeID != "" {
			ids = append(ids, edge.DstNodeID)
		}
	}
	nodes, err := e.deps.Store.OntologyNodesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, edge := range structural {
		if edge.DstPath != targetPath || edge.DstNodeID == "" {
			continue
		}
		add(inboundSectionNeighborKey(targetPath, edge.DstNodeID), edge.SrcPath)
		if node, ok := nodes[edge.DstNodeID]; ok {
			if block := strings.TrimSpace(strings.TrimPrefix(node.BlockID, "^")); block != "" {
				add(inboundSectionNeighborKey(targetPath, "block:"+block), edge.SrcPath)
			}
			if heading := strings.TrimSpace(strings.TrimPrefix(node.Fragment, "#")); heading != "" {
				add(inboundSectionNeighborKey(targetPath, "heading:"+cmp.Or(markdownsyntax.HeadingSlug(heading), "section")), edge.SrcPath)
			}
		}
	}
	for key := range index {
		sort.Strings(index[key])
	}
	if e.inboundSectionNeighbors == nil {
		e.inboundSectionNeighbors = make(map[string]map[string][]string)
	}
	e.inboundSectionNeighbors[targetPath] = index
	return index, nil
}

func inboundSectionNeighborKey(notePath, targetKey string) string {
	return notePath + "\x00" + targetKey
}

func sectionAnchorMatchKey(node *ontology.SectionNode) string {
	if node == nil {
		return ""
	}
	fragment := strings.TrimSpace(node.BlockID)
	if fragment != "" {
		return "block:" + strings.TrimPrefix(fragment, "^")
	}
	if idx := strings.Index(node.ID, "#"); idx >= 0 && idx < len(node.ID)-1 {
		fragment = strings.TrimSpace(node.ID[idx+1:])
		if strings.HasPrefix(fragment, "^") {
			return "block:" + strings.TrimPrefix(fragment, "^")
		}
	}
	if title := strings.TrimSpace(node.Title); title != "" {
		return "heading:" + cmp.Or(markdownsyntax.HeadingSlug(title), "section")
	}
	return ""
}

func resolvedTargetSectionKey(target obsidian.ResolvedNoteTarget) string {
	fragment := strings.TrimSpace(target.Fragment)
	if fragment == "" {
		return ""
	}
	if strings.HasPrefix(fragment, "^") {
		return "block:" + strings.TrimPrefix(fragment, "^")
	}
	return "heading:" + cmp.Or(markdownsyntax.HeadingSlug(fragment), "section")
}
