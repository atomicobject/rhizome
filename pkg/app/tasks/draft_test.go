package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalDefinitionsKeepScheduledVersions(t *testing.T) {
	dir := newRepo(t, map[string]string{"same.yml": "name: Same\nevery: 1d\nprompt: work\n", "changed.yml": "name: Scheduled\nevery: 1d\nprompt: old\n", "invalid.yml": "name: Valid scheduled\nevery: 1d\nprompt: work\n", "deleted.yml": "name: Deleted\nevery: 1d\nprompt: work\n"})
	writeFile(t, filepath.Join(dir, TasksDir, "changed.yml"), "name: Local\nevery: 2d\nprompt: new\n")
	writeFile(t, filepath.Join(dir, TasksDir, "invalid.yml"), "name: Broken\nevery: wrong\n")
	writeFile(t, filepath.Join(dir, TasksDir, "new.yml"), "name: New\nevery: 3d\nprompt: work\n")
	require.NoError(t, os.Remove(filepath.Join(dir, TasksDir, "deleted.yml")))
	svc := newService(t, dir, nil, "notify")
	report, err := svc.Status(context.Background())
	require.NoError(t, err)
	got := map[string]TaskStatus{}
	for _, task := range report.Tasks {
		got[task.ID] = task
	}
	require.Len(t, got, 5)
	require.True(t, got["same"].Scheduled)
	require.Equal(t, "same", got["same"].Local.State)
	require.Equal(t, "Scheduled", got["changed"].Name)
	require.Equal(t, "differs", got["changed"].Local.State)
	require.Equal(t, "Valid scheduled", got["invalid"].Name)
	require.Equal(t, "invalid", got["invalid"].Local.State)
	require.Nil(t, got["deleted"].Local)
	require.True(t, got["deleted"].Scheduled)
	require.False(t, got["new"].Scheduled)
	require.Equal(t, "new", got["new"].Local.State)
	require.ErrorContains(t, svc.SetMode(context.Background(), "new", "auto"), "not declared")
	_, err = svc.PrecheckTest(context.Background(), "invalid")
	require.Error(t, err)
}

func TestLocalDefinitionsWithoutPrimaryRef(t *testing.T) {
	dir := newRepo(t, nil)
	writeFile(t, filepath.Join(dir, TasksDir, "local.yml"), "name: Local\nevery: 1d\nprompt: work\n")
	svc := newService(t, dir, nil, "notify")
	svc.opts.Config.PrimaryRef = "missing"
	report, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, report.RefError)
	require.Len(t, report.Tasks, 1)
	require.False(t, report.Tasks[0].Scheduled)
}

