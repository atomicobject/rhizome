package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

func TestSlotOfNamesTheScheduledSlotARunIsFor(t *testing.T) {
	def, err := ParseDefinition(".rhizome/tasks/brief.yml", []byte("name: Brief\nschedule: {at: \"05:00\", timezone: America/Detroit, late: 4h}\nprompt: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{store: Store{Dir: t.TempDir()}}
	zone, _ := time.LoadLocation("America/Detroit")
	if got := svc.slotOf(def, time.Date(2026, 11, 23, 7, 30, 0, 0, zone)); got != "2026-11-23T05:00:00-05:00" {
		t.Fatalf("inside late: %q", got)
	}
	if got := svc.slotOf(def, time.Date(2026, 11, 23, 10, 0, 0, 0, zone)); got != "" {
		t.Fatalf("after late: %q", got)
	}
	every, _ := ParseDefinition(".rhizome/tasks/tick.yml", []byte("name: Tick\nevery: 1h\nprompt: x\n"))
	if got := svc.slotOf(every, time.Now()); got != "" {
		t.Fatalf("every tasks have no slot: %q", got)
	}
}

// After a 05:00 slot ran, a run inside the 06:00 slot's window is for 06:00,
// as taskDue decides, even though the 05:00 window is still open.
func TestSlotOfFollowsTheLastRun(t *testing.T) {
	def, err := ParseDefinition(".rhizome/tasks/brief.yml", []byte("name: Brief\nschedule:\n  - {at: \"05:00\", timezone: America/Detroit, late: 4h}\n  - {at: \"06:00\", timezone: America/Detroit, late: 4h}\nprompt: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	zone, _ := time.LoadLocation("America/Detroit")
	svc := &Service{store: Store{Dir: t.TempDir()}}
	ran := time.Date(2026, 11, 23, 5, 10, 0, 0, zone)
	if err := svc.store.Update(context.Background(), func(st *State) error {
		st.Record(RunRecord{ID: "20261123T101000Z-00000001", Task: def.ID, StartedAt: ran, At: ran.Add(time.Minute), Outcome: OutcomeLanded})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := svc.slotOf(def, time.Date(2026, 11, 23, 6, 10, 0, 0, zone)); got != "2026-11-23T06:00:00-05:00" {
		t.Fatalf("slot after the 05:00 run = %q, want the 06:00 slot", got)
	}
}

// Commits that waited while push was off stay Rhizome's after the run
// history that named them is trimmed.
func TestMadeCommitsOutliveTrimmedHistory(t *testing.T) {
	svc := &Service{store: Store{Dir: t.TempDir()}}
	if err := svc.store.Update(context.Background(), func(st *State) error {
		st.Record(RunRecord{ID: "20261123T101000Z-00000001", Task: "tend", Outcome: OutcomeLanded, Landing: taskdef.LandInPlace, Landed: "c0ffee"})
		st.History, st.Last = nil, nil // trimmed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !svc.madeCommits()["c0ffee"] {
		t.Fatal("a landed commit stopped counting as Rhizome's once its run record was trimmed")
	}
}
