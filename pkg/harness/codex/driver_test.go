package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestHandshakeAndTurnTranscript(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","method":"thread/started","params":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
	)
	driver := testDriver(script)
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{
		Cwd: "/tmp/project", Model: "gpt-test", Effort: "high",
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "hello") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"item_1","delta":"hello"}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())

	writes := script.Writes()
	require.Len(t, writes, 4)
	golden, err := os.ReadFile("testdata/handshake.golden.jsonl")
	require.NoError(t, err)
	goldenLines := strings.Split(strings.TrimSpace(string(golden)), "\n")
	require.Len(t, goldenLines, len(writes))
	for i := range writes {
		require.JSONEq(t, goldenLines[i], writes[i])
	}
	methods := transcriptMethods(t, writes)
	require.Equal(t, []string{"initialize", "initialized", "thread/start", "turn/start"}, methods)
	var threadMessage, turnMessage rpcMessage
	require.NoError(t, json.Unmarshal([]byte(writes[2]), &threadMessage))
	require.NoError(t, json.Unmarshal([]byte(writes[3]), &turnMessage))
	var thread threadParams
	var turn turnStartParams
	require.NoError(t, json.Unmarshal(threadMessage.Params, &thread))
	require.NoError(t, json.Unmarshal(turnMessage.Params, &turn))
	require.Equal(t, "/tmp/project", thread.Cwd)
	require.Equal(t, "gpt-test", thread.Model)
	require.Equal(t, "high", turn.Effort)
	require.Equal(t, "hello", turn.Input[0].Text)
}

func TestUnknownNotificationDoesNotAbortTurn(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
		`{"jsonrpc":"2.0","method":"future/event","params":{"value":true}}`,
	)
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "continue") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-done)
	event := awaitEvent(t, session.Events(), harness.EventDiagnostic)
	require.Contains(t, event.Text, "future/event")
	require.NoError(t, session.Stop())
}

func TestLateCompletionBetweenTurnsDoesNotCompleteNextTurn(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	first := make(chan error, 1)
	go func() { first <- session.SendTurn(context.Background(), "one") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-first)

	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	diagnostic := awaitEvent(t, session.Events(), harness.EventDiagnostic)
	require.Contains(t, diagnostic.Text, "stale Codex")

	second := make(chan error, 1)
	go func() { second <- session.SendTurn(context.Background(), "two") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 5 }, time.Second, time.Millisecond)
	select {
	case err := <-second:
		t.Fatalf("late completion completed the next turn: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	script.Push(`{"jsonrpc":"2.0","id":4,"result":{"turn":{"id":"turn_2","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_2","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_2","status":"completed"}}}`)
	require.NoError(t, <-second)
	require.NoError(t, session.Stop())
}

func TestIdleTurnScopedCodexMessagesBecomeDiagnostics(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	for _, line := range []string{
		`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"old","status":"inProgress"}}}`,
		`{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thr_1","turnId":"old","item":{"id":"item","type":"reasoning"}}}`,
		`{"jsonrpc":"2.0","method":"error","params":{"threadId":"thr_1","turnId":"old","error":{"message":"late"}}}`,
	} {
		script.Push(line)
	}
	for range 3 {
		require.Contains(t, awaitEvent(t, session.Events(), harness.EventDiagnostic).Text, "stale Codex")
	}
	require.NoError(t, session.Stop())
}

func TestAgentCompletionDoesNotDuplicateStreamedText(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
	)
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "reply") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"item_1","delta":"pong"}}`)
	script.Push(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"id":"item_1","type":"agentMessage","text":"pong"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())

	var text []string
	for event := range session.Events() {
		if event.Kind == harness.EventAssistantTextDelta {
			text = append(text, event.Text)
		}
	}
	require.Equal(t, []string{"pong"}, text)
}

func TestAgentCompletionSuppliesTextWhenNoDeltaWasStreamed(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
	)
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "reply") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"id":"item_1","type":"agentMessage","text":"pong"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())

	event := awaitEvent(t, session.Events(), harness.EventAssistantTextDelta)
	require.Equal(t, "pong", event.Text)
}

func TestResumeFallsBackToStartWhenThreadIsMissing(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"no rollout found for thread old"}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"thread":{"id":"thr_new"}}}`,
	)
	driver := testDriver(script)
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project", Resume: "old"})
	require.NoError(t, err)
	require.NoError(t, session.Stop())
	require.Equal(t, []string{"initialize", "initialized", "thread/resume", "thread/start"}, transcriptMethods(t, script.Writes()))
}

