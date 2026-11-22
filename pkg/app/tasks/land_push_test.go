package tasks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func TestWidenedLandingRunsAsNotifyUntilApproved(t *testing.T) {
	f := newLandFixture(t, "name: Tend notes\nevery: 1h\nprompt: Tend the notes.\n")
	if o := f.pass(); o.Outcome != OutcomeNoChanges {
		t.Fatalf("a task without writes lands as pr: %+v", o)
	}
	f.push(".rhizome/tasks/tend.yml", inPlaceTask)
	run(t, f.host, "fetch", "--quiet", "origin")
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	o := f.pass()
	if o.Outcome != OutcomeNotified || f.agent.calls() != 1 {
		t.Fatalf("a widened task must notify, not run: %+v", o)
	}
	st := mustState(t, f.svc)
	var notice *Item
	for i, it := range st.Items {
		if it.Kind == KindAnnouncement && strings.Contains(it.Summary, "rzm tasks approve tend") {
			notice = &st.Items[i]
		}
	}
	if notice == nil || !strings.Contains(notice.Summary, "committing only notes/**") {
		t.Fatalf("expected an announcement asking for approval, got %+v", st.Items)
	}
	report, err := f.svc.Status(context.Background())
	if err != nil || report.Tasks[0].Mode != ModeNotify || report.Tasks[0].ModeSource != "approval" || report.Tasks[0].Land != "in-place" {
		t.Fatalf("status must explain the approval wait: %+v, %v", report.Tasks, err)
	}
	if err := f.svc.Approve(context.Background(), "tend"); err != nil {
		t.Fatal(err)
	}
	if report, _ := f.svc.Status(context.Background()); report.Tasks[0].Mode != ModeAuto {
		t.Fatalf("an approved task runs in its own mode: %+v", report.Tasks[0])
	}
	if it, _ := mustStatePtr(t, f.svc).Item(notice.ID); it.Active() {
		t.Fatal("approving closes the announcement")
	}
	// The person's Run of the notify item lands in place.
	if o, err := f.svc.Run(context.Background(), o.Item); err != nil || o.Outcome != OutcomeLanded {
		t.Fatalf("%+v %v", o, err)
	}
	// Adding a write pattern widens it again.
	f.push(".rhizome/tasks/tend.yml", strings.Replace(inPlaceTask, `["notes/**"]`, `["notes/**", "docs/**"]`, 1))
	run(t, f.host, "fetch", "--quiet", "origin")
	if o := f.pass(); o.Outcome != OutcomeNotified {
		t.Fatalf("a new write pattern must ask again: %+v", o)
	}
}

func TestSlotAlreadyOnThePrimaryRefIsDone(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	slot := "2026-11-23T05:00:00-05:00"
	f.svc.slotFor = func(Definition, time.Time) string { return slot }
	f.push("notes/other.md", "other machine\n")
	run(t, f.other, "commit", "--quiet", "--allow-empty", "-m", "chore(tasks): Tend notes\n\nRhizome-Task: tend\nRhizome-Task-Slot: "+slot)
	run(t, f.other, "push", "--quiet", "origin", "HEAD:main")
	run(t, f.host, "fetch", "--quiet", "origin")
	if o := f.pass(); o.Outcome != OutcomeSkipped || !strings.Contains(o.Reason, "already landed") || f.agent.calls() != 0 {
		t.Fatalf("%+v", o)
	}
	f.svc.slotFor = func(Definition, time.Time) string { return "2026-11-24T05:00:00-05:00" }
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if !hasLine(run(t, f.host, "log", "-1", "--format=%B"), "Rhizome-Task-Slot: 2026-11-24T05:00:00-05:00") {
		t.Fatal("the commit names its slot")
	}
}

func TestPushNeedsTheRepositoryAndThePerson(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	f.svc = f.service(repoconfig.LocalTasksConfig{PrimaryRef: "origin/main", DefaultMode: "auto"}, true)
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeLanded {
		t.Fatalf("%+v", o)
	}
	if run(t, f.host, "rev-parse", "HEAD") == run(t, f.host, "rev-parse", "origin/main") {
		t.Fatal("without the repository's allowPush the commit stays local")
	}
}

func TestPRLandingPushesTheBranchAndOpensAPullRequest(t *testing.T) {
	f := newLandFixture(t, "name: Tend notes\nevery: 1h\nland: pr\nprompt: Tend the notes.\n")
	var got struct{ dir, base, head, title, body string }
	saved := openPullRequest
	t.Cleanup(func() { openPullRequest = saved })
	openPullRequest = func(_ context.Context, dir, base, head, title, body string) (string, error) {
		got.dir, got.base, got.head, got.title, got.body = dir, base, head, title, body
		return "https://example.test/pull/1", nil
	}
	f.edits(map[string]string{"notes/a.md": "a\n"}, nil)
	if o := f.pass(); o.Outcome != OutcomeReady {
		t.Fatalf("%+v", o)
	}
	it := taskItems(mustState(t, f.svc), "tend")[0]
	if it.PR != "https://example.test/pull/1" || it.Landing != "pr" || it.Worktree == "" || it.Reason != "" {
		t.Fatalf("%+v", it)
	}
	if got.base != "main" || got.head != it.Branch || !hasLine(got.body, "Rhizome-Task: tend") {
		t.Fatalf("unexpected pull request %+v", got)
	}
	if out := run(t, f.remote, "branch", "--list", it.Branch); !strings.Contains(out, it.Branch) {
		t.Fatalf("the branch must reach the remote, got %q", out)
	}
	if f.status() != "" || !strings.HasPrefix(it.Worktree, filepath.Join(f.host, ".rhizome", "task-state")) {
		t.Fatal("a pr run never touches the open checkout")
	}
}