func TestDraftSnapshotPreservesEffectiveSourceAndIndex(t *testing.T) {
	dir := newRepo(t, map[string]string{"local.yml": "name: Local\nevery: 1d\nprompt: check files\n"})
	commitFile(t, dir, "helper.txt", "original\n", "helper")
	commitFile(t, dir, "delete.txt", "remove\n", "delete fixture")
	commitFile(t, dir, ".gitignore", "ignored.txt\nvendor/\n", "ignore")
	writeFile(t, filepath.Join(dir, "helper.txt"), "staged\n")
	run(t, dir, "add", "helper.txt")
	writeFile(t, filepath.Join(dir, "helper.txt"), "unstaged\n")
	writeFile(t, filepath.Join(dir, "untracked.txt"), "new\n")
	writeFile(t, filepath.Join(dir, "ignored.txt"), "ignored\n")
	writeFile(t, filepath.Join(dir, "vendor", "tracked.txt"), "keep dependency\n")
	run(t, dir, "add", "-f", "vendor/tracked.txt")
	require.NoError(t, os.Remove(filepath.Join(dir, "delete.txt")))
	writeFile(t, filepath.Join(dir, "executable"), "#!/bin/sh\nexit 0\n")
	require.NoError(t, os.Chmod(filepath.Join(dir, "executable"), 0o755))
	require.NoError(t, os.Symlink("helper.txt", filepath.Join(dir, "link")))
	index := run(t, dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	before, err := os.ReadFile(index)
	require.NoError(t, err)
	head := run(t, dir, "rev-parse", "HEAD")
	var received AgentRequest
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		received = req
		for name, want := range map[string]string{"helper.txt": "unstaged\n", "untracked.txt": "new\n", "vendor/tracked.txt": "keep dependency\n"} {
			data, err := os.ReadFile(filepath.Join(req.Dir, name))
			require.NoError(t, err)
			require.Equal(t, want, string(data))
		}
		_, err := os.Stat(filepath.Join(req.Dir, "ignored.txt"))
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = os.Stat(filepath.Join(req.Dir, "delete.txt"))
		require.ErrorIs(t, err, os.ErrNotExist)
		target, err := os.Readlink(filepath.Join(req.Dir, "link"))
		require.NoError(t, err)
		require.Equal(t, "helper.txt", target)
		info, err := os.Stat(filepath.Join(req.Dir, "executable"))
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&0o111)
		require.Contains(t, req.Env, "RZM_TASK_TEST=1")
		require.Equal(t, "HEAD", run(t, req.Dir, "rev-parse", "--abbrev-ref", "HEAD"))
		return AgentResult{Message: "No changes", Tokens: 77}, nil
	}}
	svc := newService(t, dir, agent, "auto")
	out, err := svc.RunDraft(context.Background(), filepath.Join(dir, TasksDir, "local.yml"))
	require.NoError(t, err)
	require.Equal(t, OutcomeNoChanges, out.Outcome)
	after, err := os.ReadFile(index)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, head, run(t, dir, "rev-parse", "HEAD"))
	source, err := os.ReadFile(filepath.Join(dir, "helper.txt"))
	require.NoError(t, err)
	require.Equal(t, "unstaged\n", string(source))
	st := mustState(t, svc)
	require.Empty(t, st.History)
	require.Empty(t, st.Last)
	require.Empty(t, st.Items)
	require.Empty(t, st.Tokens)
	require.Len(t, st.Tests, 1)
	require.True(t, st.Tests[0].Test)
	require.Empty(t, st.Tests[0].Changed)
	require.Empty(t, st.Tests[0].Worktree)
	require.NotEqual(t, head, st.Tests[0].Commit)
	require.Equal(t, dir, st.Tests[0].SourceCheckout)
	require.Contains(t, received.Prompt, "without committing")
	detail, err := svc.RunDetail(st.Tests[0].ID, 0)
	require.NoError(t, err)
	require.True(t, detail.Run.Test)
}

func TestDraftCheckAndAgentShareFreshDataAndCapturedSource(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, nil)
	writeFile(t, filepath.Join(dir, TasksDir, "local.yml"), "name: Local\nevery: 1d\ncheck:\n  command: [sh, helper.sh]\nprompt: check data\n")
	writeFile(t, filepath.Join(dir, "helper.sh"), `test "$RZM_TASK_TEST" = 1 || exit 2
 test ! -f "$RZM_TASK_DATA_DIR/marker" || exit 3
 printf captured > "$RZM_TASK_DATA_DIR/marker"
 printf check-modification > from-check.txt
 printf '{"needed":true}'
`)
	var dataDirs []string
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		var dataDir string
		for _, entry := range req.Env {
			if strings.HasPrefix(entry, "RZM_TASK_DATA_DIR=") {
				dataDir = strings.TrimPrefix(entry, "RZM_TASK_DATA_DIR=")
			}
		}
		data, err := os.ReadFile(filepath.Join(dataDir, "marker"))
		require.NoError(t, err)
		require.Equal(t, "captured", string(data))
		dataDirs = append(dataDirs, dataDir)
		_, err = os.Stat(filepath.Join(req.Dir, "from-check.txt"))
		require.ErrorIs(t, err, os.ErrNotExist)
		writeFile(t, filepath.Join(req.Dir, "output.txt"), "new work\n")
		return AgentResult{Message: "Created output"}, nil
	}}
	svc := newService(t, dir, agent, "auto")
	scheduledData := filepath.Join(svc.stateDir, "data", "local", "marker")
	writeFile(t, scheduledData, "real state")
	for range 2 {
		out, err := svc.RunDraft(context.Background(), filepath.Join(dir, TasksDir, "local.yml"))
		require.NoError(t, err)
		require.Equal(t, OutcomeReady, out.Outcome)
	}
	require.Len(t, dataDirs, 2)
	require.NotEqual(t, dataDirs[0], dataDirs[1])
	bytes, err := os.ReadFile(scheduledData)
	require.NoError(t, err)
	require.Equal(t, "real state", string(bytes))
	st := mustState(t, svc)
	require.Len(t, st.Tests, 2)
	require.Empty(t, st.Tests[0].Worktree)
	require.NotEmpty(t, st.Tests[1].Worktree)
	require.Equal(t, []string{"output.txt"}, st.Tests[1].Changed)
	require.Empty(t, st.Tests[1].Branch)
	require.Empty(t, st.Items)
}

