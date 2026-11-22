package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// Each test here reproduces one way an in-place run could change the
// person's own state in their checkout.

func TestInPlaceNeverCommitsAFolderSwapOverAStagedEdit(t *testing.T) {
	f := newLandFixture(t, "name: Tend\nevery: 1h\nwrites: [\"foo\"]\nprompt: Tend.\n")
	f.push("foo/person", "p\n")
	run(t, f.host, "pull", "--quiet", "--ff-only")
	writeFile(t, filepath.Join(f.host, "foo/person"), "the person's staged edit\n")
	run(t, f.host, "add", "foo/person")
	head := run(t, f.host, "rev-parse", "HEAD")
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		if err := os.RemoveAll(filepath.Join(req.Dir, "foo")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(req.Dir, "foo"), "agent\n")
		return AgentResult{Message: "Replaced the folder."}, nil
	}
	o := f.pass()
	if got := run(t, f.host, "rev-parse", "HEAD"); got != head || o.Outcome != OutcomeNoChanges {
		t.Fatalf("a file replacing a folder with the person's staged edit must not be committed: %+v, HEAD %s", o, got)
	}
	if staged := run(t, f.host, "show", ":foo/person"); staged != "the person's staged edit" {
		t.Fatalf("the person's staged edit must stay staged, got %q", staged)
	}
	if r := mustState(t, f.svc).Last["tend"]; !slices.Contains(r.Left, "foo") {
		t.Fatalf("the record must name the left file: %+v", r)
	}
}

func TestInPlaceSkipsAFastForwardOverAnIgnoredFile(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	writeFile(t, filepath.Join(f.host, ".git/info/exclude"), "secret.env\n")
	writeFile(t, filepath.Join(f.host, "secret.env"), "the person's secret\n")
	head := run(t, f.host, "rev-parse", "HEAD")
	f.push("secret.env", "teammate's file\n")
	if o := f.pass(); o.Outcome != OutcomeSkipped || !strings.Contains(o.Reason, "secret.env") {
		t.Fatalf("%+v", o)
	}
	if f.read("secret.env") != "the person's secret\n" || run(t, f.host, "rev-parse", "HEAD") != head || f.agent.calls() != 0 {
		t.Fatal("an ignored file in the way must stop the fast-forward and the run")
	}
}

func TestInPlaceFastForwardNeverAutostashes(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	run(t, f.host, "config", "merge.autoStash", "true")
	run(t, f.host, "config", "rebase.autoStash", "true")
	writeFile(t, filepath.Join(f.host, "README.md"), "the person's edit\n")
	head := run(t, f.host, "rev-parse", "HEAD")
	f.push("README.md", "teammate's edit\n")
	if o := f.pass(); o.Outcome != OutcomeSkipped {
		t.Fatalf("%+v", o)
	}
	if f.read("README.md") != "the person's edit\n" || run(t, f.host, "stash", "list") != "" || run(t, f.host, "rev-parse", "HEAD") != head {
		t.Fatal("an incoming change to a dirty file must skip the run without stashing")
	}
}

func TestInPlaceDoesNotRestoreProtectedFilesAfterABranchSwitch(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		// The person switches to a new branch and commits there.
		writeFile(t, filepath.Join(req.Dir, ".agents/skills/a/SKILL.md"), "the person's tweak\n")
		run(t, req.Dir, "switch", "--quiet", "-c", "feature")
		run(t, req.Dir, "commit", "--quiet", "-am", "tweak the skill")
		return AgentResult{Message: "Done."}, nil
	}
	o := f.pass()
	if f.read(".agents/skills/a/SKILL.md") != "the person's tweak\n" || f.status() != "" {
		t.Fatalf("a restore after a branch switch must touch nothing: %q, %+v", f.status(), o)
	}
	if !strings.Contains(o.Reason, "did not restore") || !strings.Contains(o.Reason, protectedDiffFile) {
		t.Fatalf("the notice must say so and name the saved diff: %+v", o)
	}
}

func TestInPlaceRestoreNeverWritesThroughASymlink(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "a", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		writeFile(t, filepath.Join(req.Dir, ".git/info/exclude"), ".agents/skills\n")
		if err := os.RemoveAll(filepath.Join(req.Dir, ".agents/skills")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(req.Dir, ".agents/skills")); err != nil {
			t.Fatal(err)
		}
		return AgentResult{Message: "Linked."}, nil
	}
	o := f.pass()
	for _, p := range []string{"a/SKILL.md", "a/notes/x.md"} {
		if _, err := os.Lstat(filepath.Join(outside, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a restore wrote %s outside the checkout: %v (%+v)", p, err, o)
		}
	}
	if !strings.Contains(o.Reason, "symlink") {
		t.Fatalf("the notice must name the symlink: %+v", o)
	}
}

