package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const checkingTask = `name: Tidy
description: Tidies notes.
every: 1d
check:
  command: [sh, check.sh]
prompt: Tidy the notes.
`

const checkScript = `printf stored > "$RZM_TASK_DATA_DIR/from-check"
printf '{"needed": true, "summary": "3 notes need tidying", "context": "notes/a.md"}'
`

func editingAgent() *fakeAgent {
	return &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		if err := os.MkdirAll(filepath.Join(req.Dir, "notes"), 0o755); err != nil {
			return AgentResult{}, err
		}
		if err := os.WriteFile(filepath.Join(req.Dir, "notes", "a.md"), []byte("tidy\n"), 0o644); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Message: "Tidied notes/a.md\n\nReview the wording.", Tokens: 1200}, nil
	}}
}

func TestAutoRunCommitsWorkOnATaggedLocalBranch(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"tidy.yml": checkingTask})
	commitFile(t, dir, "check.sh", checkScript, "add check")
	agent := editingAgent()
	svc := newService(t, dir, agent, "auto")

	summary, err := svc.RunDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Outcomes) != 1 || summary.Outcomes[0].Outcome != OutcomeReady {
		t.Fatalf("expected one ready outcome, got %+v", summary)
	}
	prompt := agent.prompts[0]
	for _, want := range []string{"3 notes need tidying", "notes/a.md", "Tidy the notes.", filepath.Join(svc.stateDir, "data", "tidy")} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	data, err := os.ReadFile(filepath.Join(svc.stateDir, "data", "tidy", "from-check"))
	if err != nil || string(data) != "stored" {
		t.Fatalf("the check's stored data should be in the task's data folder: %q %v", data, err)
	}
	st := mustState(t, svc)
	items := taskItems(st, "tidy")
	if len(items) != 1 || items[0].State != StateReady {
		t.Fatalf("expected one ready item, got %+v", items)
	}
	it := items[0]
	if it.Tokens != 1200 || len(it.Changed) != 1 || it.Changed[0] != "notes/a.md" || !strings.HasPrefix(it.Branch, "rzm/tidy/") {
		t.Fatalf("unexpected ready item %+v", it)
	}
	message := run(t, dir, "log", "-1", "--format=%B", it.Branch)
	if !hasLine(message, "Rhizome-Task: tidy") || !strings.Contains(message, "Tidied notes/a.md") {
		t.Fatalf("commit message lacks the tag or the work's message:\n%s", message)
	}
	if run(t, dir, "rev-parse", "main") == run(t, dir, "rev-parse", it.Branch) {
		t.Fatal("the work must stay on its own branch")
	}
	if remotes := run(t, dir, "remote"); remotes != "" {
		t.Fatalf("nothing should be pushed anywhere: %q", remotes)
	}
	// Announced once, and not due again while its item is open.
	if again, _ := svc.RunDue(context.Background()); len(again.Outcomes) != 0 {
		t.Fatalf("an open item should keep the task from running again: %+v", again)
	}
	announcements := 0
	for _, it := range mustState(t, svc).Items {
		if it.Kind == KindAnnouncement {
			announcements++
		}
	}
	if announcements != 1 {
		t.Fatalf("expected one announcement, got %d", announcements)
	}
}

func TestRunWithoutChangesFinishesAndCleansUp(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := taskItems(mustState(t, svc), "look")
	if len(items) != 1 || items[0].State != StateDone || items[0].Message != "nothing to do" {
		t.Fatalf("expected a done item that keeps the message, got %+v", items)
	}
	if branches := run(t, dir, "branch", "--list", "rzm/*"); branches != "" {
		t.Fatalf("the empty branch should be removed: %q", branches)
	}
	if entries, _ := os.ReadDir(filepath.Join(svc.stateDir, "worktrees")); len(entries) != 0 {
		t.Fatalf("the empty worktree should be removed: %v", entries)
	}
}

func TestNotifyModeSpendsNothingUntilRun(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	agent := editingAgent()
	svc := newService(t, dir, agent, "")
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := taskItems(mustState(t, svc), "look")
	if len(items) != 1 || items[0].State != StateDue || agent.calls() != 0 {
		t.Fatalf("expected a due item and no agent call, got %+v calls=%d", items, agent.calls())
	}
	outcome, err := svc.Run(context.Background(), items[0].ID)
	if err != nil || outcome.Outcome != OutcomeReady || agent.calls() != 1 {
		t.Fatalf("Run should do the work now: %+v %v", outcome, err)
	}
}

