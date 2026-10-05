package retrieval

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/search"
)

// CompositeRetriever runs multiple retrievers and merges candidates by handle.
type CompositeRetriever struct {
	Retrievers []search.Retriever
	Label      string
}

func (r *CompositeRetriever) Name() string {
	if r.Label != "" {
		return r.Label
	}
	return "composite"
}

func (r *CompositeRetriever) NestedRetrievers() []search.Retriever {
	out := make([]search.Retriever, 0, len(r.Retrievers))
	for _, retriever := range r.Retrievers {
		if retriever != nil {
			out = append(out, retriever)
		}
	}
	return out
}

func (r *CompositeRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if len(r.Retrievers) == 0 {
		return nil, nil
	}

	results := make([]map[string]search.Candidate, len(r.Retrievers))
	errs := make([]error, len(r.Retrievers))
	namePrefix := r.Name()

	var wg sync.WaitGroup

	for i, sub := range r.Retrievers {
		if sub == nil {
			continue
		}
		sub := sub
		subCtx, cancelSub := search.RetrieverStageContext(ctx, spec, sub.Name(), i, len(r.Retrievers))
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancelSub()
			started := time.Now()
			items, err := sub.Retrieve(subCtx, spec)
			search.RecordRetrieverLane(ctx, namePrefix+"."+sub.Name(), len(items), err)
			addTiming(ctx, search.TimingEvent{
				Name:     namePrefix + "." + sub.Name(),
				Kind:     "retriever",
				Started:  started,
				Duration: time.Since(started),
				Status:   timingStatus(err),
				Err:      timingErr(err),
			})
			if err != nil {
				if search.IsBroadIntent(spec.Intent) {
					search.AddRuntimeWarning(ctx, search.Warning{
						Code:    "retriever_degraded",
						Kind:    "retrieval_error",
						Source:  namePrefix + "." + sub.Name(),
						Message: fmt.Sprintf("%s degraded: %v", sub.Name(), err),
					})
					return
				}
				errs[i] = fmt.Errorf("%s: %w", sub.Name(), err)
				return
			}

			local := make(map[string]search.Candidate, len(items))
			for _, c := range items {
				key := c.Handle.String()
				if key == "" {
					continue
				}
				if existing, ok := local[key]; ok {
					local[key] = search.MergeCandidate(existing, c)
				} else {
					local[key] = c
				}
			}

			results[i] = local
		}()
	}

	wg.Wait()

	// Merge in configured lane order: completion timing must not choose the
	// first non-empty title, path, or other metadata for a shared handle.
	merged := make(map[string]search.Candidate)
	for i, local := range results {
		if errs[i] != nil {
			return nil, errs[i]
		}
		for key, candidate := range local {
			if existing, ok := merged[key]; ok {
				merged[key] = search.MergeCandidate(existing, candidate)
			} else {
				merged[key] = candidate
			}
		}
	}

	out := make([]search.Candidate, 0, len(merged))
	for _, c := range merged {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle.String() < out[j].Handle.String() })
	return out, nil
}
