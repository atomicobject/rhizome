package noderead

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Walk builds the stable path-oriented ontology walk view on top of the
// batched NodeRead expansion primitive. It is the sole implementation used by
// CLI graph traversal output.
func (s *Scope) Walk(ctx context.Context, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, seedPath string, opts ontology.WalkOptions) (*ontology.WalkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return nil, nil
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 2
	}
	typeRows, err := s.TypesByPaths(ctx, []string{seedPath})
	if err != nil {
		return nil, err
	}
	seedType, ok := typeRows[seedPath]
	if !ok {
		return &ontology.WalkResult{}, nil
	}
	relationNames := []string(nil)
	if opts.RelationFilter != "" {
		relationNames = []string{opts.RelationFilter}
	}
	expanded, err := s.Expand(ctx, ExpansionPlan{
		Sources: []ontology.NodeRef{{NotePath: seedPath, Kind: ontology.NodeKindNote}},
		Steps: []ExpansionStep{{
			Direction:         TraversalDirectionBoth,
			RelationNames:     relationNames,
			IncludeStructural: true,
			IncludeAmbient:    opts.IncludeAmbient,
		}},
		Limits: ontologyWalkLimits(opts.MaxDepth),
	})
	if err != nil {
		return nil, err
	}
	seenPaths := map[string]struct{}{seedPath: {}}
	for _, edge := range expanded.Edges {
		seenPaths[edge.Source.NotePath] = struct{}{}
		seenPaths[edge.Target.NotePath] = struct{}{}
	}
	edgeRows := ontologyWalkEdges(expanded.Edges)
	pathsList := make([]string, 0, len(seenPaths))
	for path := range seenPaths {
		pathsList = append(pathsList, path)
	}
	sort.Strings(pathsList)
	typeRows, err = s.TypesByPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	nodes := make([]ontology.WalkNode, 0, len(pathsList))
	for _, path := range pathsList {
		row, exists := typeRows[path]
		if !exists {
			continue
		}
		nodes = append(nodes, ontology.WalkNode{
			Path:        path,
			TypeName:    row.TypeName,
			Title:       walkTitleFromPath(path),
			Frontmatter: s.walkBlessedFrontmatter(ctx, path),
		})
	}
	return &ontology.WalkResult{SeedType: seedType.TypeName, Nodes: nodes, Edges: edgeRows}, nil
}

func ontologyWalkLimits(maxDepth int) TraverseLimits {
	return TraverseLimits{MaxDepth: maxDepth}
}

func ontologyWalkEdges(edges []NeighborhoodEdge) []ontology.WalkEdge {
	seen := make(map[string]ontology.WalkEdge, len(edges))
	for _, edge := range edges {
		row := ontology.WalkEdge{
			RelationName: edge.RelationName,
			Source:       edge.Edge.SrcPath,
			Destination:  edge.Edge.DstPath,
			Provenance:   edge.Provenance,
			Structural:   edge.Structural,
		}
		key := row.Source + "\x00" + edge.Edge.SrcNodeID + "\x00" + row.RelationName + "\x00" + row.Destination + "\x00" + edge.Edge.DstNodeID + "\x00" + row.Provenance
		if existing, ok := seen[key]; ok && existing.Structural {
			continue
		}
		seen[key] = row
	}
	out := make([]ontology.WalkEdge, 0, len(seen))
	for _, edge := range seen {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		switch {
		case a.Source != b.Source:
			return a.Source < b.Source
		case a.RelationName != b.RelationName:
			return a.RelationName < b.RelationName
		default:
			return a.Destination < b.Destination
		}
	})
	return out
}

func walkTitleFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func (s *Scope) walkBlessedFrontmatter(ctx context.Context, path string) map[string]interface{} {
	records, err := s.Hydrate(ctx, []ontology.NodeRef{{NotePath: path, Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateSummary})
	if err != nil || len(records) == 0 || len(records[0].Frontmatter) == 0 {
		return nil
	}
	return frontmatter.FilterBlessed(records[0].Frontmatter)
}