func TestInPlaceRestoreLeavesACaseOnlyRename(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	if paths.CasePolicyFor(f.host) != paths.CaseFold {
		t.Skip("needs a case-insensitive file system")
	}
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		run(t, req.Dir, "mv", ".agents/skills/a/SKILL.md", ".agents/skills/a/skill.md")
		return AgentResult{Message: "Renamed."}, nil
	}
	o := f.pass()
	data, err := os.ReadFile(filepath.Join(f.host, ".agents/skills/a/skill.md"))
	if err != nil || string(data) != "agent skill\n" {
		t.Fatalf("the skill file must survive a case-only rename: %v %q (%+v)", err, data, o)
	}
	if !strings.Contains(o.Reason, "letter case") {
		t.Fatalf("the notice must name the rename: %+v", o)
	}
}

func TestInPlaceLeavesAssumeUnchangedEdits(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	run(t, f.host, "update-index", "--assume-unchanged", "notes/log.md")
	writeFile(t, filepath.Join(f.host, "notes/log.md"), "the person's private edit\n")
	f.edits(map[string]string{"notes/log.md": "the person's private edit\nand the agent's\n", "notes/new.md": "new\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{"notes/new.md"}) {
		t.Fatalf("an assume-unchanged file is the person's and is never committed, got %v", got)
	}
	if r := mustState(t, f.svc).Last["tend"]; !slices.Equal(r.Left, []string{"notes/log.md"}) {
		t.Fatalf("the record must name it: %+v", r)
	}
}

func TestInPlaceDoesNotAdoptALeftoverThePersonChanged(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.edits(map[string]string{"notes/a.md": "from the failed run\n"}, errors.New("harness crashed"))
	if o := f.pass(); o.Outcome != OutcomeFailed {
		t.Fatalf("%+v", o)
	}
	writeFile(t, filepath.Join(f.host, "notes/a.md"), "the person's rewrite\n")
	f.edits(map[string]string{"notes/b.md": "b\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{"notes/b.md"}) {
		t.Fatalf("a leftover the person changed is theirs, got %v", got)
	}
	if f.read("notes/a.md") != "the person's rewrite\n" || f.status() != "?? notes/a.md" {
		t.Fatalf("the person's file must stay as they left it: %q", f.status())
	}
}

func TestInPlacePushChecksThePushDestination(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	mirror := filepath.Join(filepath.Dir(f.remote), "mirror.git")
	run(t, filepath.Dir(f.remote), "clone", "--quiet", "--bare", f.remote, mirror)
	before := run(t, mirror, "rev-parse", "main")
	f.push("other/teammate.md", "untagged work the mirror lacks\n")
	run(t, f.host, "config", "remote.origin.pushurl", mirror)
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	o := f.pass()
	if o.Outcome != OutcomeLanded || !strings.Contains(o.Reason, "not Rhizome's task commits") {
		t.Fatalf("%+v", o)
	}
	if got := run(t, mirror, "rev-parse", "main"); got != before {
		t.Fatalf("the push destination must not receive untagged commits, it moved to %s", got)
	}
}

func TestInPlaceSkipsASlotLandedDuringItsFetch(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	slot := "2026-11-23T07:00:00-05:00"
	f.svc.slotFor = func(Definition, time.Time) string { return slot }
	// The check runs after the pass's own slot check, as another machine
	// lands the slot in the meantime.
	f.push(".rhizome/tasks/tend.yml", fmt.Sprintf("name: Tend notes\nevery: 1h\nwrites: [\"notes/**\"]\ncheck:\n  command: [\"sh\", \"-c\", \"git -C %s push --quiet origin HEAD:main 2>/dev/null; echo '{\\\"needed\\\": true}'\"]\nprompt: Tend.\n", f.other))
	run(t, f.other, "commit", "--quiet", "--allow-empty", "-m", "chore(tasks): Tend notes\n\nRhizome-Task: tend\nRhizome-Task-Slot: "+slot)
	run(t, f.host, "fetch", "--quiet", "origin")
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeSkipped || !strings.Contains(o.Reason, "already landed") || f.agent.calls() != 0 {
		t.Fatalf("a slot that landed during the fetch must not run again: %+v", o)
	}
}

func TestInPlaceRestoreAppliesCheckoutFilters(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	writeFile(t, filepath.Join(f.host, ".git/info/attributes"), ".agents/** text eol=crlf\n")
	const skill = ".agents/skills/a/SKILL.md"
	if err := os.Remove(filepath.Join(f.host, skill)); err != nil {
		t.Fatal(err)
	}
	run(t, f.host, "checkout", "--", skill)
	if f.read(skill) != "agent skill\r\n" {
		t.Fatalf("the checkout must hold CRLF line endings, got %q", f.read(skill))
	}
	f.edits(map[string]string{skill: "injected\n"}, nil)
	o := f.pass()
	if got := f.read(skill); got != "agent skill\r\n" {
		t.Fatalf("a restore must write what a checkout would, got %q (%+v)", got, o)
	}
	if f.status() != "" {
		t.Fatalf("the restored file must match the index: %q", f.status())
	}
}

func TestInPlaceNeverCommitsANestedRepository(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		for _, sub := range []string{"notes/sub", ".agents/sub"} {
			dir := filepath.Join(req.Dir, sub)
			writeFile(t, filepath.Join(dir, "x.md"), "x\n")
			run(t, dir, "init", "--quiet")
			run(t, dir, "add", "-A")
			run(t, dir, "commit", "--quiet", "-m", "nested")
		}
		writeFile(t, filepath.Join(req.Dir, "notes/a.md"), "a\n")
		return AgentResult{Message: "Nested."}, nil
	}
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{"notes/a.md"}) {
		t.Fatalf("a nested repository must not be committed, got %v", got)
	}
	if _, err := os.Stat(filepath.Join(f.host, ".agents/sub/.git")); err != nil {
		t.Fatalf("a nested repository must be left, not removed: %v", err)
	}
	it := taskItems(mustState(t, f.svc), "tend")[0]
	for _, want := range []string{"notes/sub (a nested Git repository)", ".agents/sub (a nested Git repository)"} {
		if !strings.Contains(it.Reason, want) {
			t.Fatalf("summary %q must name %q", it.Reason, want)
		}
	}
}

