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
)

func TestInPlaceLandsManyRunsWhileAnotherClonePushes(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	writeFile(t, filepath.Join(f.host, "draft.txt"), "a person's untracked draft\n")
	for i := range 20 {
		f.push(fmt.Sprintf("other/%d.md", i), "teammate\n")
		race := i%5 == 4
		f.agent.run = func(req AgentRequest) (AgentResult, error) {
			writeFile(t, filepath.Join(req.Dir, "notes", fmt.Sprintf("run-%d.md", i)), "note\n")
			if race {
				// Another machine pushes while the agent works.
				f.push(fmt.Sprintf("other/race-%d.md", i), "race\n")
			}
			return AgentResult{Message: fmt.Sprintf("Run %d.", i)}, nil
		}
		if o := f.pass(); o.Outcome != OutcomeLanded || o.Reason != "" {
			t.Fatalf("run %d: %+v", i, o)
		}
	}
	run(t, f.other, "pull", "--quiet", "--rebase", "origin", "main")
	log := run(t, f.other, "log", "--format=%B%x00", "origin/main")
	if n := strings.Count(log, "Rhizome-Task: tend\n"); n != 20 {
		t.Fatalf("expected 20 task commits on the remote, found %d:\n%s", n, log)
	}
	if n := strings.Count(log, "Rhizome-Task-Slot: "); n != 20 {
		t.Fatalf("expected 20 slot lines, found %d", n)
	}
	for i := range 20 {
		if _, err := os.Stat(filepath.Join(f.other, "notes", fmt.Sprintf("run-%d.md", i))); err != nil {
			t.Fatalf("run %d's note did not reach the remote: %v", i, err)
		}
	}
	if got := f.status(); got != "?? draft.txt" {
		t.Fatalf("the person's draft must stay untouched and nothing else left: %q", got)
	}
	if n := len(taskItems(mustState(t, f.svc), "tend")); n != 20 {
		t.Fatalf("expected one item per run, got %d", n)
	}
	for _, it := range taskItems(mustState(t, f.svc), "tend") {
		if it.State != StateDone || it.Landing != "in-place" || it.Worktree != "" || it.Branch != "" {
			t.Fatalf("a landed in-place item is done and owns no worktree: %+v", it)
		}
	}
}

func TestInPlaceLeavesDirtyAndStagedFilesAndNamesThem(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	writeFile(t, filepath.Join(f.host, "notes/dirty.md"), "the person's edit\n")
	writeFile(t, filepath.Join(f.host, "notes/staged.md"), "the person's staged edit\n")
	run(t, f.host, "add", "notes/staged.md")
	f.edits(map[string]string{
		"notes/dirty.md":  "the agent's edit\n",
		"notes/staged.md": "the agent's edit\n",
		"notes/new.md":    "new\n",
		"notes/log.md":    "log\nmore\n",
		"outside.md":      "outside\n",
	}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{"notes/log.md", "notes/new.md"}) {
		t.Fatalf("the commit must hold only the task's clean files, got %v", got)
	}
	if got := run(t, f.host, "diff", "--cached", "--name-only"); got != "notes/staged.md" {
		t.Fatalf("the staged change must stay staged, got %q", got)
	}
	if got := f.status(); !hasLine(got, " M notes/dirty.md") || !hasLine(got, "MM notes/staged.md") || !hasLine(got, "?? outside.md") {
		t.Fatalf("unexpected status:\n%s", got)
	}
	r := mustState(t, f.svc).Last["tend"]
	if !slices.Equal(r.Left, []string{"notes/dirty.md", "notes/staged.md", "outside.md"}) || r.Landed == "" {
		t.Fatalf("the record must name what was left: %+v", r)
	}
	it := taskItems(mustState(t, f.svc), "tend")[0]
	for _, want := range []string{"notes/dirty.md (changed before the run)", "notes/staged.md (staged before the run)", "outside.md (outside writes)"} {
		if !strings.Contains(it.Reason, want) {
			t.Fatalf("summary %q must name %q", it.Reason, want)
		}
	}
	if run(t, f.host, "rev-parse", "HEAD") != run(t, f.host, "rev-parse", "origin/main") {
		t.Fatal("a dirty tree does not stop a fast-forward push")
	}
}

