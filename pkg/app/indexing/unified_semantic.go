package indexing

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/app/semanticruntime"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type unifiedSemanticResources struct {
	codeSyncer *semantic.Syncer
	noteSyncer *semantic.NoteSyncer
	codeStore  *codeemb.Store
	noteStore  *sqlite.Store
	codeProv   embeddings.Provider
	noteProv   embeddings.Provider
	codeNode   *semantic.SharedEmbeddingNode
	noteNode   *semantic.SharedEmbeddingNode
	runtime    *semanticruntime.Runtime
	intentWG   sync.WaitGroup
	intentMu   sync.Mutex
	intentErr  error
	closeOnce  sync.Once
}

func (r *unifiedSemanticResources) close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		if r.runtime != nil {
			r.runtime.Close()
		}
		if r.codeStore != nil {
			_ = r.codeStore.Close()
		}
		if r.noteStore != nil {
			_ = r.noteStore.Close()
		}
		if closer, ok := r.noteProv.(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := r.codeProv.(io.Closer); ok {
			_ = closer.Close()
		}
	})
}

func (r *unifiedSemanticResources) setIntentErr(err error) {
	if r == nil || err == nil {
		return
	}
	r.intentMu.Lock()
	defer r.intentMu.Unlock()
	if r.intentErr == nil {
		r.intentErr = err
	}
}

func (r *unifiedSemanticResources) waitIntent() error {
	if r == nil {
		return nil
	}
	r.intentWG.Wait()
	r.intentMu.Lock()
	defer r.intentMu.Unlock()
	return r.intentErr
}

func (r *unifiedSemanticResources) startIntentSync(ctx context.Context, intelStore intentstore.Store) {
	if r == nil || intelStore == nil {
		return
	}
	if r.runtime == nil {
		err := fmt.Errorf("semantic runtime unavailable")
		r.setIntentErr(err)
		fmt.Fprintf(os.Stderr, "[intent] embedding sync failed: %v\n", err)
		return
	}
	r.intentWG.Add(1)
	go func() {
		defer r.intentWG.Done()
		err := runPhase(ctx, "sync_intent_embeddings", func(phaseCtx context.Context) error {
			return r.runtime.SyncIntentExemplars(phaseCtx, intelStore)
		})
		if err != nil {
			r.setIntentErr(err)
			fmt.Fprintf(os.Stderr, "[intent] embedding sync failed: %v\n", err)
		}
	}()
}