func TestInPlaceRestoreNeverRemovesAFolderHoldingWhatItLeaves(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	const skill = ".agents/skills/a/SKILL.md"
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		if err := os.Remove(filepath.Join(req.Dir, skill)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(req.Dir, skill, "x.md"), "the run's\n")
		sub := filepath.Join(req.Dir, skill, "sub")
		writeFile(t, filepath.Join(sub, "y.md"), "y\n")
		run(t, sub, "init", "--quiet")
		run(t, sub, "add", "-A")
		run(t, sub, "commit", "--quiet", "-m", "nested")
		return AgentResult{Message: "Nested a repository where a skill was."}, nil
	}
	o := f.pass()
	if _, err := os.Stat(filepath.Join(f.host, skill, "sub/.git")); err != nil {
		t.Fatalf("a restore must not remove a folder holding a nested repository: %v (%+v)", err, o)
	}
	if _, err := os.Stat(filepath.Join(f.host, skill, "x.md")); !os.IsNotExist(err) {
		t.Fatalf("the run's own protected file is still removed: %v", err)
	}
	if it := taskItems(mustState(t, f.svc), "tend")[0]; !strings.Contains(it.Reason, "left "+skill+" (") {
		t.Fatalf("the notice must name the folder it left: %q", it.Reason)
	}
}

func TestInPlacePushRecoveryChecksThePushDestination(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	mirror := filepath.Join(filepath.Dir(f.remote), "mirror.git")
	run(t, filepath.Dir(f.remote), "clone", "--quiet", "--bare", f.remote, mirror)
	// The push destination has a commit this checkout lacks, so the push fails.
	commitFile(t, f.other, "other/m.md", "only on the push destination\n", "mirror only")
	run(t, f.other, "push", "--quiet", mirror, "HEAD:main")
	run(t, f.host, "config", "remote.origin.pushurl", mirror)
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	f.onStep("push", func() {
		// The fetch URL gets the task's commit some other way.
		run(t, f.host, "push", "--quiet", f.remote, "HEAD:main")
	})
	o := f.pass()
	landed := mustState(t, f.svc).Last["tend"].Landed
	if isAncestor(context.Background(), mirror, landed, "main") {
		t.Fatal("the push destination must not get the commit")
	}
	if o.Outcome != OutcomeLanded || !strings.Contains(o.Reason, "push destination") || len(f.attention()) != 1 {
		t.Fatalf("a commit only the fetch URL has is not pushed: %+v", o)
	}
}
