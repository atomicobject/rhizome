package tasks

import (
	"context"
	"os"
	"testing"
	"time"
)

// A manual Run keeps its item due through the check; a crash there must
// still leave the item needing attention with the reason.
func TestInterruptedCheckOfAManualRunFlagsItsItem(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	svc := newService(t, dir, &fakeAgent{}, "notify")
	var itemID string
	if err := svc.Store().Update(context.Background(), func(st *State) error {
		itemID = st.AddItem(Item{Task: "look", Kind: KindTask, State: StateDue}, time.Now()).ID
		st.Running = &RunRecord{ID: newRunID(time.Now()), Task: "look", StartedAt: time.Now(), Stage: StageCheck, Item: itemID}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.finalizeInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, svc)
	it, err := st.Item(itemID)
	if err != nil {
		t.Fatal(err)
	}
	if it.State != StateAttention || it.Reason == "" {
		t.Fatalf("item = %s %q, want needs attention with the reason", it.State, it.Reason)
	}
}

// When state cannot be read, retention must not sweep recorded folders.
func TestRetainLeavesFoldersAloneWhenStateFails(t *testing.T) {
	dir := newRepo(t, nil)
	svc := newService(t, dir, nil, "notify")
	old := newRunID(time.Now().Add(-8 * 24 * time.Hour))
	writeFile(t, svc.runFile(old, transcriptFile), "{}\n")
	writeFile(t, svc.store.Path(), "{not json")

	svc.retain(context.Background())

	if _, err := os.Stat(svc.runFile(old, transcriptFile)); err != nil {
		t.Fatalf("retention deleted a run folder while state was unreadable: %v", err)
	}
}

// An attempt that leaves Running set without saving its result is recorded,
// not dropped, so its run shows in history and the clock advances.
func TestEndRecordsAnAttemptThatSavedNoResult(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	svc := newService(t, dir, &fakeAgent{}, "notify")
	a := svc.newAttempt(false)
	if err := svc.begin(context.Background(), a, a.record("look", "", time.Now()), nil); err != nil {
		t.Fatal(err)
	}

	svc.end(context.Background(), a)

	st := mustState(t, svc)
	last, ok := st.LastRun("look")
	if st.Running != nil || !ok || last.ID != a.id || last.Outcome != OutcomeFailed {
		t.Fatalf("running=%v last=%+v, want the attempt recorded as failed", st.Running, last)
	}
}

// State saved before run IDs can hold a running item with no Running
// record; the next pass records it so the task waits for its cadence.
func TestPhaseOneRunningItemIsRecordedOnUpgrade(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	agent := &fakeAgent{}
	svc := newService(t, dir, agent, "auto")
	if err := svc.Store().Update(context.Background(), func(st *State) error {
		st.Seen = map[string]time.Time{"look": time.Now()}
		st.AddItem(Item{Task: "look", Kind: KindTask, State: StateRunning}, time.Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, svc)
	last, ok := st.LastRun("look")
	if !ok || last.Outcome != OutcomeInterrupted {
		t.Fatalf("last run = %+v, want the old running item recorded as interrupted", last)
	}
	if agent.calls() != 0 {
		t.Fatalf("the task ran again %d times before its cadence", agent.calls())
	}
}

// A repository whose last task was removed after a crash still records the
// interrupted run once, instead of leaving it running forever.
func TestInterruptedRunIsRecordedAfterTheLastTaskIsRemoved(t *testing.T) {
	dir := newRepo(t, nil)
	svc := newService(t, dir, &fakeAgent{}, "auto")
	runID, _ := crashedRun(t, svc, "gone")

	summary, err := svc.RunDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st := mustState(t, svc)
	last, ok := st.LastRun("gone")
	if summary.Skipped == "" || st.Running != nil || !ok || last.ID != runID || last.Outcome != OutcomeInterrupted {
		t.Fatalf("skipped=%q running=%v last=%+v, want the run recorded as interrupted", summary.Skipped, st.Running, last)
	}
}
