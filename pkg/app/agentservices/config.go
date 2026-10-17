// Package agentservices composes shared tool services from explicit application
// dependencies. It never initializes a runtime, provider, cache, or store.
package agentservices

import (
	"slices"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/mcp"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// Options contains caller-owned request policy and collaborators. Vault and
// repository configuration come from the same captured snapshot.
type Options struct {
	Vault                        *repoconfig.Vault
	NoteMetadata                 notemeta.Indexer
	Runtime                      runtimeview.View
	Cache                        *cache.Service
	BudgetChars                  int
	Debug                        bool
	SuppressedTags               []string
	ReadWrite                    bool
	IndexedContextUnavailable    *actions.IndexedContextFreshness
	IndexedReadOnlyFileContext   bool
	IndexedReadOnlySemanticQuery bool
	IntelStorePolicy             mcp.IntelStorePolicy
}

// Compose uses the caller's single capability/configuration capture. A live
// view remains available for phase-specific readiness and capability access.
func Compose(snapshot runtimeview.Snapshot, options Options) agentapi.Config {
	return agentapi.Config{
		RepositoryConfig:             snapshot.Configuration,
		Vault:                        options.Vault,
		VaultPath:                    snapshot.VaultDefinition.BasePath(),
		VaultDef:                     snapshot.VaultDefinition,
		NoteMetadata:                 options.NoteMetadata,
		BudgetCharsOverride:          options.BudgetChars,
		Debug:                        options.Debug,
		SuppressedTags:               slices.Clone(options.SuppressedTags),
		ReadWrite:                    options.ReadWrite,
		Cache:                        options.Cache,
		Embeddings:                   snapshot.NoteIndex,
		EmbedProvider:                snapshot.NoteProvider,
		EmbeddingsPath:               snapshot.NoteIndexPath,
		EmbeddingsOn:                 snapshot.NoteIndex != nil && snapshot.NoteProvider != nil,
		CodeEmbeddings:               snapshot.CodeIndex,
		CodeEmbedProvider:            snapshot.CodeProvider,
		CodeEmbeddingsOn:             snapshot.CodeIndex != nil && snapshot.CodeProvider != nil,
		CodeAnchor:                   snapshot.CodeAnchorService,
		CodeAnchorOn:                 snapshot.CodeAnchorService != nil,
		IntelStore:                   snapshot.IntelStore,
		SessionStore:                 snapshot.SessionStore,
		Runtime:                      options.Runtime,
		IndexedContextUnavailable:    options.IndexedContextUnavailable,
		IndexedReadOnlyFileContext:   options.IndexedReadOnlyFileContext,
		IndexedReadOnlySemanticQuery: options.IndexedReadOnlySemanticQuery,
		IntelStorePolicy:             options.IntelStorePolicy,
	}
}