func TestDraftFailuresAndCheckOnlyAreVisible(t *testing.T) {
	shellAvailable(t)
	for _, tc := range []struct{ name, body, outcome string }{
		{"invalid", "name: Local\nevery: invalid\nprompt: bad\n", OutcomeFailed},
		{"failed", "name: Local\nevery: 1d\ncheck:\n  command: [sh, -c, 'exit 1']\n", OutcomeFailed},
		{"needed", "name: Local\nevery: 1d\ncheck:\n  command:\n    - sh\n    - -c\n    - printf '%s' '{\"needed\":true}'\n", OutcomeNotified},
		{"not-needed", "name: Local\nevery: 1d\ncheck:\n  command:\n    - sh\n    - -c\n    - printf '%s' '{\"needed\":false}'\n", OutcomeNotNeeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t, nil)
			writeFile(t, filepath.Join(dir, "local.yml"), tc.body)
			svc := newService(t, dir, nil, "notify")
			out, err := svc.RunDraft(context.Background(), filepath.Join(dir, "local.yml"))
			require.NoError(t, err)
			require.Equal(t, tc.outcome, out.Outcome)
			st := mustState(t, svc)
			require.Len(t, st.Tests, 1)
			require.Empty(t, st.History)
			require.Nil(t, st.Running)
			live, err := svc.LiveState(context.Background())
			require.NoError(t, err)
			require.Len(t, live.Recent, 1)
		})
	}
}

func TestDraftRejectsForeignRepository(t *testing.T) {
	dir := newRepo(t, nil)
	foreign := newRepo(t, nil)
	writeFile(t, filepath.Join(foreign, "local.yml"), "name: Local\nevery: 1d\nprompt: task\n")
	svc := newService(t, dir, nil, "notify")
	_, err := svc.RunDraft(context.Background(), filepath.Join(foreign, "local.yml"))
	require.ErrorContains(t, err, "different repository")
	require.Empty(t, mustState(t, svc).Tests)
}

func TestDraftHistoriesAreScopedAndRetained(t *testing.T) {
	dir := newRepo(t, nil)
	svc := newService(t, dir, nil, "notify")
	now := time.Now()
	svc.opts.Now = func() time.Time { return now }
	source := svc.opts.Checkout
	require.NoError(t, svc.store.Update(context.Background(), func(st *State) error {
		for i := range 12 {
			r := RunRecord{ID: newRunID(now.AddDate(0, 0, -30+i)), Task: "local", Test: true, SourceCheckout: source, At: now.AddDate(0, 0, -30+i)}
			st.Record(r)
			writeFile(t, svc.runFile(r.ID, checkLogFile), "log")
		}
		st.Record(RunRecord{ID: newRunID(now), Task: "local", Test: true, SourceCheckout: "elsewhere", At: now})
		return nil
	}))
	svc.retain(context.Background())
	st := mustState(t, svc)
	require.Len(t, st.Tests, 11)
	require.Empty(t, st.History)
	require.Empty(t, st.Last)
	runs, err := svc.Runs("local")
	require.NoError(t, err)
	require.Len(t, runs, 10)
	for _, r := range runs {
		require.True(t, r.HasCheckLog)
	}
	_, err = svc.RunDetail(st.Tests[len(st.Tests)-1].ID, 0)
	require.ErrorIs(t, err, ErrRunNotFound)
}

