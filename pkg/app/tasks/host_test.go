package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// hostService is a Service with its own user configuration file and a
// clock the test moves.
func hostService(t *testing.T, dir, primaryRef string, now *time.Time, trusted bool) *Service {
	t.Helper()
	repo, err := OpenRepo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Options{
		Repo:    repo,
		Config:  repoconfig.LocalTasksConfig{PrimaryRef: primaryRef, DefaultMode: "notify"},
		Env:     vaultconfig.Env{CLIConfigFile: filepath.Join(t.TempDir(), "config.yml")},
		Trusted: func(string) (bool, error) { return trusted, nil },
		Now:     func() time.Time { return *now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRunDueHostRunsOnlyWhereRunHereIsOn(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, dir, "main", &now, true)
	ctx := context.Background()

	summary, err := svc.RunDueHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary.Skipped, "rzm tasks machine on") || len(summary.Outcomes) != 0 || len(mustState(t, svc).History) != 0 {
		t.Fatalf("runHere off still ran: %+v", summary)
	}
	if at, ok, err := svc.NextDueAt(ctx); err != nil || ok {
		t.Fatalf("NextDueAt with runHere off = %v %v %v", at, ok, err)
	}
	report, err := svc.Status(ctx)
	if err != nil || report.RunHere || report.Tasks[0].BlockedBy != BlockedNotRunHere {
		t.Fatalf("status: %+v %v", report, err)
	}

	if err := svc.SetRunHere(ctx, true); err != nil {
		t.Fatal(err)
	}
	// No pass has seen the task yet, so it is due since it appeared.
	if at, ok, err := svc.NextDueAt(ctx); err != nil || !ok || !at.IsZero() {
		t.Fatalf("NextDueAt for a task that never ran = %v %v %v", at, ok, err)
	}
	if summary, err = svc.RunDueHost(ctx); err != nil || len(summary.Outcomes) != 1 || summary.Outcomes[0].Outcome != OutcomeNotified {
		t.Fatalf("runHere on: %+v %v", summary, err)
	}
	// The open due item re-checks an hour later, not before.
	if at, ok, err := svc.NextDueAt(ctx); err != nil || !ok || !at.Equal(now.Add(time.Hour)) {
		t.Fatalf("NextDueAt after a run = %v %v %v", at, ok, err)
	}
}

func TestRunDueRunsTheNearestDeadlineFirst(t *testing.T) {
	dir := newRepo(t, map[string]string{
		"a.yml": "name: A\nprompt: p\nschedule:\n  at: \"05:00\"\n  late: 3h\n  timezone: UTC\n",
		"b.yml": "name: B\nprompt: p\nschedule:\n  at: \"05:00\"\n  late: 30m\n  timezone: UTC\n",
		"c.yml": "name: C\nprompt: p\nschedule:\n  at: \"05:05\"\n  late: 30m\n  timezone: UTC\n",
		"d.yml": "name: D\nprompt: p\nschedule:\n  at: \"06:00\"\n  timezone: UTC\n",
	})
	now := time.Date(2026, 11, 23, 5, 10, 0, 0, time.UTC)
	svc := hostService(t, dir, "main", &now, true)
	summary, err := svc.RunDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, o := range summary.Outcomes {
		order = append(order, o.Task)
	}
	if strings.Join(order, ",") != "b,c,a" {
		t.Fatalf("ran %v, want b,c,a (D's slot has not come)", order)
	}
}

func TestPauseUntilEndsOnItsOwn(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, dir, "main", &now, true)
	ctx := context.Background()
	if err := svc.SetRunHere(ctx, true); err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Hour)
	if err := svc.PauseUntil(ctx, until); err != nil {
		t.Fatal(err)
	}
	if summary, _ := svc.RunDue(ctx); summary.Skipped != "paused" {
		t.Fatalf("paused pass: %+v", summary)
	}
	if at, ok, _ := svc.NextDueAt(ctx); !ok || !at.Equal(until) {
		t.Fatalf("NextDueAt during a timed pause = %v %v", at, ok)
	}
	report, _ := svc.Status(ctx)
	if !report.Paused || !report.PausedUntil.Equal(until) || report.Tasks[0].BlockedBy != BlockedPaused {
		t.Fatalf("status while paused: %+v", report)
	}

	now = until
	if summary, err := svc.RunDue(ctx); err != nil || len(summary.Outcomes) != 1 {
		t.Fatalf("after the pause: %+v %v", summary, err)
	}
	if st := mustState(t, svc); st.Paused || !st.PausedUntil.IsZero() {
		t.Fatalf("expired pause was not cleared: %+v", st)
	}
	if err := svc.PauseUntil(ctx, now.Add(-time.Minute)); err == nil {
		t.Fatal("a pause until the past must be refused")
	}
}

