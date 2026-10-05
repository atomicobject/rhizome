package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestRuntimeAgentRequestDiagnosticsPreserveStructuredReadFailures(t *testing.T) {
	for _, test := range []struct {
		name, operation, status, reason string
		input                           map[string]any
		indexErr                        error
		cancel                          bool
	}{
		{name: "invalid query", operation: "ontology_query", input: map[string]any{"query": "{ PRIVATE_INVALID_FIELD }"}, status: "error", reason: "structured_error"},
		{name: "index unavailable", operation: "view", input: map[string]any{"action": "list"}, indexErr: errors.New("PRIVATE_INDEX_ERROR"), status: "error", reason: "structured_error"},
		{name: "canceled", operation: "view", input: map[string]any{"action": "list"}, cancel: true, status: "canceled", reason: "canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
			require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
			require.NoError(t, os.WriteFile(schemaPath, []byte("type Task @node(paths: [\"notes/*.md\"]) {\n  status: String\n}\n"), 0o644))
			runtime := &diagnosticReadRuntime{vault: root, wait: func(ctx context.Context) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return test.indexErr
			}}
			host := newDiagnosticAgentOpHost(root, runtime)
			var stderr bytes.Buffer
			recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, recorder.Close()) })
			parent := diagnostics.NewOperation("http.request", "/api/v1/agent/ops/")
			ctx := diagnostics.WithOperation(diagnostics.WithRecorder(context.Background(), recorder), parent)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if test.cancel {
				cancel()
			}
			// Compare the caller's whole outcome with the existing runtime-read workflow.
			want := withConfigGeneration(codeModeWorkflowOutcome(host.reads.Call(ctx, test.operation, test.input)), 1)
			outcome, err := host.call(ctx, test.operation, appruntime.AgentOpRequest{Input: test.input})
			require.NoError(t, err, "domain failures must remain HTTP-successful outcomes")
			require.Equal(t, want, outcome)
			require.False(t, outcome.OK)
			require.Equal(t, 1, outcome.ExitCode)
			if test.operation == "ontology_query" {
				require.Contains(t, outcome.Diagnostic.(map[string]any), "errors")
				require.Contains(t, outcome.Stderr, "PRIVATE_INVALID_FIELD")
			} else {
				require.True(t, actions.IsCodeModeRuntimeReadPending(outcome.Diagnostic))
			}
			require.NoError(t, recorder.Close())
			require.Empty(t, stderr.String())
			events := requireRuntimeAgentRequestEvents(t, root, parent, runtime.operation, test.operation, test.status, test.reason)
			encoded, err := json.Marshal(events)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "PRIVATE_", "diagnostics must not contain query or error text")
		})
	}
}

func TestRuntimeAgentRequestDiagnosticsRecordAndPreservePanic(t *testing.T) {
	root := t.TempDir()
	panicked := &struct{ message string }{message: "PRIVATE_PANIC"}
	runtime := &diagnosticReadRuntime{vault: root, wait: func(context.Context) error { panic(panicked) }}
	host := newDiagnosticAgentOpHost(root, runtime)
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	parent := diagnostics.NewOperation("http.request", "/api/v1/agent/ops/")
	ctx := diagnostics.WithOperation(diagnostics.WithRecorder(context.Background(), recorder), parent)
	require.PanicsWithValue(t, panicked, func() {
		_, _ = host.call(ctx, "view", appruntime.AgentOpRequest{Input: map[string]any{"action": "list"}})
	})
	require.NoError(t, recorder.Close())
	require.Empty(t, stderr.String())
	events := requireRuntimeAgentRequestEvents(t, root, parent, runtime.operation, "view", "error", "handler_panicked")
	encoded, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_PANIC")
}

// An invalid query and a readiness failure never reach the other runtime methods.
type diagnosticReadRuntime struct {
	actions.CodeModeReadRuntime
	vault     string
	wait      func(context.Context) error
	operation diagnostics.Operation
}

func (r *diagnosticReadRuntime) VaultPath() string { return r.vault }

func (r *diagnosticReadRuntime) WaitForCodeIndex(ctx context.Context) error {
	r.operation = diagnostics.OperationFromContext(ctx)
	return r.wait(ctx)
}

func newDiagnosticAgentOpHost(root string, runtime *diagnosticReadRuntime) *agentOpHost {
	return &agentOpHost{
		rt:         &bootstrap.LiveRuntime{VaultPath: root},
		configPath: filepath.Join(root, ".rhizome", "config.yml"),
		reads:      &actions.CodeModeRuntimeReads{Runtime: runtime},
	}
}

func requireRuntimeAgentRequestEvents(t *testing.T, root string, parent, child diagnostics.Operation, operation, status, reason string) []diagnostics.Event {
	t.Helper()
	result, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	require.Equal(t, "request.started", result.Events[0].Name)
	require.Equal(t, "agent.request.finished", result.Events[1].Name)
	require.NotEqual(t, parent.ID, child.ID)
	for _, event := range result.Events {
		require.Equal(t, child.ID, event.OperationID)
		require.Equal(t, parent.ID, event.ParentOperationID)
		require.Equal(t, parent.TraceID, event.TraceID)
		require.Equal(t, operation, event.Attributes["tool"])
		require.Equal(t, "runtime_http", event.Attributes["transport"])
	}
	terminal := result.Events[1]
	level := "ERROR"
	if status == "canceled" {
		level = "INFO"
	}
	require.Equal(t, level, terminal.Level)
	require.Equal(t, status, terminal.Attributes["status"])
	require.Equal(t, reason, terminal.Attributes["reason_code"])
	require.Contains(t, terminal.Attributes, "duration_ms")
	return result.Events
}
