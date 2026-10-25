package tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func TestCheckWithoutAnExplicitNeededFails(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"empty.yml": "name: Empty\nevery: 1d\ncheck:\n  command: [sh, -c, 'echo {}']\nprompt: x\n"})
	agent := &fakeAgent{}
	svc := newService(t, dir, agent, "auto")
	s, _ := svc.RunDue(context.Background())
	if len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeFailed || agent.calls() != 0 {
		t.Fatalf("{} must fail the check, not skip the task: %+v", s)
	}
}

func TestFailedTaskRunsAgainAtItsNextCadence(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"bad.yml": "name: Bad\nevery: 1d\ncheck:\n  command: [sh, -c, 'exit 3']\nprompt: x\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	now := time.Now()
	svc.opts.Now = func() time.Time { return now }
	ctx := context.Background()
	_, _ = svc.RunDue(ctx)
	now = now.Add(25 * time.Hour)
	s, _ := svc.RunDue(ctx)
	if len(s.Outcomes) != 1 {
		t.Fatalf("a failed task should run again after its cadence: %+v", s)
	}
}

func TestRetryKeepsThePreviousAttemptsWorktree(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	fail := true
	agent := &fakeAgent{run: func(AgentRequest) (AgentResult, error) {
		if fail {
			return AgentResult{}, context.DeadlineExceeded
		}
		return AgentResult{Message: "done"}, nil
	}}
	svc := newService(t, dir, agent, "auto")
	ctx := context.Background()
	_, _ = svc.RunDue(ctx)
	failed := taskItems(mustState(t, svc), "look")[0]
	if failed.State != StateAttention || failed.Worktree == "" {
		t.Fatalf("expected a failed item that keeps its worktree, got %+v", failed)
	}
	fail = false
	outcome, err := svc.Run(ctx, failed.ID)
	if err != nil || outcome.Item == failed.ID {
		t.Fatalf("a retry should get its own item: %+v %v", outcome, err)
	}
	old, _ := mustStatePtr(t, svc).Item(failed.ID)
	if old.Worktree != failed.Worktree || old.State != StateAttention {
		t.Fatalf("the failed attempt should keep its location: %+v", old)
	}
}

func TestDiscardKeepsTheLocationWhenRemovalFails(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	svc := newService(t, dir, editingAgent(), "auto")
	ctx := context.Background()
	_, _ = svc.RunDue(ctx)
	ready := taskItems(mustState(t, svc), "look")[0]
	run(t, dir, "worktree", "lock", ready.Worktree)
	if err := svc.Discard(ctx, ready.ID); err == nil {
		t.Fatal("discarding a locked worktree should fail")
	}
	if it, _ := mustStatePtr(t, svc).Item(ready.ID); it.State != StateReady || it.Worktree == "" {
		t.Fatalf("a failed discard must keep the item and its location: %+v", it)
	}
	run(t, dir, "worktree", "unlock", ready.Worktree)
	if err := svc.Discard(ctx, ready.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStartRefusesAnItemThatChangedSinceItsPlan(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	svc := newService(t, dir, &fakeAgent{}, "")
	ctx := context.Background()
	_, _ = svc.RunDue(ctx)
	item := taskItems(mustState(t, svc), "look")[0]
	plan, err := svc.PlanStart(ctx, item.ID, harness.KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Run(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkStarted(ctx, plan); err == nil || !strings.Contains(err.Error(), "is now") {
		t.Fatalf("starting an item that already ran should fail, got %v", err)
	}
	svc.opts.Trusted = func(string) (bool, error) { return false, nil }
	if err := svc.MarkStarted(ctx, plan); err == nil || !strings.Contains(err.Error(), "rzm trust") {
		t.Fatalf("starting in an untrusted repository should fail, got %v", err)
	}
}

func TestLastRunSurvivesHistoryTrimming(t *testing.T) {
	var st State
	at := time.Now()
	st.Record(RunRecord{Task: "weekly", At: at, Outcome: OutcomeNotNeeded})
	for i := 0; i < maxHistory+10; i++ {
		st.Record(RunRecord{Task: "hourly", At: at.Add(time.Duration(i) * time.Hour), Outcome: OutcomeNotNeeded})
	}
	if last, ok := st.LastRun("weekly"); !ok || !last.At.Equal(at) {
		t.Fatalf("a quiet task's last run must survive trimming, got %+v %v", last, ok)
	}
}

func mustStatePtr(t *testing.T, svc *Service) *State {
	t.Helper()
	st := mustState(t, svc)
	return &st
}

func TestRunRerunsAFailedCheckBeforeAnyAgentWork(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"bad.yml": "name: Bad\nevery: 1d\ncheck:\n  command: [sh, -c, 'exit 3']\nprompt: x\n"})
	agent := &fakeAgent{}
	svc := newService(t, dir, agent, "auto")
	ctx := context.Background()
	_, _ = svc.RunDue(ctx)
	item := taskItems(mustState(t, svc), "bad")[0]
	outcome, err := svc.Run(ctx, item.ID)
	if err != nil || outcome.Outcome != OutcomeFailed || agent.calls() != 0 {
		t.Fatalf("a check that still fails must stop the retry before the agent: %+v %v calls=%d", outcome, err, agent.calls())
	}
}
