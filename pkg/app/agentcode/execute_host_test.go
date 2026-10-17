package agentcode

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecuteRequiresInvocationOwnedHost(t *testing.T) {
	_, err := Execute(context.Background(), ExecuteOptions{VaultPath: t.TempDir(), Code: `throw new Error("must not run")`})
	require.ErrorContains(t, err, "requires an operation host")
}

func TestExecuteStreamHostPreservesAuthorityAndContext(t *testing.T) {
	requireNode(t)
	type invocationKey struct{}
	ctx := context.WithValue(context.Background(), invocationKey{}, "factory invocation")
	joined := make(chan struct{})
	var sawReadWrite bool
	result, err := Execute(ctx, ExecuteOptions{VaultPath: t.TempDir(), ReadWrite: true, SessionID: "connection",
		Code: `return await rzm.files({inputs:["fixture.md"]});`,
		OperationHost: func(ctx context.Context, input io.Reader, output io.Writer, readWrite bool) error {
			defer close(joined)
			sawReadWrite = readWrite
			return Serve(ctx, input, output, func(ctx context.Context, name string, input map[string]any) (CallOutcome, error) {
				return CallOutcome{OK: true, Payload: map[string]any{"invocation": ctx.Value(invocationKey{}), "operation": name, "session": input["sessionId"]}}, nil
			})
		},
	})
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Error)
	require.JSONEq(t, `{"ok":true,"exitCode":0,"payload":{"invocation":"factory invocation","operation":"files","session":"connection"},"diagnostic":null,"stdout":"","stderr":""}`, string(result.Result))
	require.True(t, sawReadWrite)
	select {
	case <-joined:
	default:
		t.Fatal("execution returned before joining its operation host")
	}
}

func TestExecuteJoinsHostOnCancellationAndNodeExit(t *testing.T) {
	requireNode(t)
	for _, test := range []struct {
		name, code string
		cancel     bool
	}{
		{"cancel", `await rzm.files({inputs:["fixture.md"]});`, true},
		{"node exit", `const fs = await import("node:fs");
rzm.files({inputs:["fixture.md"]});
while (!fs.existsSync(input.entered)) await new Promise(resolve => setTimeout(resolve, 10));
process.exit(7);`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			entered, stopped, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
			marker := filepath.Join(t.TempDir(), "operation-entered")
			options := ExecuteOptions{VaultPath: t.TempDir(), Code: test.code, Input: []byte(`{"entered":` + string(mustJSON(t, marker)) + `}`), OperationHost: func(ctx context.Context, input io.Reader, output io.Writer, _ bool) error {
				defer close(joined)
				return Serve(ctx, input, output, func(ctx context.Context, _ string, _ map[string]any) (CallOutcome, error) {
					if err := os.WriteFile(marker, []byte("entered"), 0600); err != nil {
						return CallOutcome{}, err
					}
					close(entered)
					<-ctx.Done()
					close(stopped)
					return CallOutcome{}, ctx.Err()
				})
			}}
			if test.cancel {
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			result, err := Execute(ctx, options)
			require.NoError(t, err)
			require.False(t, result.OK)
			if test.cancel {
				require.Equal(t, "cancelled", result.Error.Code)
			} else {
				require.Equal(t, "node_execution_failed", result.Error.Code)
			}
			for _, done := range []<-chan struct{}{entered, stopped, joined} {
				select {
				case <-done:
				default:
					t.Fatal("execution returned before its active operation and host stopped")
				}
			}
		})
	}
}

func TestExecuteJoinsHostAfterFramingAndHostFailures(t *testing.T) {
	requireNode(t)
	for _, test := range []struct {
		name, code string
		hostError  bool
	}{
		{"malformed frame", `(await import("node:fs")).writeSync(1,"invalid frame\n"); await new Promise(() => {});`, false},
		{"oversized frame", `(await import("node:fs")).writeFileSync(1,"x".repeat(1200000)); await new Promise(() => {});`, false},
		{"host failed", `await rzm.files({inputs:["fixture.md"]});`, true},
		{"partial frame after result", `(await import("node:fs")).writeFileSync(1,'{"executionResult":{"ok":true,"result":42}}\npartial'); process.exit(0);`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			joined := make(chan struct{})
			result, err := Execute(context.Background(), ExecuteOptions{VaultPath: t.TempDir(), Code: test.code, Timeout: 5 * time.Second,
				OperationHost: func(ctx context.Context, input io.Reader, output io.Writer, _ bool) error {
					defer close(joined)
					if test.hostError {
						return errors.New("fixture host failed")
					}
					return Serve(ctx, input, output, func(context.Context, string, map[string]any) (CallOutcome, error) { return CallOutcome{OK: true}, nil })
				},
			})
			require.NoError(t, err)
			require.False(t, result.OK)
			require.Equal(t, "execution_transport_failed", result.Error.Code)
			select {
			case <-joined:
			default:
				t.Fatal("operation host was not joined")
			}
		})
	}
}

// A host may start a helper after Node exits, while it finishes cancellation.
// That helper owns its stdio; the transport-owned Node descriptor must not be
// inherited or closed through the callback's stream capability.
func TestExecuteJoinsDelayedProcessHostWithoutSharingDescriptors(t *testing.T) {
	requireNode(t)
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "host-stdout")
	joined := make(chan struct{})
	var exposesFD, exposesClose bool
	var startError, waitError error
	result, err := Execute(context.Background(), ExecuteOptions{
		VaultPath: t.TempDir(), Code: `return "finished";`, Timeout: 5 * time.Second,
		OperationHost: func(ctx context.Context, _ io.Reader, output io.Writer, _ bool) error {
			defer close(joined)
			_, exposesFD = output.(interface{ Fd() uintptr })
			_, exposesClose = output.(io.Closer)
			<-ctx.Done()
			// Execute has reaped Node and closed its stdin before cancelling
			// this host. The helper must still receive a valid owned stdout.
			helperCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			helper := exec.CommandContext(helperCtx, node, "-e", `const fs = require("node:fs"); fs.fstatSync(1); fs.writeFileSync(`+string(mustJSON(t, marker))+`, "stdout available"); process.stdout.write("late host output");`)
			configureExecuteCommand(helper)
			defer func() { _ = terminateExecuteProcessTree(helper) }()
			helper.Stdout = output
			helper.WaitDelay = time.Second
			startError = helper.Start()
			if startError == nil {
				waitError = helper.Wait()
			}
			return startError
		},
	})
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Error)
	require.JSONEq(t, `"finished"`, string(result.Result))
	select {
	case <-joined:
	default:
		t.Fatal("execution returned before its delayed process host exited")
	}
	require.NoError(t, startError)
	data, err := os.ReadFile(marker)
	require.NoError(t, err, "the delayed helper must own a valid stdout after Node has exited")
	require.Equal(t, "stdout available", string(data))
	require.ErrorIs(t, waitError, os.ErrClosed, "late writes must see the closed stream")
	require.False(t, exposesFD, "the operation host must not inherit the transport's file descriptor")
	require.False(t, exposesClose, "the operation host must not own transport closure")
}
