package unifiedsearch

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/search"
)

type queryRunResult struct {
	resp     search.Response
	warnings []search.Warning
	err      error
}

func runMultiQuery(ctx context.Context, inputs []QueryInput, fallbackIntent search.Intent, buildSpec func(context.Context, string, search.Intent) (search.QuerySpec, []search.Warning), runSearch func(context.Context, search.QuerySpec) (search.Response, error)) ([]search.Response, []search.Warning, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	concurrency := min(len(inputs), min(runtime.GOMAXPROCS(0), 4))
	if concurrency < 1 {
		concurrency = 1
	}
	// Each facet is a full search run, so cap fan-out below GOMAXPROCS to avoid
	// multiplying retriever/provider concurrency for one user request.
	sem := make(chan struct{}, concurrency)
	results := make([]queryRunResult, len(inputs))
	var wg sync.WaitGroup
	for i, input := range inputs {
		i, input := i, input
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			intent := fallbackIntent
			if strings.TrimSpace(input.Mode) != "" {
				intent = search.Intent(strings.TrimSpace(input.Mode))
			}
			spec, warnings := buildSpec(ctx, input.Text, intent)
			if search.ShouldBlockPrecisionFallback(spec) {
				results[i].resp = search.Response{Query: spec, Lanes: []search.LaneStatus{{Lane: "target_resolution", Status: search.LaneStateDegraded, Reason: "exact target resolution unavailable"}}}
				results[i].warnings = warnings
				return
			}
			resp, err := runSearch(ctx, spec)
			if err != nil {
				results[i].err = err
				cancel()
				return
			}
			results[i].resp = resp
			results[i].warnings = append(warnings, resp.Warnings...)
		}()
	}
	wg.Wait()
	responses := make([]search.Response, 0, len(inputs))
	var warnings []search.Warning
	for _, result := range results {
		if result.err != nil {
			return nil, nil, result.err
		}
		responses = append(responses, result.resp)
		warnings = append(warnings, result.warnings...)
	}
	return responses, dedupeWarnings(warnings), nil
}

func aggregateMultiQueryLanes(responses []search.Response) []search.LaneStatus {
	var out []search.LaneStatus
	for index, response := range responses {
		facet := strings.TrimSpace(response.Query.Text) + "\x00" + string(response.Query.Intent)
		if facet == "\x00" {
			facet = fmt.Sprintf("facet-%d", index+1)
		}
		for _, status := range response.Lanes {
			status.Facet = facet
			status.Retrievers = append([]string(nil), status.Retrievers...)
			sort.Strings(status.Retrievers)
			out = append(out, status)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Facet != out[j].Facet {
			return out[i].Facet < out[j].Facet
		}
		return out[i].Lane < out[j].Lane
	})
	return out
}

func dedupeWarnings(warnings []search.Warning) []search.Warning {
	seen := map[string]struct{}{}
	out := make([]search.Warning, 0, len(warnings))
	for _, warning := range warnings {
		key := strings.Join([]string{warning.Code, warning.Kind, warning.Source, warning.Message}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, warning)
	}
	return out
}

func cloneTargetCandidate(candidate *search.TargetCandidate) *search.TargetCandidate {
	if candidate == nil {
		return nil
	}
	copy := *candidate
	return &copy
}
