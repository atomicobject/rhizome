package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func buildNodeScopedEdges(resolver *projectionResolver, root *NodeProjection, schema *ontoschema.Schema, cache *obsidian.NotePathCache, resolveType func(string) string, updatedAt int64) ([]semdb.OntologyEdgeRow, error) {
	if resolver == nil || root == nil || cache == nil {
		return nil, nil
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

func nodeScopedNeighborEdges(schema *ontoschema.Schema, cache *obsidian.NotePathCache, projection *NodeProjection, resolveType func(string) string, updatedAt int64) []semdb.OntologyEdgeRow {
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
		if field == nil || field.Kind != ontoschema.FieldKindNeighbor || field.Scope != ontoschema.NeighborScopeSubtree {
			continue
		}
		if field.Direction == ontoschema.NeighborDirectionInbound {
			continue
		}
		for _, scanned := range ScanMarkdownBodyLinks(SectionBody(section), markdownsyntax.DefaultWikilinkOptions, markdownsyntax.DefaultMdLinkOptions) {
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
			if !ontoschema.TypeMatchesOrImplements(schema, dstType, field.TypeName) {
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

func nodeScopedLinkEdges(schema *ontoschema.Schema, cache *obsidian.NotePathCache, projection *NodeProjection, resolveType func(string) string, updatedAt int64) []semdb.OntologyEdgeRow {
	if projection == nil || projection.Type == nil || strings.TrimSpace(projection.Ref.NodeID) == "" {
		return nil
	}
	out := make([]semdb.OntologyEdgeRow, 0)
	for _, field := range projection.Type.Fields {
		if field == nil || field.Kind != ontoschema.FieldKindLink {
			continue
		}
		binding := projection.Fields[field.Name]
		for _, raw := range binding.Values {
			targetPath, ok := cache.ResolveNote(UnwrapLinkValue(raw))
			if !ok {
				continue
			}
			dstType := ""
			if resolveType != nil {
				dstType = resolveType(targetPath)
			}
			if !ontoschema.TypeMatchesOrImplements(schema, dstType, field.TypeName) {
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
func dedupeEdges(edges []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	seen := make(map[string]semdb.OntologyEdgeRow, len(edges))
	for _, edge := range edges {
		key := edge.SrcPath + "\x00" + edge.SrcNodeID + "\x00" + edge.RelationName + "\x00" + edge.DstPath + "\x00" + edge.DstNodeID + "\x00" + edge.Provenance
		if existing, ok := seen[key]; ok {
			if existing.Structural {
				continue
			}
		}
		seen[key] = edge
	}
	out := make([]semdb.OntologyEdgeRow, 0, len(seen))
	for _, edge := range seen {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		switch {
		case a.SrcPath != b.SrcPath:
			return a.SrcPath < b.SrcPath
		case a.SrcNodeID != b.SrcNodeID:
			return a.SrcNodeID < b.SrcNodeID
		case a.RelationName != b.RelationName:
			return a.RelationName < b.RelationName
		case a.DstPath != b.DstPath:
			return a.DstPath < b.DstPath
		case a.DstNodeID != b.DstNodeID:
			return a.DstNodeID < b.DstNodeID
		default:
			return a.Provenance < b.Provenance
		}
	})
	return out
}
