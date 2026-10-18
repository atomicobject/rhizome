package identifierreconcile

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type gitCommitGraph map[string][]string

func validateGitHistoryLineage(
	ctx context.Context, repoRoot string, events []gitHistoryEvent, max int,
	run func(string, []byte, ...string) ([]byte, []byte, error),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	needsGraph := false
	for index := 0; index+1 < len(events); index++ {
		if events[index].oid == events[index+1].oid {
			return fmt.Errorf("git history repeats an owned commit")
		}
		if len(events[index].parents) != 1 || events[index].parents[0] != events[index+1].oid {
			needsGraph = true
		}
	}
	if !needsGraph {
		return nil
	}
	output, _, err := run(repoRoot, nil, "rev-list", "--parents", "--topo-order", "--max-count="+strconv.Itoa(max+1), events[0].oid)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("git history lineage graph unavailable")
	}
	graph, complete := parseBoundedGitGraph(output, events[0].oid, max)
	if !complete {
		return fmt.Errorf("git history lineage graph incomplete")
	}
	for index := 0; index+1 < len(events); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !graph.ancestor(events[index+1].oid, events[index].oid) {
			return fmt.Errorf("git history contains incomparable path events")
		}
	}
	return nil
}

func parseBoundedGitGraph(output []byte, tip string, max int) (gitCommitGraph, bool) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || len(lines) > max {
		return nil, false
	}
	graph := make(gitCommitGraph, len(lines))
	positions := make(map[string]int, len(lines))
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || len(fields) > max+1 || (index == 0 && fields[0] != tip) {
			return nil, false
		}
		for _, oid := range fields {
			if !validFullOID(oid) || len(oid) != len(tip) {
				return nil, false
			}
		}
		if _, duplicate := graph[fields[0]]; duplicate {
			return nil, false
		}
		graph[fields[0]] = fields[1:]
		positions[fields[0]] = index
	}
	for oid, parents := range graph {
		seenParents := make(map[string]bool, len(parents))
		for _, parent := range parents {
			position, present := positions[parent]
			if !present || position <= positions[oid] || seenParents[parent] {
				return nil, false
			}
			seenParents[parent] = true
		}
	}
	seen := make(map[string]bool, len(graph))
	pending := []string{tip}
	for len(pending) > 0 {
		oid := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if !seen[oid] {
			seen[oid] = true
			pending = append(pending, graph[oid]...)
		}
	}
	if len(seen) != len(graph) {
		return nil, false
	}
	return graph, true
}

func (graph gitCommitGraph) ancestor(ancestor, descendant string) bool {
	if _, present := graph[ancestor]; !present {
		return false
	}
	seen := make(map[string]bool)
	pending := []string{descendant}
	for len(pending) > 0 {
		oid := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if oid == ancestor {
			return true
		}
		if !seen[oid] {
			seen[oid] = true
			pending = append(pending, graph[oid]...)
		}
	}
	return false
}