func TestInterruptedDraftDoesNotAffectScheduledState(t *testing.T) {
	dir := newRepo(t, nil)
	svc := newService(t, dir, nil, "notify")
	a := svc.newAttempt(true)
	require.NoError(t, svc.begin(context.Background(), a, a.record("local", "", time.Time{}), nil))
	require.NoError(t, svc.finalizeInterrupted(context.Background()))
	st := mustState(t, svc)
	require.Len(t, st.Tests, 1)
	require.Equal(t, OutcomeInterrupted, st.Tests[0].Outcome)
	require.Empty(t, st.History)
	require.Empty(t, st.Last)
}

func TestCodeModeDraftRequiresExistingIndex(t *testing.T) {
	dir := newRepo(t, nil)
	require.ErrorContains(t, requireTestIndex(dir), "existing index")
	writeFile(t, filepath.Join(dir, ".rhizome", "db.sqlite"), "fixture")
	require.NoError(t, requireTestIndex(dir))
}

func TestDraftCodeModeUsesCapturedScriptAndSelectedIndex(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, nil)
	writeFile(t, filepath.Join(dir, ".rhizome", "db.sqlite"), "existing fixture")
	writeFile(t, filepath.Join(dir, TasksDir, "local.yml"), "name: Local\nevery: 1d\ncheck:\n  script: check.mjs\n")
	writeFile(t, filepath.Join(dir, "check.mjs"), "captured-script-v1")
	rzm := filepath.Join(t.TempDir(), "rzm")
	writeFile(t, rzm, `#!/bin/sh
read source
[ "$source" = captured-script-v1 ] || exit 4
[ "$RZM_TASK_TEST" = 1 ] || exit 5
[ "$RZM_RUNTIME_AUTOSTART" = 0 ] || exit 8
[ "$PWD" = "$RZM_TASK_MAIN_CHECKOUT" ] || exit 6
[ -f "$PWD/.rhizome/db.sqlite" ] || exit 7
printf '%s' '{"ok":true,"result":{"needed":false,"summary":"used selected index"}}'
`)
	require.NoError(t, os.Chmod(rzm, 0o755))
	svc := newService(t, dir, nil, "notify")
	svc.opts.Rzm = rzm
	out, err := svc.RunDraft(context.Background(), filepath.Join(dir, TasksDir, "local.yml"))
	require.NoError(t, err)
	require.Equal(t, OutcomeNotNeeded, out.Outcome)
	require.Equal(t, "used selected index", out.Reason)
}

func TestLocalTaskTestsUseTheSelectedWorktree(t *testing.T) {
	dir := newRepo(t, map[string]string{"local.yml": "name: Scheduled\nevery: 1d\nprompt: task\n"})
	other := filepath.Join(t.TempDir(), "other")
	run(t, dir, "worktree", "add", "--quiet", "-b", "other", other)
	other, err := filepath.EvalSymlinks(other)
	require.NoError(t, err)
	writeFile(t, filepath.Join(other, TasksDir, "local.yml"), "name: Other checkout\nevery: 1d\nprompt: task\n")
	svc := newService(t, dir, nil, "notify")
	second := newService(t, dir, nil, "notify")
	second.opts.Checkout = other
	source, err := second.PrecheckTest(context.Background(), "local")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(other, TasksDir, "local.yml"), source.File)
	require.Equal(t, other, source.Checkout)
	out, err := second.RunDraft(context.Background(), source.File)
	require.NoError(t, err)
	require.Equal(t, OutcomeFailed, out.Outcome)
	firstRuns, err := svc.Runs("local")
	require.NoError(t, err)
	require.Empty(t, firstRuns)
	secondRuns, err := second.Runs("local")
	require.NoError(t, err)
	require.Len(t, secondRuns, 1)
	require.Equal(t, other, secondRuns[0].SourceCheckout)
}
