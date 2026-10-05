package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/stretchr/testify/require"
)

func TestSessionReaderDoesNotBlockOnUndrainedEvents(t *testing.T) {
	lines := []string{`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`}
	for i := 0; i < 140; i++ {
		lines = append(lines, fmt.Sprintf(`{"jsonrpc":"2.0","method":"warning","params":{"message":"startup-%d"}}`, i))
	}
	lines = append(lines, `{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`)
	script := harnesstest.NewScriptedIO(lines...)
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	require.Equal(t, "thr_1", session.ID())

	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "go") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	for i := 0; i < 140; i++ {
		script.Push(fmt.Sprintf(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"i","delta":"%d"}}`, i))
	}
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func TestSecondCodexTurnRejected(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "one") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	require.ErrorIs(t, session.SendTurn(context.Background(), "two"), harness.ErrTurnStart)
	require.NoError(t, session.Stop())
	require.Error(t, <-done)
}

func TestCodexTurnContextCancellationInterruptsAndCompletes(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(ctx, "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	cancel()
	require.Eventually(t, func() bool { return containsMethod(script.Writes(), "turn/interrupt") }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"interrupted"}}}`)
	err = <-done
	require.ErrorIs(t, err, harness.ErrTimeout)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, session.Stop())
}

func TestCodexTurnCancellationForceCloses(t *testing.T) {
	script := codexHandshake()
	driver := testDriver(script)
	driver.cancelTimeout = 10 * time.Millisecond
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(ctx, "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	cancel()
	err = <-done
	require.ErrorIs(t, err, harness.ErrTransportClosed)
	require.True(t, script.Closed())
	require.ErrorIs(t, session.SendTurn(context.Background(), "later"), harness.ErrTransportClosed)
}

func TestCodexEOFResolvesTurnAndApproval(t *testing.T) {
	for _, withApproval := range []bool{false, true} {
		t.Run(fmt.Sprintf("approval=%v", withApproval), func(t *testing.T) {
			script := codexHandshake()
			session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- session.SendTurn(context.Background(), "wait") }()
			require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
			script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
			if withApproval {
				script.Push(`{"jsonrpc":"2.0","id":99,"method":"item/commandExecution/requestApproval","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"item","command":"go test"}}`)
				awaitEvent(t, session.Events(), harness.EventApprovalRequested)
			}
			require.NoError(t, script.Close())
			require.ErrorIs(t, <-done, harness.ErrTransportClosed)
			if withApproval {
				require.Error(t, session.Respond("99", harness.DecisionAllow))
			}
			for range session.Events() {
			}
			require.NoError(t, session.Stop())
		})
	}
}

