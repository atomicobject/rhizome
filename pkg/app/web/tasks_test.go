package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/stretchr/testify/require"
)

const taskRunID = "20261031T183117Z-0123abcd"

type fakeTasks struct {
	testSource tasks.TestSource
	report     tasks.Report
	statusErr  error
	runs       []tasks.RunView
	detail     tasks.RunDetail
	detailErr  error
	precheck   error
	actionErr  error
	calls      []string
	snoozeTill time.Time
	prompt     string
}

func (f *fakeTasks) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeTasks) Status(context.Context) (tasks.Report, error) {
	f.record("status")
	return f.report, f.statusErr
}

func (f *fakeTasks) LiveState(context.Context) (tasks.LiveState, error) {
	return tasks.LiveState{Paused: true, Items: []tasks.Item{{ID: "4", Task: "docs-drift"}}, Recent: []tasks.RunRecord{}}, nil
}

func (f *fakeTasks) Runs(task string) ([]tasks.RunView, error) {
	f.record("runs %s", task)
	return f.runs, nil
}

func (f *fakeTasks) RunDetail(id string, after int64) (tasks.RunDetail, error) {
	f.record("detail %s %d", id, after)
	return f.detail, f.detailErr
}

func (f *fakeTasks) PrecheckRun(_ context.Context, itemID string) error {
	f.record("precheck %s", itemID)
	return f.precheck
}

func (f *fakeTasks) RequestRun(_ context.Context, task string) (string, error) {
	f.record("request %s", task)
	return "7", f.actionErr
}

func (f *fakeTasks) Snooze(_ context.Context, itemID string, until time.Time) error {
	f.record("snooze %s", itemID)
	f.snoozeTill = until
	return f.actionErr
}

func (f *fakeTasks) Skip(_ context.Context, itemID string) error {
	f.record("skip %s", itemID)
	return f.actionErr
}

func (f *fakeTasks) Discard(_ context.Context, itemID string) error {
	f.record("discard %s", itemID)
	return f.actionErr
}

func (f *fakeTasks) Done(_ context.Context, itemID string) error {
	f.record("done %s", itemID)
	return f.actionErr
}

func (f *fakeTasks) SetMode(_ context.Context, task, mode string) error {
	f.record("mode %s %s", task, mode)
	return f.actionErr
}

func (f *fakeTasks) SetPaused(_ context.Context, paused bool) error {
	f.record("paused %t", paused)
	return f.actionErr
}

func (f *fakeTasks) RecordLaunchError(_ context.Context, itemID, reason string) error {
	f.record("launch error %s %s", itemID, reason)
	return nil
}

func (f *fakeTasks) CopyPrompt(_ context.Context, itemID string) (string, error) {
	f.record("prompt %s", itemID)
	return f.prompt, f.actionErr
}

func (f *fakeTasks) PrecheckTest(_ context.Context, task string) (tasks.TestSource, error) {
	f.record("precheck test %s", task)
	if f.testSource.Checkout != "" {
		return f.testSource, f.precheck
	}
	return tasks.TestSource{File: "/repo/vault/.rhizome/tasks/" + task + ".yml", Checkout: "/repo/vault"}, f.precheck
}
func (f *fakeTasks) RecordTestLaunchError(_ context.Context, task, reason string) error {
	f.record("test launch error %s %s", task, reason)
	return nil
}

type launch struct{ vault, item string }

func newTasksTestServer(api *fakeTasks) (*Server, *[]launch) {
	launches := &[]launch{}
	s := &Server{cfg: Config{
		VaultPath:   "/repo/vault",
		TasksOpener: func(context.Context, string) (TasksAPI, error) { return api, nil },
	}}
	s.tasksLaunch = func(vault, item string) error {
		*launches = append(*launches, launch{vault, item})
		return nil
	}
	return s, launches
}

func serveTasks(t *testing.T, s *Server, method, target, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleTasks(rec, httptest.NewRequest(method, target, bytes.NewBufferString(body)))
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), rec.Body.String())
	return rec.Code, payload
}

func TestTasksRoutesAnswer503WhenUnconfigured(t *testing.T) {
	s := &Server{}
	for _, target := range []string{"/api/tasks", "/api/tasks/state", "/api/tasks/items/1/run"} {
		code, payload := serveTasks(t, s, http.MethodGet, target, "")
		require.Equal(t, http.StatusServiceUnavailable, code)
		require.Equal(t, "tasks unavailable", payload["error"])
	}
}

