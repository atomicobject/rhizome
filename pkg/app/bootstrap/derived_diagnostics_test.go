package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDerivedDiagnosticsRetainComputationAndPublicationOutcomes(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Project @node(paths: [\"notes/*.md\"]) {\n name: String!\n}\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "PRIVATE_SOURCE.md"), []byte("# Project\n\nname:: PRIVATE_TITLE\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { require.NoError(t, cacheService.Close()); require.NoError(t, store.Close()) })
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	parent := diagnostics.NewOperation("runtime", "serve")
	w.runCtx = diagnostics.WithOperation(diagnostics.WithRecorder(t.Context(), recorder), parent)
	full := diagnostics.NewOperation("index", "index")
	require.NoError(t, recorder.PublishReport(diagnostics.WithOperation(w.runCtx, full), diagnostics.Report{Status: "success"}))
	provider := &controlledWorkloadProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
	w.nodeSyncer = &semantic.OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Dimensions: 8}}
	w.noteSemanticStore = true
	cacheService.MarkDirty("notes/PRIVATE_SOURCE.md", cache.DirtyModified)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	var ticket codeanchor.DerivedWork
	for _, candidate := range work {
		if candidate.Kind == codeanchor.DerivedOntology {
			ticket = candidate
			break
		}
	}
	require.Equal(t, codeanchor.DerivedOntology, ticket.Kind)
	d := &derivedScheduler{watcher: w, ctx: w.runCtx}
	provider.fail.Store(true)
	require.Error(t, d.execute(ticket))
	provider.fail.Store(false)
	require.NoError(t, d.execute(ticket))
	require.ErrorIs(t, d.execute(ticket), errDerivedSuperseded)

	result, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "derived-work"})
	require.NoError(t, err)
	require.Len(t, result.Reports, 3)
	seen := make(map[string]diagnostics.Report)
	for _, report := range result.Reports {
		require.Equal(t, parent.TraceID, report.TraceID)
		require.Equal(t, parent.ID, report.ParentOperationID)
		require.Equal(t, "ontology", report.Attributes["domain"])
		seen[report.Status] = report
	}
	require.Contains(t, seen, "error")
	require.Contains(t, seen, "success")
	require.Equal(t, "source_changed", seen["skipped"].ReasonCode)
	require.Empty(t, seen["skipped"].Error)
	children, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "indexing-job", Status: "skipped"})
	require.NoError(t, err)
	require.Len(t, children.Reports, 1)
	require.Equal(t, seen["skipped"].OperationID, children.Reports[0].ParentOperationID)
	require.Equal(t, "job_skipped", children.Reports[0].ReasonCode)
	require.Empty(t, children.Reports[0].Error)
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{OperationID: children.Reports[0].OperationID})
	require.NoError(t, err)
	for _, event := range events.Events {
		if event.Name == "job.finished" {
			require.Equal(t, "INFO", event.Level)
			require.Equal(t, "skipped", event.Attributes["status"])
		}
	}
	var snapshot indexingperf.Snapshot
	require.NoError(t, json.Unmarshal(seen["success"].Metrics, &snapshot))
	require.Equal(t, int64(1), derivedCounterTotal(snapshot, "derived.acknowledged"))
	spans := make(map[string]string)
	for _, span := range snapshot.Spans {
		spans[span.Name] = span.Status
	}
	require.Equal(t, "ok", spans["embed_ontology_nodes"])
	var publicationMeasured bool
	for _, latency := range snapshot.Latencies {
		if latency.Name == "derived_publication" {
			publicationMeasured = latency.Count > 0
		}
	}
	require.True(t, publicationMeasured)
	latest, err := diagnostics.ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, full.ID, latest.OperationID, "scoped and derived jobs must preserve the protected full-index summary")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_")
	require.Empty(t, stderr.String())
}

