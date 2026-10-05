package diagnostics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type PhaseDuration struct {
	Name       string `json:"name"`
	DurationNS int64  `json:"duration_ns"`
	Status     string `json:"status"`
}

type FallbackCount struct {
	Phase  string `json:"phase,omitempty"`
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

type IndexSummary struct {
	OperationID    string          `json:"operation_id"`
	SlowPhases     []PhaseDuration `json:"slow_phases"`
	FallbackCounts []FallbackCount `json:"fallback_counts"`
	EvidenceGaps   []string        `json:"evidence_gaps"`
}

// SummarizeIndex ranks measured spans without adding overlapping phase durations
// or inferring timings from counters. The stored report remains authoritative.
func SummarizeIndex(report evidence.Report) IndexSummary {
	summary := IndexSummary{OperationID: report.OperationID, SlowPhases: []PhaseDuration{}, FallbackCounts: []FallbackCount{}, EvidenceGaps: []string{}}
	if report.Truncated {
		summary.EvidenceGaps = append(summary.EvidenceGaps, "Report contains bounded/incomplete detail.")
	}
	if len(report.Metrics) == 0 || string(report.Metrics) == "null" {
		summary.EvidenceGaps = append(summary.EvidenceGaps, "No typed index metrics were retained.")
		return summary
	}
	var snapshot indexingperf.Snapshot
	if err := json.Unmarshal(report.Metrics, &snapshot); err != nil {
		summary.EvidenceGaps = append(summary.EvidenceGaps, "Typed index metrics could not be decoded.")
		return summary
	}
	for _, span := range snapshot.Spans {
		summary.SlowPhases = append(summary.SlowPhases, PhaseDuration{Name: span.Name, DurationNS: span.DurationNs, Status: span.Status})
	}
	sort.Slice(summary.SlowPhases, func(i, j int) bool {
		if summary.SlowPhases[i].DurationNS == summary.SlowPhases[j].DurationNS {
			return summary.SlowPhases[i].Name < summary.SlowPhases[j].Name
		}
		return summary.SlowPhases[i].DurationNS > summary.SlowPhases[j].DurationNS
	})
	for _, counter := range snapshot.Counters {
		reason := counter.Name
		observedReason := strings.HasPrefix(reason, "calledge.fallback.") || strings.HasPrefix(reason, "scope.full_recompute.") || strings.HasPrefix(reason, "watcher.full_discovery.")
		if strings.HasPrefix(reason, indexingperf.DiagnosticFallbackPrefix) {
			// Preserve the established search-reason field values. Other
			// producer families retain their full name to avoid conflating
			// call-edge fallbacks, scope recomputes, and watcher discovery.
			observedReason = true
			reason = strings.TrimPrefix(reason, indexingperf.DiagnosticFallbackPrefix)
		}
		if observedReason {
			summary.FallbackCounts = append(summary.FallbackCounts, FallbackCount{Phase: counter.Phase, Reason: reason, Count: counter.Total})
		}
	}
	sort.Slice(summary.FallbackCounts, func(i, j int) bool {
		if summary.FallbackCounts[i].Reason == summary.FallbackCounts[j].Reason {
			return summary.FallbackCounts[i].Phase < summary.FallbackCounts[j].Phase
		}
		return summary.FallbackCounts[i].Reason < summary.FallbackCounts[j].Reason
	})
	if len(snapshot.Spans) == 0 {
		summary.EvidenceGaps = append(summary.EvidenceGaps, "No completed phase spans were retained.")
	}
	if snapshot.Coverage.RejectedObservations > 0 || snapshot.Coverage.DroppedSamples > 0 || snapshot.Coverage.DroppedIntervals > 0 {
		summary.EvidenceGaps = append(summary.EvidenceGaps, fmt.Sprintf("Metric coverage: %d rejected observations, %d dropped samples, %d dropped intervals.", snapshot.Coverage.RejectedObservations, snapshot.Coverage.DroppedSamples, snapshot.Coverage.DroppedIntervals))
	}
	partialQuantiles := false
	for _, distribution := range snapshot.Latencies {
		partialQuantiles = partialQuantiles || distribution.QuantileCoverage == "retained_prefix"
	}
	for _, distribution := range snapshot.Samples {
		partialQuantiles = partialQuantiles || distribution.QuantileCoverage == "retained_prefix"
	}
	if partialQuantiles {
		summary.EvidenceGaps = append(summary.EvidenceGaps, "Some quantiles describe only the retained sample prefix, not the full operation.")
	}
	partialIntervals := false
	for _, span := range snapshot.Spans {
		partialIntervals = partialIntervals || span.IntervalCoverage == "retained_prefix"
	}
	for _, interval := range snapshot.Intervals {
		partialIntervals = partialIntervals || interval.IntervalCoverage == "retained_prefix"
	}
	if partialIntervals {
		summary.EvidenceGaps = append(summary.EvidenceGaps, "Some interval unions cover only retained intervals, not the full operation.")
	}
	return summary
}
