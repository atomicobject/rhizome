package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/stretchr/testify/require"
)

func controlServer(t *testing.T, control *RuntimeControl) *httptest.Server {
	t.Helper()
	s := &Server{cfg: Config{RuntimeControl: control}, mux: http.NewServeMux()}
	s.registerRuntimeControlRoutes()
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, token string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		appruntime.SetBearerToken(req, token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestRuntimeControlRoutesAnswer503WhenControlIsNotConfigured(t *testing.T) {
	srv := controlServer(t, nil)
	for _, path := range []string{appruntime.HealthPath, appruntime.ShutdownPath, appruntime.IndexJobsPath, appruntime.AgentOpsPathPrefix + "files"} {
		resp := do(t, http.MethodPost, srv.URL+path, "", "")
		if path == appruntime.HealthPath {
			resp = do(t, http.MethodGet, srv.URL+path, "", "")
		}
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, path)
	}
}

func TestRuntimeControlRequiresTokenExceptHealth(t *testing.T) {
	control := &RuntimeControl{
		Token:  "secret",
		Health: func() appruntime.Health { return appruntime.Health{InstanceID: "i", PID: 7} },
	}
	srv := controlServer(t, control)

	resp := do(t, http.MethodGet, srv.URL+appruntime.HealthPath, "", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var health appruntime.Health
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	require.Equal(t, 7, health.PID)

	resp = do(t, http.MethodPost, srv.URL+appruntime.ShutdownPath, "", "")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp = do(t, http.MethodPost, srv.URL+appruntime.ShutdownPath, "wrong", "")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp = do(t, http.MethodPost, srv.URL+appruntime.ShutdownPath, "secret", "")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "authorized but shutdown callback absent")
}

func TestRuntimeControlIndexJobLifecycleAndAgentOp(t *testing.T) {
	fake := lane.NewFake()
	touched := 0
	shutdownReason := ""
	control := &RuntimeControl{
		Token:    "secret",
		Touch:    func() { touched++ },
		Shutdown: func(reason string) { shutdownReason = reason },
		SubmitIndex: func(ctx context.Context) (lane.Handle, bool, error) {
			return fake.Submit(ctx, lane.Request{Kind: lane.KindExplicitIndex, Run: func(ctx context.Context, p lane.Reporter) error {
				p.Segment("code", 3, 3)
				return nil
			}})
		},
		LookupIndex: fake.Lookup,
		AgentOp: func(ctx context.Context, name string, req appruntime.AgentOpRequest) (agentapi.CallOutcome, error) {
			return agentapi.CallOutcome{OK: true, Payload: map[string]any{"op": name, "rw": req.ReadWrite, "session": req.SessionID}}, nil
		},
	}
	srv := controlServer(t, control)

	resp := do(t, http.MethodPost, srv.URL+appruntime.IndexJobsPath, "secret", "")
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var submitted appruntime.IndexJobResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&submitted))
	require.NotEmpty(t, submitted.JobID)

	resp = do(t, http.MethodGet, srv.URL+appruntime.IndexJobsPathPrefix+submitted.JobID+"/events", "secret", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	var kinds []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "event: ") {
			kinds = append(kinds, strings.TrimPrefix(line, "event: "))
		}
	}
	require.Equal(t, []string{"progress", "done"}, kinds)

	resp = do(t, http.MethodDelete, srv.URL+appruntime.IndexJobsPathPrefix+submitted.JobID, "secret", "")
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp = do(t, http.MethodGet, srv.URL+appruntime.IndexJobsPathPrefix+"missing/events", "secret", "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = do(t, http.MethodPost, srv.URL+appruntime.AgentOpsPathPrefix+"files", "secret", `{"input":{"inputs":["README.md"]},"readWrite":true,"sessionId":"s1"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var outcome agentapi.CallOutcome
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&outcome))
	require.True(t, outcome.OK)
	require.Equal(t, map[string]any{"op": "files", "rw": true, "session": "s1"}, outcome.Payload)

	resp = do(t, http.MethodPost, srv.URL+appruntime.ShutdownPath, "secret", `{"reason":"test"}`)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Equal(t, "test", shutdownReason)
	require.Equal(t, 6, touched, "every authorized control request counts as activity")
}

func TestRuntimeControlRejectsNonLoopbackHost(t *testing.T) {
	control := &RuntimeControl{Token: "secret", Shutdown: func(string) {}}
	srv := controlServer(t, control)
	req, err := http.NewRequest(http.MethodPost, srv.URL+appruntime.ShutdownPath, nil)
	require.NoError(t, err)
	req.Host = "evil.example.com"
	appruntime.SetBearerToken(req, "secret")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