func TestModeOverridesAndPause(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	agent := &fakeAgent{}
	svc := newService(t, dir, agent, "auto")
	ctx := context.Background()
	if err := svc.SetMode(ctx, "look", "off"); err != nil {
		t.Fatal(err)
	}
	if s, _ := svc.RunDue(ctx); len(s.Outcomes) != 0 {
		t.Fatalf("an off task must not run: %+v", s)
	}
	_ = svc.SetMode(ctx, "look", "default")
	_ = svc.SetPaused(ctx, true)
	if s, _ := svc.RunDue(ctx); s.Skipped != "paused" || agent.calls() != 0 {
		t.Fatalf("a paused person runs nothing: %+v", s)
	}
	_ = svc.SetPaused(ctx, false)
	if s, _ := svc.RunDue(ctx); len(s.Outcomes) != 1 {
		t.Fatalf("resumed tasks run: %+v", s)
	}
}

func TestFailedCheckNeedsAttentionAndDoesNotRetry(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"bad.yml": "name: Bad\nevery: 1d\ncheck:\n  command: [sh, -c, 'echo oops >&2; exit 3']\nprompt: x\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	s, _ := svc.RunDue(context.Background())
	if len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeFailed {
		t.Fatalf("expected a failed outcome, got %+v", s)
	}
	items := taskItems(mustState(t, svc), "bad")
	if len(items) != 1 || items[0].State != StateAttention || !strings.Contains(items[0].Reason, "check failed") {
		t.Fatalf("expected a needs-attention item, got %+v", items)
	}
	if s, _ := svc.RunDue(context.Background()); len(s.Outcomes) != 0 {
		t.Fatalf("a failed task must not retry every tick: %+v", s)
	}
}

func TestConcurrentPassesRunATaskOnce(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	release := make(chan struct{})
	agent := &fakeAgent{run: func(AgentRequest) (AgentResult, error) {
		<-release
		return AgentResult{Message: "done"}, nil
	}}
	svc := newService(t, dir, agent, "auto")
	other := newService(t, dir, agent, "auto")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = svc.RunDue(context.Background()) }()
	for agent.calls() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	s, err := other.RunDue(context.Background())
	if err != nil || s.Skipped == "" {
		t.Fatalf("a second pass should skip while the first runs: %+v %v", s, err)
	}
	if _, err := other.Run(context.Background(), "1"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Run should refuse while a pass holds the lock, got %v", err)
	}
	close(release)
	wg.Wait()
	if agent.calls() != 1 {
		t.Fatalf("expected one run, got %d", agent.calls())
	}
}

func TestTaggedMergeClosesTheItem(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nscope: team\nprompt: Look around.\n"})
	svc := newService(t, dir, editingAgent(), "")
	ctx := context.Background()
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // committer dates have one-second precision
	commitFile(t, dir, "x.md", "x\n", "teammate did it\n\nRhizome-Task: look")
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	items := taskItems(mustState(t, svc), "look")
	if len(items) != 1 || items[0].State != StateDone || !strings.Contains(items[0].Reason, "Tester") {
		t.Fatalf("a teammate's tagged merge should close the item: %+v", items)
	}
}

func TestInterruptedRunNeedsAttention(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	ctx := context.Background()
	// A crashed pass leaves its item running.
	if err := svc.Store().Update(ctx, func(st *State) error {
		st.AddItem(Item{Task: "look", Kind: KindTask, State: StateRunning}, time.Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	items := taskItems(mustState(t, svc), "look")
	if len(items) == 0 || items[0].State != StateAttention || !strings.Contains(items[0].Reason, "interrupted") {
		t.Fatalf("an interrupted run should need attention, got %+v", items)
	}
}

func TestUntrustedRepositoryRunsNothing(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	agent := &fakeAgent{}
	svc := newService(t, dir, agent, "auto")
	svc.opts.Trusted = func(string) (bool, error) { return false, nil }
	s, err := svc.RunDue(context.Background())
	if err != nil || !strings.Contains(s.Skipped, "rzm trust") || agent.calls() != 0 {
		t.Fatalf("expected an untrusted skip, got %+v %v", s, err)
	}
}