// prepareUnifiedSemanticResources opens the note/code embedding stores, wires
// their write functions into the shared queue, and asks semanticruntime to group
// compatible provider work onto shared lanes.
func prepareUnifiedSemanticResources(
	ctx context.Context,
	opts unifiedPostIndexOptions,
	progress ProgressBar,
	sharedWriteMu *sync.Mutex,
	intelStore *semdb.Store,
	writeQueue *indexwriter.Writer,
	codeItemEmbBatch *func(context.Context, []codeindex.ItemEmbeddingUpsert) error,
	codeChunkBatch *func(context.Context, []codeindex.ItemChunksUpsert) error,
	codeChunkWriter *func(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error,
	noteMetaBatch *func(context.Context, []embeddings.NoteFileInfo) error,
	noteChunkSyncBatch *func(context.Context, []embeddings.NoteChunkSync) error,
	noteChunkSyncWriter *func(context.Context, embeddings.NoteChunkSync) error,
	codeCfg codeanchor.Config,
	embCfg embeddings.Config,
	codeEmbCfg embeddings.Config,
) (*unifiedSemanticResources, error) {
	res := &unifiedSemanticResources{runtime: semanticruntime.New()}
	ready := false
	defer func() {
		if !ready {
			res.close()
		}
	}()
	filterProgress := func(prefix string) func(format string, args ...any) {
		return func(format string, args ...any) {
			if strings.HasPrefix(format, "Embedding ") ||
				format == "Sync complete" ||
				format == "Building chunks" ||
				strings.HasPrefix(format, "Loading intel ") ||
				strings.HasPrefix(format, "Loaded note embedding states") ||
				format == "Preparing index schema" ||
				format == "No embeddings to update (index is up to date)" ||
				format == "No changed notes to embed" ||
				format == "No changed code to embed" {
				return
			}
			progress.Println(fmt.Sprintf(prefix+format, args...))
		}
	}
	batchPolicy := semanticruntime.PolicyFor(semanticruntime.IndexRequest{Source: semanticruntime.IndexSourceFullScan, LatencyClass: semanticruntime.LatencyThroughput})

	if codeEmbCfg.Enabled {
		if err := embeddings.EnsureOllamaForConfig(ctx, codeEmbCfg, os.Stderr); err != nil {
			return nil, err
		}
		codeProvider, codeProviderCfg, err := prepareProvider(codeEmbCfg, opts.APIKey)
		if err != nil {
			return nil, err
		}
		res.codeProv = codeProvider
		embStore, err := codeemb.OpenWithOptions(codeEmbCfg.IndexPath, codeProvider.Dimensions(), codeemb.OpenOptions{
			TxLockMode: opts.TxLockMode,
		})
		if err != nil {
			return nil, err
		}
		res.codeStore = embStore
		embStore.SetWriteMu(sharedWriteMu)
		if codeItemEmbBatch != nil {
			*codeItemEmbBatch = embStore.UpsertItemEmbeddingBatch
		}
		if codeChunkBatch != nil {
			*codeChunkBatch = embStore.UpsertItemChunksBatch
		}
		if codeChunkWriter != nil {
			*codeChunkWriter = embStore.UpsertItemChunks
		}
		syncer := &semantic.Syncer{
			Index:           embStore,
			Provider:        codeProvider,
			ProviderInfo:    codeProviderCfg,
			Intel:           intelStore,
			Calls:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			Root:            opts.VaultPath,
			Budget:          semantic.DefaultChunkBudget(),
			Policy:          semantic.DefaultSynthesisPolicy(semantic.PolicyOptions{Budget: semantic.DefaultChunkBudget()}),
			CodeEmbedPacker: batchPolicy.CodeEmbedPacker,
			WriteQueue:      writeQueue,
			OnProgress:      filterProgress("[code] "),
		}
		if codeEmbCfg.BatchSize > 0 {
			syncer.BatchSize = codeEmbCfg.BatchSize
		}
		if codeEmbCfg.MaxConcurrency > 0 {
			syncer.MaxConcurrent = codeEmbCfg.MaxConcurrency
		}
		res.codeSyncer = syncer
	}

	if embCfg.Enabled {
		if err := embeddings.EnsureOllamaForConfig(ctx, embCfg, os.Stderr); err != nil {
			return nil, err
		}
		noteProvider, noteProviderCfg, err := prepareProvider(embCfg, opts.APIKey)
		if err != nil {
			return nil, err
		}
		res.noteProv = noteProvider
		store, err := sqlite.OpenWithOptions(embCfg.IndexPath, noteProvider.Dimensions(), sqlite.OpenOptions{
			TxLockMode: opts.TxLockMode,
		})
		if err != nil {
			return nil, err
		}
		res.noteStore = store
		store.SetWriteMu(sharedWriteMu)
		if noteMetaBatch != nil {
			*noteMetaBatch = store.UpsertNoteMetaBatch
		}
		if noteChunkSyncBatch != nil {
			*noteChunkSyncBatch = store.SyncNoteChunksBatch
		}
		if noteChunkSyncWriter != nil {
			*noteChunkSyncWriter = store.SyncNoteChunks
		}
		syncer := &semantic.NoteSyncer{
			Index:           store,
			Provider:        noteProvider,
			ProviderInfo:    noteProviderCfg,
			Intel:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			NoteReader:      &obsidian.Note{},
			NoteEmbedPacker: batchPolicy.NoteEmbedPacker,
			WriteQueue:      writeQueue,
			OnProgress:      filterProgress("[notes] "),
			RawEligibility:  semantic.NewOntologyRawNoteEligibility(intelStore),
		}
		if embCfg.MaxSectionBytes > 0 {
			syncer.MaxSectionBytes = embCfg.MaxSectionBytes
		}
		if embCfg.BatchSize > 0 {
			syncer.BatchSize = embCfg.BatchSize
		}
		if embCfg.MaxConcurrency > 0 {
			syncer.MaxConcurrent = embCfg.MaxConcurrency
		}
		res.noteSyncer = syncer
	}

	if res.codeSyncer != nil && res.noteSyncer != nil {
		sharedMax := max(
			effectiveMaxConcurrent(codeEmbCfg.MaxConcurrency, res.codeProv),
			effectiveMaxConcurrent(embCfg.MaxConcurrency, res.noteProv),
		)
		if sharedMax < 1 {
			sharedMax = 1
		}
		// NOTE: this gate spans the code and note providers when both are active.
		// Compatible lanes may share a node, but even incompatible providers
		// should not silently multiply full-scan concurrency beyond configured
		// capacity.
		sharedGate := make(chan struct{}, sharedMax)
		res.codeSyncer.EmbedGate = sharedGate
		res.noteSyncer.EmbedGate = sharedGate
	}
	if res.noteSyncer != nil && res.noteProv != nil {
		opts := semantic.EmbedPackerOptions{}
		if res.noteSyncer.NoteEmbedPacker != nil {
			opts = *res.noteSyncer.NoteEmbedPacker
		}
		lane, err := res.runtime.EnsureLane(ctx, semanticruntime.LaneRequest{
			Kind:          semanticruntime.LaneKindNote,
			Provider:      res.noteProv,
			ProviderInfo:  res.noteSyncer.ProviderInfo,
			MaxConcurrent: res.noteSyncer.MaxConcurrent,
			BatchSize:     res.noteSyncer.BatchSize,
			EmbedGate:     res.noteSyncer.EmbedGate,
			Packer:        opts,
		})
		if err != nil {
			return nil, err
		}
		res.noteNode = lane.Node
		res.noteSyncer.NoteEmbedPacker = &lane.Packer
		res.noteSyncer.EmbeddingNode = lane.Node
	}
	if res.codeSyncer != nil && res.codeProv != nil {
		opts := semantic.EmbedPackerOptions{}
		if res.codeSyncer.CodeEmbedPacker != nil {
			opts = *res.codeSyncer.CodeEmbedPacker
		}
		lane, err := res.runtime.EnsureLane(ctx, semanticruntime.LaneRequest{
			Kind:          semanticruntime.LaneKindCode,
			Provider:      res.codeProv,
			ProviderInfo:  res.codeSyncer.ProviderInfo,
			MaxConcurrent: res.codeSyncer.MaxConcurrent,
			BatchSize:     res.codeSyncer.BatchSize,
			EmbedGate:     res.codeSyncer.EmbedGate,
			Packer:        opts,
		})
		if err != nil {
			return nil, err
		}
		res.codeNode = lane.Node
		res.codeSyncer.CodeEmbedPacker = &lane.Packer
		res.codeSyncer.EmbeddingNode = lane.Node
	}

	ready = true
	return res, nil
}