func TestSessionRejectsOldCodexVersion(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.149.0"}}`,
	)
	_, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.ErrorIs(t, err, harness.ErrInitialize)
	require.True(t, script.Closed())
}

func TestProtocolDecodeFailureNamesPhase(t *testing.T) {
	script := harnesstest.NewScriptedIO(`not json`)
	_, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.ErrorIs(t, err, harness.ErrDecode)
}

func TestPermissionModeMapping(t *testing.T) {
	tests := []struct {
		mode     harness.PermissionMode
		approval string
		sandbox  string
	}{
		{"", "untrusted", "read-only"},
		{harness.PermissionApprovalRequired, "untrusted", "read-only"},
		{harness.PermissionAutoAcceptEdits, "on-request", "workspace-write"},
		{harness.PermissionFullAccess, "never", "danger-full-access"},
	}
	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			approval, sandbox := permissionConfig(test.mode)
			require.Equal(t, test.approval, approval)
			require.Equal(t, test.sandbox, sandbox)
		})
	}
}

func TestOptionsAndMCPReachCodex(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{}}`,
		`{"jsonrpc":"2.0","id":4,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
	)
	var spawnArgs []string
	driver := testDriver(script)
	driver.start = func(_ context.Context, _ string, args []string, cwd string) (transport, error) {
		spawnArgs = append([]string(nil), args...)
		require.Equal(t, "/tmp/project", cwd)
		return newJSONTransport(script, script, script), nil
	}
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{
		Cwd: "/tmp/project", Instructions: "Be precise.",
		PermissionMode: harness.PermissionAutoAcceptEdits, Model: "gpt-test", Effort: "xhigh",
		MCPServers: []harness.MCPServer{{Name: "local", Command: "rzm", Args: []string{"mcp", "serve"}, Env: []string{"MODE=test"}}},
	})
	require.NoError(t, err)
	require.Contains(t, spawnArgs, `mcp_servers.local.command="rzm"`)
	require.Contains(t, spawnArgs, `mcp_servers.local.args=["mcp","serve"]`)
	require.Contains(t, spawnArgs, `mcp_servers.local.env={"MODE":"test"}`)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "go") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 5 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())

	writes := script.Writes()
	require.Equal(t, []string{"initialize", "initialized", "thread/start", "config/mcpServer/reload", "turn/start"}, transcriptMethods(t, writes))
	var message rpcMessage
	require.NoError(t, json.Unmarshal([]byte(writes[2]), &message))
	var params threadParams
	require.NoError(t, json.Unmarshal(message.Params, &params))
	require.Contains(t, params.DeveloperInstructions, "Be precise.")
	require.Equal(t, "Be precise.", params.DeveloperInstructions)
	require.Equal(t, "gpt-test", params.Model)
	require.Equal(t, "/tmp/project", params.Cwd)
	require.Equal(t, "on-request", params.ApprovalPolicy)
	require.Equal(t, "workspace-write", params.Sandbox)
	require.NoError(t, json.Unmarshal([]byte(writes[4]), &message))
	var turn turnStartParams
	require.NoError(t, json.Unmarshal(message.Params, &turn))
	require.Equal(t, "gpt-test", turn.Model)
	require.Equal(t, "/tmp/project", turn.Cwd)
	require.Equal(t, "xhigh", turn.Effort)
	require.Equal(t, "on-request", turn.ApprovalPolicy)
	require.Equal(t, map[string]any{"type": "workspaceWrite"}, turn.SandboxPolicy)
}

