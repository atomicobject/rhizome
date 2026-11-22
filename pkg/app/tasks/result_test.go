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
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// notifyFixture is a Service in auto mode whose user configuration runs a
// notify script that appends its stdin and environment to sent.
type notifyFixture struct {
	svc    *Service
	dir    string
	config string
	sent   string
	script string
}

func newNotifyFixture(t *testing.T, files map[string]string, agent AgentRunner, notifyBody string) notifyFixture {
	t.Helper()
	shellAvailable(t)
	dir := newRepo(t, files)
	repo, err := OpenRepo(context.Background(), dir)
	require.NoError(t, err)
	scratch := t.TempDir()
	f := notifyFixture{dir: dir, config: filepath.Join(scratch, "config.yml"), sent: filepath.Join(scratch, "sent.txt"), script: filepath.Join(scratch, "notify.sh")}
	if notifyBody == "" {
		notifyBody = `{ cat; echo "ID=$RZM_TASK_ID OUTCOME=$RZM_TASK_OUTCOME RUN=$RZM_TASK_RUN_ID ITEM=$RZM_TASK_ITEM REPO=$RZM_TASK_REPOSITORY"; } >> "$1"`
	}
	require.NoError(t, os.WriteFile(f.script, []byte(notifyBody+"\n"), 0o700))
	f.svc, err = New(Options{
		Repo:   repo,
		Config: repoconfig.LocalTasksConfig{PrimaryRef: "main", DefaultMode: "auto"},
		Agent:  agent,
		Env:    vaultconfig.Env{CLIConfigFile: f.config},
	})
	require.NoError(t, err)
	f.writeConfig(t, 0)
	return f
}

func (f notifyFixture) writeConfig(t *testing.T, tokenCap int64) {
	t.Helper()
	cfg := fmt.Sprintf("tasks:\n  notify:\n    command: [sh, %q, %q]\n", f.script, f.sent)
	if tokenCap > 0 {
		cfg += fmt.Sprintf("  repositories:\n    %q:\n      runHere: true\n      dailyTokenCap: %d\n", f.svc.opts.Repo.CommonDir, tokenCap)
	}
	require.NoError(t, os.WriteFile(f.config, []byte(cfg), 0o600))
}

func (f notifyFixture) notifications(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.sent)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func writeResult(t *testing.T, req AgentRequest, content string) {
	t.Helper()
	path := envValue(req.Env, "RZM_TASK_RESULT_FILE")
	require.NotEmpty(t, path, "the agent gets RZM_TASK_RESULT_FILE")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func activeItem(t *testing.T, svc *Service, task string) *Item {
	t.Helper()
	st := mustState(t, svc)
	return st.ActiveTaskItem(task)
}

func lastRun(t *testing.T, svc *Service, task string) RunRecord {
	t.Helper()
	st := mustState(t, svc)
	r, ok := st.LastRun(task)
	require.True(t, ok, "task %s ran", task)
	return r
}

func TestQuietRunsMakeNoItemAndUnpushedChangesStillNeedReview(t *testing.T) {
	agent := &fakeAgent{}
	agent.run = func(req AgentRequest) (AgentResult, error) {
		if req.Task == "changes" {
			writeFile(t, filepath.Join(req.Dir, "out.txt"), "work\n")
		}
		writeResult(t, req, `{"status": "quiet", "summary": "nothing new"}`)
		return AgentResult{Message: "done"}, nil
	}
	f := newNotifyFixture(t, map[string]string{
		"quiet.yml":   "name: Quiet\nevery: 1h\nprompt: p\n",
		"changes.yml": "name: Changes\nevery: 1h\nprompt: p\n",
	}, agent, "")
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)

	st := mustState(t, f.svc)
	require.Nil(t, st.ActiveTaskItem("quiet"), "a quiet run with no changes leaves no inbox item")
	quiet := lastRun(t, f.svc, "quiet")
	require.Equal(t, OutcomeNoChanges, quiet.Outcome)
	require.Equal(t, &RunResult{Status: ResultQuiet, Summary: "nothing new"}, quiet.Result)

	// A quiet run that committed to a local task branch still needs review.
	it := st.ActiveTaskItem("changes")
	require.NotNil(t, it)
	require.Equal(t, StateReady, it.State)
	sent := f.notifications(t)
	require.Equal(t, 1, strings.Count(sent, "OUTCOME="), sent)
	require.Contains(t, sent, "Changes has changes to review: nothing new\nID=changes OUTCOME=ready")
}

