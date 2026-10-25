package tasks

import (
	"context"
	"os"
	"path/filepath"
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

func TestUnneededRetryKeepsTheFailedAttemptReviewable(t *testing.T) {
	shellAvailable(t)
	check := `name: Flaky
every: 1d
check:
  command: [sh, -c, 'if [ -f "$RZM_TASK_DATA_DIR/stop" ]; then echo "{\"needed\": false}"; else echo "{\"needed\": true}"; fi']
prompt: x
`
	dir := newRepo(t, map[string]string{"flaky.yml": check})
	agent := &fakeAgent{run: func(AgentRequest) (AgentResult, error) {
		return AgentResult{Tokens: 77}, context.DeadlineExceeded
	}}
	svc := newService(t, dir, agent, "auto")
	ctx := context.Background()
	_, _ = svc.RunDue(ctx)
	failed := taskItems(mustState(t, svc), "flaky")[0]
	if failed.State != StateAttention || failed.Worktree == "" || failed.Tokens != 77 {
		t.Fatalf("expected a failed attempt with its worktree and tokens, got %+v", failed)
	}
	if last, _ := mustStatePtr(t, svc).LastRun("flaky"); last.Tokens != 77 {
		t.Fatalf("a failed run's tokens should be recorded, got %+v", last)
	}
	writeFile(t, svc.stateDir+"/data/flaky/stop", "")
	if _, err := svc.Run(ctx, failed.ID); err != nil {
		t.Fatal(err)
	}
	it, _ := mustStatePtr(t, svc).Item(failed.ID)
	if it.State != StateAttention || it.Worktree != failed.Worktree {
		t.Fatalf("the failed attempt must stay reviewable and discardable: %+v", it)
	}
	if err := svc.Discard(ctx, failed.ID); err != nil {
		t.Fatalf("discard should still work: %v", err)
	}
}

func TestRepeatedCheckFailuresReuseOneNotice(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"bad.yml": "name: Bad\nevery: 1d\ncheck:\n  command: [sh, -c, 'exit 3']\nprompt: x\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	now := time.Now()
	svc.opts.Now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, _ = svc.RunDue(ctx)
		now = now.Add(25 * time.Hour)
	}
	if items := taskItems(mustState(t, svc), "bad"); len(items) != 1 {
		t.Fatalf("repeated check failures should share one notice, got %d", len(items))
	}
}

func TestScriptCheckFailureKeepsTheScriptsError(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"scripted.yml": "name: Scripted\nevery: 1d\ncheck:\n  script: check.js\nprompt: x\n"})
	commitFile(t, dir, "check.js", "throw new Error('boom')\n", "add script")
	fakeRzm := filepath.Join(t.TempDir(), "rzm")
	writeFile(t, fakeRzm, "#!/bin/sh\ncat >/dev/null\necho '{\"ok\": false, \"error\": {\"message\": \"boom from the script\"}}'\nexit 1\n")
	if err := os.Chmod(fakeRzm, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := newService(t, dir, &fakeAgent{}, "auto")
	svc.opts.Rzm = fakeRzm
	s, _ := svc.RunDue(context.Background())
	if len(s.Outcomes) != 1 || !strings.Contains(s.Outcomes[0].Reason, "boom from the script") {
		t.Fatalf("the script's own error should reach the item, got %+v", s)
	}
}

func TestAgentCommitsThatCancelOutAreKept(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		for _, content := range []string{"draft\n", "hello\n"} {
			if err := os.WriteFile(filepath.Join(req.Dir, "README.md"), []byte(content), 0o644); err != nil {
				return AgentResult{}, err
			}
			if _, err := gitOutput(context.Background(), req.Dir, "commit", "--quiet", "-am", "agent step"); err != nil {
				return AgentResult{}, err
			}
		}
		return AgentResult{Message: "tried a change and reverted it"}, nil
	}}
	svc := newService(t, dir, agent, "auto")
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	it := taskItems(mustState(t, svc), "look")[0]
	if it.State != StateReady || it.Branch == "" {
		t.Fatalf("an agent's own commits must be kept even when they cancel out, got %+v", it)
	}
	if count := run(t, dir, "rev-list", "--count", "main.."+it.Branch); count != "3" {
		t.Fatalf("expected the agent's two commits plus the tagged one, got %s", count)
	}
}
