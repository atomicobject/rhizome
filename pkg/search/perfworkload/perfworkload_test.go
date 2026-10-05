package perfworkload

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestGeneratedWorkloadRunsThroughUnifiedSearch(t *testing.T) {
	manifest := Manifest{
		Version:    ManifestVersion,
		Owners:     OwnerManifest{Total: 8, Code: 5, Prose: 3, ChunksPerOwner: 2},
		Graph:      GraphManifest{EdgesPerCodeOwner: 2, EdgesPerNoteOwner: 2},
		Embeddings: EmbeddingManifest{Provider: "deterministic", Model: "test", Dimensions: 8},
		Queries:    []Query{{ID: "code", Text: "Owner0002", Intent: "search", LaneMode: "hybrid", Types: []string{"code"}, Pack: true}},
	}
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", IndexFilename))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, populateOwners(context.Background(), root, store, manifest))
	require.NoError(t, populateNoteGraph(context.Background(), store, manifest))
	require.NoError(t, populateChunks(context.Background(), store, manifest))
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetPackMetadata(context.Background(), codeanchor.PackMetadata{ConfigHash: "test", ModelHash: "test", AlgoVersion: "test", IndexerVersion: codeanchor.IndexerVersion}))

	var owners, chunks, vectors int
	require.NoError(t, store.DB().QueryRow("SELECT (SELECT COUNT(*) FROM intel_code_anchors) + (SELECT COUNT(*) FROM intel_doc_sections), (SELECT COUNT(*) FROM intel_chunks), (SELECT COUNT(*) FROM intel_embeddings)").Scan(&owners, &chunks, &vectors))
	require.Equal(t, 8, owners)
	require.Equal(t, 16, chunks)
	require.Equal(t, chunks, vectors)

	sample := Measure(context.Background(), root, store, manifest, unifiedsearch.ProfileInteractive, 1, "test", manifest.Queries[0])
	require.Empty(t, sample.Error)
	require.Positive(t, sample.ReturnedSources)
	require.Positive(t, sample.ProviderCalls)
	require.Positive(t, sample.ResponseBytes)
	require.Positive(t, sample.BodyReads)
	require.Positive(t, sample.BodyReadBytes)
	require.LessOrEqual(t, sample.BodyReads, int64(sample.ReturnedSources), "packing must hydrate only the selected canonical page")

	graphQuery := Query{ID: "graph", Text: "related guides", Intent: "related_to_seed", LaneMode: "graph", Seeds: []string{"notes/domain-00/guide_0000.md"}}
	graphSample := Measure(context.Background(), root, store, manifest, unifiedsearch.ProfileInteractive, 1, "test", graphQuery)
	require.Empty(t, graphSample.Error)
	require.Positive(t, graphSample.LaneResultCounts["graph"], "%+v", graphSample)
	require.Positive(t, graphSample.ReturnedSources, "%+v", graphSample)
}

func TestAggregateSamplesRetainsFailedObservationLatency(t *testing.T) {
	groups := AggregateSamples([]Sample{
		{Profile: unifiedsearch.ProfileAgent, Concurrency: 4, Temperature: "warm", QueryID: "q", LaneMode: "hybrid", DurationMS: 5},
		{Profile: unifiedsearch.ProfileAgent, Concurrency: 4, Temperature: "warm", QueryID: "q", LaneMode: "hybrid", DurationMS: 90, Error: "deadline", DeadlineExceeded: true},
	})
	require.Len(t, groups, 1)
	require.Equal(t, 2, groups[0].Samples)
	require.Equal(t, 1, groups[0].Errors)
	require.Equal(t, 1, groups[0].Deadlines)
	require.Equal(t, 90.0, groups[0].DurationMS.Max)
}

func TestRequiredLaneErrorRejectsPartialMixedVectorRetrieval(t *testing.T) {
	query := Query{ID: "mixed", LaneMode: "hybrid"}
	lanes := []search.LaneStatus{
		{Lane: "note_vector", Status: search.LaneStateRan, ResultCount: 3},
		{Lane: "code_vector", Status: search.LaneStateDegraded, Reason: "provider failed"},
	}
	require.Equal(t, "required code_vector lane was degraded with 0 results: provider failed", requiredLaneError(query, lanes))
}
