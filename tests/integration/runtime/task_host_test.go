//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

type sseEvent struct {
	name string
	data string
}

// A Rhizome Desktop host stream (SPEC-0104 US7) holds a headless runtime past
// its idle timeout, a drain answers with no run in progress, and closing the
// stream returns the runtime to its normal idle exit.
func TestTaskHostStreamHoldsTheRuntimeUntilItCloses(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "runtime:\n  idleTimeout: 3s\n")
	// The tasks service needs a Git repository; user configuration stays under
	// the fixture's home, so runHere is off.
	isolated := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(vault.home, ".config"),
		"APPDATA":         filepath.Join(vault.home, "AppData", "Roaming"),
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = vault.root
	git.Env = vault.environment(isolated)
	output, err := git.CombinedOutput()
	require.NoError(t, err, "%s", output)

	indexed := vault.run(isolated, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	client, health, err := appruntime.LiveManifest(context.Background(), vault.root)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.Manifest.HTTPURL+"/api/tasks/host", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /api/tasks/host: %s: %s", resp.Status, body)
	}
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	events := readSSE(resp.Body)

	first := nextSSE(t, events)
	require.Equal(t, "status", first.name, "data=%s", first.data)
	var status struct {
		Repository string            `json:"repository"`
		RunHere    *bool             `json:"runHere"`
		Notices    []json.RawMessage `json:"notices"`
		Draining   bool              `json:"draining"`
	}
	require.NoError(t, json.Unmarshal([]byte(first.data), &status), "data=%s", first.data)
	require.NotEmpty(t, status.Repository, "data=%s", first.data)
	require.NotNil(t, status.RunHere, "data=%s", first.data)
	require.False(t, *status.RunHere, "the isolated home has tasks off for this machine")
	require.NotNil(t, status.Notices, "data=%s", first.data)
	require.False(t, status.Draining)

	// Watch the PID, not the health route: every API request counts as activity.
	require.Never(t, func() bool { return !indexlock.PIDExists(health.PID) }, 15*time.Second, 200*time.Millisecond,
		"the runtime idle-exited under an open host stream")

	drain, err := http.Post(client.Manifest.HTTPURL+"/api/tasks/host/drain", "application/json", nil)
	require.NoError(t, err)
	drained, err := io.ReadAll(drain.Body)
	_ = drain.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, drain.StatusCode, "%s", drained)
	require.JSONEq(t, `{"running":null,"dispatching":null}`, string(drained))

	// The stream reports the drain on its next status rebuild.
	deadline := time.After(30 * time.Second)
	for !status.Draining {
		select {
		case event, ok := <-events:
			require.True(t, ok, "the host stream closed before reporting the drain")
			if event.name == "status" {
				require.NoError(t, json.Unmarshal([]byte(event.data), &status), "data=%s", event.data)
			}
		case <-deadline:
			t.Fatal("the host stream never reported draining")
		}
	}
	require.True(t, indexlock.PIDExists(health.PID), "a drain does not end the runtime")

	cancel()
	requirePIDExits(t, health.PID, 60*time.Second)
	_, err = appruntime.ReadManifest(vault.root)
	require.Error(t, err, "an idle exit removes the manifest")
}

// readSSE parses server-sent events from body until it ends.
func readSSE(body io.Reader) <-chan sseEvent {
	events := make(chan sseEvent, 16)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var event sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if event.name != "" || event.data != "" {
					events <- event
				}
				event = sseEvent{}
			case strings.HasPrefix(line, "event: "):
				event.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.data += strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return events
}

func nextSSE(t *testing.T, events <-chan sseEvent) sseEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		require.True(t, ok, "the host stream closed before its first event")
		return event
	case <-time.After(30 * time.Second):
		t.Fatal("no event on the host stream")
		return sseEvent{}
	}
}