func TestAttentionRunMakesItemAndNotifies(t *testing.T) {
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		writeResult(t, req, `{"status": "attention", "summary": "the vault has a merge conflict"}`)
		return AgentResult{Message: "stopped"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "")
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)

	it := activeItem(t, f.svc, "a")
	require.NotNil(t, it)
	require.Equal(t, StateAttention, it.State)
	require.Equal(t, "the vault has a merge conflict", it.Reason)
	r := lastRun(t, f.svc, "a")
	require.Equal(t, fmt.Sprintf("A needs attention: the vault has a merge conflict\nID=a OUTCOME=attention RUN=%s ITEM=%s REPO=%s\n", r.ID, it.ID, f.dir), f.notifications(t))
	require.Empty(t, r.NotifyError)
}

func TestFailedRunNotifiesButTestsDoNot(t *testing.T) {
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		return AgentResult{}, errors.New("harness exploded")
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "")
	out, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, OutcomeFailed, out.Outcomes[0].Outcome)
	sent := f.notifications(t)
	require.Contains(t, sent, "A failed: prompt run failed: harness exploded")
	require.Contains(t, sent, "OUTCOME=failed")

	_, err = f.svc.RunDraft(context.Background(), filepath.Join(f.dir, TasksDir, "a.yml"))
	require.NoError(t, err)
	require.Equal(t, sent, f.notifications(t), "a test run never notifies")
}

func TestFailingNotifyCommandDoesNotFailTheRun(t *testing.T) {
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		writeResult(t, req, `{"status": "attention", "summary": "look"}`)
		return AgentResult{Message: "done"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "echo no route to phone >&2\nexit 3")
	out, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, OutcomeNoChanges, out.Outcomes[0].Outcome)

	r := lastRun(t, f.svc, "a")
	require.Equal(t, OutcomeNoChanges, r.Outcome)
	require.Contains(t, r.NotifyError, "exit status 3")
	log, err := os.ReadFile(f.svc.runFile(r.ID, notifyLogFile))
	require.NoError(t, err)
	require.Equal(t, "no route to phone\n", string(log))
	require.Equal(t, StateAttention, activeItem(t, f.svc, "a").State)
}

func TestMalformedResultFileCountsAsOK(t *testing.T) {
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		writeResult(t, req, `{"status": "loud"`)
		return AgentResult{Message: "done"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "")
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)

	r := lastRun(t, f.svc, "a")
	require.Equal(t, OutcomeNoChanges, r.Outcome)
	require.NotNil(t, r.Result)
	require.Empty(t, r.Result.Status)
	require.Contains(t, r.Result.Error, "not a JSON object")
	require.Nil(t, activeItem(t, f.svc, "a"))
	require.Empty(t, f.notifications(t))
}

func TestHandoffNoteIsNamedAndTrimmed(t *testing.T) {
	long := "# Handoff\n" + strings.Repeat("a line the next run may need\n", 1000)
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		path := filepath.Join(envValue(req.Env, "RZM_TASK_DATA_DIR"), "handoff.md")
		require.Contains(t, req.Prompt, path)
		require.NoError(t, os.WriteFile(path, []byte(long), 0o600))
		return AgentResult{Message: "done"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nmemory: true\nprompt: p\n"}, agent, "")
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(f.svc.locations(Definition{ID: "a"}, "").DataDir, "handoff.md"))
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), maxHandoff)
	require.True(t, strings.HasPrefix(string(data), "# Handoff\na line the next run may need\n"))
	require.True(t, strings.HasSuffix(string(data), "need"+handoffCut), "cut on a whole line, then the marker")
}

