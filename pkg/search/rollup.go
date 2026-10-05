package search

import "context"

// Rollupper optionally collapses fine-grained results into higher-level presentation units.
// It runs after ranking and before packing.
type Rollupper interface {
	Rollup(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error)
}
