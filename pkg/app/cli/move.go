package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type MoveParams struct {
	Context            context.Context
	NoteMetadata       notemeta.Indexer
	PostApplyRefresher validate.PostApplyRefresher
	Moves              []MoveRequest

	Overwrite       bool
	UpdateBacklinks bool
	ShouldOpen      bool
	CodeRefConfig   *coderefs.Config // Optional: update code references when set
}

// MoveRequest represents a single move/rename operation.
type MoveRequest struct {
	Source string
	Target string
}

// MoveResult captures the outcome for a single move.
type MoveResult struct {
	Source              string
	Target              string
	LinkUpdates         int
	CodeRefUpdates      int // Number of code reference updates for this move
	HeadingPointers     []HeadingPointer
	GitHistoryPreserved bool
}

// MoveSummary aggregates results for a batch of moves.
type MoveSummary struct {
	Mutation            validate.NamespaceMutationResult
	Results             []MoveResult
	TotalLinkUpdates    int
	TotalCodeRefUpdates int // Total code references updated
	CodeFilesUpdated    int // Total code files modified
	Skipped             []string
	HeadingPointers     []HeadingPointer
}

// MoveNotes performs one or more move operations, defaulting to no backlink rewrites (Obsidian links by note name).
// When UpdateBacklinks is true, all rewrites are done in a single vault scan for efficiency.
func MoveNotes(vault obsidian.VaultManager, uri obsidian.UriManager, params MoveParams) (MoveSummary, error) {
	if len(params.Moves) == 0 {
		return MoveSummary{Mutation: validate.NamespaceMutationResult{Current: validate.NamespaceOutcome{Decision: validate.NamespaceNotStarted}}}, errors.New("at least one move is required")
	}
	summary, err := applyNoteNamespace(params.Context, vault, params.NoteMetadata, params.PostApplyRefresher, params.Moves, params.Overwrite, params.UpdateBacklinks, params.CodeRefConfig)
	if !namespaceOptionalEffectsReady(summary.Mutation, err) {
		return summary, err
	}

	// Open the moved note if requested and only one target is present
	if params.ShouldOpen && len(summary.Results) == 1 {
		vaultName, err := vault.DefaultName()
		if err != nil {
			return summary, fmt.Errorf("open moved note: %w", err)
		}
		target := summary.Results[0].Target
		obsidianURI := uri.Construct(ObsOpenUrl, map[string]string{
			"file":  target,
			"vault": vaultName,
		})
		if err := uri.Execute(obsidianURI); err != nil {
			return summary, fmt.Errorf("open moved note: %w", err)
		}
	}

	return summary, nil
}

type linkMapping struct {
	Old            string
	New            string
	BasenameUnique bool // true if no other note in the vault shares this basename
	OldAliases     []string
}
