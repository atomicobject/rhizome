package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/stretchr/testify/require"
)

// hostTasks serves a HostStatus the test can change while a stream reads it.
type hostTasks struct {
	*fakeTasks
	mu     sync.Mutex
	status tasks.HostStatus
}

func (h *hostTasks) HostStatus(context.Context) (tasks.HostStatus, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status, nil
}

func (h *hostTasks) set(fn func(*tasks.HostStatus)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(&h.status)
}

// machineTasks is a service with the per-machine setting.
type machineTasks struct {
	*fakeTasks
	runHere bool
}

func (m *machineTasks) RunHere(context.Context) (bool, error) { return m.runHere, nil }

func (m *machineTasks) SetRunHere(_ context.Context, on bool) error {
	m.runHere = on
	return nil
}

func readHostEvent(t *testing.T, events *bufio.Reader) (string, map[string]any) {
	t.Helper()
	var name string
	for {
		line, err := events.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload))
			return name, payload
		}
	}
}

func TestTaskHostStreamHoldsTheRuntimeAndSendsStatusChanges(t *testing.T) {
	api := &hostTasks{fakeTasks: &fakeTasks{}, status: tasks.HostStatus{Repository: "/repo", CommonDir: "/repo/.git", Notices: []tasks.HostNotice{}}}
	var held atomic.Int32
	hosts := &TaskHosts{}
	s := &Server{taskHostPoll: 10 * time.Millisecond, cfg: Config{
		VaultPath:      "/repo/vault",
		TasksOpener:    func(context.Context, string) (TasksAPI, error) { return api, nil },
		TaskHosts:      hosts,
		RuntimeControl: &RuntimeControl{Hold: func() func() { held.Add(1); return func() { held.Add(-1) } }},
	}}
	srv := httptest.NewServer(http.HandlerFunc(s.handleTasks))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/tasks/host", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	events := bufio.NewReader(resp.Body)

	name, status := readHostEvent(t, events)
	require.Equal(t, "status", name)
	require.Equal(t, "/repo", status["repository"])
	require.Equal(t, false, status["draining"])
	require.Equal(t, int32(1), held.Load(), "an open host stream keeps the runtime from idling")
	require.True(t, hosts.Dispatching())

	api.set(func(st *tasks.HostStatus) { st.Attention = 2 })
	_, status = readHostEvent(t, events)
	require.Equal(t, float64(2), status["attention"], "a change is sent without waiting for the resend")

	code, drained := serveTasks(t, s, http.MethodPost, "/api/tasks/host/drain", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, map[string]any{"running": nil}, drained)
	require.False(t, hosts.Dispatching(), "a drained runtime dispatches nothing")
	_, status = readHostEvent(t, events)
	require.Equal(t, true, status["draining"])

	cancel()
	require.Eventually(t, func() bool { return held.Load() == 0 }, 5*time.Second, 10*time.Millisecond, "closing the stream releases the runtime")
}

func TestTaskHostRoutes(t *testing.T) {
	api := &fakeTasks{stopped: true, host: tasks.HostStatus{Running: &tasks.HostRun{Task: "digest", Name: "Digest", RunID: taskRunID, Stage: "agent"}}}
	s, _ := newTasksTestServer(api)

	code, payload := serveTasks(t, s, http.MethodPost, "/api/tasks/host/drain", "")
	require.Equal(t, http.StatusServiceUnavailable, code, "no host registry, no hosting")
	require.Equal(t, "task hosting unavailable", payload["error"])

	s.cfg.TaskHosts = &TaskHosts{}
	code, payload = serveTasks(t, s, http.MethodPost, "/api/tasks/host/drain", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "digest", payload["running"].(map[string]any)["task"])
	require.True(t, s.cfg.TaskHosts.Draining())

	code, payload = serveTasks(t, s, http.MethodPost, "/api/tasks/stop", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, map[string]any{"stopped": true}, payload)
	require.Contains(t, api.calls, "stop 10s")

	api.calls = nil
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/pause", `{"until":"2026-11-23T09:00:00-05:00"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"pause until 2026-11-23T14:00:00Z"}, api.calls)
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/pause", `{"until":"tomorrow"}`)
	require.Equal(t, http.StatusBadRequest, code)

	code, payload = serveTasks(t, s, http.MethodPost, "/api/tasks/machine", `{"runHere":true}`)
	require.Equal(t, http.StatusNotImplemented, code, payload)
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/machine", `{}`)
	require.Equal(t, http.StatusBadRequest, code)
	machine := &machineTasks{fakeTasks: api}
	s.cfg.TasksOpener = func(context.Context, string) (TasksAPI, error) { return machine, nil }
	code, _ = serveTasks(t, s, http.MethodPost, "/api/tasks/machine", `{"runHere":true}`)
	require.Equal(t, http.StatusOK, code)
	require.True(t, machine.runHere)
}
