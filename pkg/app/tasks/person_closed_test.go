package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// closingAgent marks the running item done mid-run, as a person can from
// the inbox, then reports result.
func closingAgent(t *testing.T, svc **Service, result func(req AgentRequest) (AgentResult, error)) *fakeAgent {
	return &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		it := taskItems(mustState(t, *svc), "look")[0]
		if err := (*svc).Done(context.Background(), it.ID); err != nil {
			t.Errorf("done during the run: %v", err)
		}
		return result(req)
	}}
}

func TestDoneDuringARunStaysDone(t *testing.T) {
	cases := map[string]struct {
		result  func(req AgentRequest) (AgentResult, error)
		outcome string
	}{
		"ready":  {editingAgent().run, OutcomeReady},
		"failed": {func(AgentRequest) (AgentResult, error) { return AgentResult{}, errors.New("boom") }, OutcomeFailed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
			var svc *Service
			svc = newService(t, dir, closingAgent(t, &svc, tc.result), "auto")
			if _, err := svc.RunDue(context.Background()); err != nil {
				t.Fatal(err)
			}
			st := mustState(t, svc)
			items := taskItems(st, "look")
			if len(items) != 1 || items[0].State != StateDone {
				t.Fatalf("the person's done must survive the run's end: %+v", items)
			}
			if last, _ := st.LastRun("look"); last.Outcome != tc.outcome || st.Running != nil {
				t.Fatalf("the run must still be recorded as %s: %+v running=%v", tc.outcome, last, st.Running)
			}
		})
	}
}

func TestUndoStartRefusesAnItemNoLongerStarted(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": "name: Look\nevery: 1d\nprompt: Look around.\n"})
	svc := newService(t, dir, &fakeAgent{}, "")
	ctx := context.Background()
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	item := taskItems(mustState(t, svc), "look")[0]
	plan, err := svc.PlanStart(ctx, item.ID, harness.KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkStarted(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err := svc.Done(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UndoStart(ctx, plan, "late"); err == nil {
		t.Fatal("undo must refuse an item that is no longer started")
	}
	if it := taskItems(mustState(t, svc), "look")[0]; it.State != StateDone {
		t.Fatalf("undo must not reopen a closed item, got %+v", it)
	}
}
