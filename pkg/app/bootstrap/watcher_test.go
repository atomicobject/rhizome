package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func testNoteMetadataIndexer(t *testing.T) notemeta.Indexer {
	t.Helper()
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(formats)
	require.NoError(t, err)
	return indexer
}

func testNoteRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	return runtime
}

func TestClassifyInternalChange(t *testing.T) {
	require.Equal(t, internalChangeEffect{reloadVaultDef: true, refreshMeta: true, refreshOntology: true, invalidateCapabilities: true}, classifyInternalChange(".rhizome/config.yml"))
	require.Equal(t, internalChangeEffect{refreshMeta: true, refreshOntology: true}, classifyInternalChange(".rhizome/ignore"))
	require.Equal(t, internalChangeEffect{refreshOntology: true, invalidateCapabilities: true}, classifyInternalChange(".rhizome/ontology/schema.graphql"))
	require.Equal(t, internalChangeEffect{invalidateQueryRecipes: true, invalidateCapabilities: true}, classifyInternalChange(".rhizome/query-recipes/spec-driven.yaml"))
	require.Equal(t, internalChangeEffect{}, classifyInternalChange(".rhizome/db.sqlite"))
}

type publishedEvent struct {
	Kind string
	Data any
}

func recordingWatcher(t *testing.T, published chan publishedEvent) *unifiedSemanticWatcher {
	return &unifiedSemanticWatcher{
		noteMetadataIndexer: testNoteMetadataIndexer(t),
		publishEvent: func(kind string, data any) {
			published <- publishedEvent{Kind: kind, Data: data}
		},
	}
}

func TestUnifiedWatcherPublishesProcessedPathEvents(t *testing.T) {
	published := make(chan publishedEvent, 4)
	w := recordingWatcher(t, published)

	w.publishProcessedEvents(false, []string{"notes/a.md"}, nil, []string{"main.go"}, nil, nil, false, false)

	changed := <-published
	require.Equal(t, "node.changed", changed.Kind)
	require.Equal(t, map[string]any{"paths": []string{"notes/a.md", "main.go"}, "source": "vault-runtime", "domains": []string{"metadata", "ontology", "code"}}, changed.Data)
	invalidated := <-published
	require.Equal(t, globalEventValidationInvalidated, invalidated.Kind)
	require.Empty(t, published)
}

func TestUnifiedWatcherPublishesResyncInvalidationWithoutPathChange(t *testing.T) {
	published := make(chan publishedEvent, 4)
	w := recordingWatcher(t, published)

	w.publishProcessedEvents(true, nil, nil, nil, nil, nil, false, false)

	index := <-published
	require.Equal(t, globalEventIndexInvalidated, index.Kind)
	require.Equal(t, map[string]any{"reason": "resync", "source": "vault-runtime"}, index.Data)
	require.Equal(t, globalEventValidationInvalidated, (<-published).Kind)
	require.Empty(t, published)
}

func TestProcessedFreshnessEventsClassifiesInternalInvalidations(t *testing.T) {
	events := processedFreshnessEvents(nil, nil, nil, nil, []string{".rhizome/ontology/schema.graphql", ".rhizome/config.yml", ".rhizome/query-recipes/spec-driven.yaml"}, true, true)
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	require.NotContains(t, kinds, "node.changed", "schema/config paths are not committed node identities")
	require.Contains(t, kinds, globalEventValidationInvalidated)
	require.Contains(t, kinds, globalEventSchemaInvalidated)
	require.Contains(t, kinds, globalEventCapabilitiesInvalidated)
	require.Contains(t, kinds, globalEventQueryRecipeInvalidated)
}

