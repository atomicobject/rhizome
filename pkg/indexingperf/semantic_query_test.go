package indexingperf

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSemanticQueryDiagnosticsExposeStablePhasesAndOperationAvailability(t *testing.T) {
	collector := New()
	ctx := WithCollector(context.Background(), collector)
	done := StartSpan(ctx, SemanticQueryPhaseBootstrap)
	done(nil)
	AddCount(ctx, SemanticQueryOpFallbackStoreOpens, 1)

	diagnostics := collector.SemanticQueryDiagnostics()
	require.Equal(t, SemanticQueryPhaseTotal, diagnostics.Phases[0].Label)
	require.Equal(t, int64(1), diagnostics.Phases[0].Count)
	require.True(t, diagnostics.Phases[0].Available)
	require.Equal(t, SemanticQueryPhaseBootstrap, diagnostics.Phases[1].Label)
	require.Equal(t, int64(1), diagnostics.Phases[1].Count)
	require.True(t, diagnostics.Phases[1].Available)

	operations := make(map[string]SemanticQueryOperationDiagnostic, len(diagnostics.Operations))
	for _, operation := range diagnostics.Operations {
		operations[operation.Label] = operation
	}
	require.Equal(t, int64(1), operations[SemanticQueryOpFallbackStoreOpens].Count)
	require.True(t, operations[SemanticQueryOpFallbackStoreOpens].Available)
	require.False(t, operations[SemanticQueryOpIntegrityChecks].Available)
	require.True(t, operations[SemanticQueryOpVectorKNNQueries].Available)
	require.Zero(t, operations[SemanticQueryOpVectorKNNQueries].Count)
	require.True(t, operations[SemanticQueryOpVectorTieRetries].Available)
	require.True(t, operations[SemanticQueryOpVectorScalarFallbacks].Available)
}

func TestSemanticQueryDiagnosticsMarksUnobservedPhasesUnavailable(t *testing.T) {
	collector := New()
	diagnostics := collector.SemanticQueryDiagnostics()

	var searchPhase SemanticQueryPhaseDiagnostic
	for _, phase := range diagnostics.Phases {
		if phase.Label == SemanticQueryPhaseSearch {
			searchPhase = phase
			break
		}
	}
	require.False(t, searchPhase.Available)
	require.Zero(t, searchPhase.Count)
	require.LessOrEqual(t, diagnostics.Phases[0].DurationMs, time.Since(collector.startedAt).Milliseconds())
}

func TestSemanticQueryCollectorDropsUnrelatedHighVolumeMetrics(t *testing.T) {
	collector := NewSemanticQueryCollector()
	ctx := WithCollector(context.Background(), collector)
	AddCount(ctx, "unrelated.hot_loop", 1000)
	AddCount(ctx, AgentStartOpNoteReads, 2)

	require.Empty(t, collector.counters[Metric{Name: "unrelated.hot_loop", Kind: kindCounter}])
	diagnostics := collector.SemanticQueryDiagnostics()
	require.Equal(t, int64(2), diagnostics.Operations[2].Count)
}
