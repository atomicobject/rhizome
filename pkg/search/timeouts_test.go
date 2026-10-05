package search

import (
	"context"
	"testing"
	"time"
)

func TestDefaultQueryTimeout(t *testing.T) {
	if DefaultQueryTimeout != 8*time.Second {
		t.Fatalf("expected 8s default query timeout, got %s", DefaultQueryTimeout)
	}
}

func TestRetrieverStageBudgetLeavesTimeForLaterStages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	budget := RetrieverStageBudget(ctx, QuerySpec{Intent: IntentOverview}, "vector", 0, 3)
	if budget <= 0 {
		t.Fatalf("expected positive stage budget")
	}
	if budget >= 900*time.Millisecond {
		t.Fatalf("expected stage budget to leave time for later stages, got %s", budget)
	}
}

func TestRetrieverStageBudgetGivesVectorLaneLargerShare(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	vectorBudget := RetrieverStageBudget(ctx, QuerySpec{Intent: IntentOverview}, "vector", 0, 3)
	graphBudget := RetrieverStageBudget(ctx, QuerySpec{Intent: IntentOverview}, "graph", 0, 3)
	if vectorBudget <= graphBudget {
		t.Fatalf("expected vector budget %s to exceed graph budget %s", vectorBudget, graphBudget)
	}
}
