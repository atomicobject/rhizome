package unifiedsearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type searchResources struct {
	noteProvider, codeProvider embeddings.Provider
	codeStore                  *embsqlite.CodeStore
	intelStore                 *semdb.Store
	config                     repoconfig.Resolved
	cleanups                   *cleanupStack
}

// openSearchResources owns all request-lifetime handles and validates query-only
// metadata before returning any usable providers or indexes.
func openSearchResources(ctx context.Context, opts Options) (searchResources, error) {
	// Root search owns direct provider/store construction, not an agent JSON
	// runtime, so flags cannot become a second, implicit runtime-policy switch.
	// These are the capabilities oneshotruntime.UnifiedSearchPlan declares; they
	// stay inline because oneshotruntime and bootstrap import the MCP adapter,
	// which now calls Execute from this package.
	usesVectorProvider := opts.UseVector
	usesIntelStore := opts.UseIntel || opts.UseRefs || opts.UseVector

	cleanups := &cleanupStack{}
	fail := func(err error) (searchResources, error) {
		cleanups.Close()
		return searchResources{}, err
	}

	var (
		codeStore    *embsqlite.CodeStore
		noteProvider = opts.NoteProvider
		noteCfg      = opts.NoteProviderConfig
		codeProvider = opts.CodeProvider
		intelStore   = opts.IntelStore
	)

	if usesVectorProvider && opts.EmbCfg.Enabled {
		provider, providerCfg, err := provideraccess.Default().NewEmbeddings(vaultconfig.FromContext(ctx), opts.EmbCfg, opts.ProviderAPIKey)
		if err != nil {
			return fail(err)
		}
		noteProvider = provider
		noteCfg = providerCfg
		if closer, ok := provider.(io.Closer); ok {
			cleanups.Add(func() { _ = closer.Close() })
		}
		if err := embsqlite.ValidateExisting(ctx, embsqlite.File(opts.EmbCfg.IndexPath), embsqlite.Notes, embeddings.MetadataForProvider(provider, providerCfg)); err != nil {
			return fail(fmt.Errorf("semantic index metadata mismatch at %s: %w", opts.EmbCfg.IndexPath, err))
		}
	}
	var err error
	var resolved repoconfig.Resolved
	if opts.Config != nil {
		resolved = *opts.Config
		if err := resolved.ValidateFor(opts.VaultPath); err != nil {
			return fail(err)
		}
	} else {
		var err error
		resolved, err = repoconfig.Load(opts.VaultPath)
		if err != nil {
			return fail(err)
		}
	}
	codeEmbCfg, _ := resolved.CodeEmbeddingsFor(opts.EmbCfg)

	if codeProvider == nil && usesVectorProvider && codeEmbCfg.Enabled {
		if _, statErr := os.Stat(codeEmbCfg.IndexPath); statErr == nil {
			var providerCfg embeddings.ProviderConfig
			providerCfg, err = provideraccess.Default().EmbeddingConfig(vaultconfig.FromContext(ctx), codeEmbCfg, opts.ProviderAPIKey)
			if err != nil {
				return fail(err)
			}
			var provider embeddings.Provider
			if noteProvider != nil && providerConfigsMatch(providerCfg, noteCfg) {
				// Reuse the note provider when configs match so one search request
				// does not create duplicate provider clients/rate-limit buckets for
				// code and note embeddings.
				provider = noteProvider
				providerCfg = noteCfg
			} else {
				provider, err = embeddings.NewConfiguredProvider(vaultconfig.FromContext(ctx), providerCfg)
				if err != nil {
					return fail(err)
				}
				if closer, ok := provider.(io.Closer); ok {
					cleanups.Add(func() { _ = closer.Close() })
				}
			}
			codeProvider = provider
			codeMetadata := embeddings.MetadataForProvider(codeProvider, providerCfg)
			store, err := embsqlite.OpenCode(ctx, embsqlite.File(codeEmbCfg.IndexPath), embsqlite.Options{Access: embsqlite.ReadExisting, Dimensions: codeMetadata.Dimensions, Metadata: &codeMetadata})
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					return fail(fmt.Errorf("code embeddings metadata mismatch at %s: %w", codeEmbCfg.IndexPath, metaErr))
				}
				return fail(fmt.Errorf("open code embeddings index: %w", err))
			}
			codeStore = store
			cleanups.Add(func() { _ = codeStore.Close() })
		}
	}
	codeCfg := resolved.Code
	if intelStore == nil && codeCfg.IndexPath != "" && usesIntelStore {
		requireEnabled := codeCfg.Enabled
		if opts.EmbCfg.Enabled || codeEmbCfg.Enabled {
			requireEnabled = false
		}
		if !requireEnabled || codeCfg.Enabled {
			indexPath := repoconfig.UnifiedIndexPath(opts.VaultPath, codeCfg.IndexPath)
			if _, statErr := os.Stat(indexPath); statErr == nil {
				store, err := semdb.OpenReadOnlyExisting(indexPath, ctx, sqliteutil.Options{})
				if err != nil {
					return fail(fmt.Errorf("open search index (run `rzm index` to update it): %w", err))
				}
				intelStore = store
				cleanups.Add(func() { _ = store.Close() })
			} else if !os.IsNotExist(statErr) {
				return fail(statErr)
			}
		}
	}

	return searchResources{noteProvider: noteProvider, codeProvider: codeProvider, codeStore: codeStore, intelStore: intelStore, config: resolved, cleanups: cleanups}, nil
}
