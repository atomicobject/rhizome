package retrieval

import (
	"context"

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
	return search.RunLanes(ctx, spec, r.Retrievers, r.Name())
}
