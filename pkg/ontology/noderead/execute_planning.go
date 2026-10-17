package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

func relationLimits(selection RelationSelection, parent TraverseLimits) TraverseLimits {
	limits := parent
	if selection.FirstPerSource > 0 {
		limits.FirstPerSource = selection.FirstPerSource
	}
	if selection.FirstTotal > 0 {
		limits.FirstTotal = selection.FirstTotal
	}
	return limits
}

func coalesceRelationWork(items []relationWork) []relationWork {
	byKey := make(map[string]relationWork, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		item.sources = normalizeNeighborhoodSources(item.sources, nil)
		if len(item.sources) == 0 {
			continue
		}
		key := relationWorkKey(item.selection)
		current, exists := byKey[key]
		if !exists {
			order = append(order, key)
			current = item
			current.sources = nil
			current.selection.Children = nil
		}
		current.sources = mergeNodeRefs(current.sources, item.sources)
		current.selection.Children = append(current.selection.Children, item.selection.Children...)
		byKey[key] = current
	}
	out := make([]relationWork, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

func relationWorkKey(selection RelationSelection) string {
	return strings.Join([]string{
		selection.Name,
		strings.Join(normalizeStrings(selection.SourceTypes), ","),
		string(selection.Direction),
		strings.Join(normalizeStrings(selection.RelationNames), ","),
		provenanceCacheKey(selection.Provenance),
		boolString(selection.IncludeStructural),
		boolString(selection.IncludeAmbient),
		strings.Join(normalizeStrings(selection.TargetTypes), ","),
		strings.Join(normalizeStrings(selection.TargetInterfaces), ","),
		string(selection.NeighborScope),
		intString(selection.FirstPerSource),
		intString(selection.FirstTotal),
		fieldSelectionKey(selection.Fields),
		string(selection.Hydrate.Profile),
	}, "|")
}

func hydrateOptionsForFieldSelection(hydrate HydrateOptions, fields FieldSelection) HydrateOptions {
	if hydrate.Profile != "" {
		return hydrate
	}
	if fields.IncludeContent {
		hydrate.Profile = HydrateContent
		return hydrate
	}
	if fields.IncludeBuiltin || fields.IncludeIssues || fields.IncludeLocators || len(fields.Names) > 0 {
		hydrate.Profile = HydrateSummary
	}
	return hydrate
}

func fieldSelectionKey(fields FieldSelection) string {
	return strings.Join([]string{
		strings.Join(normalizeStrings(fields.Names), ","),
		boolString(fields.IncludeBuiltin),
		boolString(fields.IncludeIssues),
		boolString(fields.IncludeContent),
		boolString(fields.IncludeLocators),
	}, ",")
}

func mergeNodeRefs(existing, refs []ontology.NodeRef) []ontology.NodeRef {
	out := append([]ontology.NodeRef(nil), existing...)
	seen := make(map[string]struct{}, len(out)+len(refs))
	for _, ref := range out {
		seen[nodeRefResultKey(ref)] = struct{}{}
	}
	for _, ref := range refs {
		key := nodeRefResultKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func filterRelationSources(sources []ontology.NodeRef, sourceTypes []string) []ontology.NodeRef {
	sourceTypes = normalizeStrings(sourceTypes)
	if len(sourceTypes) == 0 {
		return sources
	}
	allowed := make(map[string]struct{}, len(sourceTypes))
	for _, typeName := range sourceTypes {
		allowed[typeName] = struct{}{}
	}
	out := make([]ontology.NodeRef, 0, len(sources))
	for _, source := range sources {
		if _, ok := allowed[source.TypeName]; ok {
			out = append(out, source)
		}
	}
	return out
}

func trimNewNodeRefs(refs []ontology.NodeRef, seen map[string]struct{}, remaining int) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(refs))
	newCount := 0
	for _, ref := range refs {
		key := nodeRefResultKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			out = append(out, ref)
			continue
		}
		if remaining > 0 && newCount >= remaining {
			break
		}
		out = append(out, ref)
		newCount++
	}
	return out
}

func trimNeighborhoodEdgesByTargets(edges []NeighborhoodEdge, seen map[string]struct{}, remaining int) ([]NeighborhoodEdge, bool) {
	out := make([]NeighborhoodEdge, 0, len(edges))
	localSeen := make(map[string]struct{})
	newCount := 0
	truncated := false
	for _, edge := range edges {
		key := nodeRefResultKey(edge.Target)
		if key == "" {
			out = append(out, edge)
			continue
		}
		if _, ok := seen[key]; ok {
			out = append(out, edge)
			continue
		}
		if _, ok := localSeen[key]; ok {
			out = append(out, edge)
			continue
		}
		if newCount >= remaining {
			truncated = true
			continue
		}
		localSeen[key] = struct{}{}
		newCount++
		out = append(out, edge)
	}
	return out, truncated
}

type remainingNodeRead struct {
	remaining int
	exhausted bool
}

func remainingNodeReadLimit(limit, used int) remainingNodeRead {
	if limit <= 0 {
		return remainingNodeRead{}
	}
	if used >= limit {
		return remainingNodeRead{exhausted: true}
	}
	return remainingNodeRead{remaining: limit - used}
}
