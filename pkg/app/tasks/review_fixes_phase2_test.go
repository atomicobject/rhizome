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
