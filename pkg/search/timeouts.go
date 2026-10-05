package search

import (
	"context"
	"strings"
	"time"
)

// DefaultQueryTimeout is the default ceiling for answer-oriented search calls
// that do not provide their own deadline.
const DefaultQueryTimeout = 8 * time.Second

// RetrieverStageContext returns a child context for one retriever in a
// deadline-bound progressive run. The budget is intentionally soft: retrievers
// that respect context cannot consume the whole request, but callers without a
// deadline keep the original context unchanged.
func RetrieverStageContext(ctx context.Context, spec QuerySpec, name string, index, total int) (context.Context, context.CancelFunc) {
	budget := RetrieverStageBudget(ctx, spec, name, index, total)
	if budget <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, budget)
}

func RetrieverStageBudget(ctx context.Context, _ QuerySpec, name string, index, total int) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return remaining
	}
	stagesLeft := total - index
	if stagesLeft <= 1 {
		return remaining
	}
	reserve := remaining / 4
	if reserve > 500*time.Millisecond {
		reserve = 500 * time.Millisecond
	}
	usable := remaining - reserve
	if usable <= 0 {
		return remaining / time.Duration(stagesLeft+1)
	}
	budget := usable * 2 / time.Duration(stagesLeft+1)
	if strings.Contains(strings.ToLower(name), "vector") {
		vectorBudget := budget + usable/10
		if vectorBudget > budget {
			budget = vectorBudget
		}
	}
	if floor := 75 * time.Millisecond; budget < floor && remaining > floor {
		budget = floor
	}
	if budget >= remaining {
		return remaining
	}
	return budget
}
