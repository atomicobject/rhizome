package tasks

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func TestPlanStartBuildsTheHarnessCommandWithoutChangingAnything(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	svc := newService(t, dir, &fakeAgent{}, "")
	ctx := context.Background()
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	item := taskItems(mustState(t, svc), "look")[0]
	before := mustState(t, svc)

	// Today's branch name is taken, so the plan takes the next one.
	base := "rzm/look/" + svc.opts.Now().Format("2006-01-02")
	run(t, dir, "branch", base)

	plan, err := svc.PlanStart(ctx, item.ID, harness.KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	commit := run(t, dir, "rev-parse", "main")
	if plan.Branch != base+"-2" {
		t.Fatalf("expected a suffixed branch, got %q", plan.Branch)
	}
	if !strings.Contains(plan.Prompt, "git switch -c "+plan.Branch+" "+commit) || !strings.Contains(plan.Prompt, "Rhizome-Task: look") || !strings.Contains(plan.Prompt, "Look around.") {
		t.Fatalf("prompt is missing the branch step, tag, or instructions:\n%s", plan.Prompt)
	}
	if len(plan.Argv) != 4 || plan.Argv[0] != "claude" || plan.Argv[1] != "--worktree" || plan.Argv[3] != plan.Prompt {
		t.Fatalf("unexpected claude argv %q", plan.Argv)
	}
	if codex, _ := svc.PlanStart(ctx, item.ID, harness.KindCodex); len(codex.Argv) != 3 || codex.Argv[0] != "codex" || codex.Argv[1] != "--worktree" {
		t.Fatalf("unexpected codex argv %q", codex.Argv)
	}
	after := mustState(t, svc)
	if len(after.Items) != len(before.Items) || after.Items[len(after.Items)-1].State != StateDue || len(after.History) != len(before.History) {
		t.Fatal("planning a start must not change state")
	}

	if err := svc.MarkStarted(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if it := taskItems(mustState(t, svc), "look")[0]; it.State != StateStarted || it.Branch != plan.Branch {
		t.Fatalf("expected a started item, got %+v", it)
	}
	if err := svc.UndoStart(ctx, plan, "claude did not trust the folder"); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, svc)
	if it := taskItems(st, "look")[0]; it.State != StateDue || it.Reason == "" {
		t.Fatalf("undo should restore the due item with a reason, got %+v", it)
	}
	if last, _ := st.LastRun("look"); last.Outcome == OutcomeStarted {
		t.Fatal("undo should drop the started run")
	}
}
