package indexcore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestSharedCoreMetricsDescribeFailedPublicationAndStructuralRecovery(t *testing.T) {
	collector := indexingperf.NewBounded()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	f := newFixtureWithContext(t, ctx)
	discovery, err := indexcore.Discover(ctx, f.request, f.store)
	require.NoError(t, err)
	failure := errors.New("synthetic publication interruption")
	_, err = indexcore.Publish(ctx, f.request, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{AfterPreparedOwnershipCommit: func(context.Context) error { return failure }})
	require.ErrorIs(t, err, failure)
	scoped := f.request
	scoped.Paths = []paths.RelPath{"notes/source.md"}
	discovery, err = indexcore.Discover(ctx, scoped, f.store)
	require.NoError(t, err)
	require.True(t, discovery.Recovery)
	require.True(t, discovery.FullDiscovery)
	result, err := indexcore.Publish(ctx, scoped, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{})
	require.NoError(t, err)
	require.NoError(t, f.writer.AcknowledgeOwnershipReconciliation(ctx, result.StructuralGeneration))
	require.NoError(t, f.writer.Close())
	snapshot := collector.Snapshot()
	counts := map[string]int64{}
	for _, counter := range snapshot.Counters {
		counts[counter.Name] += counter.Total
	}
	require.Equal(t, int64(1), counts["ownership.pending_reconciliation"])
	require.Equal(t, int64(1), counts["derived.recovery.inactive"])
	require.Equal(t, int64(1), counts["ownership.reconciliation_acknowledged"])
	require.Equal(t, int64(2), counts["ownership.discovery.complete"])
	require.Positive(t, counts["structural.note_candidates"])
	require.Positive(t, counts["queue.rows.committed"])
	spans := map[string]indexingperf.SpanSnapshot{}
	for _, span := range snapshot.Spans {
		spans[span.Name] = span
	}
	require.Equal(t, int64(2), spans["ownership_discovery"].Count)
	require.Equal(t, int64(2), spans["structural_publication"].Count)
	require.Equal(t, "error", spans["structural_publication"].Status)
	require.Equal(t, int64(1), spans["ingest_notes"].Count)
	require.Equal(t, int64(1), spans["sync_ontology"].Count)
}
