package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestWithIndexTimingsPrintsSummary(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ctx, finish := withIndexTimings(context.Background(), &out, true)

	phaseCtx := indexingperf.WithPhase(ctx, "embed_notes")
	done := indexingperf.StartSpan(phaseCtx, "embed_notes")
	indexingperf.ObserveLatency(phaseCtx, "provider.latency", 15*time.Millisecond)
	indexingperf.AddCount(phaseCtx, "semantic.chunks", 4)
	done(nil)

	finish(nil)

	text := out.String()
	require.Contains(t, text, "Index timings:")
	require.Contains(t, text, "embed_notes")
	require.Contains(t, text, "chunks=4")
	require.Contains(t, text, "provider_cum=15ms")
	require.Contains(t, text, "total")
}

func TestWithIndexTimingsPersistsSilentFailedAndSuccessfulAttempts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var warnings bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &warnings})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	collector := indexingperf.NewBounded()
	base := indexingperf.WithCollector(diagnostics.WithRecorder(context.Background(), recorder), collector)
	var out bytes.Buffer
	ctx, finish := withIndexTimings(base, &out, false, indexDiagnosticOptions{Trigger: "rebuild", Rebuild: true})
	require.Same(t, collector, indexingperf.FromContext(ctx))
	indexingperf.AddCount(ctx, "test.pipeline_inputs", 3)
	finish(errors.New("provider response body must never persist"))
	finish(nil) // Finishing again cannot overwrite the actual failure.
	failed, err := diagnostics.ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, "error", failed.Status)
	require.Equal(t, "rebuild", failed.Trigger)
	require.Equal(t, true, failed.Attributes["rebuild"])
	require.Equal(t, "operation_failed", failed.Error)
	require.Contains(t, string(failed.Metrics), "test.pipeline_inputs")
	_, successFinish := withIndexTimings(diagnostics.WithRecorder(context.Background(), recorder), &out, false)
	successFinish(nil)
	success, err := diagnostics.ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, "success", success.Status, "diagnostic warnings: %s", warnings.String())
	require.NotEqual(t, failed.OperationID, success.OperationID)
	require.Empty(t, out.String(), "persistent collection must not enable console timings")
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "index"})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 2)
}

func TestWithIndexTimingsDisabledIsSilent(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ctx, finish := withIndexTimings(context.Background(), &out, false)
	require.NotNil(t, ctx)
	finish(nil)
	require.Empty(t, out.String())
}

func TestWithIndexTimingsIncludesOntologyPhasesAndCounters(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ctx, finish := withIndexTimings(context.Background(), &out, true)

	planCtx := indexingperf.WithPhase(ctx, "plan_ontology")
	donePlan := indexingperf.StartSpan(planCtx, "plan_ontology")
	donePlan(nil)

	computeCtx := indexingperf.WithPhase(ctx, "compute_ontology")
	doneCompute := indexingperf.StartSpan(computeCtx, "compute_ontology")
	indexingperf.AddCount(computeCtx, "ontology.notes_considered", 3)
	indexingperf.AddCount(computeCtx, "ontology.notes_skipped", 1)
	indexingperf.AddCount(computeCtx, "ontology.notes_assessed", 2)
	indexingperf.AddCount(computeCtx, "ontology.relations", 4)
	doneCompute(nil)

	writeCtx := indexingperf.WithPhase(ctx, "write_ontology")
	doneWrite := indexingperf.StartSpan(writeCtx, "write_ontology")
	indexingperf.AddCount(writeCtx, "ontology.delta_rows", 7)
	indexingperf.ObserveDBWrite(indexingperf.WithOp(writeCtx, "intel.apply_ontology_delta"), "intel-store", 0, 2*time.Millisecond)
	doneWrite(nil)

	finish(nil)

	text := out.String()
	require.Contains(t, text, "plan_ontology")
	require.Contains(t, text, "compute_ontology")
	require.Contains(t, text, "write_ontology")
	require.Contains(t, text, "notes=3")
	require.Contains(t, text, "skipped=1")
	require.Contains(t, text, "assessed=2")
	require.Contains(t, text, "relations=4")
	require.Contains(t, text, "delta_rows=7")
	require.Contains(t, text, "intel.apply_ontology_delta")
}
