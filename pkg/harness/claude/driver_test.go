package claude

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestPongGoldenTranscript(t *testing.T) {
	lines := fixtureLines(t, "testdata/pong.golden.jsonl")
	script := harnesstest.NewScriptedIO(lines[:2]...)
	var args []string
	driver := testDriver(script)
	driver.start = func(_ context.Context, _ string, spawnArgs []string, cwd string) (transport, error) {
		args = append([]string(nil), spawnArgs...)
		require.Equal(t, "/tmp/project", cwd)
		return newJSONTransport(script, script, script), nil
	}
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project", Model: "haiku", Effort: "high"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "ping") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	for _, line := range lines[2:] {
		script.Push(line)
	}
	require.NoError(t, <-done)
	events := drainAvailable(session.Events())
	require.NoError(t, session.Stop())
	require.Equal(t, []harness.EventKind{harness.EventDiagnostic, harness.EventTurnStarted, harness.EventAssistantTextDelta, harness.EventTokenUsage, harness.EventTurnCompleted}, eventKinds(events))
	require.Equal(t, "pong", events[2].Text)
	require.Equal(t, int64(8274), events[4].Usage.TotalTokens)
	require.Contains(t, args, "--include-partial-messages")
	require.Equal(t, "haiku", argValue(t, args, "--model"))
	require.Equal(t, "high", argValue(t, args, "--effort"))
	writes := script.Writes()
	require.Len(t, writes, 2)
	require.JSONEq(t, `{"type":"control_request","request_id":"init-1","request":{"subtype":"initialize","hooks":{}}}`, writes[0])
	require.JSONEq(t, `{"type":"user","message":{"role":"user","content":"ping"}}`, writes[1])
}

func TestOptionsPermissionAndMCPArgs(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	var args []string
	driver := testDriver(script)
	driver.start = func(_ context.Context, _ string, spawnArgs []string, _ string) (transport, error) {
		args = spawnArgs
		return newJSONTransport(script, script, script), nil
	}
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project", Instructions: "Be precise.", AllowedTools: []string{"Read", "Bash"}, DisallowedTools: []string{"Write"}, PermissionMode: harness.PermissionFullAccess, Model: "opus", Effort: "xhigh", Resume: "session-old", MCPServers: []harness.MCPServer{{Name: "local", Command: "rzm", Args: []string{"mcp", "serve"}, Env: []string{"MODE=test"}}}})
	require.NoError(t, err)
	require.NoError(t, session.Stop())
	require.Equal(t, "bypassPermissions", argValue(t, args, "--permission-mode"))
	require.Contains(t, args, "--dangerously-skip-permissions")
	require.Equal(t, "session-old", argValue(t, args, "--resume"))
	require.Equal(t, "Be precise.", argValue(t, args, "--append-system-prompt"))
	require.Equal(t, "opus", argValue(t, args, "--model"))
	require.Equal(t, "xhigh", argValue(t, args, "--effort"))
	require.Contains(t, args, "--allowedTools")
	allowed := slices.Index(args, "--allowedTools")
	require.Equal(t, []string{"Read", "Bash"}, args[allowed+1:allowed+3])
	require.Equal(t, "Write", argValue(t, args, "--disallowedTools"))
	config := argValue(t, args, "--mcp-config")
	require.JSONEq(t, `{"mcpServers":{"local":{"command":"rzm","args":["mcp","serve"],"env":{"MODE":"test"}}}}`, config)
	require.Equal(t, "/tmp/project", argValue(t, args, "--add-dir"))
}

func TestPermissionModeMapping(t *testing.T) {
	tests := []struct {
		mode   harness.PermissionMode
		want   string
		danger bool
	}{{"", "default", false}, {harness.PermissionApprovalRequired, "default", false}, {harness.PermissionAutoAcceptEdits, "acceptEdits", false}, {harness.PermissionFullAccess, "bypassPermissions", true}}
	for _, test := range tests {
		mode, danger := permissionMode(test.mode)
		require.Equal(t, test.want, mode)
		require.Equal(t, test.danger, danger)
	}
}

