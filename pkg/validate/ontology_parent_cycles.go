package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// IssueCodeParentCycle reports records whose PARENT-role chain repeats.
const IssueCodeParentCycle = "parent_cycle"

// ParentCycleData lists the records in one parent cycle, starting from the
// lexically first path and following each record's parent link.
type ParentCycleData struct {
	Records []string `json:"records"`
}

// parentCycleIssues follows every note record's @display(role: PARENT) link
// and reports each cycle once, with all of its records. It belongs to the
// ontology check because a cycle is a typed-record integrity problem.
func parentCycleIssues(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime) ([]Issue, error) {
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		return nil, nil
	}
	typeNames := make([]string, 0, len(runtime.Schema.Types))
	for name := range runtime.Schema.Types {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)

	type parentLink struct{ typeName, field, target string }
	parentOf := map[string]parentLink{}
	for _, typeName := range typeNames {
		field := ontology.ParentField(runtime.Schema.Types[typeName].Fields)
		if field == nil {
			continue
		}
		paths, err := runtime.Store.OntologyPathsByType(ctx, typeName, 0)
		if err != nil {
			return nil, err
		}
		typed := make(map[string]struct{}, len(paths))
		for _, path := range paths {
			typed[path] = struct{}{}
		}
		edges, err := runtime.Store.OntologyEdgesForPaths(ctx, paths, false, field.Name, 0)
		if err != nil {
			return nil, err
		}
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].SrcPath != edges[j].SrcPath {
				return edges[i].SrcPath < edges[j].SrcPath
			}
			return edges[i].DstPath < edges[j].DstPath
		})
		for _, edge := range edges {
			// The schema confines PARENT to note types, so a parent is a note
			// root; a link into a heading or an embedded node is not one.
			if edge.SrcNodeID != "" || edge.DstNodeID != "" || strings.TrimSpace(edge.DstPath) == "" {
				continue
			}
			if _, ok := typed[edge.SrcPath]; !ok {
				continue
			}
			if _, set := parentOf[edge.SrcPath]; !set {
				parentOf[edge.SrcPath] = parentLink{typeName: typeName, field: field.Name, target: edge.DstPath}
			}
		}
	}

	targets := make(map[string]string, len(parentOf))
	for path, link := range parentOf {
		targets[path] = link.target
	}
	var issues []Issue
	for _, cycle := range parentCycles(targets) {
		if !runCtx.inPostcheckScope(cycle...) {
			continue
		}
		first := parentOf[cycle[0]]
		issues = append(issues, Issue{
			Code:              IssueCodeParentCycle,
			Path:              cycle[0],
			Type:              first.typeName,
			Field:             first.field,
			Target:            first.target,
			Message:           fmt.Sprintf("Parent chain repeats: %s → %s. Change or clear the parent link on one of these records so the chain reaches a root.", strings.Join(cycle, " → "), cycle[0]),
			Data:              mustMarshal(ParentCycleData{Records: cycle}),
			AffectedPaths:     cycle,
			AffectedNotePaths: cycle,
		})
	}
	return issues, nil
}

// parentCycles returns every cycle in the parent graph, each rotated to start
// at its lexically first record, ordered by that first record. Every record
// has at most one parent, so each walk reaches at most one cycle.
func parentCycles(parentOf map[string]string) [][]string {
	starts := make([]string, 0, len(parentOf))
	for path := range parentOf {
		starts = append(starts, path)
	}
	sort.Strings(starts)

	const (
		unvisited = iota
		onWalk
		finished
	)
	state := make(map[string]int, len(parentOf))
	var cycles [][]string
	for _, start := range starts {
		var walk []string
		node := start
		for node != "" && state[node] == unvisited {
			state[node] = onWalk
			walk = append(walk, node)
			node = parentOf[node] // "" ends a chain without a parent
		}
		if node != "" && state[node] == onWalk {
			for i, path := range walk {
				if path == node {
					cycles = append(cycles, rotateToFirst(walk[i:]))
					break
				}
			}
		}
		for _, path := range walk {
			state[path] = finished
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return cycles[i][0] < cycles[j][0] })
	return cycles
}

func rotateToFirst(cycle []string) []string {
	first := 0
	for i, path := range cycle {
		if path < cycle[first] {
			first = i
		}
	}
	return append(append([]string{}, cycle[first:]...), cycle[:first]...)
}
