package tasks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// answerTask's check prints whatever the test wrote to its data folder.
const answerTask = "name: Tidy\nevery: 1d\ncheck:\n  command: [sh, -c, 'cat \"$RZM_TASK_DATA_DIR/answer\"']\nprompt: Tidy the notes.\n"

func answer(t *testing.T, svc *Service, json string) {
	t.Helper()
	writeFile(t, filepath.Join(svc.stateDir, "data", "tidy", "answer"), json)
}

func clockedService(t *testing.T, agent AgentRunner) (*Service, *time.Time) {
	t.Helper()
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"tidy.yml": answerTask})
	svc := newService(t, dir, agent, "")
	now := time.Now()
	svc.opts.Now = func() time.Time { return now }
	return svc, &now
}

func TestDueItemRechecksAtItsCadence(t *testing.T) {
	agent := &fakeAgent{}
	svc, now := clockedService(t, agent)
	ctx := context.Background()
	answer(t, svc, `{"needed": true, "summary": "3 notes need tidying", "context": "notes/a.md"}`)
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	item := taskItems(mustState(t, svc), "tidy")[0]
	snooze := now.Add(72 * time.Hour)
	if err := svc.Snooze(ctx, item.ID, snooze); err != nil {
		t.Fatal(err)
	}
	// Auto mode now, so a re-check that started work would show.
	if err := svc.SetMode(ctx, "tidy", "auto"); err != nil {
		t.Fatal(err)
	}

	answer(t, svc, `{"needed": true, "summary": "1 note needs tidying", "context": "notes/b.md"}`)
	if s, _ := svc.RunDue(ctx); len(s.Outcomes) != 0 {
		t.Fatalf("a due item must not re-check before its cadence: %+v", s)
	}
	*now = now.Add(25 * time.Hour)
	s, err := svc.RunDue(ctx)
	if err != nil || len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeNotified || s.Outcomes[0].Item != item.ID {
		t.Fatalf("expected the item refreshed in place: %+v %v", s, err)
	}
	items := taskItems(mustState(t, svc), "tidy")
	if len(items) != 1 || items[0].ID != item.ID || items[0].State != StateDue {
		t.Fatalf("a re-check must keep one due item, got %+v", items)
	}
	if it := items[0]; it.Summary != "1 note needs tidying" || it.Context != "notes/b.md" || !it.Snoozed.Equal(snooze) {
		t.Fatalf("expected a fresh summary and context with the snooze kept, got %+v", it)
	}
	if agent.calls() != 0 {
		t.Fatal("a re-check must never start work")
	}

	answer(t, svc, `{"needed": false}`)
	*now = now.Add(25 * time.Hour)
	s, _ = svc.RunDue(ctx)
	if len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeNotNeeded {
		t.Fatalf("expected a not-needed re-check: %+v", s)
	}
	st := mustState(t, svc)
	if items := taskItems(st, "tidy"); len(items) != 1 || items[0].State != StateDone {
		t.Fatalf("nothing needed should close the item as done, got %+v", items)
	}
	if last, _ := st.LastRun("tidy"); last.Outcome != OutcomeNotNeeded || last.Item != item.ID {
		t.Fatalf("expected a not-needed run record for the item, got %+v", last)
	}
}

func TestRecheckLeavesWorkInProgressAlone(t *testing.T) {
	for _, state := range []ItemState{StateReady, StateStarted} {
		t.Run(string(state), func(t *testing.T) {
			svc, now := clockedService(t, &fakeAgent{})
			ctx := context.Background()
			answer(t, svc, `{"needed": true, "summary": "old"}`)
			_, _ = svc.RunDue(ctx)
			item := taskItems(mustState(t, svc), "tidy")[0]
			_ = svc.store.Update(ctx, func(st *State) error {
				it, _ := st.Item(item.ID)
				it.State = state
				return nil
			})
			answer(t, svc, `{"needed": false}`)
			*now = now.Add(25 * time.Hour)
			if s, _ := svc.RunDue(ctx); len(s.Outcomes) != 0 {
				t.Fatalf("a %s item must keep its task from checking: %+v", state, s)
			}
			if items := taskItems(mustState(t, svc), "tidy"); len(items) != 1 || items[0].State != state || items[0].Summary != "old" {
				t.Fatalf("expected the item untouched, got %+v", items)
			}
		})
	}
}

func TestFailedRecheckNeedsAttentionWithoutASecondItem(t *testing.T) {
	svc, now := clockedService(t, &fakeAgent{})
	ctx := context.Background()
	answer(t, svc, `{"needed": true, "summary": "3 notes need tidying"}`)
	_, _ = svc.RunDue(ctx)
	answer(t, svc, `not json`)
	*now = now.Add(25 * time.Hour)
	if s, _ := svc.RunDue(ctx); len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeFailed {
		t.Fatalf("expected a failed re-check: %+v", s)
	}
	items := taskItems(mustState(t, svc), "tidy")
	if len(items) != 1 || items[0].State != StateAttention || !strings.Contains(items[0].Reason, "check failed") {
		t.Fatalf("expected the one item to need attention, got %+v", items)
	}
}

func TestCopyPromptHasNoBranchStep(t *testing.T) {
	svc, _ := clockedService(t, &fakeAgent{})
	ctx := context.Background()
	answer(t, svc, `not json`)
	_, _ = svc.RunDue(ctx)
	item := taskItems(mustState(t, svc), "tidy")[0]
	if item.State != StateAttention {
		t.Fatalf("expected a needs-attention item, got %+v", item)
	}
	_ = svc.store.Update(ctx, func(st *State) error {
		it, _ := st.Item(item.ID)
		it.Summary = "3 notes need tidying"
		return nil
	})
	before := mustState(t, svc)
	prompt, err := svc.CopyPrompt(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Tidy the notes.", "3 notes need tidying", "Rhizome-Task: tidy", svc.opts.Repo.MainCheckout, filepath.Join(svc.stateDir, "data", "tidy")} {
		if !strings.Contains(prompt, want) {
			t.Errorf("copied prompt is missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "git switch") {
		t.Fatalf("a copied prompt must not move a branch:\n%s", prompt)
	}
	if after := mustState(t, svc); len(after.Items) != len(before.Items) || len(after.History) != len(before.History) {
		t.Fatal("copying a prompt must not change state")
	}

	if err := svc.Done(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CopyPrompt(ctx, item.ID); err == nil {
		t.Fatal("a closed item has no prompt to copy")
	}
}
