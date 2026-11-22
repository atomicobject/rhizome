package tasks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubPullRequest replaces gh for one test; during, when set, runs as the
// pull request opens, after the push and before cleanup.
func stubPullRequest(t *testing.T, prErr error, during func(dir string)) {
	t.Helper()
	saved := openPullRequest
	t.Cleanup(func() { openPullRequest = saved })
	openPullRequest = func(_ context.Context, dir, _, _, _, _ string) (string, error) {
		if during != nil {
			during(dir)
		}
		return "https://example.test/pull/1", prErr
	}
}

func okNotesAgent(t *testing.T, result string) func(AgentRequest) (AgentResult, error) {
	return func(req AgentRequest) (AgentResult, error) {
		writeFile(t, filepath.Join(req.Dir, "notes/a.md"), "a\n")
		writeResult(t, req, result)
		return AgentResult{Message: "Tended the notes."}, nil
	}
}

// Cleaning up after an opened pull request never removes a worktree the
// person switched to another branch or left uncommitted work in, and it
// keeps the task branch the pull request uses.
func TestPullRequestCleanupLeavesTheWorktreeThePersonUses(t *testing.T) {
	for name, touch := range map[string]func(t *testing.T, dir string){
		"switched": func(t *testing.T, dir string) { run(t, dir, "switch", "--quiet", "-c", "mine") },
		"dirty":    func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "notes/mine.md"), "mine\n") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newLandFixture(t, "name: Tend notes\nevery: 1h\nland: pr\nprompt: Tend the notes.\n")
			var worktree string
			stubPullRequest(t, nil, func(dir string) { worktree = dir; touch(t, dir) })
			f.agent.run = okNotesAgent(t, `{"status": "ok"}`)
			require.Equal(t, OutcomeReady, f.pass().Outcome)

			require.DirExists(t, worktree, "the person's worktree is left")
			r := lastRun(t, f.svc, "tend")
			run(t, f.host, "show-ref", "--verify", "--quiet", "refs/heads/"+r.Branch)
			it := activeItem(t, f.svc, "tend")
			require.NotNil(t, it, "the item stays, so the worktree can be found")
			require.Equal(t, worktree, it.Worktree)
			require.Contains(t, it.Reason, "The pull request is open, but its local worktree could not be removed")
		})
	}
}
