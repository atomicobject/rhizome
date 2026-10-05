package validate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
)

func prepareNativeNamespace(ctx context.Context, runCtx RunContext, plan compiledNamespacePlan, hooks *repairExecutionHooks, withoutGit ...bool) (*preparedRepairTransaction, error) {
	states, err := composeRepairFileStates(runCtx, plan.operations)
	if err != nil {
		return nil, err
	}
	if err := prepareNamespaceParents(runCtx, states); err != nil {
		return nil, err
	}
	var git *namespacegit.Preparation
	if len(withoutGit) == 0 || !withoutGit[0] {
		git, err = prepareNativeGit(ctx, runCtx, plan.operations, states)
	}
	if err != nil {
		return nil, err
	}
	purpose := &namespaceJournalPurpose{PurposeHeader: namespaceadmission.PurposeHeader{Version: namespaceadmission.PurposeVersion}, Summary: plan.summary, Moves: namespaceMoves(plan.operations)}
	if git != nil {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		purpose.Git = &namespaceGitJournal{GitDir: git.GitDir, IndexPath: git.IndexPath, LockPath: git.LockPath,
			RawOriginal: namespaceGitWitness{Exists: git.RawOriginal.Exists, Hash: git.RawOriginal.Hash, Mode: repairModeBits(git.RawOriginal.Mode)}, Token: hex.EncodeToString(token[:]), LockMode: 0o600}
		purpose.Git.LockHash = SourceHash(namespaceGitLockContent(purpose.Git))
		manifest := repairJournalManifest{TransactionID: plan.transaction.ID, Namespace: purpose}
		rollback, candidate := namespaceGitPaths(manifest)
		purpose.Git.Rollback = namespaceGitArtifact{Path: rollback, Hash: git.Rollback.Hash, Mode: repairModeBits(git.Rollback.Mode)}
		purpose.Git.Candidate = namespaceGitArtifact{Path: candidate, Hash: git.Candidate.Hash, Mode: repairModeBits(git.Candidate.Mode)}
		for _, move := range git.GitMoves {
			purpose.GitMoves = append(purpose.GitMoves, PathRename{From: move.Source, To: move.Destination})
		}
	}
	receipt := filepath.Join(runCtx.VaultPath, repairCompletionDirectory, strings.TrimPrefix(SourceHash([]byte(plan.transaction.ID)), "sha256:")+".json")
	receiptRel, err := filepath.Rel(runCtx.VaultPath, receipt)
	if err != nil {
		return nil, err
	}
	outcome := NamespaceOutcome{Decision: NamespaceCommitted, TransactionID: plan.transaction.ID, Moves: purpose.Moves, GitMoves: purpose.GitMoves, Summary: purpose.Summary, ReceiptPath: filepath.ToSlash(receiptRel)}
	content, err := json.Marshal(outcome)
	if err != nil {
		return nil, err
	}
	completion, err := prepareRepairCompletionState(runCtx, &RepairCompletionArtifact{Path: receipt, Content: append(content, '\n')})
	if err != nil {
		return nil, err
	}
	states = append(states, *completion)
	sortRepairFileStates(states)
	prepared, err := stageRepairFileStates(runCtx, plan.fingerprint, plan.transaction, plan.operations, states, purpose, hooks)
	if err != nil {
		return nil, err
	}
	if git != nil {
		for _, artifact := range []struct {
			snapshot namespacegit.Snapshot
			path     string
		}{{git.Rollback, purpose.Git.Rollback.Path}, {git.Candidate, purpose.Git.Candidate.Path}} {
			if err := createOwnedRepairArtifact(prepared.dir, &prepared.manifest, artifact.path, artifact.snapshot.Content, artifact.snapshot.Mode); err != nil {
				return prepared, err
			}
		}
		if err := acquireNamespaceGitExclusion(runCtx, prepared); err != nil {
			if errors.Is(err, errNamespaceGitAdmission) {
				if err := abandonNativeGitPreparation(runCtx, prepared); err != nil {
					return prepared, err
				}
				return prepareNativeNamespace(ctx, runCtx, plan, hooks, true)
			}
			return prepared, err
		}
		if err := verifyNamespaceGitOriginal(runCtx, prepared.manifest); err != nil {
			// A cooperating Git writer may finish before our exclusion was
			// acquired. Nothing has been published, so today's index is outside
			// rollback authority. Abort only owned preparation and exclusion.
			cleanupErr := cleanupFailedPrepare(runCtx, prepared.dir, prepared.manifest, hooks)
			if cleanupErr == nil {
				return nil, err
			}
			return prepared, errors.Join(err, cleanupErr)
		}
	}
	return prepared, nil
}

// Sibling staging needs destination parents before publication. As with the
// existing CLI and receipt preparation, rollback retains new empty folders.
func prepareNamespaceParents(runCtx RunContext, states []repairFileState) error {
	for _, state := range states {
		if !state.finalExists || state.internal {
			continue
		}
		rel, err := filepath.Rel(runCtx.VaultPath, filepath.Dir(state.abs))
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("namespace destination parent escapes vault")
		}
		current := runCtx.VaultPath
		for _, component := range strings.Split(rel, string(filepath.Separator)) {
			if component == "." || component == "" {
				continue
			}
			current = filepath.Join(current, component)
			if err := ensureRepairDirectDirectory(current, 0o755); err != nil {
				return err
			}
		}
	}
	return nil
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
	parent := filepath.Join(runCtx.VaultPath, ".rhizome")
	if err := ensureRepairDirectDirectory(parent, 0o755); err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp(parent, "namespace-prepare-")
	if err != nil {
		return nil, err
	}
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