func TestTasksReadRoutes(t *testing.T) {
	api := &fakeTasks{
		report: tasks.Report{Paused: true, StateDir: "/state", Tasks: []tasks.TaskStatus{}},
		runs:   []tasks.RunView{{RunRecord: tasks.RunRecord{Task: "docs-drift", Outcome: "ready"}, HasTranscript: true}},
		detail: tasks.RunDetail{Events: []json.RawMessage{json.RawMessage(`{"id":1}`)}, Next: 42},
	}
	s, _ := newTasksTestServer(api)

	code, payload := serveTasks(t, s, http.MethodGet, "/api/tasks", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, payload["paused"])
	require.Equal(t, "/state", payload["stateDir"])

	code, payload = serveTasks(t, s, http.MethodGet, "/api/tasks/state", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, payload["paused"])
	require.Len(t, payload["items"], 1)
	require.Equal(t, []any{}, payload["recent"])

	code, payload = serveTasks(t, s, http.MethodGet, "/api/tasks/runs?task=docs-drift", "")
	require.Equal(t, http.StatusOK, code)
	runs := payload["runs"].([]any)
	require.Len(t, runs, 1)
	require.Equal(t, "docs-drift", runs[0].(map[string]any)["task"])
	require.Equal(t, true, runs[0].(map[string]any)["hasTranscript"])

	api.runs = nil
	_, payload = serveTasks(t, s, http.MethodGet, "/api/tasks/runs?task=docs-drift", "")
	require.Equal(t, []any{}, payload["runs"])

	code, payload = serveTasks(t, s, http.MethodGet, "/api/tasks/runs/"+taskRunID+"?after=7", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, float64(42), payload["next"])
	require.Equal(t, []any{map[string]any{"id": float64(1)}}, payload["events"])
	require.Contains(t, api.calls, "detail "+taskRunID+" 7")

	_, _ = serveTasks(t, s, http.MethodGet, "/api/tasks/runs/"+taskRunID, "")
	require.Contains(t, api.calls, "detail "+taskRunID+" 0")

	api.prompt = "Tidy the notes."
	code, payload = serveTasks(t, s, http.MethodGet, "/api/tasks/items/4/prompt", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "Tidy the notes.", payload["prompt"])
	require.Contains(t, api.calls, "prompt 4")

	api.actionErr = errors.New("item 4 is done; only an open task item has a prompt")
	code, _ = serveTasks(t, s, http.MethodGet, "/api/tasks/items/4/prompt", "")
	require.Equal(t, http.StatusBadRequest, code)
}