func TestCodexRejectsToolRestrictions(t *testing.T) {
	for _, options := range []harness.SessionOptions{{AllowedTools: []string{"read"}}, {DisallowedTools: []string{"shell"}}} {
		_, err := testDriver(harnesstest.NewScriptedIO()).StartSession(context.Background(), options)
		require.ErrorIs(t, err, harness.ErrSpawn)
		require.Contains(t, err.Error(), "codex cannot enforce AllowedTools/DisallowedTools")
	}
}

func TestApprovalBlocksUntilResponse(t *testing.T) {
	for _, decision := range []harness.Decision{harness.DecisionAllow, harness.DecisionDeny, harness.DecisionAllowForSession} {
		t.Run(string(decision), func(t *testing.T) {
			script := approvalScript()
			session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- session.SendTurn(context.Background(), "run") }()
			require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
			script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
			script.Push(`{"jsonrpc":"2.0","id":99,"method":"item/commandExecution/requestApproval","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"item_1","command":"go test","cwd":"/tmp/project","reason":"run tests"}}`)
			event := awaitEvent(t, session.Events(), harness.EventApprovalRequested)
			require.Equal(t, "99", event.RequestID)
			require.Equal(t, "go test", event.Command)
			require.True(t, event.AllowForSession)
			require.NotContains(t, strings.Join(script.Writes(), "\n"), `"decision"`)
			require.NoError(t, session.Respond(event.RequestID, decision))
			script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
			require.NoError(t, <-done)
			writes := script.Writes()
			require.Len(t, writes, 5)
			response := responseWithID(t, writes, "99")
			var body map[string]string
			require.NoError(t, json.Unmarshal(response.Result, &body))
			expected := map[harness.Decision]string{harness.DecisionAllow: "accept", harness.DecisionDeny: "decline", harness.DecisionAllowForSession: "acceptForSession"}[decision]
			require.Equal(t, expected, body["decision"])
			require.NoError(t, session.Stop())
		})
	}
}

func TestStopDeniesPendingApproval(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "change") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","id":99,"method":"item/fileChange/requestApproval","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"item_1","grantRoot":"/tmp/project"}}`)
	awaitEvent(t, session.Events(), harness.EventApprovalRequested)
	require.NoError(t, session.Stop())
	require.Error(t, <-done)
	require.True(t, script.Closed())
	writes := script.Writes()
	response := responseWithID(t, writes, "99")
	require.Contains(t, string(response.Result), `"decision":"decline"`)
}

func TestInterruptSendsTurnInterrupt(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return containsMethod(script.Writes(), "turn/start") }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	require.NoError(t, session.Interrupt(context.Background()))
	require.Eventually(t, func() bool { return script.WriteCount() == 5 }, time.Second, time.Millisecond)
	require.Equal(t, "turn/interrupt", transcriptMethods(t, script.Writes())[4])
	script.Push(`{"jsonrpc":"2.0","id":4,"result":{}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"interrupted"}}}`)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func testDriver(script *harnesstest.ScriptedIO) *Driver {
	return &Driver{
		binary: "codex",
		runner: &fakeRunner{results: []command.Result{{Stdout: "codex-cli 0.154.0"}}},
		start: func(context.Context, string, []string, string) (transport, error) {
			return newJSONTransport(script, script, script), nil
		},
	}
}

func approvalScript() *harnesstest.ScriptedIO {
	return harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
	)
}

func transcriptMethods(t *testing.T, lines []string) []string {
	t.Helper()
	methods := make([]string, 0, len(lines))
	for _, line := range lines {
		var message rpcMessage
		require.NoError(t, json.Unmarshal([]byte(line), &message))
		methods = append(methods, message.Method)
	}
	return methods
}

func responseWithID(t *testing.T, lines []string, id string) rpcMessage {
	t.Helper()
	for _, line := range lines {
		var message rpcMessage
		require.NoError(t, json.Unmarshal([]byte(line), &message))
		if string(message.ID) == id && message.Method == "" {
			return message
		}
	}
	t.Fatalf("response %s not found in %v", id, lines)
	return rpcMessage{}
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
