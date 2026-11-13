package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// initCodeEmbeddings initializes code embeddings store (optional, failures don't block).
func (rt *LiveRuntime) initCodeEmbeddings(intelStore *semdb.Store) {
	resolved := rt.Configuration()
	codeEmbCfg, exists := resolved.CodeEmbeddings, resolved.CodeEmbeddingsExplicit
	var err error
	if err != nil || !exists || !codeEmbCfg.Enabled {
		return
	}

	rt.codeEmbCfg.Store(&codeEmbCfg)

	var codeProvider embeddings.Provider
	codeProviderCfg := codeEmbCfg.ProviderCfg("")
	if rt.queryProvidersOnly {
		if noteCfg := rt.embCfg.Load(); noteCfg != nil && queryEmbeddingConfigsCompatible(*noteCfg, codeEmbCfg) {
			if noteProvider := rt.queryNoteProvider.Load(); noteProvider != nil {
				codeProvider = *noteProvider
			}
		}
	}
	if codeProvider == nil {
		codeProvider, codeProviderCfg, err = provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(rt.ctx), codeEmbCfg, "")
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "code_provider.unavailable", err)
			}
			return
		}
		if closer, ok := codeProvider.(io.Closer); ok {
			rt.addCloser(func() { _ = closer.Close() })
		}
	}

	if _, err := os.Stat(codeEmbCfg.IndexPath); err != nil {
		if diagnostics.FromContext(rt.ctx) != nil {
			rt.recordDiagnostic(slog.LevelInfo, "code_embeddings.index_unavailable", err)
		}
		return
	}

	ctxReady, cancelReady := context.WithTimeout(rt.ctx, 5*time.Minute)
	defer cancelReady()

	meta := embeddings.MetadataForProvider(codeProvider, codeProviderCfg)
	meta.FingerprintVersion = 2
	if rt.queryProvidersOnly {
		if err := embsqlite.ValidateExisting(rt.ctx, embsqlite.Borrowed(intelStore.DB()), embsqlite.Code, meta); err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "code_embeddings.metadata_unavailable", err)
			}
			return
		}
		rt.codeEmbProvider.Store(&codeProvider)
		return
	}
	codeEmbStore, err := rt.openCodeEmbeddingsStoreForServe(ctxReady, codeEmbCfg.IndexPath, codeProvider, meta, intelStore)
	if err != nil {
		var metaErr embeddings.MetadataError
		if errors.As(err, &metaErr) {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_embeddings.metadata_init_failed", metaErr)
			}
		} else {
			rt.recordDiagnostic(slog.LevelWarn, "code_embeddings.open_failed", err)
		}
		return
	}

	codeEmbStore.SetWriteMu(&rt.sharedWriteMu)
	rt.codeEmbStore.Store(codeEmbStore)
	rt.codeEmbProvider.Store(&codeProvider)
	rt.addCloser(func() {
		if err := codeEmbStore.Close(); err != nil {
			rt.recordDiagnostic(slog.LevelWarn, "code_embeddings.close_failed", err)
		}
	})
}

func queryEmbeddingConfigsCompatible(note, code embeddings.Config) bool {
	return strings.EqualFold(strings.TrimSpace(note.Provider), strings.TrimSpace(code.Provider)) &&
		strings.TrimSpace(note.Model) == strings.TrimSpace(code.Model) &&
		strings.TrimRight(strings.TrimSpace(note.Endpoint), "/") == strings.TrimRight(strings.TrimSpace(code.Endpoint), "/") &&
		note.Dimensions == code.Dimensions
}

func (rt *LiveRuntime) openCodeEmbeddingsStoreForServe(ctx context.Context, indexPath string, provider embeddings.Provider, meta embeddings.IndexMetadata, intelStore *semdb.Store) (*embsqlite.CodeStore, error) {
	if codeCfg := rt.codeCfg.Load(); codeCfg != nil && sameSQLitePath(indexPath, codeCfg.IndexPath) && intelStore != nil && intelStore.DB() != nil {
		return embsqlite.OpenCode(ctx, embsqlite.Borrowed(intelStore.DB()), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: provider.Dimensions(), Metadata: &meta})
	}
	return embsqlite.OpenCode(ctx, embsqlite.File(indexPath), embsqlite.Options{Pool: liveSQLitePoolOptions(), Access: embsqlite.WriteIndex, Dimensions: provider.Dimensions(), Metadata: &meta})
}

func sameSQLitePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// createSyncers creates note and code syncers for leader work.
func (rt *LiveRuntime) createSyncers(intelStore *semdb.Store) {
	policy := embedindex.PolicyFor(embedindex.IndexRequest{
		Source:       embedindex.IndexSourceWatcher,
		LatencyClass: embedindex.LatencyThroughput,
	})
	runtime := embedindex.NewRuntime()
	// Create code syncer if code embeddings are available
	codeEmbStore := rt.codeEmbStore.Load()
	codeProvider := rt.codeEmbProvider.Load()
	codeEmbCfg := rt.codeEmbCfg.Load()
	var codeSyncer *embedindex.Syncer
	if codeEmbStore != nil && codeProvider != nil && codeEmbCfg != nil && intelStore != nil {
		codeSyncer = &embedindex.Syncer{
			Index:           codeEmbStore,
			Provider:        *codeProvider,
			ProviderInfo:    codeEmbCfg.ProviderCfg(""),
			Intel:           intelStore,
			Calls:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			Root:            rt.VaultPath,
			Budget:          embedindex.DefaultChunkBudget(),
			Policy:          embedindex.DefaultSynthesisPolicy(embedindex.PolicyOptions{Budget: embedindex.DefaultChunkBudget()}),
			CodeEmbedPacker: policy.CodeEmbedPacker,
		}
		if codeEmbCfg.BatchSize > 0 {
			codeSyncer.BatchSize = codeEmbCfg.BatchSize
		}
		if codeEmbCfg.MaxConcurrency > 0 {
			codeSyncer.MaxConcurrent = codeEmbCfg.MaxConcurrency
		}
	}

	// Create note syncer if note embeddings are available
	noteIndex := rt.noteIndex.Load()
	noteProvider := rt.noteProvider.Load()
	embCfg := rt.embCfg.Load()
	noteSvc := rt.codeAnchorSvc.Load()
	var noteSyncer *embedindex.NoteSyncer
	if noteIndex != nil && noteProvider != nil && embCfg != nil && noteSvc != nil && intelStore != nil {
		noteSyncer = &embedindex.NoteSyncer{
			Index:           noteIndex,
			Provider:        *noteProvider,
			ProviderInfo:    embCfg.ProviderCfg(""),
			Intel:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			EmbeddingCache:  intelStore,
			NoteReader:      &obsidian.Note{},
			BatchSize:       embCfg.BatchSize,
			MaxConcurrent:   embCfg.MaxConcurrency,
			MaxSectionBytes: embCfg.MaxSectionBytes,
			NoteEmbedPacker: policy.NoteEmbedPacker,
			RawEligibility:  embedindex.NewOntologyRawNoteEligibility(intelStore),
		}
	}
	if codeSyncer != nil && noteSyncer != nil {
		sharedMax := max(
			embedindex.EffectiveMaxConcurrent(codeSyncer.Provider, codeSyncer.MaxConcurrent),
			embedindex.EffectiveMaxConcurrent(noteSyncer.Provider, noteSyncer.MaxConcurrent),
		)
		sharedGate := make(chan struct{}, sharedMax)
		codeSyncer.EmbedGate = sharedGate
		noteSyncer.EmbedGate = sharedGate
	}
	physicalCtx, finishPhysical := rt.ctx, func() {}
	if noteSyncer != nil || codeSyncer != nil {
		physicalCtx, finishPhysical = observePhysicalEmbeddings(rt.ctx)
	}
	if noteSyncer != nil && noteSyncer.Provider != nil {
		opts := embedindex.EmbedPackerOptions{}
		if noteSyncer.NoteEmbedPacker != nil {
			opts = *noteSyncer.NoteEmbedPacker
		}
		lane, err := runtime.EnsureLane(physicalCtx, embedindex.LaneRequest{
			Kind:          embedindex.LaneKindNote,
			Provider:      noteSyncer.Provider,
			ProviderInfo:  noteSyncer.ProviderInfo,
			MaxConcurrent: noteSyncer.MaxConcurrent,
			BatchSize:     noteSyncer.BatchSize,
			EmbedGate:     noteSyncer.EmbedGate,
			Packer:        opts,
		})
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "note_semantic.lane_unavailable", err)
			}
		} else {
			noteSyncer.NoteEmbedPacker = &lane.Packer
			noteSyncer.EmbeddingNode = lane.Node
		}
	}
	if codeSyncer != nil && codeSyncer.Provider != nil {
		opts := embedindex.EmbedPackerOptions{}
		if codeSyncer.CodeEmbedPacker != nil {
			opts = *codeSyncer.CodeEmbedPacker
		}
		lane, err := runtime.EnsureLane(physicalCtx, embedindex.LaneRequest{
			Kind:          embedindex.LaneKindCode,
			Provider:      codeSyncer.Provider,
			ProviderInfo:  codeSyncer.ProviderInfo,
			MaxConcurrent: codeSyncer.MaxConcurrent,
			BatchSize:     codeSyncer.BatchSize,
			EmbedGate:     codeSyncer.EmbedGate,
			Packer:        opts,
		})
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_semantic.lane_unavailable", err)
			}
		} else {
			codeSyncer.CodeEmbedPacker = &lane.Packer
			codeSyncer.EmbeddingNode = lane.Node
		}
	}
	if codeSyncer != nil && codeSyncer.EmbeddingNode != nil {
		rt.codeSyncer.Store(codeSyncer)
	}
	if noteSyncer != nil && noteSyncer.EmbeddingNode != nil {
		rt.noteSyncer.Store(noteSyncer)
	}
	if (codeSyncer != nil && codeSyncer.EmbeddingNode != nil) || (noteSyncer != nil && noteSyncer.EmbeddingNode != nil) {
		rt.addCloser(func() {
			runtime.Close()
			finishPhysical()
		})
	} else {
		finishPhysical()
	}
}