func TestInPlaceRestoresProtectedPaths(t *testing.T) {
	f := newLandFixture(t, "name: Tend notes\nevery: 1h\nwrites: [\"notes/**\", \".rhizome/memory/**\", \"**/x.md\"]\nprompt: Tend.\n")
	f.edits(map[string]string{
		".agents/skills/a/SKILL.md":   "injected\n",
		".agents/skills/a/notes/x.md": "matched only by **\n",
		".rhizome/tasks/tend.yml":     "name: Widened\nevery: 1m\nwrites: [\"**\"]\nprompt: x\n",
		".rhizome/new.yml":            "new\n",
		".rhizome/memory/handoff.md":  "next time\n",
		"notes/log.md":                "log\nmore\n",
	}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded || !strings.Contains(o.Reason, "protected files") {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{".rhizome/memory/handoff.md", "notes/log.md"}) {
		t.Fatalf("only writes outside protected folders, or naming them, are committed: %v", got)
	}
	if f.read(".agents/skills/a/SKILL.md") != "agent skill\n" || f.read(".agents/skills/a/notes/x.md") != "x\n" || !strings.HasPrefix(f.read(".rhizome/tasks/tend.yml"), "name: Tend notes") {
		t.Fatal("protected files must be restored to their snapshot content")
	}
	if _, err := os.Stat(filepath.Join(f.host, ".rhizome/new.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a protected file the run created must be removed: %v", err)
	}
	if got := f.status(); got != "" {
		t.Fatalf("nothing may be left dirty: %q", got)
	}
	attention := f.attention()
	if len(attention) != 1 || !strings.Contains(attention[0].Reason, protectedDiffFile) {
		t.Fatalf("expected one needs-attention item naming the saved diff, got %+v", attention)
	}
	diff, err := os.ReadFile(f.svc.runFile(mustState(t, f.svc).Last["tend"].ID, protectedDiffFile))
	if err != nil || !strings.Contains(string(diff), "+injected") || !strings.Contains(string(diff), ".rhizome/new.yml") {
		t.Fatalf("the run's protected diff must be saved: %v\n%s", err, diff)
	}
}

func TestInPlaceDivergenceWithDirtyTreeKeepsTheCommit(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	writeFile(t, filepath.Join(f.host, "README.md"), "the person's edit\n")
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		writeFile(t, filepath.Join(req.Dir, "notes/log.md"), "log\nmore\n")
		f.push("other/race.md", "race\n")
		return AgentResult{Message: "Logged."}, nil
	}
	if o := f.pass(); o.Outcome != OutcomeLanded || !strings.Contains(o.Reason, "did not rebase") {
		t.Fatalf("%+v", o)
	}
	head := run(t, f.host, "rev-parse", "HEAD")
	if !hasLine(run(t, f.host, "log", "-1", "--format=%B"), "Rhizome-Task: tend") || isAncestor(context.Background(), f.host, head, "origin/main") {
		t.Fatal("the task commit must stay local and unpushed")
	}
	if f.read("README.md") != "the person's edit\n" || run(t, f.host, "status", "--porcelain") != " M README.md" {
		t.Fatal("the person's dirty file must be untouched")
	}
	if attention := f.attention(); len(attention) != 1 || attention[0].Landed != head {
		t.Fatalf("expected one notice holding the local commit, got %+v", attention)
	}
}

func TestInPlaceAdoptsAFailedRunsFiles(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.edits(map[string]string{"notes/a.md": "from the failed run\n", "outside.md": "x\n"}, errors.New("harness crashed"))
	if o := f.pass(); o.Outcome != OutcomeFailed {
		t.Fatalf("%+v", o)
	}
	if got := mustState(t, f.svc).Leftover["tend"]; !slices.Equal(got, []string{"notes/a.md"}) {
		t.Fatalf("a failed run records the task's files, got %v", got)
	}
	failed := f.attention()
	if len(failed) != 1 {
		t.Fatalf("expected one needs-attention item, got %+v", failed)
	}
	f.edits(map[string]string{"notes/b.md": "from the next run\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if got := f.committed("HEAD"); !slices.Equal(got, []string{"notes/a.md", "notes/b.md"}) {
		t.Fatalf("the next successful run must adopt the failed run's files, got %v", got)
	}
	if got := mustState(t, f.svc).Leftover["tend"]; got != nil {
		t.Fatalf("adopted files are no longer left over: %v", got)
	}
	if f.status() != "?? outside.md" {
		t.Fatalf("a file outside writes is never adopted: %q", f.status())
	}

	// Discard on the failed run's in-place item only dismisses the notice.
	before := run(t, f.host, "worktree", "list", "--porcelain")
	writeFile(t, filepath.Join(f.host, "notes/c.md"), "still here\n")
	if err := f.svc.Discard(context.Background(), failed[0].ID); err != nil {
		t.Fatal(err)
	}
	if f.read("notes/c.md") != "still here\n" || run(t, f.host, "worktree", "list", "--porcelain") != before || run(t, f.host, "branch", "--show-current") != "main" {
		t.Fatal("Discard must leave the open checkout intact")
	}
	if it, _ := mustStatePtr(t, f.svc).Item(failed[0].ID); it.State != StateDismissed {
		t.Fatalf("the notice must be dismissed: %+v", it)
	}
	if err := f.svc.removeWorktree(context.Background(), "main", f.host); err == nil {
		t.Fatal("cleanup must refuse the open checkout and a branch Rhizome did not make")
	}
}

func TestInPlaceHeadMovedDuringTheRunDoesNotCommit(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		commitFile(t, req.Dir, "README.md", "the person's commit\n", "person commits during the run")
		writeFile(t, filepath.Join(req.Dir, "notes/a.md"), "agent\n")
		return AgentResult{Message: "Done."}, nil
	}
	if o := f.pass(); o.Outcome != OutcomeFailed || !strings.Contains(o.Reason, "HEAD moved") {
		t.Fatalf("%+v", o)
	}
	if run(t, f.host, "log", "-1", "--format=%s") != "person commits during the run" || f.status() != "?? notes/a.md" {
		t.Fatal("Rhizome must not commit after HEAD moved")
	}
	if got := mustState(t, f.svc).Leftover["tend"]; !slices.Equal(got, []string{"notes/a.md"}) {
		t.Fatalf("the files must be recorded, got %v", got)
	}
	if len(f.attention()) != 1 {
		t.Fatalf("expected one notice, got %+v", f.attention())
	}
}

func TestInPlaceSkipsAWrongBranchWithOneNotice(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	run(t, f.host, "switch", "--quiet", "-c", "feature")
	for range 3 {
		if o := f.pass(); o.Outcome != OutcomeSkipped || !strings.Contains(o.Reason, "is on feature") {
			t.Fatalf("%+v", o)
		}
	}
	if f.agent.calls() != 0 {
		t.Fatal("no agent work may run on the wrong branch")
	}
	if n := len(taskItems(mustState(t, f.svc), "tend")); n != 1 || len(f.attention()) != 1 {
		t.Fatalf("expected one deduplicated notice, got %+v", taskItems(mustState(t, f.svc), "tend"))
	}
}
