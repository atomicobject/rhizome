package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// StartPlan is how `rzm tasks start` opens a task in the person's harness.
type StartPlan struct {
	ItemID    string       `json:"item"`
	Task      string       `json:"task"`
	Harness   harness.Kind `json:"harness,omitempty"`
	Dir       string       `json:"dir"`
	Argv      []string     `json:"argv,omitempty"`
	Branch    string       `json:"branch"`
	Prompt    string       `json:"prompt"`
	OpenPulls []string     `json:"openPullRequests,omitempty"`
	prior     ItemState
}

// PlanStart prepares an interactive start of a due or needs-attention item.
// It changes nothing; MarkStarted records the start once the harness runs.
func (s *Service) PlanStart(ctx context.Context, itemID string, kind harness.Kind) (StartPlan, error) {
	st, err := s.store.Load()
	if err != nil {
		return StartPlan{}, err
	}
	it, err := st.Item(itemID)
	if err != nil {
		return StartPlan{}, err
	}
	if it.Kind != KindTask || (it.State != StateDue && it.State != StateAttention) {
		return StartPlan{}, fmt.Errorf("item %s is %s; only a due or needs-attention task can be started", it.ID, it.State)
	}
	ref, commit, def, err := s.currentDefinition(ctx, it.Task)
	if err != nil {
		return StartPlan{}, err
	}
	if strings.TrimSpace(def.Prompt) == "" {
		return StartPlan{}, fmt.Errorf("task %q has no prompt to start", def.ID)
	}
	branch, err := s.freeBranch(ctx, def)
	if err != nil {
		return StartPlan{}, err
	}
	plan := StartPlan{
		ItemID: it.ID, Task: def.ID, Harness: kind, Dir: s.opts.Repo.MainCheckout, Branch: branch, prior: it.State,
		Prompt: InteractivePrompt(def, s.locations(def, commit), CheckResult{Summary: it.Summary, Context: it.Context}, ref, branch),
	}
	switch kind {
	case harness.KindClaude:
		plan.Argv = []string{"claude", "--worktree", strings.ReplaceAll(strings.TrimPrefix(branch, "rzm/"), "/", "-"), plan.Prompt}
	case harness.KindCodex:
		plan.Argv = []string{"codex", "--worktree", plan.Prompt}
	}
	plan.OpenPulls = openPulls(ctx, s.opts.Repo.MainCheckout, def.ID)
	return plan, nil
}

// InteractivePrompt is the preamble plus the task's prompt for a session
// the person starts. The harness chose the worktree's starting commit, so
// the first step moves to a new branch at the resolved commit.
func InteractivePrompt(def Definition, loc Locations, check CheckResult, ref, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are starting the scheduled task %q (%s) with the person.\n\n", def.Name, def.ID)
	fmt.Fprintf(&b, "First, in this new worktree, run: git switch -c %s %s\nThat puts the work on its own branch at commit %s of %s.\n\n", branch, loc.Commit, loc.Commit, ref)
	writeLocations(&b, loc, check)
	fmt.Fprintf(&b, "Confirm the task is still needed against the current code. End every commit message with the line %q. If nothing is left to do, say so and stop without opening a pull request.\n\n", def.Tag())
	b.WriteString("Task instructions:\n\n")
	b.WriteString(strings.TrimSpace(def.Prompt))
	b.WriteString("\n")
	return b.String()
}

// MarkStarted records that the harness launched for the item, and a run of
// the task.
func (s *Service) MarkStarted(ctx context.Context, plan StartPlan) error {
	return s.updateItem(ctx, plan.ItemID, func(st *State, it *Item) error {
		it.set(StateStarted, s.opts.Now())
		it.Branch, it.Reason = plan.Branch, ""
		st.Record(RunRecord{Task: it.Task, At: s.opts.Now(), Outcome: OutcomeStarted})
		return nil
	})
}

// UndoStart returns an item to where it was when the harness exited at
// once, for example because it did not trust the folder.
func (s *Service) UndoStart(ctx context.Context, plan StartPlan, reason string) error {
	return s.updateItem(ctx, plan.ItemID, func(st *State, it *Item) error {
		it.set(plan.prior, s.opts.Now())
		it.Branch, it.Reason = "", reason
		if n := len(st.History); n > 0 && st.History[n-1].Task == it.Task && st.History[n-1].Outcome == OutcomeStarted {
			st.History = st.History[:n-1]
		}
		return nil
	})
}

// openPulls lists open pull requests on this task's branches, using gh
// when it is installed. Any failure means none are known.
func openPulls(ctx context.Context, dir, task string) []string {
	if _, err := exec.LookPath("gh"); err != nil || dir == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--state", "open", "--limit", "100", "--json", "url,headRefName")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var pulls []struct {
		URL  string `json:"url"`
		Head string `json:"headRefName"`
	}
	if json.Unmarshal(out, &pulls) != nil {
		return nil
	}
	var urls []string
	for _, p := range pulls {
		if strings.HasPrefix(p.Head, "rzm/"+task+"/") {
			urls = append(urls, p.URL)
		}
	}
	return urls
}
