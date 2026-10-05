package bootstrap

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestCapabilityFailurePreservesProtocolOutputAndDiagnosticHistory(t *testing.T) {
	for _, oneShot := range []bool{true, false} {
		name := "runtime"
		if oneShot {
			name = "one-shot"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var stderr bytes.Buffer
			recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
			require.NoError(t, err)
			defer recorder.Close()
			rt := &LiveRuntime{
				ctx: diagnostics.WithRecorder(context.Background(), recorder), disableLeaderWork: oneShot,
				searchReady: make(chan struct{}), semanticReady: make(chan struct{}), codeReady: make(chan struct{}),
			}
			capabilityErr := error(os.ErrNotExist)
			rt.codeErr.Store(&capabilityErr)
			close(rt.codeReady)
			rt.observeCapabilityPhase("code")()
			reports, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "runtime.capability"})
			require.NoError(t, err)
			require.Empty(t, reports.Reports, "capability phases retain events without per-phase report publication")
			require.Empty(t, stderr.String())
			events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
			require.NoError(t, err)
			require.Len(t, events.Events, 2)
			event := events.Events[1]
			require.Equal(t, "runtime.capability.finished", event.Name)
			require.Equal(t, "ERROR", event.Level)
			require.Equal(t, "error", event.Attributes["status"])
			require.Equal(t, "not_found", event.Attributes["reason_code"])
			require.Equal(t, false, event.Attributes["ready"])
			require.Contains(t, event.Attributes, "duration_ms")
		})
	}
}

func TestCapabilityEventsRetainReadinessAndSkippedReason(t *testing.T) {
	for _, test := range []struct {
		name, status, reason string
		ready, readOnly      bool
		err                  error
	}{
		{name: "ready", status: "success", ready: true},
		{name: "unindexed read", status: "skipped", reason: "index_missing", readOnly: true, err: os.ErrNotExist},
		{name: "not requested", status: "skipped", reason: "capability_not_requested", err: capabilityNotRequested(RuntimeCapabilityCodeIndex)},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			recorder, err := diagnostics.Open(root, diagnostics.Options{})
			require.NoError(t, err)
			parent := diagnostics.NewOperation("command", "agent files")
			rt := &LiveRuntime{
				ctx:               diagnostics.WithOperation(diagnostics.WithRecorder(context.Background(), recorder), parent),
				readOnlyCodeIndex: test.readOnly,
				searchReady:       make(chan struct{}), semanticReady: make(chan struct{}), codeReady: make(chan struct{}),
			}
			if test.err != nil {
				rt.codeErr.Store(&test.err)
			}
			close(rt.codeReady)
			rt.observeCapabilityPhase("code")()
			require.NoError(t, recorder.Close())
			events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
			require.NoError(t, err)
			require.Len(t, events.Events, 2)
			event := events.Events[1]
			require.Equal(t, "INFO", event.Level)
			require.Equal(t, test.status, event.Attributes["status"])
			require.Equal(t, test.reason, event.Attributes["reason_code"])
			require.Equal(t, test.ready, event.Attributes["ready"])
			require.Contains(t, event.Attributes, "duration_ms")
			require.Equal(t, parent.ID, event.ParentOperationID)
			require.Equal(t, parent.TraceID, event.TraceID)
		})
	}
}
