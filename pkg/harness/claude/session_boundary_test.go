package claude

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/stretchr/testify/require"
)

func TestClaudeReaderDoesNotBlockOnUndrainedEvents(t *testing.T) {
	lines := make([]string, 0, 142)
	for i := 0; i < 140; i++ {
		lines = append(lines, fmt.Sprintf(`{"type":"system","subtype":"notice-%d","session_id":"sid","claude_code_version":"2.1.269"}`, i))
	}
	lines = append(lines, `{"type":"control_response","response":{"subtype":"success","request_id":"init-1","response":{"account":{"email":"person@example.com"}}}}`)
	script := harnesstest.NewScriptedIO(lines...)
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	require.Equal(t, "sid", session.ID())
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "go") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	for i := 0; i < 140; i++ {
		script.Push(fmt.Sprintf(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"%d"}},"session_id":"sid"}`, i))
	}
	script.Push(resultMessage("success", false))
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func TestClaudeImmediateEOFDrainsQueuedEvents(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	drainAvailable(session.Events())
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "go") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	const deltas = 200
	for i := 0; i < deltas; i++ {
		script.Push(fmt.Sprintf(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"%d"}},"session_id":"sid"}`, i))
	}
	script.Push(resultMessage("success", false))
	require.NoError(t, <-done)
	require.NoError(t, script.Close())

	textCount, completed := 0, false
	for event := range session.Events() {
		if event.Kind == harness.EventAssistantTextDelta {
			textCount++
		}
		completed = completed || event.Kind == harness.EventTurnCompleted
	}
	require.Equal(t, deltas, textCount)
	require.True(t, completed)
	require.NoError(t, session.Stop())
}

func TestClaudeTurnContextCancellationInterruptsAndCompletes(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(ctx, "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	cancel()
	require.Eventually(t, func() bool { return script.WriteCount() >= 3 }, time.Second, time.Millisecond)
	script.Push(resultMessage("interrupted", false))
	err = <-done
	require.ErrorIs(t, err, harness.ErrTimeout)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, session.Stop())
}

func TestClaudeTurnCancellationForceCloses(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	driver := testDriver(script)
	driver.cancelTimeout = 10 * time.Millisecond
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(ctx, "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	cancel()
	err = <-done
	require.ErrorIs(t, err, harness.ErrTransportClosed)
	require.True(t, script.Closed())
	require.ErrorIs(t, session.SendTurn(context.Background(), "later"), harness.ErrTransportClosed)
}

func TestClaudeEOFResolvesTurnAndApproval(t *testing.T) {
	for _, withApproval := range []bool{false, true} {
		t.Run(fmt.Sprintf("approval=%v", withApproval), func(t *testing.T) {
			script := harnesstest.NewScriptedIO(initResponse("init-1"))
			session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- session.SendTurn(context.Background(), "wait") }()
			require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
			if withApproval {
				script.Push(`{"type":"control_request","request_id":"req","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test"}},"session_id":"sid"}`)
				event := awaitEvent(t, session.Events(), harness.EventApprovalRequested)
				require.NotEmpty(t, event.Input)
			}
			require.NoError(t, script.Close())
			require.ErrorIs(t, <-done, harness.ErrTransportClosed)
			if withApproval {
				require.Error(t, session.Respond("req", harness.DecisionAllow))
			}
			for range session.Events() {
			}
			require.NoError(t, session.Stop())
		})
	}
}

func TestClaudeControlCancelResolvesApproval(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"control_request","request_id":"req","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test"}},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventApprovalRequested)
	script.Push(`{"type":"control_cancel_request","request_id":"req","session_id":"sid"}`)
	require.Eventually(t, func() bool {
		for _, event := range drainAvailable(session.Events()) {
			if event.Kind == harness.EventApprovalRequested && event.Phase == harness.PhaseCancelled && event.RequestID == "req" {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	require.Error(t, session.Respond("req", harness.DecisionAllow))
	script.Push(resultMessage("success", false))
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func TestClaudeDoesNotDowngradeAllowForSession(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"control_request","request_id":"req","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test"}},"session_id":"sid"}`)
	event := awaitEvent(t, session.Events(), harness.EventApprovalRequested)
	require.False(t, event.AllowForSession)
	require.Error(t, session.Respond("req", harness.DecisionAllowForSession))
	require.NoError(t, session.Respond("req", harness.DecisionDeny))
	script.Push(resultMessage("success", false))
	require.NoError(t, <-done)
	require.NoError(t, session.Stop())
}

func TestClaudeUnknownControlRequestGetsError(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	script.Push(`{"type":"control_request","request_id":"unknown","request":{"subtype":"future"},"session_id":"sid"}`)
	require.Eventually(t, func() bool {
		return strings.Contains(strings.Join(script.Writes(), "\n"), `"request_id":"unknown"`) && strings.Contains(strings.Join(script.Writes(), "\n"), `"subtype":"error"`)
	}, time.Second, time.Millisecond)
	require.NoError(t, session.Stop())
}

func TestClaudeStopRacesTurnOperations(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	turnDone := make(chan error, 1)
	go func() { turnDone <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	script.Push(`{"type":"control_request","request_id":"req","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test"}},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventApprovalRequested)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); _ = session.Respond("req", harness.DecisionDeny) }()
	go func() { defer wg.Done(); _ = session.Interrupt(context.Background()) }()
	go func() { defer wg.Done(); _ = session.Stop() }()
	wg.Wait()
	require.Error(t, <-turnDone)
}

func TestClaudeRejectsInvalidMCPServers(t *testing.T) {
	for _, servers := range [][]harness.MCPServer{{{Name: "missing"}}, {{Name: "same", Command: "one"}, {Name: "same", Command: "two"}}, {{Name: "bad-env", Command: "one", Env: []string{"NO_VALUE"}}}} {
		_, err := sessionArgs(harness.SessionOptions{MCPServers: servers}, "/tmp/project")
		require.Error(t, err)
	}
}

func TestClaudeRejectsUnsupportedPermissionMode(t *testing.T) {
	_, err := testDriver(harnesstest.NewScriptedIO()).StartSession(context.Background(), harness.SessionOptions{PermissionMode: "future"})
	require.ErrorIs(t, err, harness.ErrSpawn)
}
