// Package runtimeview defines the narrow, read-only live capability contract
// shared by bootstrap producers and agent/web consumers.
package runtimeview

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// CapabilityState is one independently initialized runtime phase.
type CapabilityState struct {
	Done  bool
	Ready bool
	Err   error
}

// Snapshot is an immutable per-call view of currently available capabilities
// and dependencies.
type Snapshot struct {
	VaultDefinition obsidian.VaultDefinition
	Configuration   *repoconfig.Resolved
	Search          CapabilityState
	Semantic        CapabilityState
	Code            CapabilityState
	Leader          bool

	WatchHub          *watchhub.Hub
	NoteReader        obsidian.NoteReader
	NoteIndex         embeddings.Index
	NoteProvider      embeddings.Provider
	NoteIndexPath     string
	CodeIndex         codeindex.Index
	CodeProvider      embeddings.Provider
	CodeAnchorService *codeanchor.Service
	IntelStore        *semdb.Store
	SessionStore      semdb.SessionDedupeStore
}

// View is the non-blocking snapshot plus phase-specific wait contract.
type View interface {
	Snapshot() Snapshot
	WaitForSearch(context.Context) error
	WaitForSemantic(context.Context) error
	WaitForCodeIndex(context.Context) error
}
