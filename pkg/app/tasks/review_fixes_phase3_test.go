package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

const (
	attentionResult = `{"status": "attention", "summary": "look"}`
	// notNeededTask is a check-only task whose check finds nothing to do.
	notNeededTask = "name: Look\nevery: 1h\ncheck:\n  command: [sh, -c, \"echo '{\\\"needed\\\": false}'\"]\n"
)

func attentionAgent(t *testing.T) *fakeAgent {
	return &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		writeResult(t, req, attentionResult)
		return AgentResult{Message: "done"}, nil
	}}
}

// writeHostConfig turns on running the repository's tasks on this machine,
// with the fixture's notify command.
func (f notifyFixture) writeHostConfig(t *testing.T) {
	t.Helper()
	cfg := fmt.Sprintf("tasks:\n  notify:\n    command: [sh, %q, %q]\n  repositories:\n    %q:\n      runHere: true\n", f.script, f.sent, f.svc.opts.Repo.CommonDir)
	require.NoError(t, os.WriteFile(f.config, []byte(cfg), 0o600))
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 20*time.Second, 20*time.Millisecond, "waiting for %s", path)
}

// A slow notify command must not hold run.lock: the next run could not
// start, and Stop would kill the alert with the run.
func TestAlertsRunAfterRunLockIsReleased(t *testing.T) {
	body := `touch "$1.started"; i=0; while [ ! -e "$1.go" ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done; cat >> "$1"`
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, attentionAgent(t), body)
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.RunDue(context.Background())
		done <- err
	}()
	waitForFile(t, f.sent+".started")
	held := f.svc.RunLockHeld()
	writeFile(t, f.sent+".go", "")
	require.NoError(t, <-done)
	require.False(t, held, "the notify command ran while run.lock was held")
	require.Equal(t, "A needs attention: look\n", f.notifications(t))
	require.Empty(t, mustState(t, f.svc).PendingAlerts)
}

