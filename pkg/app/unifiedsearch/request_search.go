package unifiedsearch

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
)

// requestSearch binds shared catalog, seeds, and policy once. Each facet copies
// the query spec, then owns its target resolution and executable plan.
type requestSearch struct {
	Planner      searchplanner.Planner
	Spec         search.QuerySpec
	VaultPath    string
	DeferPacking bool
}

func (r requestSearch) buildSpec(ctx context.Context, text string, intent search.Intent) (search.QuerySpec, []search.Warning) {
	// Docs: [[search-answer-workflow#^spec-0034-us1-ac3]] and
	// [[search-diagnostics-explain-architecture#^spec-0043-us1-ac3]].
	spec := r.Spec
	spec.Text, spec.Intent = text, intent
	spec, repairWarnings := search.RepairQuerySpec(r.VaultPath, spec)
	spec, resolutionWarnings := search.ResolveQuerySpecTargets(ctx, r.VaultPath, r.Planner.Deps.IntelStore, spec)
	warnings := append([]search.Warning{}, repairWarnings...)
	return spec, append(warnings, resolutionWarnings...)
}

func (r requestSearch) search(ctx context.Context, spec search.QuerySpec) (search.Response, error) {
	// Docs: [[unified-search-answer-architecture#^spec-0035-us3-ac1]].
	plan, err := r.Planner.Plan(ctx, spec)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return search.Response{}, ErrTimedOutBeforePlanning
		}
		return search.Response{}, err
	}
	packer := plan.Packer
	if r.DeferPacking {
		plan.Packer = nil
	}
	response, err := plan.Search(ctx, spec)
	if err == nil && r.DeferPacking {
		response.DeferredPacker = packer
	}
	return response, err
}