func TestUnifiedWatcherNodeSyncFailureRetainsDurableRetryWithoutStructuralInvalidation(t *testing.T) {
	root := t.TempDir()
	writeWatcherConfig(t, root, "both")
	writeWatcherSchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	notePath := filepath.Join(root, "notes", "project.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
type: Project
name: Roadmap
---
old
`), 0o644))

	cacheSvc, err := cache.NewService(root, cache.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cacheSvc.Close() })
	require.NoError(t, cacheSvc.EnsureReady(context.Background()))

	store, err := anchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	noteSvc, _ := NewCodeAnchorService(CodeAnchorServiceConfig{
		VaultPath:   root,
		Store:       store,
		WriteAccess: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &recordingEmbeddingProvider{}
	provider.fail.Store(true)
	published := make(chan publishedEvent, 16)
	indexLane := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(root), PriorityPoll: 20 * time.Millisecond})
	t.Cleanup(indexLane.Close)
	w := &unifiedSemanticWatcher{
		runCtx:              ctx,
		lane:                indexLane,
		vaultPath:           root,
		vaultDef:            obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		cacheService:        cacheSvc,
		noteSvc:             noteSvc,
		noteRuntime:         testNoteRuntime(t),
		noteMetadataIndexer: testNoteMetadataIndexer(t),
		intelStore:          store,
		nodeSyncer:          &semantic.OntologyNodeSyncer{Store: store, Provider: provider},
		health:              newLiveHealthTracker(root),
		publishEvent: func(kind string, data any) {
			published <- publishedEvent{Kind: kind, Data: data}
		},
		opts: UnifiedSemanticWatcherOptions{}.normalize(),
	}

	w.derived = newDerivedScheduler(w)
	t.Cleanup(func() { cancel(); w.derived.close() })

	require.NoError(t, os.WriteFile(notePath, []byte(`---
type: Project
name: Roadmap
---
new
`), 0o644))
	cacheSvc.MarkDirty("notes/project.md", cache.DirtyModified)
	failedEmbedding := w.processOwnershipBatch()
	terminal := watcherJobTerminal(t, failedEmbedding)
	require.NoError(t, failedEmbedding.Err(), "ontology-node embeddings remain advisory")
	require.Equal(t, lane.OutcomeOK, terminal.Outcome)
	require.True(t, terminal.OK)
	require.NoError(t, indexLane.Status().LastError)

	var changedPaths []any
	require.Eventually(t, func() bool {
		for {
			select {
			case event := <-published:
				require.NotEqual(t, globalEventIndexInvalidated, event.Kind, "node embedding failure must not invalidate the index")
				if event.Kind == "node.changed" {
					data, _ := event.Data.(map[string]any)
					changedPaths, _ = data["paths"].([]any)
					if paths, ok := data["paths"].([]string); ok {
						changedPaths = make([]any, 0, len(paths))
						for _, path := range paths {
							changedPaths = append(changedPaths, path)
						}
					}
					return true
				}
			default:
				return false
			}
		}
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []any{"notes/project.md"}, changedPaths)
	_, last := w.health.snapshot()
	require.NotNil(t, last)
	require.Equal(t, "completed", last.Status)
	require.Empty(t, last.DegradedReasons)
	pending, err := store.PendingDerivedWork(ctx, time.Now().Add(time.Minute), 100)
	require.NoError(t, err)
	require.NotEmpty(t, pending)
	require.Zero(t, cacheSvc.Metrics().StaleCount, "node embedding failure must not mark the cache stale")
	failedEpoch := last.ID

	// Once the provider recovers, an unrelated change retries the failed path.
	provider.fail.Store(false)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "other.md"), []byte("unrelated\n"), 0o644))
	cacheSvc.MarkDirty("notes/other.md", cache.DirtyCreated)
	retry := w.processOwnershipBatch()
	require.Equal(t, lane.OutcomeOK, watcherJobTerminal(t, retry).Outcome)
	require.NoError(t, retry.Err())
	require.Eventually(t, func() bool {
		_, last := w.health.snapshot()
		return last != nil && last.ID != failedEpoch && last.Status == "completed"
	}, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		pending, err := store.PendingDerivedWork(ctx, time.Now().Add(time.Minute), 100)
		return err == nil && len(pending) == 0
	}, 5*time.Second, 10*time.Millisecond)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Contains(t, strings.Join(provider.texts, "\n"), "Roadmap")
}

func TestLiveHealthTrackerEpochCompleteAndFailedStatus(t *testing.T) {
	root := t.TempDir()
	tracker := newLiveHealthTracker(root)
	completed := tracker.begin(map[string]cache.DirtyKind{"notes/a.md": cache.DirtyModified}, false)
	tracker.phase(completed.ID, "metadata-ontology")
	tracker.complete(completed.ID)

	current, last := tracker.snapshot()
	require.Nil(t, current)
	require.NotNil(t, last)
	require.Equal(t, "completed", last.Status)
	require.Equal(t, "complete", last.Phase)

	failed := tracker.begin(map[string]cache.DirtyKind{"notes/b.md": cache.DirtyRemoved}, false)
	tracker.fail(failed.ID, "delete-cleanup", os.ErrPermission)
	current, last = tracker.snapshot()
	require.Nil(t, current)
	require.NotNil(t, last)
	require.Equal(t, "failed", last.Status)
	require.Equal(t, "delete-cleanup", last.Phase)
	require.Contains(t, last.Error, "permission")

	_, err := os.Stat(filepath.Join(root, ".rhizome", "live-epochs.jsonl"))
	require.ErrorIs(t, err, os.ErrNotExist, "health remains in memory; durable aggregate evidence belongs to diagnostics")
}

func receiveProcessedPath(t *testing.T, processed <-chan string) string {
	t.Helper()
	select {
	case rel := <-processed:
		return rel
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for processed path")
		return ""
	}
}

func writeWatcherConfig(t *testing.T, root, links string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"notes/*.md\"]\n  links: "+links+"\n"), 0o644))
}

func writeWatcherSchema(t *testing.T, root, schema string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(schema), 0o644))
}

func TestProcessedFreshnessEventsInvalidateAllValidationInputs(t *testing.T) {
	for _, test := range []struct {
		name                               string
		codeChanges, codeDeletes, internal []string
		ontology                           bool
	}{
		{name: "code changed", codeChanges: []string{"main.go"}},
		{name: "code deleted", codeDeletes: []string{"main.go"}},
		{name: "schema", internal: []string{".rhizome/ontology/schema.graphql"}, ontology: true},
		{name: "recipes", internal: []string{".rhizome/query-recipes/example.yaml"}},
		{name: "schema resync", ontology: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := processedFreshnessEvents(nil, nil, test.codeChanges, test.codeDeletes, test.internal, false, test.ontology)
			count := 0
			for _, event := range events {
				if event.Kind == globalEventValidationInvalidated {
					count++
				}
			}
			require.Equal(t, 1, count)
		})
	}
}
