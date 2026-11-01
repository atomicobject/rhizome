package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

const lookTask = "name: Look\nevery: 1d\nprompt: Look around.\n"

// crashedRun leaves state as a process that died mid-run would: a Running
// record, its item running, and a transcript in its folder.
func crashedRun(t *testing.T, svc *Service, task string) (runID, itemID string) {
	t.Helper()
	runID = newRunID(time.Now())
	if err := svc.Store().Update(context.Background(), func(st *State) error {
		st.Seen = map[string]time.Time{task: time.Now()}
		itemID = st.AddItem(Item{Task: task, Kind: KindTask, State: StateRunning, Run: runID}, time.Now()).ID
		st.Running = &RunRecord{ID: runID, Task: task, StartedAt: time.Now(), Stage: StageAgent, Item: itemID}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, svc.runFile(runID, transcriptFile), `{"id":1,"type":"text"}`+"\n")
	return runID, itemID
}

func TestInterruptedRunIsRecordedOnceInEveryPath(t *testing.T) {
	paths := map[string]func(*testing.T, *Service, string) error{
		"run-due": func(t *testing.T, svc *Service, _ string) error {
			_, err := svc.RunDue(context.Background())
			return err
		},
		"run": func(t *testing.T, svc *Service, _ string) error {
			var due string
			if err := svc.Store().Update(context.Background(), func(st *State) error {
				due = st.AddItem(Item{Task: "look", Kind: KindTask, State: StateDue}, time.Now()).ID
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			_, err := svc.Run(context.Background(), due)
			return err
		},
		"draft": func(t *testing.T, svc *Service, dir string) error {
			draft := filepath.Join(dir, "draft.yml")
			writeFile(t, draft, "name: Draft\nevery: 1d\nprompt: Try it.\n")
			_, err := svc.RunDraft(context.Background(), draft)
			return err
		},
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			dir := newRepo(t, map[string]string{"look.yml": lookTask})
			agent := &fakeAgent{}
			svc := newService(t, dir, agent, "auto")
			runID, itemID := crashedRun(t, svc, "look")
			if err := path(t, svc, dir); err != nil {
				t.Fatal(err)
			}
			st := mustState(t, svc)
			var interrupted []RunRecord
			for _, r := range st.History {
				if r.Item == itemID && r.Outcome != OutcomeInterrupted {
					t.Errorf("the crashed item got a second record: %+v", r)
				}
				if r.Outcome == OutcomeInterrupted {
					interrupted = append(interrupted, r)
				}
			}
			if len(interrupted) != 1 || interrupted[0].ID != runID || interrupted[0].Item != itemID || interrupted[0].Stage != "" {
				t.Fatalf("expected one interrupted record for %s, got %+v", runID, interrupted)
			}
			if st.Running != nil {
				t.Fatalf("Running should be cleared, got %+v", st.Running)
			}
			if it, _ := st.Item(itemID); it.State != StateAttention || !strings.Contains(it.Reason, "interrupted") {
				t.Fatalf("the crashed item should need attention: %+v", it)
			}
			if !fileExists(svc.runFile(runID, transcriptFile)) {
				t.Fatal("the interrupted run keeps its transcript")
			}
			if name == "run-due" && agent.calls() != 0 {
				t.Fatal("the interrupted run counts for the cadence")
			}
		})
	}
}

func TestSkipDuringALiveRunKeepsRunning(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	svc := newService(t, dir, nil, "notify")
	ctx := context.Background()
	live := RunRecord{ID: newRunID(time.Now()), Task: "other", Stage: StageCheck}
	var due string
	if err := svc.Store().Update(ctx, func(st *State) error {
		st.Running = &live
		due = st.AddItem(Item{Task: "look", Kind: KindTask, State: StateDue}, time.Now()).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Skip(ctx, due); err != nil {
		t.Fatal(err)
	}
	if st := mustState(t, svc); st.Running == nil || st.Running.ID != live.ID {
		t.Fatalf("a skip must not clear another run's Running: %+v", st.Running)
	}
}

func TestRetentionIsPerTaskAndKeepsTranscripts(t *testing.T) {
	dir := newRepo(t, nil)
	svc := newService(t, dir, nil, "notify")
	now := time.Now().UTC()
	svc.opts.Now = func() time.Time { return now }
	base := now.Add(-200 * time.Hour)
	var transcriptIDs []string
	var st State
	st.Record(RunRecord{Task: "weekly", At: base, Outcome: OutcomeNotNeeded})
	// 25 agent runs, then 150 hourly no-op checks that each wrote a log.
	for i := 0; i < 175; i++ {
		at := base.Add(time.Duration(i+1) * time.Hour)
		r := RunRecord{ID: newRunID(at), Task: "hourly", At: at, Outcome: OutcomeNotNeeded}
		if i < 25 {
			r.Outcome = OutcomeReady
			writeFile(t, svc.runFile(r.ID, transcriptFile), "{}\n")
			transcriptIDs = append(transcriptIDs, r.ID)
		}
		writeFile(t, svc.runFile(r.ID, checkLogFile), "noise\n")
		st.Record(r)
	}
	if err := svc.Store().Update(context.Background(), func(s *State) error { *s = st; return nil }); err != nil {
		t.Fatal(err)
	}
	oldDraft := newRunID(now.Add(-8 * 24 * time.Hour))
	youngDraft := newRunID(now.Add(-time.Hour))
	empty := newRunID(now)
	writeFile(t, svc.runFile(oldDraft, checkLogFile), "x")
	writeFile(t, svc.runFile(youngDraft, checkLogFile), "x")
	if err := os.MkdirAll(filepath.Dir(svc.runFile(empty, checkLogFile)), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(svc.stateDir, "logs", "old.log"), "x")

	svc.retain(context.Background())

	got := mustState(t, svc)
	var withTranscript, others int
	for _, r := range got.History {
		if r.Task != "hourly" {
			continue
		}
		if fileExists(svc.runFile(r.ID, transcriptFile)) {
			withTranscript++
		} else {
			others++
		}
	}
	if withTranscript != keepTranscripts || others != keepOtherRuns {
		t.Fatalf("expected %d runs with transcripts and %d others, got %d and %d", keepTranscripts, keepOtherRuns, withTranscript, others)
	}
	for i, id := range transcriptIDs {
		if kept := fileExists(svc.runFile(id, transcriptFile)); kept != (i >= 5) {
			t.Errorf("transcript %d kept=%v; only the newest 20 stay", i, kept)
		}
	}
	logs := 0
	for _, r := range got.History {
		if fileExists(svc.runFile(r.ID, checkLogFile)) {
			logs++
		}
	}
	if logs != keepCheckLogs {
		t.Errorf("expected %d check logs, got %d", keepCheckLogs, logs)
	}
	if last, ok := got.LastRun("weekly"); !ok || !last.At.Equal(base) {
		t.Errorf("a quiet task's last run must survive trimming, got %+v %v", last, ok)
	}
	for id, want := range map[string]bool{oldDraft: false, youngDraft: true, empty: false} {
		if _, err := os.Stat(filepath.Join(svc.stateDir, runsDir, id)); (err == nil) != want {
			t.Errorf("folder %s exists=%v, want %v", id, err == nil, want)
		}
	}
	if _, err := os.Stat(filepath.Join(svc.stateDir, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the phase 1 logs folder should be removed")
	}
}

func TestTokenBucketsSeedOnceAndSurviveTrimming(t *testing.T) {
	now := time.Now().UTC()
	st := State{History: []RunRecord{
		{Task: "a", At: now.Add(-40 * 24 * time.Hour), Tokens: 1000},
		{Task: "a", At: now.Add(-48 * time.Hour), Tokens: 30},
	}}
	if got := st.TokensLast30Days("a", now); got != 30 {
		t.Fatalf("unseeded state sums history from the last 30 days: %d", got)
	}
	st.Record(RunRecord{Task: "a", At: now, Tokens: 12})
	st.History = append(st.History, RunRecord{Task: "a", At: now, Tokens: 5000}) // not via Record
	st.Record(RunRecord{Task: "a", At: now, Tokens: 0})
	st.History = nil
	st.trimTokens(now)
	if got := st.TokensLast30Days("a", now); got != 42 {
		t.Fatalf("buckets seed once and outlive history: got %d, want 42", got)
	}
	if len(st.Tokens["a"]) != 2 {
		t.Fatalf("buckets older than 30 days are trimmed: %+v", st.Tokens)
	}
}

func TestDraftRunsDoNotAdvanceTheClock(t *testing.T) {
	dir := newRepo(t, nil)
	var transcript string
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		transcript = req.Transcript
		writeFile(t, filepath.Join(req.Dir, "note.md"), "x\n")
		return AgentResult{Message: "Wrote a note.", Tokens: 50}, nil
	}}
	svc := newService(t, dir, agent, "auto")
	draft := filepath.Join(dir, "draft.yml")
	writeFile(t, draft, "name: Draft\nevery: 1d\nprompt: Try it.\n")
	out, err := svc.RunDraft(context.Background(), draft)
	if err != nil || out.Outcome != OutcomeReady {
		t.Fatalf("draft run: %+v %v", out, err)
	}
	st := mustState(t, svc)
	if len(st.History) != 0 || len(st.Last) != 0 || st.TokensLast30Days("draft", time.Now()) != 0 || st.Running != nil {
		t.Fatalf("a draft must not be recorded: %+v", st)
	}
	items, _ := svc.Inbox()
	if len(items) != 1 || items[0].Run == "" || items[0].State != StateReady {
		t.Fatalf("the draft's item stays in the inbox: %+v", items)
	}
	if transcript != svc.runFile(items[0].Run, transcriptFile) {
		t.Fatalf("the draft's transcript goes to its run folder, got %q", transcript)
	}
	if _, err := svc.RunDetail(items[0].Run, 0); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("drafts are not listed: %v", err)
	}
}

func TestRunRecordKeepsItsOwnWorkAndTranscript(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"tidy.yml": checkingTask})
	commitFile(t, dir, "check.sh", "echo checking >&2\n"+checkScript, "add check")
	agent := editingAgent()
	edit := agent.run
	agent.run = func(req AgentRequest) (AgentResult, error) {
		f, err := os.OpenFile(req.Transcript, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return AgentResult{}, err
		}
		_, _ = f.WriteString(`{"id":1,"type":"text"}` + "\n" + `{"id":2,"type":"final-message"}` + "\n" + `{"id":3,"ty`)
		_ = f.Close()
		return edit(req)
	}
	svc := newService(t, dir, agent, "auto")
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, svc)
	r := st.Last["tidy"]
	it, _ := st.Item(r.Item)
	if r.Outcome != OutcomeReady || r.StartedAt.IsZero() || r.Branch != it.Branch || r.Worktree != it.Worktree || r.Commit == "" || len(r.Changed) != 1 || it.Run != r.ID || st.Running != nil {
		t.Fatalf("the record should carry its own work: %+v item %+v", r, it)
	}
	runs, err := svc.Runs("tidy")
	if err != nil || len(runs) != 1 || !runs[0].HasTranscript || !runs[0].HasCheckLog {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	detail, err := svc.RunDetail(r.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 2 || detail.CheckLog != "checking\n" {
		t.Fatalf("detail drops the partial line and carries the log: %+v", detail)
	}
	more, err := svc.RunDetail(r.ID, detail.Next)
	if err != nil || len(more.Events) != 0 || more.Next != detail.Next || more.CheckLog != "" {
		t.Fatalf("resuming from next returns nothing new: %+v %v", more, err)
	}
}

func TestFailedRunRecordsItsWorktree(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	svc := newService(t, dir, &fakeAgent{run: func(AgentRequest) (AgentResult, error) {
		return AgentResult{Tokens: 9}, errors.New("boom")
	}}, "auto")
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, svc)
	r := st.Last["look"]
	it, _ := st.Item(r.Item)
	if r.Outcome != OutcomeFailed || r.Worktree == "" || r.Worktree != it.Worktree || r.Branch != it.Branch || r.Tokens != 9 {
		t.Fatalf("a failed run keeps its worktree as fields: %+v item %+v", r, it)
	}
}

