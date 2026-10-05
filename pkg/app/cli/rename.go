package actions

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// RenameParams configures a note rename operation.
//
// When UpdateBacklinks is true (default), all wikilinks pointing to the source
// note are rewritten to point to the target. When CodeRefConfig is set, code
// references (wikilinks/mentions in source code) are also updated.
type RenameParams struct {
	Context            context.Context
	NoteMetadata       notemeta.Indexer
	PostApplyRefresher validate.PostApplyRefresher
	Source             string
	Target             string
	Overwrite          bool
	UpdateBacklinks    bool
	CodeRefConfig      *coderefs.Config // Optional: update code references when set
}

type RenameResult struct {
	Mutation            validate.NamespaceMutationResult
	RenamedPath         string
	LinkUpdates         int
	CodeRefUpdates      int // Number of code reference updates
	CodeFilesUpdated    int // Number of code files modified
	Skipped             []string
	HeadingPointers     []HeadingPointer
	GitHistoryPreserved bool
}

type HeadingPointer struct {
	SourceNote string
	TargetNote string
	Fragment   string
	Command    string
}

func RenameNote(vault obsidian.VaultManager, params RenameParams) (RenameResult, error) {
	summary, err := applyNoteNamespace(params.Context, vault, params.NoteMetadata, params.PostApplyRefresher,
		[]MoveRequest{{Source: params.Source, Target: params.Target}}, params.Overwrite, params.UpdateBacklinks, params.CodeRefConfig)
	result := RenameResult{
		Mutation: summary.Mutation, Skipped: summary.Skipped, HeadingPointers: summary.HeadingPointers,
		CodeRefUpdates: summary.TotalCodeRefUpdates, CodeFilesUpdated: summary.CodeFilesUpdated,
	}
	if len(summary.Results) == 1 {
		result.RenamedPath = summary.Results[0].Target
		result.LinkUpdates = summary.Results[0].LinkUpdates
		result.GitHistoryPreserved = summary.Results[0].GitHistoryPreserved
	}
	return result, err
}

func splitHeadingPointerFragment(target string) (string, string) {
	idx := strings.Index(target, "#")
	if idx < 0 {
		return target, ""
	}
	return target[:idx], target[idx+1:]
}
