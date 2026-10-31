package tasks

import (
	"context"
	"testing"
	"time"
)

func TestCachedMergeWarningAsksGitHubOncePerTTL(t *testing.T) {
	calls := 0
	readMergeWarning = func(context.Context, string) string { calls++; return "squash drops tags" }
	t.Cleanup(func() { readMergeWarning = mergeWarning })
	s := &Service{}
	start := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{start, start.Add(time.Minute), start.Add(mergeWarningTTL - time.Second)} {
		if got := s.cachedMergeWarning(context.Background(), at); got != "squash drops tags" {
			t.Fatalf("warning = %q", got)
		}
	}
	if calls != 1 {
		t.Fatalf("gh asked %d times within the TTL, want 1", calls)
	}
	s.cachedMergeWarning(context.Background(), start.Add(mergeWarningTTL))
	if calls != 2 {
		t.Fatalf("gh asked %d times after the TTL, want 2", calls)
	}
}
