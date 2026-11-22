package tasks

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Files Git ignored before the run are the person's, even when the run
// changes the ignore rules so they show up as new.

func TestInPlaceRestoreKeepsFilesTheRunUnignored(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.push(".rhizome/.gitignore", "*.db\n")
	run(t, f.host, "pull", "--quiet", "--ff-only")
	writeFile(t, filepath.Join(f.host, ".rhizome/index.db"), "the person's index\n")
	f.edits(map[string]string{".rhizome/.gitignore": "", "notes/a.md": "a\n"}, nil)
	o := f.pass()
	if got := f.read(".rhizome/index.db"); got != "the person's index\n" {
		t.Fatalf("an ignored file must survive the restore, got %q (%+v)", got, o)
	}
	if got := f.read(".rhizome/.gitignore"); got != "*.db\n" {
		t.Fatalf("the protected ignore file must be restored, got %q", got)
	}
	if !strings.Contains(o.Reason, "ignore rules") || len(f.attention()) != 1 {
		t.Fatalf("the run must need attention and say it changed ignore rules: %+v", o)
	}
}

func TestInPlaceNeverCommitsFilesTheRunUnignored(t *testing.T) {
	f := newLandFixture(t, "name: Tend\nevery: 1h\nwrites: [\"docs/**\"]\nprompt: Tend.\n")
	f.push("docs/.gitignore", "secret.env\nbuild/\n")
	run(t, f.host, "pull", "--quiet", "--ff-only")
	writeFile(t, filepath.Join(f.host, "docs/secret.env"), "the person's secret\n")
	writeFile(t, filepath.Join(f.host, "docs/build/out.html"), "built\n")
	f.edits(map[string]string{"docs/.gitignore": "", "docs/a.md": "a\n"}, nil)
	o := f.pass()
	if o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{"docs/.gitignore", "docs/a.md"}) {
		t.Fatalf("files ignored before the run must not be committed, got %v", got)
	}
	if got := run(t, f.remote, "ls-tree", "-r", "--name-only", "main"); strings.Contains(got, "secret.env") {
		t.Fatalf("an ignored file reached the remote:\n%s", got)
	}
	it := taskItems(mustState(t, f.svc), "tend")[0]
	for _, want := range []string{"docs/secret.env (ignored before the run)", "docs/build/out.html (ignored before the run)"} {
		if !strings.Contains(it.Reason, want) {
			t.Fatalf("summary %q must name %q", it.Reason, want)
		}
	}
	if strings.Contains(o.Reason, "ignore rules") || it.State != StateDone {
		t.Fatalf("an ignore file inside writes is the task's to change: %+v", o)
	}
}

func TestInPlaceFlagsARunThatChangesIgnoreRules(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.edits(map[string]string{".gitignore": ".rhizome/task-state/\n*.tmp\n", ".git/info/exclude": "*.bak\n", "notes/a.md": "a\n"}, nil)
	o := f.pass()
	if !strings.Contains(o.Reason, "ignore rules") || !strings.Contains(o.Reason, ".gitignore") || !strings.Contains(o.Reason, ".git/info/exclude") {
		t.Fatalf("the notice must name both changed ignore files: %+v", o)
	}
	if len(f.attention()) != 1 {
		t.Fatalf("the run must need attention: %+v", taskItems(mustState(t, f.svc), "tend"))
	}
}