func TestPhysicalEmbeddingDiagnosticsRetainRealSharedHTTPRetriesAfterDrain(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	parent := diagnostics.NewOperation("runtime", "serve")
	ctx := diagnostics.WithOperation(diagnostics.WithRecorder(t.Context(), recorder), parent)
	physicalCtx, finishPhysical := observePhysicalEmbeddings(ctx)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("PRIVATE_PROVIDER_RESPONSE"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
	}))
	t.Cleanup(server.Close)
	provider, err := embeddings.NewOpenAIProvider(embeddings.ProviderConfig{APIKey: "synthetic-private-key", Model: "PRIVATE_MODEL", Endpoint: server.URL, Dimensions: 2})
	require.NoError(t, err)
	node := semantic.NewSharedEmbeddingNode(physicalCtx, provider, 1, nil, semantic.EmbedPackerOptions{MaxTexts: 2})
	rt := &LiveRuntime{ctx: ctx}
	rt.addCloser(func() { node.Close(); finishPhysical() })
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	requestCtx, finishRequest := observeDerivedWork(ctx, codeanchor.DerivedWork{DerivedScope: codeanchor.DerivedScope{Kind: codeanchor.DerivedNotes, Path: "PRIVATE_SOURCE.md"}})
	vectors, err := node.Submit(requestCtx, "embed_notes", []string{"PRIVATE_TEXT", "PRIVATE_TEXT"})
	require.NoError(t, err)
	require.Equal(t, []embeddings.Embedding{{1, 0}, {1, 0}}, vectors)
	finishRequest(nil)
	before, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "runtime.embedding-provider"})
	require.NoError(t, err)
	require.Empty(t, before.Reports, "physical summary is available only after the shared nodes drain")
	require.NoError(t, rt.Close())
	finishPhysical()
	result, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "runtime.embedding-provider"})
	require.NoError(t, err)
	require.Len(t, result.Reports, 1, "normal close and repeated finalization publish once")
	report := result.Reports[0]
	require.Equal(t, "success", report.Status)
	require.Equal(t, "nodes_drained", report.ReasonCode)
	require.Equal(t, "shared_runtime_lifetime", report.Attributes["scope"])
	require.Equal(t, parent.ID, report.ParentOperationID)
	require.Equal(t, parent.TraceID, report.TraceID)
	var snapshot indexingperf.Snapshot
	require.NoError(t, json.Unmarshal(report.Metrics, &snapshot))
	for name, want := range map[string]int64{
		"provider.request.openai.attempt": 2, "provider.request.openai.http_retry": 1,
		"provider.request.openai.status.429": 1, "provider.request.openai.status.200": 1,
		"provider.request.openai.backoff.reason.http": 1, "embed.dedupe.saved": 1,
	} {
		require.Equal(t, want, derivedCounterTotal(snapshot, name), name)
	}
	require.Equal(t, int32(2), calls.Load())
	logical, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "derived-work"})
	require.NoError(t, err)
	require.Len(t, logical.Reports, 1)
	require.NoError(t, json.Unmarshal(logical.Reports[0].Metrics, &snapshot))
	require.Zero(t, derivedCounterTotal(snapshot, "provider.request.openai.attempt"), "physical retries must not be attributed to one ticket")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_")
	require.NotContains(t, string(encoded), "synthetic-private-key")
	require.Empty(t, stderr.String())
}

func derivedCounterTotal(snapshot indexingperf.Snapshot, name string) int64 {
	var total int64
	for _, counter := range snapshot.Counters {
		if counter.Name == name {
			total += counter.Total
		}
	}
	return total
}

func TestDerivedDiagnosticsRetainExternalPriorityCancellationCause(t *testing.T) {
	root := t.TempDir()
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { require.NoError(t, cacheService.Close()); require.NoError(t, store.Close()) })
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	ctx := diagnostics.WithRecorder(t.Context(), recorder)
	ctx, finish := observeDerivedWork(ctx, codeanchor.DerivedWork{DerivedScope: codeanchor.DerivedScope{Kind: codeanchor.DerivedNotes}})
	priority, err := indexlock.RequestPriority(obsidian.IndexLockPath(root))
	require.NoError(t, err)
	t.Cleanup(priority.Close)
	d := &derivedScheduler{watcher: w, ctx: ctx}
	err = d.compute(ctx, "embed_notes", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, errDerivedExternalPriority)
	finish(err)
	result, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "derived-work"})
	require.NoError(t, err)
	require.Len(t, result.Reports, 1)
	require.Equal(t, "preempted", result.Reports[0].Status)
	require.Equal(t, "external_priority", result.Reports[0].ReasonCode)
	require.Empty(t, stderr.String())
}
