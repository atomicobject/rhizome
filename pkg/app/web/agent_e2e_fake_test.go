//go:build e2efake

package web

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestAgentE2EFakeRequiresApprovalThenRespondsPong(t *testing.T) {
	require.NotNil(t, agentHarnessOverride)
	driver := agentHarnessOverride()[harness.KindCodex]
	status, err := driver.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, "e2e-fake", status.Version)
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Stop()) })

	turnResult := make(chan error, 1)
	go func() { turnResult <- session.SendTurn(context.Background(), "ping") }()
	require.Equal(t, harness.EventTurnStarted, (<-session.Events()).Kind)
	approval := <-session.Events()
	require.Equal(t, harness.EventApprovalRequested, approval.Kind)
	require.Equal(t, "printf pong", approval.Command)
	require.NoError(t, session.Respond(approval.RequestID, harness.DecisionAllow))

	var kinds []harness.EventKind
	var answer string
	for {
		event := <-session.Events()
		kinds = append(kinds, event.Kind)
		if event.Kind == harness.EventAssistantTextDelta {
			answer += event.Text
		}
		if event.Kind == harness.EventTurnCompleted {
			break
		}
	}
	require.NoError(t, <-turnResult)
	require.Equal(t, "pong", answer)
	require.Contains(t, kinds, harness.EventCommandExecution)
	require.Contains(t, kinds, harness.EventTokenUsage)
}