func TestApprovalBlocksAndResponds(t *testing.T) {
	for _, decision := range []harness.Decision{harness.DecisionAllow, harness.DecisionDeny, harness.DecisionAllowForSession} {
		t.Run(string(decision), func(t *testing.T) {
			lines := fixtureLines(t, "testdata/tool-control.golden.jsonl")
			script := harnesstest.NewScriptedIO(lines[:2]...)
			session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- session.SendTurn(context.Background(), "write") }()
			require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
			script.Push(lines[2])
			script.Push(lines[3])
			event := awaitEvent(t, session.Events(), harness.EventApprovalRequested)
			require.Equal(t, "9bb1f814-357e-4c97-b3b4-ebc3520ef611", event.RequestID)
			require.Equal(t, []string{"/private/tmp/rz-harness/ctl/hello.txt"}, event.Paths)
			require.True(t, event.AllowForSession)
			require.NotContains(t, strings.Join(script.Writes(), "\n"), `"behavior"`)
			require.NoError(t, session.Respond(event.RequestID, decision))
			script.Push(lines[4])
			script.Push(lines[5])
			require.NoError(t, <-done)
			writes := script.Writes()
			require.Len(t, writes, 3)
			response := controlResponseFor(t, writes, event.RequestID)
			var body struct {
				Response map[string]any `json:"response"`
			}
			require.NoError(t, json.Unmarshal(response.Response, &body))
			expected := "allow"
			if decision == harness.DecisionDeny {
				expected = "deny"
			}
			require.Equal(t, expected, body.Response["behavior"])
			if decision == harness.DecisionAllowForSession {
				require.NotNil(t, body.Response["updatedPermissions"])
			}
			require.NoError(t, session.Stop())
		})
	}
}

