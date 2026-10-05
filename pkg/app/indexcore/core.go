// Package indexcore publishes structural indexes for batch and live callers.
// Callers hold one indexing lease across Discover and Publish. Provider-backed
// derived work belongs to their executor and never runs inside this package.
package indexcore

import (
	"context"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	sqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Request struct {
	VaultDefinition obsidian.VaultDefinition
	CodeConfig      anchors.Config
	NoteMetadata    notemeta.Indexer
	// Nil discovers the complete vault. A nonnil empty slice is a no-op.
	Paths         []paths.RelPath
	ForceMetadata bool
	ForceOntology bool
}

type Writer interface {
	codeintel.WriteQueue
	SubmitOwnershipTransitions(context.Context, []sqlite.OwnershipTransition) (sqlite.OwnershipTransitionResult, error)
	SubmitNoteMetadataDelta(context.Context, sqlite.NoteMetadataDelta) error
	SubmitOntologyDelta(context.Context, sqlite.OntologyDelta) error
	SubmitOntologyNodeReadModel(context.Context, anchors.IntelOntologyNodeReadModel) error
	SubmitStructuralFinalize(context.Context, indexwriter.StructuralFinalize) error
	SubmitDerivedDirty(context.Context, []anchors.DerivedScope) ([]anchors.DerivedWork, error)
	SubmitActivateDerived(context.Context, []anchors.DerivedWork) error
	FlushAndWait(context.Context) error
}

type PublishOptions struct {
	// BeforeIngest lets a batch adapter attach bounded progress/streaming work
	// once the sealed destination set is known.
	BeforeIngest                 func(codeFiles, noteFiles int) *indexingpipe.ProgressCallbacks
	AfterPreparedOwnershipCommit func(context.Context) error
}

type Discovery struct {
	Snapshot         noteownership.Snapshot
	Reconciliation   *noteownership.ReconciliationTracker
	CodeRoots        []paths.AbsPath
	FullDiscovery    bool
	Recovery         bool
	GraphRecovery    bool
	CodeRecovery     bool
	NotesRecovery    bool
	OntologyRecovery bool
	input            noteownership.DiscoveryInput
	baseline         notemeta.PublishedBaseline
	affected         map[string]struct{}
	persistedNotes   []paths.NotePath
	noop             bool
}

type Result struct {
	Snapshot        noteownership.Snapshot
	Reconciliation  *noteownership.ReconciliationTracker
	Code            codeintel.IndexResult
	Notes           codeintel.NoteIngestResult
	CodeRetirements []string
	Ontology        *ontology.SyncResult
	FullDiscovery   bool
	// StructuralGeneration is the exact committed ownership generation. The
	// adapter may acknowledge it only after its own durable obligations exist.
	StructuralGeneration int64
	DerivedWork          []anchors.DerivedWork
}

func (d Discovery) MarkdownPaths() []paths.NotePath {
	var result []paths.NotePath
	for _, c := range d.Snapshot.PresentNoteCandidates() {
		if c.Provider == noteownership.MarkdownFormatID {
			result = append(result, paths.NotePath(c.Path))
		}
	}
	return result
}
func (d Discovery) CodeKeepPaths() []paths.CodePath { return d.Snapshot.CodeKeepPaths() }
func (d Discovery) CompleteNotePaths() []paths.NotePath {
	set := make(map[string]struct{})
	if !d.FullDiscovery {
		for _, p := range d.persistedNotes {
			set[p.String()] = struct{}{}
		}
	}
	for _, c := range d.Snapshot.Candidates() {
		if c.Present && c.Owner == notediscovery.Note {
			set[c.Path.String()] = struct{}{}
		} else {
			delete(set, c.Path.String())
		}
	}
	strings := sortedSet(set)
	result := make([]paths.NotePath, len(strings))
	for i, p := range strings {
		result[i] = paths.NotePath(p)
	}
	return result
}
