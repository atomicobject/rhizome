package tasks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestInPlaceStopDuringTheFastForwardLeavesNoGitLocks(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.push("other/x.md", "x\n")
	// A git that pauses in the fast-forward's read-tree, so the stop lands
	// while Git holds its locks.
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	started := filepath.Join(bin, "read-tree-started")
	writeFile(t, filepath.Join(bin, "git"), fmt.Sprintf("#!/bin/sh\ncase \" $* \" in *\" read-tree -m -u \"*) : > '%s'; sleep 1 ;; esac\nexec '%s' \"$@\"\n", started, real))
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	f.now = f.now.Add(2 * time.Hour)
	go func() {
		defer close(done)
		_, _ = f.svc.RunDue(ctx)
	}()
	for !fileExists(started) {
		select {
		case <-done:
			t.Fatal("the pass ended before the fast-forward")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	gitDir := run(t, f.host, "rev-parse", "--absolute-git-dir")
	for _, lock := range []string{"index.lock", "HEAD.lock", "refs/heads/main.lock"} {
		if fileExists(filepath.Join(gitDir, lock)) {
			t.Fatalf("a stop during the fast-forward left %s behind", lock)
		}
	}
	if run(t, f.host, "rev-parse", "HEAD") != run(t, f.host, "rev-parse", "origin/main") || f.status() != "" {
		t.Fatalf("the fast-forward must finish whole: %q", f.status())
	}
}

func TestInPlaceSaysWhenAFastForwardStopsPartway(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	if os.Geteuid() == 0 {
		t.Skip("folder permissions do not stop root")
	}
	f.push("README.md", "teammate's readme\n")
	f.push("other/new.md", "new\n")
	// Git updates README.md, then cannot create other/new.md.
	locked := filepath.Join(f.host, "other")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	o := f.pass()
	if o.Outcome != OutcomeSkipped || f.agent.calls() != 0 {
		t.Fatalf("%+v", o)
	}
	for _, want := range []string{"partly updated", "README.md", "git status", "git checkout -- README.md"} {
		if !strings.Contains(o.Reason, want) {
			t.Fatalf("the notice must say the checkout may be partly updated and how to recover (%q): %s", want, o.Reason)
		}
	}
}
