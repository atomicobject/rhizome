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
	report     tasks.Report
	statusErr  error
	runs       []tasksRunView
	detail     tasksRunDetail
	detailErr  error
	precheck   error
	actionErr  error
	calls      []string
	snoozeTill time.Time
}

func (f *fakeTasks) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeTasks) Status(context.Context) (tasks.Report, error) {
	f.record("status")
	return f.report, f.statusErr
}

func (f *fakeTasks) LiveState(context.Context) (tasksLiveState, error) {
	return tasksLiveState{Paused: true, Items: []tasks.Item{{ID: "4", Task: "docs-drift"}}, Recent: []tasks.RunRecord{}}, nil
}

func (f *fakeTasks) Runs(task string) ([]tasksRunView, error) {
	f.record("runs %s", task)
	return f.runs, nil
}

func (f *fakeTasks) RunDetail(id string, after int64) (tasksRunDetail, error) {
	f.record("detail %s %d", id, after)
	return f.detail, f.detailErr
}

func (f *fakeTasks) PrecheckRun(_ context.Context, itemID string) error {
	f.record("precheck %s", itemID)
	return f.precheck
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
		runs:   []tasksRunView{{RunRecord: tasks.RunRecord{Task: "docs-drift", Outcome: "ready"}, HasTranscript: true}},
		detail: tasksRunDetail{Events: []json.RawMessage{json.RawMessage(`{"id":1}`)}, Next: 42},
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
	for _, err := range []error{ErrTaskRunNotFound, fmt.Errorf("open run: %w", os.ErrNotExist)} {
		s, _ := newTasksTestServer(&fakeTasks{detailErr: err})
		code, payload := serveTasks(t, s, http.MethodGet, "/api/tasks/runs/"+taskRunID, "")
		require.Equal(t, http.StatusNotFound, code)
		require.NotEmpty(t, payload["error"])
	}
}

func TestTasksRunLaunchesDetachedAfterPrecheck(t *testing.T) {
	api := &fakeTasks{}
	s, launches := newTasksTestServer(api)
	before := time.Now().UTC().Truncate(time.Second)

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

func TestTasksStatusCachesMergeWarning(t *testing.T) {
	api := &fakeTasks{report: tasks.Report{MergeWarning: "first", Tasks: []tasks.TaskStatus{}}}
	s, _ := newTasksTestServer(api)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	s.tasksMergeWarning.now = func() time.Time { return now }

	_, payload := serveTasks(t, s, http.MethodGet, "/api/tasks", "")
	require.Equal(t, "first", payload["mergeWarning"])

	api.report.MergeWarning = ""
	now = now.Add(mergeWarningTTL - time.Second)
	_, payload = serveTasks(t, s, http.MethodGet, "/api/tasks", "")
	require.Equal(t, "first", payload["mergeWarning"], "a fresh cached warning wins")

	now = now.Add(time.Second)
	_, payload = serveTasks(t, s, http.MethodGet, "/api/tasks", "")
	require.NotContains(t, payload, "mergeWarning", "an expired warning is refreshed")
}
