package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// Phase 2: Semantic search capability (note embeddings)
func (rt *LiveRuntime) initPhase2Semantic() {
	defer rt.observeCapabilityPhase("semantic")()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("semantic init panic: %v", r)
			rt.semanticErr.Store(&err)
			close(rt.semanticReady)
			// Continue to next phases so WaitFor* methods don't hang forever
			rt.startWorker(rt.initPhase3Code)
		}
	}()
	if rt.isClosed() {
		err := context.Canceled
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	if !rt.requirements.Includes(RuntimeCapabilitySemantic) {
		err := capabilityNotRequested(RuntimeCapabilitySemantic)
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	// Validate embeddings provider is configured
	if err := rt.Configuration().ValidateEmbeddingsProvider(); err != nil {
		err = fmt.Errorf("semantic search unavailable: %w\n\nTo fix: run 'rzm init' in your terminal to configure an embeddings provider", err)
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	embCfg := rt.Configuration().NoteEmbeddings
	var err error
	if err != nil || !embCfg.Enabled {
		if err != nil {
			rt.semanticErr.Store(&err)
		} else {
			err = ErrEmbeddingsDisabled
			rt.semanticErr.Store(&err)
		}
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	provider, providerCfg, err := provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(rt.ctx), embCfg, "")
	if err != nil {
		err = embeddings.ProviderUnavailableError(embCfg, err)
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	if closer, ok := provider.(io.Closer); ok {
		rt.addCloser(func() { _ = closer.Close() })
	}
	if rt.queryProvidersOnly {
		meta := embeddings.MetadataForProvider(provider, providerCfg)
		rt.queryNoteProvider.Store(&provider)
		rt.queryNoteMeta.Store(&meta)
		rt.embCfg.Store(&embCfg)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	ctxReady, cancelReady := context.WithTimeout(rt.ctx, 5*time.Minute)
	defer cancelReady()

	storeCtx := indexingperf.WithPhase(ctxReady, indexingperf.AgentStartPhaseStoreWarmOpen)
	doneStore := indexingperf.StartSpan(storeCtx, indexingperf.AgentStartPhaseStoreWarmOpen)
	noteMetadata := embeddings.MetadataForProvider(provider, providerCfg)
	store, err := embsqlite.OpenNotes(storeCtx, embsqlite.File(embCfg.IndexPath), embsqlite.Options{Pool: liveSQLitePoolOptions(), Access: embsqlite.WriteIndex, Dimensions: provider.Dimensions(), Metadata: &noteMetadata})
	doneStore(err)
	if err != nil {
		var metaErr embeddings.MetadataError
		if errors.As(err, &metaErr) {
			err = fmt.Errorf("semantic index: %w", metaErr)
		} else {
			err = fmt.Errorf("semantic index unavailable at %s: %w", embCfg.IndexPath, err)
		}
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	if rt.isClosed() {
		_ = store.Close()
		err := context.Canceled
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	store.SetWriteMu(&rt.sharedWriteMu)
	rt.noteIndex.Store(store)
	rt.noteProvider.Store(&provider)
	rt.embCfg.Store(&embCfg)
	rt.addCloser(func() {
		if err := store.Close(); err != nil {
			rt.recordDiagnostic(slog.LevelWarn, "note_embeddings.close_failed", err)
		}
	})

	// Semantic capability is ready
	close(rt.semanticReady)

	// Continue to phase 3
	rt.startWorker(rt.initPhase3Code)
}
