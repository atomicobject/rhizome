package indexing

import (
	"context"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"

	"io"
	"os"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/atomicobject/rhizome/pkg/embedindex"
)

type unifiedSemanticSetupOptions struct {
	VaultPath  string
	APIKey     string
	TxLockMode string
}

type unifiedSemanticResources struct {
	codeSyncer *embedindex.Syncer
	noteSyncer *embedindex.NoteSyncer
	codeStore  *codeemb.CodeStore
	noteStore  *codeemb.NoteStore
	codeProv   embeddings.Provider
	noteProv   embeddings.Provider
	codeNode   *embedindex.SharedEmbeddingNode
	noteNode   *embedindex.SharedEmbeddingNode
	runtime    *embedindex.Runtime
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

// prepareUnifiedSemanticResources opens stores and provider lanes before writer
// construction. Producers start only after bindWriter installs the writer.
func prepareUnifiedSemanticResources(
	ctx context.Context,
	opts unifiedSemanticSetupOptions,
	progress ProgressBar,
	sharedWriteMu *sync.Mutex,
	intelStore *semdb.Store,
	codeCfg codeanchor.Config,
	embCfg embeddings.Config,
	codeEmbCfg embeddings.Config,
) (*unifiedSemanticResources, error) {
	res := &unifiedSemanticResources{runtime: embedindex.NewRuntime()}
	ready := false
	defer func() {
		if !ready {
			res.close()
		}
	}()
	batchPolicy := embedindex.PolicyFor(embedindex.IndexRequest{Source: embedindex.IndexSourceFullScan, LatencyClass: embedindex.LatencyThroughput})

	if codeEmbCfg.Enabled {
		if err := embeddings.EnsureOllamaForConfig(ctx, codeEmbCfg, os.Stderr); err != nil {
			return nil, err
		}
		codeProvider, codeProviderCfg, err := provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(ctx), codeEmbCfg, opts.APIKey)
		if err != nil {
			return nil, err
		}
		res.codeProv = codeProvider
		embStore, err := codeemb.OpenCode(context.Background(), codeemb.File(codeEmbCfg.IndexPath), codeemb.Options{TxLockMode: opts.TxLockMode, Access: codeemb.WriteIndex, Dimensions: codeProvider.Dimensions()})
		if err != nil {
			return nil, err
		}
		res.codeStore = embStore
		embStore.SetWriteMu(sharedWriteMu)
		syncer := &embedindex.Syncer{
			Index:           embStore,
			Provider:        codeProvider,
			ProviderInfo:    codeProviderCfg,
			Intel:           intelStore,
			Calls:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			Root:            opts.VaultPath,
			Budget:          embedindex.DefaultChunkBudget(),
			Policy:          embedindex.DefaultSynthesisPolicy(embedindex.PolicyOptions{Budget: embedindex.DefaultChunkBudget()}),
			CodeEmbedPacker: batchPolicy.CodeEmbedPacker,
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
		noteProvider, noteProviderCfg, err := provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(ctx), embCfg, opts.APIKey)
		if err != nil {
			return nil, err
		}
		res.noteProv = noteProvider
		store, err := codeemb.OpenNotes(context.Background(), codeemb.File(embCfg.IndexPath), codeemb.Options{TxLockMode: opts.TxLockMode, Access: codeemb.WriteIndex, Dimensions: noteProvider.Dimensions()})
		if err != nil {
			return nil, err
		}
		res.noteStore = store
		store.SetWriteMu(sharedWriteMu)
		syncer := &embedindex.NoteSyncer{
			Index:           store,
			Provider:        noteProvider,
			ProviderInfo:    noteProviderCfg,
			Intel:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			NoteReader:      &obsidian.Note{},
			NoteEmbedPacker: batchPolicy.NoteEmbedPacker,
			RawEligibility:  embedindex.NewOntologyRawNoteEligibility(intelStore),
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
		opts := embedindex.EmbedPackerOptions{}
		if res.noteSyncer.NoteEmbedPacker != nil {
			opts = *res.noteSyncer.NoteEmbedPacker
		}
		lane, err := res.runtime.EnsureLane(ctx, embedindex.LaneRequest{
			Kind:          embedindex.LaneKindNote,
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
		opts := embedindex.EmbedPackerOptions{}
		if res.codeSyncer.CodeEmbedPacker != nil {
			opts = *res.codeSyncer.CodeEmbedPacker
		}
		lane, err := res.runtime.EnsureLane(ctx, embedindex.LaneRequest{
			Kind:          embedindex.LaneKindCode,
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