func TestTokenCapSkipsAgentWorkButNotTheCheck(t *testing.T) {
	agent := &fakeAgent{}
	f := newNotifyFixture(t, map[string]string{
		"a.yml": "name: A\nevery: 1h\nprompt: p\n",
		"b.yml": "name: B\nevery: 1h\ncheck:\n  command: [sh, check.sh]\nprompt: p\n",
		"n.yml": "name: N\nevery: 1h\nprompt: p\n",
	}, agent, "")
	commitFile(t, f.dir, "check.sh", `touch "$RZM_TASK_DATA_DIR/checked"; echo '{"needed": true}'`+"\n", "check")
	ctx := context.Background()
	require.NoError(t, f.svc.SetMode(ctx, "n", "notify"))
	f.writeConfig(t, 100)
	today := tokenDay(time.Now())
	require.NoError(t, f.svc.Store().Update(ctx, func(st *State) error {
		st.ScheduledTokens = map[string]int64{today: 100}
		return nil
	}))

	_, err := f.svc.RunDue(ctx)
	require.NoError(t, err)
	require.Zero(t, agent.calls(), "no agent work past the cap")
	for _, task := range []string{"a", "b"} {
		r := lastRun(t, f.svc, task)
		require.Equal(t, OutcomeSkipped, r.Outcome, task)
		require.Contains(t, r.Message, "token cap reached: 100 of 100 tokens", task)
	}
	_, err = os.Stat(filepath.Join(f.svc.locations(Definition{ID: "b"}, "").DataDir, "checked"))
	require.NoError(t, err, "the check still ran")
	require.Equal(t, OutcomeNotified, lastRun(t, f.svc, "n").Outcome, "notify mode makes no agent calls, so the cap does not stop it")
	sent := f.notifications(t)
	require.Equal(t, 1, strings.Count(sent, "OUTCOME=token-cap"), "one cap notice a day, not one per task: %s", sent)

	report, err := f.svc.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, &TokenCap{Cap: 100, Used: 100, Day: today, Reached: true}, report.TokenCap)
	blocked := map[string]string{}
	for _, ts := range report.Tasks {
		blocked[ts.ID] = ts.BlockedBy
	}
	require.Equal(t, BlockedTokenCap, blocked["a"])
	require.Equal(t, BlockedTokenCap, blocked["b"])
	require.NotEqual(t, BlockedTokenCap, blocked["n"])
}

func TestNotifyTestSendsASample(t *testing.T) {
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, nil, "")
	n, command, _, err := f.svc.NotifyTest(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"sh", f.script, f.sent}, command)
	require.Contains(t, f.notifications(t), "your notify command works.\nID=notify-test OUTCOME=test RUN="+n.Run)

	require.NoError(t, os.WriteFile(f.config, nil, 0o600))
	_, _, _, err = f.svc.NotifyTest(context.Background())
	require.ErrorContains(t, err, "no notify command is set")
}

func TestInPlaceRunsFollowTheQuietRules(t *testing.T) {
	status := `{"status": "quiet", "summary": "filed one note"}`
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		require.Contains(t, req.Prompt, envValue(req.Env, "RZM_TASK_RESULT_FILE"), "the in-place preamble names the result file")
		writeFile(t, filepath.Join(req.Dir, "notes", fmt.Sprintf("%d.md", time.Now().UnixNano())), "note\n")
		writeResult(t, req, status)
		return AgentResult{Message: "filed"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"file.yml": "name: File\nevery: 1h\nwrites: [\"notes/*.md\"]\nprompt: p\n"}, agent, "")
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, OutcomeLanded, lastRun(t, f.svc, "file").Outcome)
	require.Nil(t, activeItem(t, f.svc, "file"), "a quiet landed run leaves no inbox item")
	require.Empty(t, f.notifications(t), "and sends no alert")

	status = `{"status": "attention", "summary": "a note needs a person"}`
	later := time.Now().Add(2 * time.Hour)
	f.svc.opts.Now = func() time.Time { return later }
	_, err = f.svc.RunDue(context.Background())
	require.NoError(t, err)
	it := activeItem(t, f.svc, "file")
	require.NotNil(t, it)
	require.Equal(t, StateAttention, it.State)
	require.Equal(t, "a note needs a person", it.Reason)
	require.Contains(t, f.notifications(t), "OUTCOME=attention")
}

func TestAPassHandsItsAlertsToAnotherProcess(t *testing.T) {
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		writeResult(t, req, `{"status": "attention", "summary": "look at this"}`)
		return AgentResult{Message: "done"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "")
	handed := 0
	f.svc.opts.DeliverAlerts = func() error { handed++; return nil }
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, handed, "the pass starts a deliverer")
	require.Empty(t, f.notifications(t), "the pass itself runs no notify command, so a slow one cannot keep it alive")
	require.Len(t, mustState(t, f.svc).PendingAlerts, 1)

	f.svc.DeliverAlerts(context.Background())
	require.Contains(t, f.notifications(t), "OUTCOME=attention")
	require.Empty(t, mustState(t, f.svc).PendingAlerts)
}
