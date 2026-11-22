package tasks

import (
	"path/filepath"

	"context"
	"encoding/json"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var hostNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func statusHostService(t *testing.T) *Service {
	t.Helper()
	dir := newRepo(t, map[string]string{
		"digest.yml": "name: Digest\nevery: 1d\nprompt: x\n",
		"lint.yml":   "name: Lint\nevery: 1h\nprompt: x\n",
		"notes.yml":  "name: Notes\nevery: 1h\nprompt: x\n",
	})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	svc.opts.Now = func() time.Time { return hostNow }
	svc.opts.Env = vaultconfig.Env{CLIConfigFile: filepath.Join(t.TempDir(), "config.yml")}
	require.NoError(t, svc.SetRunHere(context.Background(), true))
	return svc
}

func setState(t *testing.T, svc *Service, fn func(*State)) {
	t.Helper()
	require.NoError(t, svc.Store().Update(context.Background(), func(st *State) error {
		fn(st)
		return nil
	}))
}

func nextDueAt(t *testing.T, svc *Service) (time.Time, bool) {
	t.Helper()
	at, ok, err := svc.NextDueAt(context.Background())
	require.NoError(t, err)
	return at.UTC(), ok
}

func TestNextDueAtSkipsWhatRunDueSkips(t *testing.T) {
	svc := statusHostService(t)
	ran := func(task string, ago time.Duration) RunRecord {
		return RunRecord{Task: task, At: hostNow.Add(-ago), Outcome: OutcomeNotNeeded}
	}
	setState(t, svc, func(st *State) {
		st.Record(ran("digest", 2*time.Hour)) // due in 22h
		st.Record(ran("lint", 30*time.Minute))
		st.Record(ran("notes", 50*time.Minute))
	})
	at, ok := nextDueAt(t, svc)
	require.True(t, ok)
	require.Equal(t, hostNow.Add(10*time.Minute), at, "Notes comes due first")

	setState(t, svc, func(st *State) {
		st.AddItem(Item{Task: "notes", Kind: KindTask, State: StateReady}, hostNow)
		st.Modes = map[string]Mode{"lint": ModeOff}
	})
	at, _ = nextDueAt(t, svc)
	require.Equal(t, hostNow.Add(22*time.Hour), at, "a ready item blocks Notes and Lint is off")

	until := hostNow.Add(48 * time.Hour)
	require.NoError(t, svc.PauseUntil(context.Background(), until))
	at, ok = nextDueAt(t, svc)
	require.True(t, ok)
	require.Equal(t, until, at, "nothing runs before a timed pause lifts")

	require.NoError(t, svc.SetPaused(context.Background(), true))
	_, ok = nextDueAt(t, svc)
	require.False(t, ok, "an open-ended pause runs nothing")

	require.NoError(t, svc.SetPaused(context.Background(), false))
	svc.opts.Trusted = func(string) (bool, error) { return false, nil }
	_, ok = nextDueAt(t, svc)
	require.True(t, ok, "an untrusted repository still gets one pass, which reports it")
	setState(t, svc, func(st *State) { st.PassError = &PassError{At: hostNow, Code: PassUntrusted, Message: "untrusted"} })
	_, ok = nextDueAt(t, svc)
	require.False(t, ok, "once reported, an untrusted repository runs nothing")
}

func TestNextDueAtTreatsANewTaskAsDueNow(t *testing.T) {
	svc := statusHostService(t)
	seen := hostNow.Add(-time.Minute)
	setState(t, svc, func(st *State) {
		st.Seen = map[string]time.Time{"digest": seen, "lint": seen, "notes": seen}
	})
	at, ok := nextDueAt(t, svc)
	require.True(t, ok)
	require.Equal(t, hostNow, at)
}

func TestHostStatusShape(t *testing.T) {
	svc := statusHostService(t)
	failedAt := hostNow.Add(-30 * time.Minute)
	setState(t, svc, func(st *State) {
		st.Record(RunRecord{Task: "digest", At: hostNow.Add(-2 * time.Hour), Outcome: OutcomeReady})
		st.AddItem(Item{Task: "digest", Kind: KindTask, State: StateReady, Message: "Wrote the digest."}, hostNow)
		lint := st.AddItem(Item{Task: "lint", Kind: KindTask, State: StateAttention, Reason: "check failed: exit 1"}, hostNow)
		st.Record(RunRecord{Task: "lint", At: failedAt, Outcome: OutcomeFailed, Message: "check failed: exit 1", Item: lint.ID})
		st.AddItem(Item{Task: "notes", Kind: KindAnnouncement, State: StateDue, Summary: "New task \"Notes\" runs every 1h."}, hostNow)
		st.AddItem(Item{Task: "notes", Kind: KindTask, State: StateDue, Summary: "Hidden", Snoozed: hostNow.Add(time.Hour)}, hostNow)
		st.Record(RunRecord{Task: "notes", At: hostNow.Add(-50 * time.Minute), Outcome: OutcomeNotified})
		// Left by a crash: nothing holds the run lock.
		st.Running = &RunRecord{ID: "20261122T110000Z-0123abcd", Task: "notes", Stage: StageCheck}
	})

	status, err := svc.HostStatus(context.Background())
	require.NoError(t, err)
	data, err := json.Marshal(status)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Equal(t, map[string]any{
		"repository": svc.opts.Repo.MainCheckout,
		"commonDir":  svc.opts.Repo.CommonDir,
		"runHere":    true,
		"paused":     false,
		"draining":   false,
		"next":       map[string]any{"task": "notes", "name": "Notes", "at": "2026-11-22T12:10:00Z"},
		"attention":  float64(3),
		"lastFailure": map[string]any{
			"task": "lint", "name": "Lint", "at": "2026-11-22T11:30:00Z", "message": "check failed: exit 1", "item": "2",
		},
		"notices": []any{
			map[string]any{"id": "item-1-ready", "kind": "ready", "task": "digest", "title": "Digest is ready for review", "body": "Wrote the digest."},
			map[string]any{"id": "item-2-needs-attention", "kind": "failed", "task": "lint", "title": "Lint failed", "body": "check failed: exit 1"},
			map[string]any{"id": "item-3-due", "kind": "due", "task": "notes", "title": "New task: Notes", "body": "New task \"Notes\" runs every 1h."},
		},
	}, raw, "a stale running record and a snoozed item are left out")

	// Ids stay put while items keep their state, so Desktop notifies once.
	again, err := svc.HostStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, status.Notices, again.Notices)

	until := hostNow.Add(time.Hour)
	require.NoError(t, svc.PauseUntil(context.Background(), until))
	paused, err := svc.HostStatus(context.Background())
	require.NoError(t, err)
	require.True(t, paused.Paused)
	require.Equal(t, until, paused.PausedUntil.UTC())
	require.Equal(t, until, paused.Next.At.UTC())
}
