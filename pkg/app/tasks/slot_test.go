package tasks

import (
	"testing"
	"time"
)

func TestSlotOfNamesTheScheduledSlotARunIsFor(t *testing.T) {
	def, err := ParseDefinition(".rhizome/tasks/brief.yml", []byte("name: Brief\nschedule: {at: \"05:00\", timezone: America/Detroit, late: 4h}\nprompt: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{}
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
