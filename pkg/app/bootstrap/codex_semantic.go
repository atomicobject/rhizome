package bootstrap

import (
	"context"

	"errors"
	"fmt"
	"io"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func initSemanticTools(ctx context.Context, vaultPath string, cfg *agentapi.Config) (func(), []string) {
	warnings := []string{}
	closers := []func(){}
	if cfg == nil || vaultPath == "" {
		return nil, warnings
	}
	var resolved repoconfig.Resolved
	var err error
	if cfg.RepositoryConfig != nil {
		resolved = *cfg.RepositoryConfig
		err = resolved.ValidateFor(vaultPath)
	} else {
		resolved, err = repoconfig.Load(vaultPath)
	}
	embCfg := resolved.NoteEmbeddings
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("semantic: failed to load embeddings config: %v", err))
		return nil, warnings
	}
	embCfg.IndexPath = repoconfig.UnifiedIndexPath(vaultPath, embCfg.IndexPath)
	codeEmbCfg, _ := resolved.CodeEmbeddingsFor(embCfg)

	if embCfg.Enabled {
		provider, providerCfg, err := provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(ctx), embCfg, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic: %v", embeddings.ProviderUnavailableError(embCfg, err)))
		} else {
			if closer, ok := provider.(io.Closer); ok {
				closers = append(closers, func() { _ = closer.Close() })
			}
			noteMetadata := embeddings.MetadataForProvider(provider, providerCfg)
			store, err := embsqlite.OpenNotes(ctx, embsqlite.File(embCfg.IndexPath), embsqlite.Options{Access: embsqlite.ReadExisting, Dimensions: provider.Dimensions(), Metadata: &noteMetadata})
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					warnings = append(warnings, fmt.Sprintf("semantic: metadata validation failed: %v; run `rzm index` to refresh the index", metaErr))
				} else {
					warnings = append(warnings, fmt.Sprintf("semantic: index unavailable at %s: %v; run `rzm index` to create the index", embCfg.IndexPath, err))
				}
			} else {
				cfg.EmbeddingsOn = true
				cfg.Embeddings = store
				cfg.EmbedProvider = provider
				cfg.EmbeddingsPath = embCfg.IndexPath
				closers = append(closers, func() { _ = store.Close() })
			}
		}
	}

	if codeEmbCfg.Enabled {
		provider, providerCfg, err := provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(ctx), codeEmbCfg, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic: %v", embeddings.ProviderUnavailableError(codeEmbCfg, err)))
		} else {
			if closer, ok := provider.(io.Closer); ok {
				closers = append(closers, func() { _ = closer.Close() })
			}
			codeMetadata := embeddings.MetadataForProvider(provider, providerCfg)
			store, err := embsqlite.OpenCode(ctx, embsqlite.File(codeEmbCfg.IndexPath), embsqlite.Options{Access: embsqlite.ReadExisting, Dimensions: provider.Dimensions(), Metadata: &codeMetadata})
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					warnings = append(warnings, fmt.Sprintf("semantic: code metadata validation failed: %v; run `rzm index` to refresh the index", metaErr))
				} else {
					warnings = append(warnings, fmt.Sprintf("semantic: code index unavailable at %s: %v; run `rzm index` to create the index", codeEmbCfg.IndexPath, err))
				}
			} else {
				cfg.CodeEmbeddingsOn = true
				cfg.CodeEmbeddings = store
				cfg.CodeEmbedProvider = provider
				closers = append(closers, func() { _ = store.Close() })
			}
		}
	}

	if len(closers) == 0 {
		return nil, warnings
	}
	return func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}, warnings
}
