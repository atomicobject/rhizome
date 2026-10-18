package search

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// RunLanes runs every retriever concurrently under one shared retrieval deadline.
// Retrievers must honor context cancellation; all started lanes finish before return.
// Metadata merges follow configured order rather than completion order.
func RunLanes(ctx context.Context, spec QuerySpec, retrievers []Retriever, prefix string) ([]Candidate, error) {
	ctx, cancel := retrievalContext(ctx)
	defer cancel()
	type result struct {
		items []Candidate
		err   error
	}
	results := make([]result, len(retrievers))
	var wg sync.WaitGroup
	for i, retriever := range retrievers {
		if retriever == nil {
			continue
		}
		laneCtx, cancelLane := context.WithCancel(ctx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancelLane()
			started := time.Now()
			items, err := retriever.Retrieve(laneCtx, spec)
			results[i] = result{items: items, err: err}
			name := retriever.Name()
			if prefix != "" {
				name = prefix + "." + name
			}
			RecordRetrieverLane(ctx, name, len(items), err)
			if timings := TimingsFromContext(ctx); timings != nil {
				timings.Add(TimingEvent{Name: name, Kind: "retriever", Started: started, Duration: time.Since(started), Status: TimingStatus(err), Err: TimingError(err)})
			}
		}()
	}
	wg.Wait()

	merged := make(map[string]Candidate)
	for i, result := range results {
		if result.err != nil {
			name := retrievers[i].Name()
			if prefix != "" {
				name = prefix + "." + name
			}
			if !IsBroadIntent(spec.Intent) {
				return nil, fmt.Errorf("%s retriever: %w", name, result.err)
			}
			AddRuntimeWarning(ctx, Warning{Code: "retriever_degraded", Kind: "retrieval_error", Source: name, Message: fmt.Sprintf("%s degraded: %v", name, result.err)})
			continue
		}
		for key, candidate := range mergeRetrieverItems(result.items) {
			if existing, ok := merged[key]; ok {
				merged[key] = MergeCandidate(existing, candidate)
			} else {
				merged[key] = candidate
			}
		}
	}
	out := make([]Candidate, 0, len(merged))
	for _, candidate := range merged {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle.String() < out[j].Handle.String() })
	if !IsBroadIntent(spec.Intent) {
		return out, ctx.Err()
	}
	return out, nil
}

func mergeRetrieverItems(items []Candidate) map[string]Candidate {
	local := make(map[string]Candidate, len(items))
	for _, candidate := range items {
		candidate, key := ensureCandidateKey(candidate)
		if key == "" {
			continue
		}
		if existing, ok := local[key]; ok {
			local[key] = MergeCandidate(existing, candidate)
		} else {
			local[key] = candidate
		}
	}
	return local
}
