package tasks

import (
	"context"
	"os"
)

const keepTestRuns = 10

func (s *Service) retainTests(ctx context.Context, tests []RunRecord, referenced map[string]bool) []RunRecord {
	counts := map[string]int{}
	kept := make([]RunRecord, 0, len(tests))
	for i := len(tests) - 1; i >= 0; i-- {
		r := tests[i]
		key := r.SourceCheckout + "\x00" + r.Task
		counts[key]++
		if counts[key] > keepTestRuns {
			// Retain ownership if removing the worktree fails, so a later pass retries.
			if r.Worktree == "" || s.removeWorktree(ctx, "", r.Worktree) == nil {
				if dir, err := s.runDir(r.ID); err == nil {
					_ = os.RemoveAll(dir)
				}
				continue
			}
		}
		kept = append(kept, r)
		referenced[r.ID] = true
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}
