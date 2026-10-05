package indexingperf

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentStartDiagnosticsStableProjection(t *testing.T) {
	collector := New()
	ctx := WithCollector(context.Background(), collector)

	collector.RecordSpan(AgentStartPhaseTotal, 12*time.Millisecond, nil)
	collector.RecordSpan(AgentStartPhaseVaultContext, 5*time.Millisecond, nil)
	AddCount(ctx, AgentStartOpNotePasses, 1)
	AddCount(ctx, AgentStartOpNoteReads, 3)

	diagnostics := collector.AgentStartDiagnostics()
	require.Len(t, diagnostics.Phases, len(agentStartPhaseDescriptors))
	require.Len(t, diagnostics.Operations, len(agentStartOperationDescriptors))
	require.Equal(t, AgentStartPhaseTotal, diagnostics.Phases[0].Label)
	require.Equal(t, int64(12), diagnostics.Phases[0].DurationMs)
	require.Equal(t, AgentStartPhaseVaultContext, diagnostics.Phases[8].Label)
	require.Equal(t, int64(5), diagnostics.Phases[8].DurationMs)
	require.Equal(t, AgentStartOpNotePasses, diagnostics.Operations[0].Label)
	require.True(t, diagnostics.Operations[0].Available)
	require.Equal(t, int64(1), diagnostics.Operations[0].Count)
	require.Equal(t, AgentStartOpNoteReads, diagnostics.Operations[2].Label)
	require.Equal(t, int64(3), diagnostics.Operations[2].Count)
	require.Equal(t, AgentStartOpSchemaStatements, diagnostics.Operations[4].Label)
	require.False(t, diagnostics.Operations[4].Available)
	require.Equal(t, int64(0), diagnostics.Operations[4].Count)
	require.Equal(t, AgentStartOpIntegrityChecks, diagnostics.Operations[5].Label)
	require.False(t, diagnostics.Operations[5].Available)
	require.Equal(t, int64(0), diagnostics.Operations[5].Count)
	// Zero-valued descriptors remain present so benchmark consumers do not infer
	// instrumentation absence from a missing field.
	require.Equal(t, int64(0), diagnostics.Operations[len(diagnostics.Operations)-1].Count)
}

func TestAgentStartDiagnosticsMarksAttemptedIntegrityCheckAvailable(t *testing.T) {
	collector := New()
	ctx := WithCollector(context.Background(), collector)
	AddCount(ctx, AgentStartOpIntegrityChecks, 1)

	diagnostics := collector.AgentStartDiagnostics()
	require.Equal(t, AgentStartOpIntegrityChecks, diagnostics.Operations[5].Label)
	require.True(t, diagnostics.Operations[5].Available)
	require.Equal(t, int64(1), diagnostics.Operations[5].Count)
}

func TestAgentStartDiagnosticsMarksMeasuredSchemaProbeAvailable(t *testing.T) {
	collector := New()
	ctx := WithCollector(context.Background(), collector)
	AddCount(ctx, AgentStartOpSchemaStatements, 2)

	diagnostics := collector.AgentStartDiagnostics()
	require.Equal(t, AgentStartOpSchemaStatements, diagnostics.Operations[4].Label)
	require.True(t, diagnostics.Operations[4].Available)
	require.Equal(t, int64(2), diagnostics.Operations[4].Count)
}

func TestAgentStartDiagnosticsIndexedEnrichmentKeepsLiveWorkCountersZero(t *testing.T) {
	collector := New()
	ctx := WithCollector(context.Background(), collector)
	AddCount(ctx, AgentStartOpIndexedReads, 5)
	AddCount(ctx, AgentStartOpIndexedResults, 12)
	AddCount(ctx, AgentStartOpIndexedStatusAvailable, 1)

	byLabel := map[string]AgentStartOperationDiagnostic{}
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		byLabel[operation.Label] = operation
	}
	for _, label := range []string{
		AgentStartOpNotePasses, AgentStartOpRepoWalks, AgentStartOpNoteReads, AgentStartOpCodeReads,
		AgentStartOpIndexWrites,
	} {
		require.Zero(t, byLabel[label].Count, label)
	}
	require.Equal(t, int64(5), byLabel[AgentStartOpIndexedReads].Count)
	require.Equal(t, int64(12), byLabel[AgentStartOpIndexedResults].Count)
	require.Equal(t, int64(1), byLabel[AgentStartOpIndexedStatusAvailable].Count)
}
