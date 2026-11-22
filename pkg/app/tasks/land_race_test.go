package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// onStep acts as the person at one step of an in-place landing.
func (f *landFixture) onStep(step string, act func()) {
	f.t.Cleanup(func() { landingHook = nil })
	landingHook = func(s string) {
		if s == step {
			act()
		}
	}
}

func TestInPlaceLandsNothingOnABranchSwitchedMidway(t *testing.T) {
	for _, step := range []string{"fetch", "fast-forward", "commit"} {
		t.Run(step, func(t *testing.T) {
			f := newLandFixture(t, inPlaceTask)
			f.push("other/x.md", "x\n")
			f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
			var feature string
			f.onStep(step, func() {
				run(t, f.host, "switch", "--quiet", "-c", "feature")
				feature = run(t, f.host, "rev-parse", "HEAD")
			})
			o := f.pass()
			if o.Outcome == OutcomeLanded {
				t.Fatalf("%+v", o)
			}
			if got := run(t, f.host, "rev-parse", "feature"); got != feature {
				t.Fatalf("the branch the person switched to must not move: %s, was %s", got, feature)
			}
			if log := run(t, f.host, "log", "--all", "--format=%B"); strings.Contains(log, "Rhizome-Task: tend") {
				t.Fatalf("no branch may get the task's commit:\n%s", log)
			}
		})
	}
}

func TestInPlacePushRecoveryLeavesThePersonsWork(t *testing.T) {
	t.Run("a commit of theirs on top", func(t *testing.T) {
		f := newLandFixture(t, inPlaceTask)
		f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
		var mine string
		f.onStep("push", func() {
			f.push("other/race.md", "race\n")
			commitFile(t, f.host, "README.md", "mine\n", "the person's commit")
			mine = run(t, f.host, "rev-parse", "HEAD")
		})
		o := f.pass()
		if o.Outcome != OutcomeLanded || !strings.Contains(o.Reason, "did not rebase") {
			t.Fatalf("%+v", o)
		}
		if run(t, f.host, "rev-parse", "HEAD") != mine || !hasLine(run(t, f.host, "log", "-1", "--format=%B", "HEAD^"), "Rhizome-Task: tend") {
			t.Fatal("the person's commit and the task commit under it must stay as they are")
		}
		if isAncestor(context.Background(), f.host, mine+"^", "origin/main") {
			t.Fatal("nothing may be pushed")
		}
	})
	t.Run("a bisect under way", func(t *testing.T) {
		f := newLandFixture(t, inPlaceTask)
		f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
		f.onStep("push", func() {
			f.push("other/race.md", "race\n")
			run(t, f.host, "bisect", "start")
		})
		o := f.pass()
		if o.Outcome != OutcomeLanded || !strings.Contains(o.Reason, "a bisect is in progress") {
			t.Fatalf("%+v", o)
		}
		gitDir := run(t, f.host, "rev-parse", "--absolute-git-dir")
		if _, err := os.Stat(filepath.Join(gitDir, "BISECT_LOG")); err != nil || !hasLine(run(t, f.host, "log", "-1", "--format=%B"), "Rhizome-Task: tend") {
			t.Fatalf("the bisect and the local task commit must stay: %v", err)
		}
	})
}

func TestInPlaceKeepsAFileStagedWhileItCommits(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	head := run(t, f.host, "rev-parse", "HEAD")
	f.onStep("commit", func() {
		writeFile(t, filepath.Join(f.host, "notes/a.md"), "the person's newer version\n")
		run(t, f.host, "add", "notes/a.md")
	})
	o := f.pass()
	if staged := run(t, f.host, "show", ":notes/a.md"); staged != "the person's newer version" {
		t.Fatalf("the person's staged version must stay staged, got %q (%+v)", staged, o)
	}
	if got := run(t, f.host, "rev-parse", "HEAD"); got != head || o.Outcome != OutcomeFailed || !strings.Contains(o.Reason, "notes/a.md was staged") {
		t.Fatalf("nothing may be committed over a file staged meanwhile: HEAD %s, %+v", got, o)
	}
	gitDir := run(t, f.host, "rev-parse", "--absolute-git-dir")
	if _, err := os.Stat(filepath.Join(gitDir, "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("the index lock must be released: %v", err)
	}
}
