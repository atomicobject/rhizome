package diagnostics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
)

// readCacheEvent names the server read-cache window pkg/app/web records
// (SPEC-0136 R10). Queries appear only as hashes.
const readCacheEvent = "web.read_cache"

// maxTextReadCacheQueries keeps the human view to the costliest queries.
const maxTextReadCacheQueries = 10

// ReadCacheQuery totals one query hash across windows.
type ReadCacheQuery struct {
	Hash        string  `json:"hash"`
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	Joins       uint64  `json:"joins"`
	Bypasses    uint64  `json:"bypasses"`
	Executions  uint64  `json:"executions"`
	ExecTotalMs float64 `json:"exec_total_ms"`
	ExecMaxMs   float64 `json:"exec_max_ms"`
	StoredBytes int64   `json:"stored_bytes"`
	Mismatches  uint64  `json:"mismatches"`
}

// ReadCacheSummary aggregates read-cache windows; Entries and Bytes are the
// latest window's cache size.
type ReadCacheSummary struct {
	Windows int              `json:"windows"`
	Entries int              `json:"entries"`
	Bytes   int64            `json:"bytes"`
	Queries []ReadCacheQuery `json:"queries"`
}

type readCacheWindow struct {
	Entries int                       `json:"entries"`
	Bytes   int64                     `json:"bytes"`
	Queries map[string]ReadCacheQuery `json:"queries"`
}

type readCacheAggregator struct {
	summary ReadCacheSummary
	queries map[string]*ReadCacheQuery
}

func (a *readCacheAggregator) visit(event evidence.Event) {
	var window readCacheWindow
	if len(event.Metrics) == 0 || json.Unmarshal(event.Metrics, &window) != nil {
		return
	}
	if a.queries == nil {
		a.queries = map[string]*ReadCacheQuery{}
	}
	// Events arrive in chronological order, so the last window is the latest.
	a.summary.Windows++
	a.summary.Entries, a.summary.Bytes = window.Entries, window.Bytes
	for hash, counts := range window.Queries {
		total := a.queries[hash]
		if total == nil {
			total = &ReadCacheQuery{Hash: hash}
			a.queries[hash] = total
		}
		total.Hits += counts.Hits
		total.Misses += counts.Misses
		total.Joins += counts.Joins
		total.Bypasses += counts.Bypasses
		total.Executions += counts.Executions
		total.ExecTotalMs += counts.ExecTotalMs
		total.ExecMaxMs = max(total.ExecMaxMs, counts.ExecMaxMs)
		total.StoredBytes += counts.StoredBytes
		total.Mismatches += counts.Mismatches
	}
}

func (a *readCacheAggregator) result() *ReadCacheSummary {
	if a.summary.Windows == 0 {
		return nil
	}
	out := a.summary
	for _, query := range a.queries {
		out.Queries = append(out.Queries, *query)
	}
	sort.Slice(out.Queries, func(i, j int) bool {
		if out.Queries[i].ExecTotalMs != out.Queries[j].ExecTotalMs {
			return out.Queries[i].ExecTotalMs > out.Queries[j].ExecTotalMs
		}
		return out.Queries[i].Hash < out.Queries[j].Hash
	})
	return &out
}

func writeReadCacheText(b *strings.Builder, summary *ReadCacheSummary) {
	if summary == nil {
		return
	}
	fmt.Fprintf(b, "Read cache: %d windows; latest size %d entries, %d bytes. Queries by execution time:\n", summary.Windows, summary.Entries, summary.Bytes)
	for i, query := range summary.Queries {
		if i == maxTextReadCacheQueries {
			fmt.Fprintf(b, "  ... %d more queries; use --json for all.\n", len(summary.Queries)-maxTextReadCacheQueries)
			break
		}
		fmt.Fprintf(b, "  %s: hits=%d misses=%d joined=%d bypassed=%d executions=%d exec total=%s max=%s stored=%dB",
			query.Hash, query.Hits, query.Misses, query.Joins, query.Bypasses, query.Executions, msDuration(query.ExecTotalMs), msDuration(query.ExecMaxMs), query.StoredBytes)
		if query.Mismatches > 0 {
			fmt.Fprintf(b, " MISMATCHES=%d", query.Mismatches)
		}
		b.WriteByte('\n')
	}
}