// An alert is queued with the run's record, so a process stopped while
// delivering it leaves it for the next run-due, which sends it once.
func TestStoppedDeliveryLeavesTheAlertForTheNextPass(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, attentionAgent(t), `touch "$1.started"; sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.RunDue(ctx)
		done <- err
	}()
	waitForFile(t, f.sent+".started")
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a stop did not end delivery")
	}
	r := lastRun(t, f.svc, "a")
	pending := mustState(t, f.svc).PendingAlerts
	require.Len(t, pending, 1)
	require.Equal(t, r.ID, pending[0].Run)
	require.Empty(t, r.NotifyError, "an interrupted delivery is not a failure")

	require.NoError(t, os.WriteFile(f.script, []byte(`cat >> "$1"`+"\n"), 0o700))
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, "A needs attention: look\n", f.notifications(t))
	require.Empty(t, mustState(t, f.svc).PendingAlerts)
}

// The queue keeps the newest alerts and says how many it dropped.
func TestAlertQueueIsBounded(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, nil, `{ cat; echo; } >> "$1"`)
	ctx := context.Background()
	require.NoError(t, f.svc.Store().Update(ctx, func(st *State) error {
		for i := range maxPendingAlerts + 5 {
			f.svc.queueAlert(st, Notification{Task: "a", Outcome: OutcomeFailed, Summary: fmt.Sprintf("alert %d", i)})
		}
		return nil
	}))
	f.svc.deliverAlerts(ctx)

	lines := strings.Split(strings.TrimSpace(f.notifications(t)), "\n\n")
	require.Len(t, lines, maxPendingAlerts)
	require.Equal(t, "alert 5 (Rhizome dropped 5 older alerts.)", lines[0])
	require.Equal(t, fmt.Sprintf("alert %d", maxPendingAlerts+4), lines[len(lines)-1])
	st := mustState(t, f.svc)
	require.Empty(t, st.PendingAlerts)
	require.Zero(t, st.DroppedAlerts)
}

// A verbose notify command's stderr is capped like its stdout.
func TestVerboseNotifyCommandOutputIsCapped(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, attentionAgent(t), `head -c 300000 /dev/zero | tr '\0' e >&2`)
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)
	r := lastRun(t, f.svc, "a")
	require.Empty(t, r.NotifyError)
	info, err := os.Stat(f.svc.runFile(r.ID, notifyLogFile))
	require.NoError(t, err)
	require.Equal(t, int64(maxNotifyOutput), info.Size())
}

// A crash during a scheduled run's check leaves a needs-attention item, so
// its alert names one.
func TestCrashDuringCheckLeavesAnItemToAlertAbout(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, nil, "")
	ctx := context.Background()
	id := newRunID(time.Now())
	require.NoError(t, f.svc.Store().Update(ctx, func(st *State) error {
		st.Running = &RunRecord{ID: id, Task: "a", StartedAt: time.Now(), Stage: StageCheck}
		return nil
	}))
	_, err := f.svc.RunDue(ctx)
	require.NoError(t, err)

	it := activeItem(t, f.svc, "a")
	require.NotNil(t, it)
	require.Equal(t, StateAttention, it.State)
	st := mustState(t, f.svc)
	r, ok := st.runByID(id)
	require.True(t, ok)
	require.Equal(t, OutcomeInterrupted, r.Outcome)
	require.Equal(t, it.ID, r.Item)
	require.Contains(t, f.notifications(t), fmt.Sprintf("OUTCOME=interrupted RUN=%s ITEM=%s", id, it.ID))
}

// A host alerts once per distinct pass-level failure until it clears, then
// again if it recurs.
func TestHostAlertsOnceWhileUntrusted(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"look.yml": notNeededTask}, nil, "")
	f.writeHostConfig(t)
	trusted := false
	f.svc.opts.Trusted = func(string) (bool, error) { return trusted, nil }
	ctx := context.Background()
	for range 2 {
		_, err := f.svc.RunDueHost(ctx)
		require.NoError(t, err)
	}
	sent := f.notifications(t)
	require.Equal(t, 1, strings.Count(sent, "OUTCOME=untrusted"), sent)
	require.Contains(t, sent, "ID= OUTCOME=untrusted RUN= ITEM=", "a pass-level alert names no task or run")

	trusted = true
	_, err := f.svc.RunDueHost(ctx)
	require.NoError(t, err)
	require.Nil(t, mustState(t, f.svc).PassAlert, "the failure cleared")
	trusted = false
	_, err = f.svc.RunDueHost(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(f.notifications(t), "OUTCOME=untrusted"), "it alerts again when it recurs")
}

// A primary ref that cannot be resolved still gets a host pass every 15
// minutes, which records the error and alerts once.
func TestHostAlertsWhenThePrimaryRefIsMissing(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, nil, "")
	f.writeHostConfig(t)
	now := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	f.svc.opts.Now = func() time.Time { return now }
	f.svc.opts.Config.PrimaryRef = "origin/missing"
	ctx := context.Background()

	at, ok, err := f.svc.NextDueAt(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, now, at, "with nothing recorded, a pass may run now")
	for range 2 {
		_, err = f.svc.RunDueHost(ctx)
		require.Error(t, err)
	}
	st := mustState(t, f.svc)
	require.NotNil(t, st.PassError)
	sent := f.notifications(t)
	require.Equal(t, 1, strings.Count(sent, "OUTCOME=pass-failed"), sent)
	require.Contains(t, sent, "could not run: ")

	at, ok, err = f.svc.NextDueAt(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, st.PassError.At.Add(passRetryEvery), at)
}

// A fetch of the primary ref that keeps failing alerts once it has failed
// for six hours, not on every pass.
func TestHostAlertsWhenFetchKeepsFailing(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"look.yml": notNeededTask}, nil, "")
	f.writeHostConfig(t)
	run(t, f.dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "gone.git"))
	run(t, f.dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	f.svc.opts.Config.PrimaryRef = "origin/main"
	now := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	f.svc.opts.Now = func() time.Time { return now }
	ctx := context.Background()
	alerts := func() int { return strings.Count(f.notifications(t), "OUTCOME=fetch-failed") }

	for _, step := range []struct {
		after time.Duration
		want  int
	}{{0, 0}, {2 * time.Hour, 0}, {4 * time.Hour, 0}, {8 * time.Hour, 1}, {10 * time.Hour, 1}} {
		now = time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC).Add(step.after)
		_, err := f.svc.RunDueHost(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, mustState(t, f.svc).Fetch.Error)
		require.Equal(t, step.want, alerts(), "after %s", step.after)
	}
	require.Contains(t, f.notifications(t), "running from a stale primary ref")
}

// A pull request is the review surface: a quiet or ok run that opened one
// leaves history only, while attention or a failed push still alerts.
func TestPushedPullRequestIsTheReviewSurface(t *testing.T) {
	f := newLandFixture(t, "name: Tend notes\nevery: 1h\nland: pr\nprompt: Tend the notes.\n")
	sent := filepath.Join(t.TempDir(), "sent.txt")
	config := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(config, []byte(fmt.Sprintf("tasks:\n  notify:\n    command: [sh, -c, 'cat >> %q; echo \" OUTCOME=$RZM_TASK_OUTCOME\" >> %q']\n", sent, sent)), 0o600))
	f.env = vaultconfig.Env{CLIConfigFile: config}
	f.svc = f.service(f.svc.opts.Config, true)
	saved := openPullRequest
	t.Cleanup(func() { openPullRequest = saved })
	prErr := error(nil)
	openPullRequest = func(context.Context, string, string, string, string, string) (string, error) {
		return "https://example.test/pull/1", prErr
	}
	pass := func(result string, n int) RunRecord {
		f.agent.run = func(req AgentRequest) (AgentResult, error) {
			writeFile(t, filepath.Join(req.Dir, "notes/a.md"), fmt.Sprintf("a%d\n", n))
			writeResult(t, req, result)
			return AgentResult{Message: "Tended the notes."}, nil
		}
		require.Equal(t, OutcomeReady, f.pass().Outcome)
		return lastRun(t, f.svc, "tend")
	}

	r := pass(`{"status": "ok"}`, 1)
	require.Equal(t, "https://example.test/pull/1", r.PR)
	require.Nil(t, activeItem(t, f.svc, "tend"), "no inbox item")
	require.NoFileExists(t, sent, "no alert")
	_, err := os.Stat(filepath.Join(f.host, ".rhizome", "task-state", "worktrees", worktreeName(Definition{ID: "tend"}, r.Branch)))
	require.True(t, errors.Is(err, os.ErrNotExist), "the worktree is gone: %v", err)

	pass(attentionResult, 2)
	it := activeItem(t, f.svc, "tend")
	require.NotNil(t, it)
	require.Equal(t, StateReady, it.State)
	require.Equal(t, "https://example.test/pull/1", it.PR)
	// Discarding drops the local branch; the pushed one still holds the
	// name, so the next run takes another.
	require.NoError(t, f.svc.Discard(context.Background(), it.ID))

	prErr = errors.New("gh is not signed in")
	pass(`{"status": "quiet"}`, 3)
	data, err := os.ReadFile(sent)
	require.NoError(t, err)
	require.Contains(t, string(data), "Tend notes needs attention: Pushed rzm/tend/2026-11-23-3, but opening its pull request failed")
	require.Contains(t, string(data), "gh is not signed in")
	require.Equal(t, 2, strings.Count(string(data), "OUTCOME=attention"), string(data))
}

// An in-place landing's alert says what went wrong before the agent's
// summary.
func TestLandingAlertNamesItsNotices(t *testing.T) {
	f := newLandFixture(t, inPlaceTask)
	sent := filepath.Join(t.TempDir(), "sent.txt")
	config := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(config, []byte(fmt.Sprintf("tasks:\n  notify:\n    command: [sh, -c, 'cat >> %q; echo \" OUTCOME=$RZM_TASK_OUTCOME\" >> %q']\n", sent, sent)), 0o600))
	f.env = vaultconfig.Env{CLIConfigFile: config}
	f.svc = f.service(f.svc.opts.Config, true)
	f.edits(map[string]string{"notes/a.md": "a\n", ".agents/skills/a/SKILL.md": "rewritten\n"}, nil)
	require.Equal(t, OutcomeLanded, f.pass().Outcome)

	data, err := os.ReadFile(sent)
	require.NoError(t, err)
	text := string(data)
	require.True(t, strings.HasPrefix(text, "Tend notes needs attention: The run changed protected files (.agents/skills/a/SKILL.md); Rhizome restored them"), text)
	require.Contains(t, text, ". Tended the notes.\n OUTCOME=attention")
}
