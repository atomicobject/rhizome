package search

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type LaneState string

const (
	LaneStateRan      LaneState = "ran"
	LaneStateEmpty    LaneState = "empty"
	LaneStateSkipped  LaneState = "skipped"
	LaneStateTimedOut LaneState = "timed_out"
	LaneStateDegraded LaneState = "degraded"
	LaneStateCanceled LaneState = "canceled"
	LaneStateUnknown  LaneState = "unknown"
)

type LaneStatus struct {
	Lane        string    `json:"lane"`
	Facet       string    `json:"facet,omitempty"`
	Status      LaneState `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	Retrievers  []string  `json:"retrievers,omitempty"`
	ResultCount int       `json:"resultCount,omitempty"`
}

type laneRun struct {
	name        string
	status      LaneState
	reason      string
	resultCount int
}

type laneCollectorKey struct{}

type laneCollector struct {
	mu   sync.Mutex
	runs []laneRun
}

func withLaneCollector(ctx context.Context) (context.Context, *laneCollector) {
	collector := &laneCollector{}
	return context.WithValue(ctx, laneCollectorKey{}, collector), collector
}

func RecordRetrieverLane(ctx context.Context, name string, resultCount int, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	indexingperf.AddCount(ctx, "search.retriever."+name+".outcome."+string(laneStateFromErrAndCount(err, resultCount)), 1)
	indexingperf.AddCount(ctx, "search.retriever."+name+".results", int64(max(0, resultCount)))
	collector, _ := ctx.Value(laneCollectorKey{}).(*laneCollector)
	if collector == nil {
		return
	}
	run := laneRun{
		name:        name,
		status:      laneStateFromErrAndCount(err, resultCount),
		reason:      strings.TrimSpace(errString(err)),
		resultCount: max(0, resultCount),
	}
	collector.mu.Lock()
	collector.runs = append(collector.runs, run)
	collector.mu.Unlock()
}

func (c *laneCollector) snapshot() []laneRun {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]laneRun, len(c.runs))
	copy(out, c.runs)
	return out
}

func BuildLaneStatuses(retrievers []Retriever, runs []laneRun, results []RankedResult) []LaneStatus {
	planned := plannedRetrieverNames(retrievers)
	resultCounts := laneResultCounts(results)
	runByLane := map[string][]laneRun{}
	retrieversByLane := map[string][]string{}
	for _, name := range planned {
		for _, lane := range lanesForRetriever(name) {
			retrieversByLane[lane] = appendUniqueString(retrieversByLane[lane], name)
		}
	}
	for _, run := range runs {
		for _, lane := range lanesForRetriever(run.name) {
			runByLane[lane] = append(runByLane[lane], run)
			retrieversByLane[lane] = appendUniqueString(retrieversByLane[lane], run.name)
		}
	}

	laneOrder := []string{
		"note_vector",
		"code_vector",
		"intel_fts",
		"rationale_fts",
		"symbol_probe",
		"refs",
		"graph",
		"ontology",
		"call_edges",
		"definition",
		"tests",
		"note_lexical",
		"local_context",
	}
	out := make([]LaneStatus, 0, len(laneOrder))
	for _, lane := range laneOrder {
		status := LaneStatus{
			Lane:        lane,
			Status:      LaneStateSkipped,
			Reason:      "not selected by planner",
			Retrievers:  retrieversByLane[lane],
			ResultCount: resultCounts[lane],
		}
		laneRuns := runByLane[lane]
		if len(laneRuns) > 0 {
			status.Status, status.Reason = summarizeLaneRuns(laneRuns, lane, resultCounts[lane])
			if status.ResultCount == 0 && !isEvidenceSplitLane(lane) {
				for _, run := range laneRuns {
					status.ResultCount += run.resultCount
				}
			}
		} else if len(retrieversByLane[lane]) > 0 {
			status.Status = LaneStateUnknown
			status.Reason = "planned but no runtime status recorded"
		}
		if status.ResultCount > 0 && len(laneRuns) == 0 {
			status.Status = LaneStateRan
			status.Reason = ""
		}
		out = append(out, status)
	}
	return out
}

func plannedRetrieverNames(retrievers []Retriever) []string {
	var out []string
	var walk func(prefix string, rs []Retriever)
	walk = func(prefix string, rs []Retriever) {
		for _, r := range rs {
			if r == nil {
				continue
			}
			name := strings.TrimSpace(r.Name())
			if name == "" {
				continue
			}
			fullName := name
			if prefix != "" {
				fullName = prefix + "." + name
			}
			out = appendUniqueString(out, fullName)
			if nested, ok := r.(NestedRetriever); ok {
				walk(fullName, nested.NestedRetrievers())
			}
		}
	}
	walk("", retrievers)
	return out
}

func isEvidenceSplitLane(lane string) bool {
	return lane == "note_vector" || lane == "code_vector"
}

func summarizeLaneRuns(runs []laneRun, lane string, evidenceCount int) (LaneState, string) {
	if len(runs) == 0 {
		return LaneStateSkipped, "not selected by planner"
	}
	var ran, empty bool
	for _, run := range runs {
		switch run.status {
		case LaneStateTimedOut, LaneStateCanceled, LaneStateDegraded:
			return run.status, firstNonEmpty(run.reason, string(run.status))
		case LaneStateRan:
			ran = true
		case LaneStateEmpty:
			empty = true
		}
	}
	if evidenceCount > 0 || ran {
		if lane == "code_vector" && evidenceCount == 0 {
			return LaneStateEmpty, "vector ran but returned no code evidence"
		}
		if lane == "note_vector" && evidenceCount == 0 {
			return LaneStateEmpty, "vector ran but returned no note evidence"
		}
		return LaneStateRan, ""
	}
	if empty {
		return LaneStateEmpty, "ran but returned no evidence"
	}
	return LaneStateUnknown, "planned but no runtime status recorded"
}

func laneStateFromErrAndCount(err error, count int) LaneState {
	if err == nil {
		if count > 0 {
			return LaneStateRan
		}
		return LaneStateEmpty
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return LaneStateTimedOut
	}
	if errors.Is(err, context.Canceled) {
		return LaneStateCanceled
	}
	return LaneStateDegraded
}

func laneResultCounts(results []RankedResult) map[string]int {
	counts := map[string]int{}
	for _, result := range results {
		seen := map[string]struct{}{}
		for _, evidence := range result.Evidence {
			for _, lane := range lanesForEvidence(evidence.Type) {
				seen[lane] = struct{}{}
			}
		}
		for lane := range seen {
			counts[lane]++
		}
	}
	return counts
}

func lanesForEvidence(evidenceType string) []string {
	switch strings.TrimSpace(evidenceType) {
	case "note_vector_similarity":
		return []string{"note_vector"}
	case "code_vector_similarity", "anchor_vector_similarity":
		return []string{"code_vector"}
	case "intel_fts_match", "intel_doc_match":
		return []string{"intel_fts"}
	case "rationale_fts_match":
		return []string{"rationale_fts"}
	case "symbol_exact", "symbol_match":
		return []string{"symbol_probe"}
	case "definition_anchor":
		return []string{"definition"}
	case "call_edge":
		return []string{"call_edges"}
	case "ontology_structural", "ontology_ambient", "ontology_vector_similarity":
		return []string{"ontology"}
	case "graph_link", "graph_score", "graph_anchor_pagerank", "anchor_graph_edge":
		return []string{"graph"}
	case "doc_link", "code_ref":
		return []string{"refs"}
	default:
		return nil
	}
}

func lanesForRetriever(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	parts := strings.Split(name, ".")
	leaf := parts[len(parts)-1]
	switch leaf {
	case "vector", "seed_vector":
		return []string{"note_vector", "code_vector"}
	case "intel_lexical":
		return []string{"intel_fts"}
	case "rationale_fts":
		return []string{"rationale_fts"}
	case "symbol_probe":
		return []string{"symbol_probe"}
	case "doc_links", "code_anchor_notes", "code_anchor_refs", "anchor_graph", "refs":
		return []string{"refs"}
	case "graph", "outgoing_links", "graph_ppr", "diffusion":
		return []string{"graph"}
	case "ontology":
		return []string{"ontology"}
	case "call_edges":
		return []string{"call_edges"}
	case "definition":
		return []string{"definition"}
	case "tests_for_code":
		return []string{"tests"}
	case "note_lexical":
		return []string{"note_lexical"}
	case "explicit_seeds", "local_docs":
		return []string{"local_context"}
	default:
		return nil
	}
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// LanesForEvidence reports which retrieval lanes an evidence type belongs to.
// Ranking-only observations (query specificity, retriever ranks, graph
// authority) belong to no lane and never corroborate.
func LanesForEvidence(evidenceType string) []string { return lanesForEvidence(evidenceType) }