func TestTasksRouteValidation(t *testing.T) {
	api := &fakeTasks{}
	s, _ := newTasksTestServer(api)
	for _, tc := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodGet, "/api/tasks/runs/not-a-run", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/runs/20261031T183117Z-0123ABCD", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/runs/" + taskRunID + "?after=-1", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/runs/" + taskRunID + "?after=abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/runs", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/runs?task=../x", "", http.StatusBadRequest},
		{http.MethodPost, "/api/tasks/items/../run", "", http.StatusBadRequest},
		{http.MethodPost, "/api/tasks/items/abc/run", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/items/abc/prompt", "", http.StatusBadRequest},
		{http.MethodGet, "/api/tasks/items/4/run", "", http.StatusNotFound},
		{http.MethodPost, "/api/tasks/items/4/snooze", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/tasks/items/4/snooze", `{"until":"tomorrow"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/tasks/modes/Bad_Task", `{"mode":"auto"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/tasks/modes/docs-drift", `not json`, http.StatusBadRequest},
		{http.MethodPost, "/api/tasks/items/4/explode", "", http.StatusNotFound},
		{http.MethodPost, "/api/tasks/state", "", http.StatusNotFound},
		{http.MethodDelete, "/api/tasks", "", http.StatusMethodNotAllowed},
	} {
		code, payload := serveTasks(t, s, tc.method, tc.target, tc.body)
		require.Equal(t, tc.want, code, "%s %s", tc.method, tc.target)
		require.NotEmpty(t, payload["error"], "%s %s", tc.method, tc.target)
	}
	require.Empty(t, api.calls, "invalid requests must not reach the service")
}

func TestTasksRunDetailMissingRunIs404(t *testing.T) {
	for _, err := range []error{tasks.ErrRunNotFound, fmt.Errorf("open run: %w", os.ErrNotExist)} {
		s, _ := newTasksTestServer(&fakeTasks{detailErr: err})
		code, payload := serveTasks(t, s, http.MethodGet, "/api/tasks/runs/"+taskRunID, "")
		require.Equal(t, http.StatusNotFound, code)
		require.NotEmpty(t, payload["error"])
	}
}

func TestTasksRunLaunchesDetachedAfterPrecheck(t *testing.T) {
	api := &fakeTasks{}
	s, launches := newTasksTestServer(api)
	before := time.Now().UTC()

	code, payload := serveTasks(t, s, http.MethodPost, "/api/tasks/items/4/run", "")
	require.Equal(t, http.StatusOK, code)
	launchedAt, err := time.Parse(time.RFC3339, payload["launchedAt"].(string))
	require.NoError(t, err)
	require.False(t, launchedAt.Before(before))
	require.Equal(t, []launch{{"/repo/vault", "4"}}, *launches)
	require.Equal(t, []string{"precheck 4"}, api.calls)

	api.precheck = errors.New("item 4 is done")
	code, payload = serveTasks(t, s, http.MethodPost, "/api/tasks/items/4/run", "")
	require.Equal(t, http.StatusConflict, code)
	require.Equal(t, "item 4 is done", payload["error"])
	require.Len(t, *launches, 1, "a failed precheck must not spawn")

	api.precheck = nil
	api.calls = nil
	s.tasksLaunch = func(string, string) error { return errors.New("no rzm") }
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/items/4/run", "")
	require.Equal(t, http.StatusInternalServerError, code)
	require.Equal(t, []string{"precheck 4", "launch error 4 no rzm"}, api.calls, "a failed spawn is recorded on the item")
}

func TestTasksRunATaskWithoutAnItem(t *testing.T) {
	api := &fakeTasks{}
	s, launches := newTasksTestServer(api)
	code, payload := serveTasks(t, s, http.MethodPost, "/api/tasks/run/compounding%2Fmine", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "7", payload["item"])
	require.Equal(t, []launch{{"/repo/vault", "7"}}, *launches)
	require.Equal(t, []string{"request compounding/mine", "precheck 7"}, api.calls)

	api.actionErr = errors.New("task has no prompt")
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/run/compounding%2Fmine", "")
	require.Equal(t, http.StatusBadRequest, code)
	require.Len(t, *launches, 1, "a refused request must not spawn")
}

func TestTasksActions(t *testing.T) {
	api := &fakeTasks{}
	s, _ := newTasksTestServer(api)
	for _, tc := range []struct{ target, body, call string }{
		{"/api/tasks/items/4/snooze", `{"until":"2026-11-01T09:00:00Z"}`, "snooze 4"},
		{"/api/tasks/items/4/skip", "", "skip 4"},
		{"/api/tasks/items/4/discard", "", "discard 4"},
		{"/api/tasks/items/4/done", "", "done 4"},
		{"/api/tasks/modes/docs-drift", `{"mode":"notify"}`, "mode docs-drift notify"},
		{"/api/tasks/pause", "", "paused true"},
		{"/api/tasks/resume", "", "paused false"},
	} {
		api.calls = nil
		code, payload := serveTasks(t, s, http.MethodPost, tc.target, tc.body)
		require.Equal(t, http.StatusOK, code, tc.target)
		require.Empty(t, payload, tc.target)
		require.Equal(t, []string{tc.call}, api.calls)
	}
	require.Equal(t, time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), api.snoozeTill.UTC())

	api.actionErr = tasks.ErrBusy
	code, _ := serveTasks(t, s, http.MethodPost, "/api/tasks/items/4/discard", "")
	require.Equal(t, http.StatusConflict, code)
	api.actionErr = errors.New("item 4 is done; only a due item can be skipped")
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/items/4/skip", "")
	require.Equal(t, http.StatusBadRequest, code)
}

func TestTasksRoutesTakeAQualifiedTaskID(t *testing.T) {
	api := &fakeTasks{}
	s, _ := newTasksTestServer(api)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTasks)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/modes/compounding%2Fsweep", bytes.NewBufferString(`{"mode":"off"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks/runs?task=compounding%2Fsweep", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []string{"mode compounding/sweep off", "runs compounding/sweep"}, api.calls)

	code, _ := serveTasks(t, s, http.MethodPost, "/api/tasks/modes/a%2Fb%2Fc", `{"mode":"off"}`)
	require.Equal(t, http.StatusBadRequest, code, "only one slash is allowed")
}

func TestTasksLaunchLocalTestByID(t *testing.T) {
	api := &fakeTasks{testSource: tasks.TestSource{File: "/canonical/worktree/.rhizome/modules/compounding/tasks/mine.yml", Checkout: "/canonical/worktree"}}
	s, _ := newTasksTestServer(api)
	var gotVault, gotFile string
	s.tasksTestLaunch = func(vault, file string) error { gotVault, gotFile = vault, file; return nil }
	w := httptest.NewRecorder()
	s.handleTasks(w, httptest.NewRequest(http.MethodPost, "/api/tasks/tests/compounding%2Fmine", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "/repo/vault", gotVault)
	require.Equal(t, api.testSource.File, gotFile)
	require.Contains(t, api.calls, "precheck test compounding/mine")
	var response map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotEmpty(t, response["launchedAt"])
	require.Equal(t, "/canonical/worktree", response["sourceCheckout"])
}

func TestTasksTestRefusesBusyAndRecordsLaunchFailure(t *testing.T) {
	api := &fakeTasks{precheck: tasks.ErrBusy}
	s, _ := newTasksTestServer(api)
	s.tasksTestLaunch = func(string, string) error { t.Fatal("busy test launched"); return nil }
	w := httptest.NewRecorder()
	s.handleTasks(w, httptest.NewRequest(http.MethodPost, "/api/tasks/tests/local", nil))
	require.Equal(t, http.StatusConflict, w.Code)
	api.precheck = nil
	s.tasksTestLaunch = func(string, string) error { return errors.New("launch failed") }
	w = httptest.NewRecorder()
	s.handleTasks(w, httptest.NewRequest(http.MethodPost, "/api/tasks/tests/local", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, api.calls, "test launch error local launch failed")
}
