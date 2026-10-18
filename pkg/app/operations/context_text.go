package operations

import (
	"context"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
)

type ContextProfile string

const (
	ContextProfileAuto  ContextProfile = "auto"
	ContextProfileVault ContextProfile = "vault"
	ContextProfileCode  ContextProfile = "code"
)

type ContextFormat string

const (
	ContextFormatText ContextFormat = "text"
	ContextFormatJSON ContextFormat = "json"
)

const thinContextWarningCode = "thin_context"

type VaultContextRequestScope string

const (
	// VaultContextRequestScopeRich preserves the standalone vault-context contract.
	VaultContextRequestScopeRich VaultContextRequestScope = "rich"
	// VaultContextRequestScopeMinimalBootstrap identifies start requests that need
	// only bounded repository guidance. Phase 7 narrows the emitted packet.
	VaultContextRequestScopeMinimalBootstrap VaultContextRequestScope = "minimal_bootstrap"
	// VaultContextRequestScopeIndexedBootstrap is the explicit-rich agent-start
	// path. It appends bounded durable-index evidence to minimal guidance without
	// initializing the live discovery/runtime pipeline.
	VaultContextRequestScopeIndexedBootstrap VaultContextRequestScope = "indexed_bootstrap"
)

type VaultContextTextParams struct {
	Context      context.Context
	BudgetChars  int
	Profile      ContextProfile
	RequestScope VaultContextRequestScope
	// RequireOntology asks indexed bootstrap to require and render the persisted
	// ontology summary. It never authorizes live projection or repair.
	RequireOntology bool
	// IndexedUnavailable carries store-open authority into fail-soft rendering.
	// It is set only by one-shot startup orchestration; the renderer does not
	// duplicate migration/schema probing.
	IndexedUnavailable *IndexedContextFreshness

	SkipAnchors bool
	SkipEmbeds  bool
	IncludeTags bool
	// IncludeTagsSet indicates IncludeTags was explicitly set by the caller.
	IncludeTagsSet bool
	RecencyCascade bool
	// RecencyCascadeSet indicates RecencyCascade was explicitly set by the caller.
	RecencyCascadeSet bool

	ContextFiles   []string
	Files          []string
	KeyPatterns    []string
	SubmoduleDepth int

	IncludeComponents bool
	GraphSummary      bool

	// Optional: if provided, boosts authority for notes referenced from code.
	CodeRefsByNote map[string][]coderefs.CodeRef

	// Optional: if provided, preserves file-level coderef note linking for bundled file context.
	CodeRefsByFile map[string][]coderefs.CodeRef

	// Optional: when set, reuses an existing code anchor service for bundled file context.
	CodeAnchor *codeanchor.Service

	// Optional: when set, dedupes pieces already returned to a session.
	Dedupe DedupeTracker

	// Optional: when set, reuses an existing metadata/session store.
	SessionStore *semdb.Store
	NoteMetadata notemeta.Indexer
	// IndexedReadOnly uses only a current persisted projection for rich context.
	// It never materializes ontology state through the caller-managed reader.
	IndexedReadOnly bool
	// MetadataStoreFallback prevents nested graph/filter actions from opening a
	// writable store when the planned one-shot reader is unavailable.
	MetadataStoreFallback MetadataStoreFallbackPolicy

	// Optional: when set, shares one ontology freshness result across a
	// compound command's concurrent consumers.
	OntologyRuntimeProvider OntologyRuntimeProvider

	// Optional: intent describing what the caller is trying to accomplish.
	// Used for compression when enabled.
	Intent string

	// Optional: when set, enables intent-driven compression when over budget.
	Compressor contextpack.Compressor
}

// OntologyRuntimeProvider supplies a command-scoped, fresh ontology runtime.
// Implementations may memoize within one compound command; they must not cache
// across commands because Markdown remains the source of truth.
type OntologyRuntimeProvider interface {
	LoadOntologyRuntime(context.Context) (*ontology.Runtime, error)
}

type FileContextTextParams struct {
	Context     context.Context
	BudgetChars int
	Profile     ContextProfile
	// IndexedReadOnly restricts file-context enrichment to bounded reads from
	// SessionStore plus authored ontology schema. It never initializes live
	// graph/ontology state or performs repair/index writes.
	IndexedReadOnly bool
	// IndexedUnavailable carries store-open authority from one-shot bootstrap.
	// When set, indexed enrichment remains disabled and the result preserves the
	// exact missing/incompatible classification and remediation.
	IndexedUnavailable *IndexedContextFreshness

	SkipAnchors bool
	SkipEmbeds  bool
	AnchorKinds []codeanchor.AnchorKind

	Files []string

	// SubmoduleDepth controls how deep to search for module docs in subdirectories.
	// 0 (default): only immediate ancestor docs (current behavior)
	// 1: also include */CONTEXT.md
	// 2: also include */*/CONTEXT.md
	// Budget: 25000 chars dedicated to submodule docs.
	SubmoduleDepth int

	ExcludeNotePaths []string
	ExcludeDocPaths  []string

	// Optional: if provided, improves note relevance and code linking.
	CodeRefsByFile map[string][]coderefs.CodeRef

	// Optional: when set, enriches code files with code anchors.
	CodeAnchor *codeanchor.Service

	// Optional: when set, dedupes pieces already returned to a session.
	Dedupe DedupeTracker

	// Optional: when set, reuses an existing metadata/session store.
	SessionStore            *semdb.Store
	NoteMetadata            notemeta.Indexer
	MetadataStoreFallback   MetadataStoreFallbackPolicy
	OntologyRuntimeProvider OntologyRuntimeProvider
	EnsureLinkTarget        ontology.EnsureLinkTargetMode
	// ApplyLinkTargets must publish to the same live vault/store supplied here.
	// Commands compose it; read bundles and preview consumers leave it nil.
	ApplyLinkTargets func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error)

	// Optional: intent describing what the caller is trying to accomplish.
	// Used for compression when enabled.
	Intent string

	// Optional: when set, enables intent-driven compression when over budget.
	Compressor contextpack.Compressor
}

func ParseEnsureLinkTargetMode(raw string) (ontology.EnsureLinkTargetMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ontology.EnsureLinkTargetNever):
		return ontology.EnsureLinkTargetNever, nil
	case string(ontology.EnsureLinkTargetPlan):
		return ontology.EnsureLinkTargetPlan, nil
	case string(ontology.EnsureLinkTargetApply):
		return ontology.EnsureLinkTargetApply, nil
	default:
		return ontology.EnsureLinkTargetNever, fmt.Errorf("invalid ensure link target mode %q (expected never, plan, or apply)", raw)
	}
}

// KeyNoteMatch captures a key/MOC note and which pattern matched.
type KeyNoteMatch struct {
	Path    string
	Pattern string
}