func TestStopDeniesPendingApproval(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "run") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool","name":"Bash","input":{"command":"go test"}}]},"session_id":"sid"}`)
	script.Push(`{"type":"control_request","request_id":"req","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test"},"tool_use_id":"tool"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventApprovalRequested)
	require.NoError(t, session.Stop())
	require.Error(t, <-done)
	require.True(t, script.Closed())
	response := controlResponseFor(t, script.Writes(), "req")
	require.Contains(t, string(response.Response), `"behavior":"deny"`)
}

func TestSecondTurnRejected(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "one") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	err = session.SendTurn(context.Background(), "two")
	require.ErrorIs(t, err, harness.ErrTurnStart)
	require.NoError(t, session.Stop())
	require.Error(t, <-done)
}

func TestInterruptTimeoutClosesSession(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	driver := testDriver(script)
	driver.interruptTimeout = 10 * time.Millisecond
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	err = session.Interrupt(context.Background())
	require.ErrorIs(t, err, harness.ErrTimeout)
	require.True(t, script.Closed())
	require.Error(t, <-done)
	require.NoError(t, session.Stop())
}

func TestUnknownMessageIsDiagnostic(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"), `{"type":"future_message","session_id":"sid"}`)
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for _, event := range drainAvailable(session.Events()) {
			if event.Kind == harness.EventDiagnostic && strings.Contains(event.Text, "future_message") {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "continue") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(resultMessage("success", false))
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func TestSessionRejectsOldClaudeVersion(t *testing.T) {
	script := harnesstest.NewScriptedIO(`{"type":"system","subtype":"init","claude_code_version":"2.0.9"}`)
	_, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.ErrorIs(t, err, harness.ErrInitialize)
	require.True(t, script.Closed())
}

func TestCapturedInitializeBeforeSystemDefersVersionFailureToFirstTurn(t *testing.T) {
	data, err := os.ReadFile("testdata/initialize-before-system.golden.jsonl")
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 2)
	script := harnesstest.NewScriptedIO(lines[0])
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "first") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(lines[1])
	err = <-done
	require.ErrorIs(t, err, harness.ErrInitialize)
	require.NoError(t, session.Stop())
}

func TestLateResultBetweenTurnsDoesNotCompleteNextTurn(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	drainAvailable(session.Events())
	first := make(chan error, 1)
	go func() { first <- session.SendTurn(context.Background(), "one") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	script.Push(resultMessage("success", false))
	require.NoError(t, <-first)
	drainAvailable(session.Events())

	script.Push(resultMessage("success", false))
	diagnostic := awaitEvent(t, session.Events(), harness.EventDiagnostic)
	require.Contains(t, diagnostic.Text, "stale Claude")

	second := make(chan error, 1)
	go func() { second <- session.SendTurn(context.Background(), "two") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 3 }, time.Second, time.Millisecond)
	select {
	case err := <-second:
		t.Fatalf("late result completed the next turn: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	script.Push(resultMessage("success", false))
	require.NoError(t, <-second)
	require.NoError(t, session.Stop())
}

func TestIdleTurnScopedClaudeMessagesBecomeDiagnostics(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	drainAvailable(session.Events())
	for _, line := range []string{
		`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`,
		`{"type":"assistant","message":{"content":[]},"session_id":"sid"}`,
		`{"type":"user","message":{"content":[]},"session_id":"sid"}`,
	} {
		script.Push(line)
	}
	for range 3 {
		require.Contains(t, awaitEvent(t, session.Events(), harness.EventDiagnostic).Text, "stale Claude")
	}
	require.NoError(t, session.Stop())
}

func TestProtocolDecodeFailureNamesPhase(t *testing.T) {
	script := harnesstest.NewScriptedIO(`not json`)
	_, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.ErrorIs(t, err, harness.ErrDecode)
}

func TestInterruptCompletesWhenResultArrives(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	interrupted := make(chan error, 1)
	go func() { interrupted <- session.Interrupt(context.Background()) }()
	require.Eventually(t, func() bool { return script.WriteCount() == 3 }, time.Second, time.Millisecond)
	script.Push(resultMessage("interrupted", false))
	require.NoError(t, <-interrupted)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func testDriver(script *harnesstest.ScriptedIO) *Driver {
	return &Driver{binary: "claude", runner: &fakeRunner{}, interruptTimeout: time.Second, start: func(context.Context, string, []string, string) (transport, error) {
		return newJSONTransport(script, script, script), nil
	}}
}
func fixtureLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}
func initResponse(id string) string {
	return `{"type":"system","subtype":"init","session_id":"sid","claude_code_version":"2.1.269"}` + "\n" + `{"type":"control_response","response":{"subtype":"success","request_id":"` + id + `","response":{"account":{"email":"person@example.com","subscriptionType":"Max"}}}}`
}
func resultMessage(subtype string, isError bool) string {
	return `{"type":"result","subtype":"` + subtype + `","result":"done","is_error":` + map[bool]string{true: "true", false: "false"}[isError] + `,"session_id":"sid"}`
}
func awaitEvent(t *testing.T, events <-chan harness.Event, kind harness.EventKind) harness.Event {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == kind {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", kind)
		}
	}
}

// drainAvailable collects events until the turn completes or no event arrives
// for a short idle window; publication is asynchronous, so a purely
// non-blocking drain would race it.
func drainAvailable(events <-chan harness.Event) []harness.Event {
	var result []harness.Event
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return result
			}
			result = append(result, event)
			if event.Kind == harness.EventTurnCompleted {
				return result
			}
		case <-time.After(200 * time.Millisecond):
			return result
		}
	}
}
func eventKinds(events []harness.Event) []harness.EventKind {
	result := make([]harness.EventKind, len(events))
	for i, event := range events {
		result[i] = event.Kind
	}
	return result
}
func argValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("flag %s not found in %v", flag, args)
	return ""
}

func controlResponseFor(t *testing.T, lines []string, requestID string) message {
	t.Helper()
	for _, line := range lines {
		var response message
		require.NoError(t, json.Unmarshal([]byte(line), &response))
		if response.Type != "control_response" {
			continue
		}
		var body struct {
			RequestID string `json:"request_id"`
		}
		require.NoError(t, json.Unmarshal(response.Response, &body))
		if body.RequestID == requestID {
			return response
		}
	}
	t.Fatalf("control response %s not found in %v", requestID, lines)
	return message{}
}

type fakeRunner struct {
	mu       sync.Mutex
	lookErr  error
	results  []command.Result
	errors   []error
	commands []command.Spec
	run      func(context.Context, command.Spec) (command.Result, error)
}

func (r *fakeRunner) LookPath(name string) (string, error) {
	if r.lookErr != nil {
		return "", r.lookErr
	}
	return "/fake/" + name, nil
}
func (r *fakeRunner) Run(ctx context.Context, spec command.Spec) (command.Result, error) {
	r.mu.Lock()
	r.commands = append(r.commands, spec)
	if r.run != nil {
		run := r.run
		r.mu.Unlock()
		return run(ctx, spec)
	}
	var result command.Result
	var err error
	if len(r.results) > 0 {
		result, r.results = r.results[0], r.results[1:]
	}
	if len(r.errors) > 0 {
		err, r.errors = r.errors[0], r.errors[1:]
	}
	r.mu.Unlock()
	return result, err
}
func notInstalledRunner() *fakeRunner { return &fakeRunner{lookErr: exec.ErrNotFound} }
func commandInput(t *testing.T, spec command.Spec) string {
	t.Helper()
	data, err := io.ReadAll(spec.Stdin)
	require.NoError(t, err)
	return string(data)
}
