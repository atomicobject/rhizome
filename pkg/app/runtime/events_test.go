package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/stretchr/testify/require"
)

// sseServer replays a scripted event stream exactly as pkg/app/web writes it.
func sseServer(t *testing.T, events []lane.Event) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, ": connected\n\n")
		fmt.Fprint(w, ": heartbeat 1\n\n")
		flusher.Flush()
		for _, event := range events {
			data, err := json.Marshal(event)
			require.NoError(t, err)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return &Client{
		Manifest: InstanceManifest{HTTPURL: server.URL, ControlToken: "token"},
		HTTP:     &http.Client{},
	}
}

func TestStreamIndexJobDeliversEventsUntilDone(t *testing.T) {
	client := sseServer(t, []lane.Event{
		{Type: lane.EventProgress, Label: "Indexing", Done: 1, Total: 4},
		{Type: lane.EventLog, Line: "scanned 4 files"},
		{Type: lane.EventDone, Outcome: lane.OutcomeOK, OK: true, Summary: json.RawMessage(`"took 2s"`)},
		{Type: lane.EventLog, Line: "never delivered"},
	})

	var seen []lane.Event
	require.NoError(t, StreamIndexJob(t.Context(), client, "job-1", func(event lane.Event) error {
		seen = append(seen, event)
		return nil
	}))
	require.Len(t, seen, 3, "the stream stops at the terminal event")
	require.Equal(t, lane.EventDone, seen[2].Type)
	require.JSONEq(t, `"took 2s"`, string(seen[2].Summary))
}

func TestStreamIndexJobReportsAStreamThatEndsWithoutAResult(t *testing.T) {
	client := sseServer(t, []lane.Event{{Type: lane.EventProgress, Label: "Indexing"}})
	err := StreamIndexJob(t.Context(), client, "job-1", func(lane.Event) error { return nil })
	require.ErrorContains(t, err, "ended without a result")
}

func TestSubmitIndexJobReportsJobID(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	job, err := SubmitIndexJob(t.Context(), fake.client())
	require.NoError(t, err)
	require.Equal(t, "job-1", job.JobID)
}

func TestCancelIndexJobSendsDelete(t *testing.T) {
	var method, path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	client := &Client{Manifest: InstanceManifest{HTTPURL: server.URL, ControlToken: "t"}, HTTP: &http.Client{}}
	require.NoError(t, CancelIndexJob(t.Context(), client, "job-9"))
	require.Equal(t, http.MethodDelete, method)
	require.Equal(t, IndexJobsPathPrefix+"job-9", path)
}

func TestRequestShutdownWaitsForTheRuntimeToRelease(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	require.NoError(t, RequestShutdown(t.Context(), fake.client(), 5*time.Second))
	_, _, err := LiveManifest(t.Context(), vault)
	require.ErrorIs(t, err, ErrNoRuntime)
}

func TestRequestShutdownReportsARuntimeThatIgnoresIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	client := &Client{
		Manifest: InstanceManifest{HTTPURL: server.URL, ControlToken: "t", PID: os.Getpid(), RunID: "r"},
		HTTP:     &http.Client{},
	}
	require.ErrorContains(t, RequestShutdown(t.Context(), client, 200*time.Millisecond), "did not exit")
}

func TestUnauthorizedControlRequestIsNamed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	client := &Client{Manifest: InstanceManifest{HTTPURL: server.URL, ControlToken: "wrong"}, HTTP: &http.Client{}}
	_, err := SubmitIndexJob(t.Context(), client)
	require.ErrorIs(t, err, ErrUnauthorized)
}