func TestCodexRepliesToEveryServerRequestDuringRPCAndTurn(t *testing.T) {
	for _, duringTurn := range []bool{false, true} {
		t.Run(fmt.Sprintf("turn=%v", duringTurn), func(t *testing.T) {
			lines := []string{`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`}
			if !duringTurn {
				lines = append(lines, codexServerRequests(100)...)
			}
			lines = append(lines, `{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`)
			script := harnesstest.NewScriptedIO(lines...)
			session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
			require.NoError(t, err)
			var turnDone chan error
			if duringTurn {
				turnDone = make(chan error, 1)
				go func() { turnDone <- session.SendTurn(context.Background(), "go") }()
				require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
				script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
				for _, line := range codexServerRequests(200) {
					script.Push(line)
				}
			}
			base := 100
			if duringTurn {
				base = 200
			}
			sawLegacyCommand, sawLegacyPath := false, false
			approvalCount := 2
			if duringTurn {
				approvalCount = 5
			}
			for range approvalCount {
				event := awaitEvent(t, session.Events(), harness.EventApprovalRequested)
				require.NotEmpty(t, event.Input)
				sawLegacyCommand = sawLegacyCommand || event.Command == "go test"
				sawLegacyPath = sawLegacyPath || len(event.Paths) == 1 && event.Paths[0] == "a.go"
				require.NoError(t, session.Respond(event.RequestID, harness.DecisionDeny))
			}
			require.True(t, sawLegacyCommand)
			require.True(t, sawLegacyPath)
			require.Eventually(t, func() bool { return hasResponses(script.Writes(), base, 10) }, time.Second, time.Millisecond)
			permission := responseWithID(t, script.Writes(), fmt.Sprint(base+2))
			require.JSONEq(t, `{"permissions":{},"scope":"turn"}`, string(permission.Result))
			for _, offset := range []int{3, 4} {
				legacy := responseWithID(t, script.Writes(), fmt.Sprint(base+offset))
				require.JSONEq(t, `{"decision":{"denied":{"rejection":"denied by user"}}}`, string(legacy.Result))
			}
			for offset := 5; offset < 10; offset++ {
				require.NotNil(t, responseWithID(t, script.Writes(), fmt.Sprint(base+offset)).Error)
			}
			if duringTurn {
				script.Push(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
				require.NoError(t, <-turnDone)
			}
			require.NoError(t, session.Stop())
		})
	}
}

func TestCodexStopRacesTurnOperations(t *testing.T) {
	script := codexHandshake()
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	turnDone := make(chan error, 1)
	go func() { turnDone <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
	script.Push(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"inProgress"}}}`)
	script.Push(`{"jsonrpc":"2.0","id":99,"method":"item/commandExecution/requestApproval","params":{"threadId":"thr_1","turnId":"turn_1","itemId":"item","command":"go test"}}`)
	event := awaitEvent(t, session.Events(), harness.EventApprovalRequested)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); _ = session.Respond(event.RequestID, harness.DecisionDeny) }()
	go func() { defer wg.Done(); _ = session.Interrupt(context.Background()) }()
	go func() { defer wg.Done(); _ = session.Stop() }()
	wg.Wait()
	require.Error(t, <-turnDone)
}

func TestCodexRejectsInvalidMCPServers(t *testing.T) {
	for _, servers := range [][]harness.MCPServer{{{Name: "missing"}}, {{Name: "same", Command: "one"}, {Name: "same", Command: "two"}}, {{Name: "bad.env", Command: "one"}}, {{Name: "bad-env", Command: "one", Env: []string{"NO_VALUE"}}}} {
		_, err := appServerArgs(servers)
		require.Error(t, err)
	}
}

func TestCodexRejectsUnsupportedPermissionMode(t *testing.T) {
	_, err := testDriver(harnesstest.NewScriptedIO()).StartSession(context.Background(), harness.SessionOptions{PermissionMode: "future"})
	require.ErrorIs(t, err, harness.ErrSpawn)
}

func codexHandshake() *harnesstest.ScriptedIO {
	return harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr_1"}}}`,
	)
}

func codexServerRequests(base int) []string {
	methods := []string{"item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "applyPatchApproval", "execCommandApproval", "item/tool/requestUserInput", "mcpServer/elicitation/request", "item/tool/call", "account/chatgptAuthTokens/refresh", "attestation/generate"}
	lines := make([]string, len(methods))
	for i, method := range methods {
		params := `{"threadId":"thr_1","turnId":"turn_1","itemId":"item","command":"go test"}`
		if method == "item/permissions/requestApproval" {
			params = `{"threadId":"thr_1","turnId":"turn_1","itemId":"item","cwd":"/tmp/project","permissions":{"network":{"enabled":true}}}`
		}
		if method == "execCommandApproval" {
			params = `{"conversationId":"thr_1","callId":"call","command":["go","test"],"cwd":"/tmp/project","parsedCmd":[]}`
		}
		if method == "applyPatchApproval" {
			params = `{"conversationId":"thr_1","callId":"call","fileChanges":{"a.go":{"type":"update","unified_diff":"@@"}}}`
		}
		lines[i] = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"%s","params":%s}`, base+i, method, params)
	}
	return lines
}

func containsMethod(lines []string, method string) bool {
	for _, line := range lines {
		if strings.Contains(line, `"method":"`+method+`"`) {
			return true
		}
	}
	return false
}

func hasResponses(lines []string, base, count int) bool {
	seen := make(map[string]bool)
	for _, line := range lines {
		var msg rpcMessage
		if json.Unmarshal([]byte(line), &msg) == nil && msg.Method == "" && len(msg.ID) > 0 {
			seen[string(msg.ID)] = true
		}
	}
	for i := 0; i < count; i++ {
		if !seen[fmt.Sprint(base+i)] {
			return false
		}
	}
	return true
}
