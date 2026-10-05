// Package mcp provides the shared JSON tool handlers used by agent-facing APIs.
//
// Docs: [CONTEXT.md](pkg/app/mcp/CONTEXT.md)
//
// These handlers wrap pkg/app/cli actions with structured request/response
// payloads and manage runtime-initialized dependencies (embeddings, code
// anchors) through a per-call LiveRuntime capability view.
package mcp

import (
	"context"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Compressor is the interface for compressing context when over budget.
// Implementations live in pkg/app/contextpack/compress.
type Compressor = contextpack.Compressor

// IntelStorePolicy controls whether a handler may reopen an indexed store
// when the caller-managed runtime did not publish one.
type IntelStorePolicy string

const (
	// IntelStoreFallbackAllowed preserves long-lived MCP behavior.
	IntelStoreFallbackAllowed IntelStorePolicy = ""
	// IntelStoreManagedReadOnly requires the single caller-owned existing index.
	IntelStoreManagedReadOnly IntelStorePolicy = "managed_read_only"
	// IntelStoreNoFallback keeps live-note operations from silently opening a
	// second index only for optional enrichment.
	IntelStoreNoFallback IntelStorePolicy = "no_fallback"
)

// Config holds configuration for shared tool handlers.
//
// Docs: [CONTEXT.md](pkg/app/mcp/CONTEXT.md)
//
// Config combines static configuration (vault definition, cache, debug flags)
// with optional runtime-initialized dependencies (embeddings, code anchors).
// When Runtime is set, accessors take a fresh non-blocking snapshot from the
// LiveRuntime-backed view so callers observe independent capabilities as they
// arrive without copied mutable state.
type Config struct {
	Vault     *obsidian.Vault
	VaultPath string
	VaultDef  obsidian.VaultDefinition // vault definition for note operations
	// NoteMetadata is the command-scoped provider-aware metadata indexer. It
	// must be composed by the application before a handler performs a durable
	// metadata or ontology refresh.
	NoteMetadata        notemeta.Indexer
	BudgetCharsOverride int
	Debug               bool
	SuppressedTags      []string
	ReadWrite           bool
	// ApplyLinkTargets is required for embedded apply in writable embeddings.
	// Its owner must update the same canonical live vault/store as file_context.
	ApplyLinkTargets          func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error)
	ToolInventory             ToolInventory
	Cache                     *cache.Service
	NoteReader                obsidian.NoteReader // Immutable per-command cache snapshot when available.
	OntologyRuntimeProvider   actions.OntologyRuntimeProvider
	IndexedContextUnavailable *actions.IndexedContextFreshness
	// IndexedReadOnlyFileContext selects bounded persisted-index reads for the
	// one-shot agent file_context command. Long-lived and read-write MCP callers
	// retain the live-runtime path.
	IndexedReadOnlyFileContext bool
	// IndexedReadOnlySemanticQuery prevents the one-shot agent command from
	// silently reopening or rebuilding indexed state when the managed reader is
	// unavailable. Long-lived MCP callers retain their existing fallback policy.
	IndexedReadOnlySemanticQuery bool
	// IntelStorePolicy is explicit composition policy. One-shot exact-index
	// tools use ManagedReadOnly and must never open a handler fallback store.
	IntelStorePolicy       IntelStorePolicy
	Embeddings             embeddings.Index
	EmbedProvider          embeddings.Provider
	EmbeddingsPath         string
	EmbeddingsOn           bool
	CodeEmbeddings         codeindex.Index
	CodeEmbedProvider      embeddings.Provider
	CodeEmbeddingsOn       bool
	CodeAnchor             *codeanchor.Service
	CodeAnchorOn           bool
	IncludeCodeRefSnippets bool
	IntelStore             *semdb.Store
	SessionStore           semdb.SessionDedupeStore
	Runtime                runtimeview.View
	Compressor             Compressor // Optional compressor for intent-driven compression
}

// ToolInventory supplies the authoritative advertised tool sets without
// coupling the handler package back to its agentapi catalog and factories.
type ToolInventory interface {
	AvailableToolNames(readWrite bool) []string
	MutatingToolNames(readWrite bool) []string
}

func (c Config) NoteEmbeddings() (on bool, idx embeddings.Index, provider embeddings.Provider, path string) {
	if c.Runtime != nil {
		snapshot := c.Runtime.Snapshot()
		on := snapshot.NoteIndex != nil && snapshot.NoteProvider != nil
		if c.IndexedReadOnlySemanticQuery {
			on = snapshot.NoteProvider != nil
		}
		return on, snapshot.NoteIndex, snapshot.NoteProvider, snapshot.NoteIndexPath
	}
	return c.EmbeddingsOn, c.Embeddings, c.EmbedProvider, c.EmbeddingsPath
}

func (c Config) CodeEmbeddingsState() (on bool, idx codeindex.Index, provider embeddings.Provider) {
	if c.Runtime != nil {
		snapshot := c.Runtime.Snapshot()
		on := snapshot.CodeIndex != nil && snapshot.CodeProvider != nil
		if c.IndexedReadOnlySemanticQuery {
			on = snapshot.CodeProvider != nil
		}
		return on, snapshot.CodeIndex, snapshot.CodeProvider
	}
	return c.CodeEmbeddingsOn, c.CodeEmbeddings, c.CodeEmbedProvider
}

func (c Config) CodeAnchorState() (on bool, svc *codeanchor.Service) {
	if c.Runtime != nil {
		svc := c.Runtime.Snapshot().CodeAnchorService
		return svc != nil, svc
	}
	return c.CodeAnchorOn, c.CodeAnchor
}

// GetIntelStore returns the indexed-data reader from a fresh runtime snapshot
// when available. Read-only runtimes keep this separate from session writes.
func (c Config) GetIntelStore() *semdb.Store {
	if (c.IndexedReadOnlySemanticQuery || c.IntelStorePolicy == IntelStoreManagedReadOnly) && c.IndexedContextUnavailable != nil && c.IndexedContextUnavailable.State != actions.IndexedContextAvailable {
		return nil
	}
	if c.Runtime != nil {
		snapshot := c.Runtime.Snapshot()
		return snapshot.IntelStore
	}
	if c.IntelStore != nil {
		return c.IntelStore
	}
	return nil
}

// BudgetChars returns the effective budget for LLM context packing.
// Uses the config from VaultPath if available, otherwise returns the default.
func (c Config) BudgetChars() int {
	if c.BudgetCharsOverride > 0 {
		return c.BudgetCharsOverride
	}
	return obsidian.GetBudgetChars(c.VaultPath)
}

// DefaultBudgetChars returns the global default budget constant.
func DefaultBudgetChars() int {
	return contextpack.DefaultBudgetChars
}
