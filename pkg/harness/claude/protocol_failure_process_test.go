package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestClaudeProcessProtocolFailureRetiresSession(t *testing.T) {
	for _, mode := range []string{"malformed", "eof"} {
		t.Run(mode, func(t *testing.T) {
			binary, err := os.Executable()
			require.NoError(t, err)
			trigger := filepath.Join(t.TempDir(), "fail")
			var process *stdioTransport
			driver := testDriver(nil)
			driver.start = func(ctx context.Context, _ string, _ []string, cwd string) (transport, error) {
				connection, err := startStdio(ctx, binary, []string{"-test.run=^TestClaudeProtocolFailureHelper$", "--", "protocol-failure-helper", mode, trigger}, cwd)
				if err == nil {
					process = connection.(*stdioTransport)
				}
				return connection, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			active, err := driver.StartSession(ctx, harness.SessionOptions{Cwd: t.TempDir()})
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = process.Close(ctx)
			})
			turn := make(chan error, 1)
			go func() { turn <- active.SendTurn(context.Background(), "synthetic turn") }()
			awaitEvent(t, active.Events(), harness.EventApprovalRequested)
			closed := make(chan bool, 1)
			go func() {
				sawError := false
				for event := range active.Events() {
					sawError = sawError || event.Kind == harness.EventError
				}
				closed <- sawError
			}()
			require.NoError(t, os.WriteFile(trigger, []byte("fail"), 0o600))
			require.Eventually(t, func() bool { _, err := os.Stat(trigger + ".emitted"); return err == nil }, time.Second, time.Millisecond)
			select {
			case err := <-turn:
				require.ErrorIs(t, err, harness.ErrTransportClosed)
				if mode == "malformed" {
					require.ErrorIs(t, err, harness.ErrDecode)
				}
			case <-time.After(time.Second):
				t.Fatal("active turn waited for the still-live child's stderr")
			}
			require.Error(t, active.Respond("fixture-request", harness.DecisionAllow))
			state := active.(*session)
			state.stateMu.Lock()
			pending := len(state.pending)
			state.stateMu.Unlock()
			require.Zero(t, pending)
			select {
			case sawError := <-closed:
				require.True(t, sawError, "terminal error must precede Events closure")
			case <-time.After(4 * time.Second):
				t.Fatal("session failure did not close Events")
			}
			select {
			case <-process.Exited():
			default:
				t.Fatal("session failure did not reap the child")
			}
		})
	}
}

func TestClaudeProtocolFailureHelper(t *testing.T) {
	arg := slices.Index(os.Args, "protocol-failure-helper")
	if arg < 0 {
		return
	}
	mode, trigger := os.Args[arg+1], os.Args[arg+2]
	failProtocol := func() {
		for {
			if _, err := os.Stat(trigger); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if mode == "malformed" {
			_, _ = fmt.Fprintln(os.Stdout, "not-json")
		} else {
			_ = os.Stdout.Close()
		}
		_ = os.WriteFile(trigger+".emitted", []byte("emitted"), 0o600)
		for {
			time.Sleep(time.Hour)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var input message
		if json.Unmarshal(scanner.Bytes(), &input) != nil {
			os.Exit(1)
		}

		switch input.Type {
		case "control_request":
			_, _ = fmt.Fprintln(os.Stdout, initResponse(input.RequestID))
		case "user":
			_, _ = fmt.Fprintln(os.Stdout, `{"type":"control_request","request_id":"fixture-request","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"synthetic command"}},"session_id":"sid"}`)
			failProtocol()
		}

	}
	os.Exit(0)
}
