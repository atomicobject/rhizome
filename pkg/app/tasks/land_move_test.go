package tasks

import (
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover how an in-place run moves the person's checkout to the
// primary ref.

func TestInPlaceChecksIncomingFilesUnderTheLock(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	writeFile(t, filepath.Join(f.host, ".git/info/exclude"), "secret.env\n")
	head := run(t, f.host, "rev-parse", "HEAD")
	f.push("secret.env", "teammate's file\n")
	// The person saves an ignored file just before the move.
	f.onStep("fast-forward", func() {
		writeFile(t, filepath.Join(f.host, "secret.env"), "the person's secret\n")
	})
	o := f.pass()
	if f.read("secret.env") != "the person's secret\n" || run(t, f.host, "rev-parse", "HEAD") != head {
		t.Fatalf("the fast-forward must not replace an ignored file saved before it took the lock: %+v", o)
	}
	if o.Outcome != OutcomeSkipped || !strings.Contains(o.Reason, "secret.env") {
		t.Fatalf("%+v", o)
	}
}