func TestStatusExplainsWhyATaskWaits(t *testing.T) {
	dir := newRepo(t, map[string]string{
		"a.yml": "name: A\nevery: 1h\nprompt: p\n",
		"b.yml": "name: B\nevery: 1h\nprompt: p\n",
		"c.yml": "name: C\nprompt: p\nschedule:\n  at: \"23:00\"\n  timezone: Asia/Tokyo\n",
	})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, dir, "main", &now, true)
	ctx := context.Background()
	if err := svc.SetRunHere(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMode(ctx, "a", "off"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Update(ctx, func(st *State) error {
		st.AddItem(Item{Task: "b", Kind: KindTask, State: StateReady}, now)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]TaskStatus{}
	for _, ts := range report.Tasks {
		got[ts.ID] = ts
	}
	if got["a"].BlockedBy != BlockedOff || got["b"].BlockedBy != BlockedOpenItem || got["c"].BlockedBy != "" {
		t.Fatalf("blockedBy: a=%q b=%q c=%q", got["a"].BlockedBy, got["b"].BlockedBy, got["c"].BlockedBy)
	}
	if c := got["c"]; c.Timezone != "Asia/Tokyo" || !c.NextAt.Equal(time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)) || c.Schedule != "daily at 23:00 (Asia/Tokyo)" {
		t.Fatalf("scheduled task status: %+v", c)
	}
}

func TestUntrustedHostPassIsReportedOnce(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, dir, "main", &now, false)
	ctx := context.Background()
	if err := svc.SetRunHere(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := svc.NextDueAt(ctx); !ok {
		t.Fatal("an unreported untrusted repository should get one host pass")
	}
	if summary, err := svc.RunDueHost(ctx); err != nil || !strings.Contains(summary.Skipped, "rzm trust") {
		t.Fatalf("untrusted host pass: %+v %v", summary, err)
	}
	first := mustState(t, svc).PassError
	if first == nil || first.Code != PassUntrusted {
		t.Fatalf("pass error %+v", first)
	}
	now = now.Add(time.Minute)
	_, _ = svc.RunDueHost(ctx)
	if again := mustState(t, svc).PassError; !again.At.Equal(first.At) {
		t.Fatalf("reported again at %v", again.At)
	}
	if _, ok, _ := svc.NextDueAt(ctx); ok {
		t.Fatal("a reported untrusted repository should not wake the host")
	}
	if report, _ := svc.Status(ctx); report.Tasks[0].BlockedBy != BlockedUntrusted {
		t.Fatalf("status blockedBy %q", report.Tasks[0].BlockedBy)
	}
}

func TestRunDueFetchesOnlyWhenSomethingIsDue(t *testing.T) {
	isolateGit(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	seed := filepath.Join(root, "seed")
	run(t, root, "init", "--quiet", "-b", "main", seed)
	commitFile(t, seed, TasksDir+"/a.yml", "name: A\nevery: 1h\nprompt: p\n", "add task")
	run(t, seed, "remote", "add", "origin", remote)
	run(t, seed, "push", "--quiet", "origin", "main")
	host := filepath.Join(root, "host")
	run(t, root, "clone", "--quiet", remote, host)

	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, host, "", &now, true)
	ctx := context.Background()
	if summary, err := svc.RunDue(ctx); err != nil || len(summary.Outcomes) != 1 {
		t.Fatalf("first pass: %+v %v", summary, err)
	}
	if f := mustState(t, svc).Fetch; f == nil || f.Error != "" || !f.At.Equal(now) {
		t.Fatalf("first fetch: %+v", f)
	}

	// Nothing due: no fetch, so a teammate's new commit is not seen yet.
	commitFile(t, seed, "README.md", "new\n", "teammate")
	run(t, seed, "push", "--quiet", "origin", "main")
	fetchedAt := now
	now = now.Add(10 * time.Minute)
	if summary, _ := svc.RunDue(ctx); len(summary.Outcomes) != 0 {
		t.Fatalf("nothing was due: %+v", summary)
	}
	if f := mustState(t, svc).Fetch; !f.At.Equal(fetchedAt) {
		t.Fatalf("a pass with nothing due fetched at %v", f.At)
	}

	// Due again: the pass fetches and uses the teammate's commit.
	head := run(t, seed, "rev-parse", "HEAD")
	now = now.Add(time.Hour)
	if summary, err := svc.RunDue(ctx); err != nil || summary.Commit != head {
		t.Fatalf("due pass used %s, want the fetched %s (%v)", summary.Commit, head, err)
	}

	// The remote is gone: the pass records the failure and runs from the
	// last fetched ref.
	if err := os.Rename(remote, remote+"-gone"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	summary, err := svc.RunDue(ctx)
	if err != nil || summary.Commit != head || len(summary.Outcomes) != 1 {
		t.Fatalf("fallback pass: %+v %v", summary, err)
	}
	report, err := svc.Status(ctx)
	if err != nil || report.Fetch == nil || report.Fetch.Error == "" || !report.Fetch.At.Equal(now) {
		t.Fatalf("status fetch: %+v %v", report.Fetch, err)
	}
}

// A failed fetch's error names the remote URL. What is stored and shown
// keeps no userinfo, query string, fragment, or token from it.
func TestFetchErrorsKeepNoCredentials(t *testing.T) {
	isolateGit(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	seed := filepath.Join(root, "seed")
	run(t, root, "init", "--quiet", "-b", "main", seed)
	commitFile(t, seed, TasksDir+"/a.yml", "name: A\nevery: 1h\nprompt: p\n", "add task")
	run(t, seed, "remote", "add", "origin", remote)
	run(t, seed, "push", "--quiet", "origin", "main")
	host := filepath.Join(root, "host")
	run(t, root, "clone", "--quiet", remote, host)

	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, host, "", &now, true)
	ctx := context.Background()
	for _, url := range []string{
		"https://127.0.0.1:1/repo.git?access_token=SECRET-QUERY#SECRET-FRAGMENT",
		"https://x-access-token:SECRET-USERINFO@127.0.0.1:1/repo.git",
	} {
		run(t, host, "remote", "set-url", "origin", url)
		now = now.Add(2 * time.Hour)
		if _, err := svc.RunDue(ctx); err != nil {
			t.Fatal(err)
		}
		f := mustState(t, svc).Fetch
		if f == nil || f.Error == "" || strings.Contains(f.Error, "SECRET") {
			t.Fatalf("fetch from %s stored %+v", url, f)
		}
	}
	for _, text := range []string{
		"remote: token=SECRET-A rejected",
		"auth x-access-token:SECRET-B failed",
		"see http://h/p?ref=main&access_token=SECRET-C",
	} {
		if got := redactURLCredentials(text); strings.Contains(got, "SECRET") {
			t.Fatalf("redact(%q) = %q", text, got)
		}
	}
}
