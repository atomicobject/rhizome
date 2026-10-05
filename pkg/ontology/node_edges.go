package ontology

import (
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func buildNodeScopedStructuralEdges(doc *noteDoc, schema *Schema, cache *obsidian.NotePathCache, resolveType func(string) string, updatedAt int64) ([]semdb.OntologyEdgeRow, error) {
	if doc == nil || doc.Snapshot == nil || schema == nil || cache == nil || strings.TrimSpace(doc.Path) == "" || strings.TrimSpace(doc.Content) == "" {
		return nil, nil
	}
	snapshot, err := documentSnapshotForDoc(doc)
	if err != nil {
		return nil, err
	}
	return buildNodeScopedStructuralEdgesFromSnapshot(snapshot, schema, cache, resolveType, updatedAt)
}

func buildNodeScopedStructuralEdgesFromSnapshot(snapshot *DocumentSnapshot, schema *Schema, cache *obsidian.NotePathCache, resolveType func(string) string, updatedAt int64) ([]semdb.OntologyEdgeRow, error) {
	if snapshot == nil || schema == nil || cache == nil {
		return nil, nil
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil, err
	}
	root, err := resolver.project(NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindNote})
	if err != nil || root == nil {
		return nil, err
	}
	out := make([]semdb.OntologyEdgeRow, 0)
	seen := make(map[string]struct{})
	var walk func(*NodeProjection) error
	walk = func(projection *NodeProjection) error {
		if projection == nil {
			return nil
		}
		if projection.Ref.Kind != "" && projection.Ref.Kind != NodeKindNote {
			out = append(out, nodeScopedLinkEdges(schema, cache, projection, resolveType, updatedAt)...)
			out = append(out, nodeScopedNeighborEdges(schema, cache, projection, resolveType, updatedAt)...)
		}
		for _, binding := range projection.Fields {
			for _, childRef := range binding.SectionNodes {
				key := childRef.String()
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				child, err := resolver.projectCurrent(childRef)
				if err != nil {
					return err
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	globalRefs := resolver.globalSourceNodeRefs()
	for _, ref := range globalRefs {
		key := ref.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		child, err := resolver.projectCurrent(ref)
		if err != nil {
			return nil, err
		}
		if err := walk(child); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func nodeScopedNeighborEdges(schema *Schema, cache *obsidian.NotePathCache, projection *NodeProjection, resolveType func(string) string, updatedAt int64) []semdb.OntologyEdgeRow {
	if projection == nil || projection.Type == nil || projection.Snapshot == nil || strings.TrimSpace(projection.Ref.NodeID) == "" {
		return nil
	}
	section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]
	if section == nil {
		return nil
	}
	out := make([]semdb.OntologyEdgeRow, 0)
	seen := make(map[string]struct{})
	for _, field := range projection.Type.Fields {
		if field == nil || field.Kind != FieldKindNeighbor || field.Scope != NeighborScopeSubtree {
			continue
		}
		if field.Direction == NeighborDirectionInbound {
			continue
		}
		for _, scanned := range ScanMarkdownBodyLinks(SectionBody(section), obsidian.DefaultWikilinkOptions, obsidian.DefaultMdLinkOptions) {
			target := scanned.Detail
			var (
				targetPath string
				ok         bool
			)
			switch target.LinkType {
			case "mdlink":
				targetPath, ok = cache.ResolveMdLink(target.Target, projection.Ref.NotePath)
			default:
				targetPath, ok = cache.ResolveNote(target.Target)
			}
			if !ok || targetPath == "" {
				continue
			}
			dstType := ""
			if resolveType != nil {
				dstType = resolveType(targetPath)
			}
			if !typeMatchesOrImplements(schema, dstType, field.TypeName) {
				continue
			}
			key := projection.Ref.NodeID + "\x00" + field.Name + "\x00" + targetPath
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, semdb.OntologyEdgeRow{
				SrcPath:      projection.Ref.NotePath,
				SrcNodeID:    projection.Ref.NodeID,
				RelationName: field.Name,
				DstPath:      targetPath,
				DstType:      firstNonEmpty(dstType, field.TypeName),
				Provenance:   "section_neighbor",
				Structural:   true,
				SchemaHash:   schema.Hash,
				UpdatedAt:    updatedAt,
			})
		}
	}
	return out
}

func nodeScopedLinkEdges(schema *Schema, cache *obsidian.NotePathCache, projection *NodeProjection, resolveType func(string) string, updatedAt int64) []semdb.OntologyEdgeRow {
	if projection == nil || projection.Type == nil || strings.TrimSpace(projection.Ref.NodeID) == "" {
		return nil
	}
	out := make([]semdb.OntologyEdgeRow, 0)
	for _, field := range projection.Type.Fields {
		if field == nil || field.Kind != FieldKindLink {
			continue
		}
		binding := projection.Fields[field.Name]
		for _, raw := range binding.Values {
			targetPath, ok := cache.ResolveNote(unwrapLinkValue(raw))
			if !ok {
				continue
			}
			dstType := ""
			if resolveType != nil {
				dstType = resolveType(targetPath)
			}
			if !typeMatchesOrImplements(schema, dstType, field.TypeName) {
				continue
			}
			out = append(out, semdb.OntologyEdgeRow{
				SrcPath:      projection.Ref.NotePath,
				SrcNodeID:    projection.Ref.NodeID,
				RelationName: field.Name,
				DstPath:      targetPath,
				DstType:      firstNonEmpty(dstType, field.TypeName),
				Provenance:   "field",
				Structural:   true,
				SchemaHash:   schema.Hash,
				UpdatedAt:    updatedAt,
			})
		}
	}
	return out
}
