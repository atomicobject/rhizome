package noderead

import (
	"context"
)

// Execute is the high-level request/job scoped read plan boundary. It executes
// relation work breadth-first so every depth shares scope-level edge and
// hydration batches before GraphQL or another caller assembles its own result.
func (s *Scope) Execute(ctx context.Context, plan NodeReadPlan) (NodeReadResult, error) {
	if err := ctx.Err(); err != nil {
		return NodeReadResult{}, err
	}
	if s == nil {
		return NodeReadResult{}, nil
	}
	before := s.Diagnostics()
	result := NodeReadResult{}
	rootRefs := nodeReadRootRefs(plan.Roots)
	rootHydrate := hydrateOptionsForFieldSelection(plan.Hydrate, plan.Fields)
	if rootHydrate.Profile != "" && len(rootRefs) > 0 {
		records, err := s.Hydrate(ctx, rootRefs, rootHydrate)
		if err != nil {
			return NodeReadResult{}, err
		}
		result.Roots = nodeViewsFromRecords(records)
	}
	if plan.Fields.IncludeLocators && len(rootRefs) > 0 {
		if err := s.attachViewLocators(ctx, result.Roots, rootRefs); err != nil {
			return NodeReadResult{}, err
		}
	}

	current := make([]relationWork, 0, len(plan.Relations))
	for _, relation := range plan.Relations {
		sources := relation.Sources
		if len(sources) == 0 {
			sources = rootRefs
		}
		current = append(current, relationWork{selection: relation, sources: sources})
	}
	seenNodeRefs := make(map[string]struct{})
	for depth := 0; len(current) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			return NodeReadResult{}, err
		}
		if maxDepth := plan.Limits.MaxDepth; maxDepth > 0 && depth >= maxDepth {
			result.Truncated = true
			result.Diagnostics.Truncations++
			break
		}
		level := coalesceRelationWork(current)
		next := make([]relationWork, 0)
		for _, work := range level {
			if work.selection.MaxDepth > 0 && depth >= work.selection.MaxDepth {
				result.Truncated = true
				result.Diagnostics.Truncations++
				continue
			}
			limits := relationLimits(work.selection, plan.Limits)
			remainingEdges := remainingNodeReadLimit(minPositiveLimit(plan.Limits.MaxEdges, plan.Budget.MaxEdges), len(result.Edges))
			if remainingEdges.exhausted {
				result.Truncated = true
				result.Diagnostics.Truncations++
				continue
			}
			if remainingEdges.remaining > 0 {
				limits.FirstTotal = minPositiveLimit(limits.FirstTotal, remainingEdges.remaining)
			}
			remainingNodes := remainingNodeReadLimit(minPositiveLimit(plan.Limits.MaxNodes, plan.Budget.MaxNodes), len(seenNodeRefs))
			if remainingNodes.exhausted {
				result.Truncated = true
				result.Diagnostics.Truncations++
				continue
			}

			work.selection.Sources = normalizeNeighborhoodSources(work.sources, nil)
			if len(work.selection.Sources) == 0 {
				continue
			}
			neighborhood, err := s.executeSingleRelationSelection(ctx, work.selection, limits)
			if err != nil {
				return NodeReadResult{}, err
			}
			if remainingNodes.remaining > 0 {
				trimmed, truncated := trimNeighborhoodEdgesByTargets(neighborhood.Edges, seenNodeRefs, remainingNodes.remaining)
				if truncated {
					neighborhood.Edges = trimmed
					neighborhood = regroupNeighborhood(neighborhood, neighborhood.Edges, limits)
					result.Truncated = true
					result.Diagnostics.Truncations++
				}
			}
			if remainingEdges.remaining > 0 && len(neighborhood.Edges) > remainingEdges.remaining {
				neighborhood.Edges = neighborhood.Edges[:remainingEdges.remaining]
				neighborhood = regroupNeighborhood(neighborhood, neighborhood.Edges, limits)
				result.Truncated = true
				result.Diagnostics.Truncations++
			}
			result.Edges = append(result.Edges, neighborhood.Edges...)
			for _, source := range neighborhood.Sources {
				result.Groups = append(result.Groups, RelationGroup{
					Name:      work.selection.Name,
					Source:    source.Source,
					Edges:     append([]NeighborhoodEdge(nil), source.Edges...),
					Truncated: source.Truncated,
				})
				if source.Truncated {
					result.Truncated = true
					result.Diagnostics.Truncations++
				}
			}

			targets := uniqueNeighborhoodTargets(neighborhood.Edges)
			targets = trimNewNodeRefs(targets, seenNodeRefs, remainingNodes.remaining)
			if remainingNodes.remaining > 0 && len(uniqueNeighborhoodTargets(neighborhood.Edges)) > len(targets) {
				result.Truncated = true
				result.Diagnostics.Truncations++
			}
			for _, target := range targets {
				seenNodeRefs[nodeRefResultKey(target)] = struct{}{}
			}
			hydrate := hydrateOptionsForFieldSelection(work.selection.Hydrate, work.selection.Fields)
			if hydrate.Profile != "" && len(targets) > 0 {
				records, err := s.Hydrate(ctx, targets, hydrate)
				if err != nil {
					return NodeReadResult{}, err
				}
				result.Nodes = append(result.Nodes, nodeViewsFromRecords(records)...)
			}
			if work.selection.Fields.IncludeLocators && len(targets) > 0 {
				if err := s.attachViewLocators(ctx, result.Nodes, targets); err != nil {
					return NodeReadResult{}, err
				}
			}
			if work.selection.MaxDepth > 0 && depth+1 >= work.selection.MaxDepth {
				if len(work.selection.Children) > 0 && len(targets) > 0 {
					result.Truncated = true
					result.Diagnostics.Truncations++
				}
				continue
			}
			for _, child := range work.selection.Children {
				if child.MaxDepth > 0 && depth+1 >= child.MaxDepth {
					result.Truncated = true
					result.Diagnostics.Truncations++
					continue
				}
				next = append(next, relationWork{selection: child, sources: targets, depth: depth + 1})
			}
		}
		current = next
	}
	if plan.Diagnostics {
		result.Diagnostics = result.Diagnostics.add(nodeReadDiagnosticsDelta(before, s.Diagnostics()))
	}
	return result, nil
}
