package validate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
)

func prepareNativeNamespace(ctx context.Context, run RunContext, plan compiledNamespacePlan, withoutGit ...bool) (*sourcetxn.Transaction, error) {
	states, err := composeRepairFileStates(run, plan.operations)
	if err != nil {
		return nil, err
	}
	var git *namespacegit.Preparation
	if len(withoutGit) == 0 || !withoutGit[0] {
		git, err = prepareNativeGit(ctx, run, plan.operations, states)
	}
	if err != nil {
		return nil, err
	}
	changed, renamed, deleted := repairOperationDelta(plan.operations)
	request := sourcetxn.NamespaceRequest{RepairRequest: sourcetxn.RepairRequest{TransactionID: plan.transaction.ID, Fingerprint: plan.fingerprint, OperationIDs: append([]string(nil), plan.transaction.OperationIDs...), Changed: changed, Renamed: renamed, Deleted: deleted, Files: sourceChanges(states)}, Summary: plan.summary, Moves: namespaceMoves(plan.operations)}
	if git != nil {
		request.Git = &sourcetxn.GitInput{GitDir: git.GitDir, IndexPath: git.IndexPath, LockPath: git.LockPath,
			RawOriginal: sourcetxn.GitWitness{Exists: git.RawOriginal.Exists, Hash: git.RawOriginal.Hash, Mode: repairModeBits(git.RawOriginal.Mode)},
			Rollback:    sourcetxn.Snapshot{Exists: true, Content: git.Rollback.Content, Mode: git.Rollback.Mode},
			Candidate:   sourcetxn.Snapshot{Exists: true, Content: git.Candidate.Content, Mode: git.Candidate.Mode}}
		for _, move := range git.GitMoves {
			request.Git.Moves = append(request.Git.Moves, PathRename{From: move.Source, To: move.Destination})
		}
	}
	return run.sourceWriter.PrepareNamespace(request)
}

func prepareNativeGit(ctx context.Context, runCtx RunContext, operations []RepairOperation, states []repairFileState) (*namespacegit.Preparation, error) {
	var moves []namespacegit.Move
	endpoints := map[string]bool{}
	for _, op := range operations {
		if op.Kind != RepairOperationRename {
			continue
		}
		moves = append(moves, namespacegit.Move{Source: op.Path, Destination: op.DestinationPath, Overwrite: op.DestinationState.Kind == RepairDestinationOccupied})
		endpoints[op.Path], endpoints[op.DestinationPath] = true, true
	}
	var originals []namespacegit.File
	for _, state := range states {
		if state.originalExists && endpoints[state.originalRel] {
			originals = append(originals, namespacegit.File{Path: state.originalRel, Content: append([]byte(nil), state.originalContent...), Mode: state.originalMode})
		}
	}
	scratch, err := os.MkdirTemp("", "rhizome-namespace-prepare-")
	if err != nil {
		return nil, err
	}
	scratch = paths.ResolveSymlinks(scratch).String()
	preparation, prepareErr := namespacegit.Prepare(ctx, runCtx.VaultPath, scratch, moves, originals)
	// This exclusively created scratch has no durable publication authority.
	// The helper returns isolated bytes; no live artifact depends on its files.
	cleanupErr := os.RemoveAll(scratch)
	if cleanupErr != nil {
		return nil, errors.Join(prepareErr, fmt.Errorf("remove native Git preparation scratch: %w", cleanupErr))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errors.Is(prepareErr, namespacegit.ErrFallback) {
		return nil, nil
	}
	return preparation, prepareErr
}
