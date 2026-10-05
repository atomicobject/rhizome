package codex

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

func TestCodexProcessProtocolFailureRetiresSession(t *testing.T) {
	for _, mode := range []string{"malformed", "eof"} {
		t.Run(mode, func(t *testing.T) {
			binary, err := os.Executable()
			require.NoError(t, err)
			trigger := filepath.Join(t.TempDir(), "fail")
			var process *stdioTransport
			driver := testDriver(nil)
			driver.start = func(ctx context.Context, _ string, _ []string, cwd string) (transport, error) {
				connection, err := startStdio(ctx, binary, []string{"-test.run=^TestCodexProtocolFailureHelper$", "--", "protocol-failure-helper", mode, trigger}, cwd)
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
			require.Error(t, active.Respond("91", harness.DecisionAllow))
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

func TestCodexProtocolFailureHelper(t *testing.T) {
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
		var input rpcMessage
		if json.Unmarshal(scanner.Bytes(), &input) != nil {
			os.Exit(1)
		}

		switch input.Method {
		case "initialize":
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{"userAgent":"codex-cli/0.154.0"}}`+"\n", input.ID)
		case "thread/start":
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{"thread":{"id":"thr_fixture"}}}`+"\n", input.ID)
		case "turn/start":
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{"turn":{"id":"turn_fixture","status":"inProgress"}}}`+"\n", input.ID)
			_, _ = fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","id":91,"method":"item/commandExecution/requestApproval","params":{"threadId":"thr_fixture","turnId":"turn_fixture","itemId":"fixture-item","command":"synthetic command"}}`)
			failProtocol()
		}

	}
	os.Exit(0)
}
