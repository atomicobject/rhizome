package web

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/stretchr/testify/require"
)

// A run this runtime started that has not taken the run lock yet is still
// a run: drain reports it as dispatching and stop ends it.
func TestTaskHostReportsAndStopsARunItStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stopping a task run is not supported on Windows")
	}
	api := &fakeTasks{}
	s, _ := newTasksTestServer(api)
	hosts := &TaskHosts{}
	s.cfg.TaskHosts = hosts
	defer hosts.Hold()()
	child, err := hosts.Dispatch(func() (*appruntime.Detached, error) {
		return appruntime.StartDetachedTo(nil, "sleep", os.TempDir(), "30")
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tasks.StopProcess(context.Background(), child.PID, 0) })

	code, payload := serveTasks(t, s, http.MethodPost, "/api/tasks/host/drain", "")
	require.Equal(t, http.StatusOK, code)
	require.Nil(t, payload["running"])
	dispatching, ok := payload["dispatching"].(map[string]any)
	require.True(t, ok, "drain did not report the run-due it started: %v", payload)
	require.Equal(t, float64(child.PID), dispatching["pid"])

	code, payload = serveTasks(t, s, http.MethodPost, "/api/tasks/stop", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, map[string]any{"stopped": true}, payload, "stop left the run it started")
	select {
	case <-child.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("the started run survived stop")
	}
}

// Run now starts one run at a time from a runtime: a second request
// before the first run takes the run lock answers 409.
func TestRunNowStartsOneRunAtATime(t *testing.T) {
	api := &fakeTasks{}
	s, launches := newTasksTestServer(api)
	s.cfg.TaskHosts = &TaskHosts{}
	running := make(chan struct{})
	defer close(running)
	s.tasksLaunch = func(vault, item string) (*appruntime.Detached, error) {
		*launches = append(*launches, launch{vault, item})
		return &appruntime.Detached{PID: 1, Done: running}, nil
	}
	code, _ := serveTasks(t, s, http.MethodPost, "/api/tasks/tasks/digest/run", "")
	require.Equal(t, http.StatusOK, code)
	code, payload := serveTasks(t, s, http.MethodPost, "/api/tasks/tasks/digest/run", "")
	require.Equal(t, http.StatusConflict, code, payload)
	require.Len(t, *launches, 1)
}

// Inside a runtime the status report knows whether a Desktop host holds
// it: a scheduled task that could run is blocked by no-host until one does.
func TestTasksStatusReportsNoHost(t *testing.T) {
	api := &fakeTasks{report: tasks.Report{RunHere: true, Tasks: []tasks.TaskStatus{
		{ID: "a", Scheduled: true},
		{ID: "b", Scheduled: true, BlockedBy: tasks.BlockedOff},
		{ID: "local"},
	}}}
	s, _ := newTasksTestServer(api)
	blocked := func() []string {
		_, payload := serveTasks(t, s, http.MethodGet, "/api/tasks", "")
		var out []string
		for _, task := range payload["tasks"].([]any) {
			code, _ := task.(map[string]any)["blockedBy"].(string)
			out = append(out, code)
		}
		return out
	}
	require.Equal(t, []string{"", "off", ""}, blocked(), "without a host registry nothing is known")
	hosts := &TaskHosts{}
	s.cfg.TaskHosts = hosts
	require.Equal(t, []string{"no-host", "off", ""}, blocked())
	release := hosts.Hold()
	require.Equal(t, []string{"", "off", ""}, blocked())
	release()
	api.report.RunHere = false
	require.Equal(t, []string{"", "off", ""}, blocked(), "with tasks off here, not-run-here comes from the service")
}