func TestLaunchErrorsAreRecordedAndCleared(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	svc := newService(t, dir, &fakeAgent{}, "notify")
	ctx := context.Background()
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	item := taskItems(mustState(t, svc), "look")[0].ID
	if err := svc.PrecheckRun(ctx, item); err != nil {
		t.Fatalf("a due item in a trusted, idle repository passes: %v", err)
	}
	release, err := svc.lockRun(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.PrecheckRun(ctx, item); !errors.Is(err, ErrBusy) {
		t.Fatalf("precheck reports a busy lock: %v", err)
	}
	_, err = svc.Run(ctx, item)
	if !errors.Is(err, ErrBusy) || !NotStarted(err) || err.Error() != ErrBusy.Error() {
		t.Fatalf("a busy run did not start: %v", err)
	}
	if err := svc.RecordLaunchError(ctx, item, err.Error()); err != nil {
		t.Fatal(err)
	}
	if it, _ := mustStatePtr(t, svc).Item(item); it.LaunchError != ErrBusy.Error() {
		t.Fatalf("launch error not saved: %+v", it)
	}
	_ = release()
	if out, err := svc.Run(ctx, item); err != nil || NotStarted(err) || out.Outcome != OutcomeNoChanges {
		t.Fatalf("run: %+v %v", out, err)
	}
	if it, _ := mustStatePtr(t, svc).Item(item); it.LaunchError != "" {
		t.Fatalf("the next state change clears the launch error: %+v", it)
	}
	if err := svc.PrecheckRun(ctx, item); err == nil || !strings.Contains(err.Error(), "only a due or needs-attention") {
		t.Fatalf("a finished item cannot be run: %v", err)
	}
	var due string
	if err := svc.Store().Update(ctx, func(st *State) error {
		due = st.AddItem(Item{Task: "look", Kind: KindTask, State: StateDue}, time.Now()).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordLaunchError(ctx, due, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Skip(ctx, due); err != nil {
		t.Fatal(err)
	}
	if it, _ := mustStatePtr(t, svc).Item(due); it.LaunchError != "" {
		t.Fatalf("any state change clears the launch error: %+v", it)
	}
	svc.opts.Trusted = func(string) (bool, error) { return false, nil }
	if err := svc.PrecheckRun(ctx, item); err == nil || !strings.Contains(err.Error(), "rzm trust") {
		t.Fatalf("precheck reports an untrusted repository: %v", err)
	}
}

func TestCheckLogCapDoesNotFailTheCheck(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"noisy.yml": "name: Noisy\nevery: 1d\ncheck:\n  command: [sh, noisy.sh]\nprompt: x\n"})
	commitFile(t, dir, "noisy.sh", "yes x | head -c 1100000 >&2\nprintf '{\"needed\": false}'\n", "add check")
	svc := newService(t, dir, &fakeAgent{}, "auto")
	summary, err := svc.RunDue(context.Background())
	if err != nil || len(summary.Outcomes) != 1 || summary.Outcomes[0].Outcome != OutcomeNotNeeded {
		t.Fatalf("a noisy check still passes: %+v %v", summary, err)
	}
	r := mustState(t, svc).Last["noisy"]
	data, err := os.ReadFile(svc.runFile(r.ID, checkLogFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != maxCheckLog+len(checkLogCut) || !strings.HasSuffix(string(data), checkLogCut) {
		t.Fatalf("check log is %d bytes, want the cap and the marker", len(data))
	}
}

func TestRunIDsAreValidatedBeforeUse(t *testing.T) {
	if id := newRunID(time.Now()); !ValidRunID(id) {
		t.Fatalf("minted ID %q is invalid", id)
	}
	svc := newService(t, newRepo(t, nil), nil, "notify")
	for _, id := range []string{"", "../../etc", "20261031T120000Z-abcdef0/../x", "20261031T120000Z-ABCDEF01", "20261031T120000Z-abcdef012"} {
		if ValidRunID(id) {
			t.Errorf("%q should be invalid", id)
		}
		if _, err := svc.runDir(id); !errors.Is(err, ErrInvalidRunID) {
			t.Errorf("runDir(%q) = %v", id, err)
		}
		if _, err := svc.RunDetail(id, 0); !errors.Is(err, ErrInvalidRunID) {
			t.Errorf("RunDetail(%q) = %v", id, err)
		}
	}
}

func TestLiveStateDetectsAGoneRunHolder(t *testing.T) {
	dir := newRepo(t, map[string]string{"look.yml": lookTask})
	svc := newService(t, dir, nil, "notify")
	ctx := context.Background()
	if live, err := svc.LiveState(ctx); err != nil || live.Running != nil || live.Stale {
		t.Fatalf("idle: %+v %v", live, err)
	}
	crashedRun(t, svc, "look")
	if live, _ := svc.LiveState(ctx); !live.Stale || live.Running == nil || len(live.Items) != 1 {
		t.Fatalf("no lock holder means stale: %+v", live)
	}
	release, err := svc.lockRun(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if live, _ := svc.LiveState(ctx); live.Stale {
		t.Fatal("a live holder is not stale")
	}
	_ = release()
	identity, err := indexlock.RuntimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for name, holder := range map[string]indexlock.LockData{
		"dead pid":      {PID: 1 << 30, Runtime: identity},
		"other runtime": {PID: os.Getpid(), Runtime: "elsewhere:boot-1"},
	} {
		data, _ := json.Marshal(holder)
		writeFile(t, filepath.Join(svc.stateDir, runLock), string(data))
		if live, _ := svc.LiveState(ctx); !live.Stale {
			t.Errorf("%s: should be stale", name)
		}
	}
}
