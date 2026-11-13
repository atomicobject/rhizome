package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestWatcherFullDiscoveryReparsesOldIndexerBeforeBootCatchUp(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/config.yml"), []byte("code:\n  enabled: true\n  goRoots: [src]\nnoteEmbeddings: {enabled: false}\ncodeEmbeddings: {enabled: false}\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/value.go"), []byte("package example\n// Value follows [[note]].\nfunc Value() int { return 1 }\n"), 0o600))
	configuration, err := repoconfig.Load(root)
	require.NoError(t, err)
	definition := repoconfig.LocalConfigToDefinition(root, configuration.Local)
	indexLane := lane.New(lane.Options{LockPath: repoconfig.IndexLockPath(root)})
	defer indexLane.Close()
	batch := func() {
		t.Helper()
		handle, _, err := indexLane.Submit(t.Context(), lane.Request{Kind: lane.KindBootCatchUp,
			Run: func(ctx context.Context, lease *indexlock.Lease, _ lane.Reporter) error {
				return indexing.RunUnifiedCore(ctx, lease, indexing.UnifiedOptions{
					VaultPath: root, VaultDef: definition, Config: &configuration,
					NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true,
				})
			}})
		require.NoError(t, err)
		watcherJobTerminal(t, handle)
		require.NoError(t, handle.Err())
	}
	batch()
	store, err := semdb.Open(configuration.Code.IndexPath)
	require.NoError(t, err)
	var originalComment string
	require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT doc_comment FROM intel_code_anchors WHERE path='src/value.go'`).Scan(&originalComment))
	seed, _, err := indexLane.Submit(t.Context(), lane.Request{Kind: lane.KindBootCatchUp,
		Run: func(ctx context.Context, _ *indexlock.Lease, _ lane.Reporter) error {
			_, err := store.DB().ExecContext(ctx, `UPDATE files SET indexer_version='old-indexer'; UPDATE intel_code_anchors SET doc_comment='old parser output'; DELETE FROM doc_links`)
			if err != nil {
				return err
			}
			if err := store.SetIndexerVersion(ctx, "old-indexer"); err != nil {
				return err
			}
			return store.SetReverseIndexVersion(ctx, "old-reverse-index")
		}})
	require.NoError(t, err)
	watcherJobTerminal(t, seed)
	require.NoError(t, seed.Err())
	require.NoError(t, store.Close())
	rt, err := NewLiveRuntime(t.Context(), LiveOptions{
		VaultName: root, DisableLeaderWork: true, DisableWatchHub: true, DisableSessionStore: true,
		Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilityCodeIndex, RuntimeCapabilityCodeAnchorWarmup),
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, rt.Close()) }()
	require.NoError(t, rt.WaitForSearch(t.Context()))
	require.NoError(t, rt.WaitForCodeIndex(t.Context()))
	store = rt.IntelStore()
	ctx, cancel := context.WithCancel(t.Context())
	w := StartUnifiedSemanticWatcher(ctx, root, WatcherDeps{
		VaultDef: definition, NoteRuntime: rt.noteFormats, CodeConfig: configuration.Code,
		NoteMetadataIndexer: rt.NoteMetadataIndexer(), CacheService: rt.Cache(),
		NoteSvc: rt.CodeAnchorService(), IntelStore: store, Lane: indexLane,
		Options: &UnifiedSemanticWatcherOptions{Tick: make(chan time.Time)},
	}, false)
	defer func() { cancel(); indexLane.Close(); w.stopValidationTimer(); w.stopWatchHub(); w.closeDerived() }()
	require.NoError(t, rt.Cache().EnsureReady(t.Context()))
	interrupted := errors.New("stop before parsing")
	inspected := false
	w.afterPreparedOwnershipCommit = func(ctx context.Context) error {
		version, _, err := store.IndexerVersion(ctx)
		require.NoError(t, err)
		require.Equal(t, "old-indexer", version)
		reverse, _, err := store.ReverseIndexVersion(ctx)
		require.NoError(t, err)
		require.Equal(t, "old-reverse-index", reverse)
		inspected = true
		return interrupted
	}
	// A resync waits for a cache recrawl still running from startup, so a
	// batch can finish without discovery; drive batches until the full
	// discovery reaches the hook. retainInputs does not wake the watcher loop,
	// so only this test submits batches and the hook's handle is first.
	w.retainInputs(nil, true, nil)
	var first lane.Handle
	require.Eventually(t, func() bool {
		first = w.processOwnershipBatch()
		watcherJobTerminal(t, first)
		return inspected
	}, 30*time.Second, 25*time.Millisecond, "full discovery after resync")
	require.ErrorIs(t, first.Err(), interrupted)
	version, _, err := store.IndexerVersion(t.Context())
	require.NoError(t, err)
	require.Equal(t, "old-indexer", version, "an interrupted full discovery must not certify old rows")
	w.afterPreparedOwnershipCommit = nil
	rt.Cache().MarkDirty("src/value.go", cache.DirtyModified)
	second := w.processOwnershipBatch()
	watcherJobTerminal(t, second)
	require.NoError(t, second.Err())
	var rowVersion, comment string
	require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT f.indexer_version, a.doc_comment FROM files f JOIN intel_code_anchors a ON a.path=f.path WHERE f.path='src/value.go'`).Scan(&rowVersion, &comment))
	require.Equal(t, codeanchor.IndexerVersion, rowVersion)
	require.Equal(t, originalComment, comment)
	links, err := store.DocLinksForNote(t.Context(), "note.md", 10)
	require.NoError(t, err)
	require.Len(t, links, 1, "watcher upgrade must repair missing links before boot catch-up")
	version, _, err = store.IndexerVersion(t.Context())
	require.NoError(t, err)
	require.Equal(t, codeanchor.IndexerVersion, version)
	reverse, _, err := store.ReverseIndexVersion(t.Context())
	require.NoError(t, err)
	require.Equal(t, codeanchor.ReverseIndexVersion, reverse)
	batch()
	require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT doc_comment FROM intel_code_anchors WHERE path='src/value.go'`).Scan(&comment))
	require.Equal(t, originalComment, comment)
}
