package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
)

// NamespaceMutationPlanner plans read-only while borrowing the engine's lease.
type NamespaceMutationPlanner interface {
	PlanNamespaceMutation(context.Context, *indexlock.Lease) (NamespaceMutationPlan, error)
}

type NamespaceMutationPlan struct {
	Operations []RepairOperation
	Summary    json.RawMessage
}

type NamespaceDecision string

const (
	NamespaceNotStarted NamespaceDecision = "not_started"
	NamespaceRestored   NamespaceDecision = "restored"
	NamespaceCommitted  NamespaceDecision = "committed"
	NamespaceUnresolved NamespaceDecision = "unresolved"
)

type NamespaceOutcome struct {
	Decision        NamespaceDecision `json:"decision"`
	TransactionID   string            `json:"transactionId,omitempty"`
	Moves           []PathRename      `json:"moves,omitempty"`
	GitMoves        []PathRename      `json:"gitMoves,omitempty"`
	RecoveryPending bool              `json:"recoveryPending"`
	ReceiptPath     string            `json:"receiptPath,omitempty"`
	Summary         json.RawMessage   `json:"summary,omitempty"`
}

type NamespaceMutationResult struct {
	Current   NamespaceOutcome   `json:"current"`
	Recovered []NamespaceOutcome `json:"recovered,omitempty"`
}

// NamespacePostCommit borrows the current request's lease for optional writes
// after required convergence and cleanup. It is synchronous and never replays;
// optional failures and counts remain caller-owned.
type NamespacePostCommit func(context.Context, *indexlock.Lease)

// ApplyNamespaceMutation owns the complete required namespace publication.
// Its planner and refresher borrow the same lease and cannot release it.
func ApplyNamespaceMutation(ctx context.Context, runCtx RunContext, planner NamespaceMutationPlanner, refresher PostApplyRefresher, postCommit NamespacePostCommit) (NamespaceMutationResult, error) {
	return applyNamespaceMutation(ctx, runCtx, planner, refresher, postCommit, nil)
}

func applyNamespaceMutation(ctx context.Context, runCtx RunContext, planner NamespaceMutationPlanner, refresher PostApplyRefresher, postCommit NamespacePostCommit, hooks *repairExecutionHooks) (result NamespaceMutationResult, resultErr error) {
	result.Current.Decision = NamespaceNotStarted
	if planner == nil || refresher == nil {
		return result, fmt.Errorf("native namespace mutation requires a planner and post-apply refresher before publication or recovery")
	}
	if err := validateRepairSessionRoots(runCtx); err != nil {
		return result, err
	}
	vault, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil {
		return result, err
	}
	runCtx.VaultPath = vault.Root()
	lease, release, err := waitRepairIndexLockLease(ctx, filepath.Join(runCtx.VaultPath, ".rhizome", "index.lock"))
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if err := lease.Validate(runCtx.VaultPath); err != nil {
		return result, err
	}
	runCtx.sourceLease = lease

	writer, recovered, recoveryErr := sourcetxn.Open(runCtx.VaultPath, lease, sourcetxn.WriterOptions{Role: sourcetxn.RepairOwner, RepairPolicy: validateSourceJournalChecks, Hooks: hooks})
	if writer != nil {
		defer writer.Close()
	}
	runCtx.sourceWriter = writer
	recovery := repairRecovery(recovered)
	result.Recovered, err = convergeRecoveredNamespaces(ctx, runCtx, lease, refresher, recovery.native, hooks)
	result.Recovered = append(result.Recovered, namespaceCleanupOutcomes(recovery.nativeCleanup)...)
	recoveryErr = errors.Join(recoveryErr, err)
	oldWork := len(recovery.native) + len(recovery.nativeCleanup) + len(recovery.committed) + len(recovery.rolledBack) + len(recovery.noMutation) + len(recovery.partial) + len(recovery.cleanupPending)
	if oldWork > 0 {
		genericErr := convergeNamespaceGenericBarrier(ctx, runCtx, lease, refresher, recovery, hooks)
		return result, errors.Join(recoveryErr, genericErr, fmt.Errorf("older repair work recovered; replan before starting this namespace request"))
	}
	if recoveryErr != nil {
		return result, recoveryErr
	}
	if err := sourcetxn.CheckPending(runCtx.VaultPath); err != nil {
		return result, err
	}
	plan, err := planner.PlanNamespaceMutation(ctx, lease)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	compiled, err := compileNamespacePlan(runCtx, plan)
	if err != nil {
		return result, err
	}
	prepared, prepareErr := prepareNativeNamespace(ctx, runCtx, compiled)
	if prepared == nil {
		return result, prepareErr
	}
	var publication sourcetxn.Completion
	applyErr := prepareErr
	if applyErr == nil {
		applyErr = ctx.Err()
	}
	if applyErr != nil {
		if !errors.Is(applyErr, errSimulatedRepairInterruption) {
			applyErr = errors.Join(applyErr, writer.Abort(prepared))
		}
		return result, applyErr
	}
	publication, applyErr = writer.Publish(prepared)
	journal := repairJournal(publication)
	result.Current = namespaceJournalOutcome(journal)
	if result.Current.Decision != NamespaceUnresolved && journal.manifest.Namespace.SettledDecision != "" {
		convergeErr := convergeNativeNamespace(ctx, runCtx, lease, refresher, journal, result.Current.Decision == NamespaceCommitted, hooks)
		if convergeErr == nil {
			result.Current.RecoveryPending = false
		}
		applyErr = errors.Join(applyErr, convergeErr)
	}
	if applyErr == nil && result.Current.Decision == NamespaceCommitted && !result.Current.RecoveryPending && postCommit != nil {
		postCommit(ctx, lease)
	}
	return result, applyErr
}
