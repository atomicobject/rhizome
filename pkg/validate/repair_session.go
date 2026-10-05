package validate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// ApplyRepairSession applies one already-reviewed validation result. The
// in-memory Result is intentional: identifier plans carry sealed private
// assembly and action-binding authority that must survive planning into apply.
//
// A successful mutating or recovery session returns the exact prepared-runtime
// postcheck result produced while the repair engine still owns the index-lock
// lease. It never refreshes or validates a second time after releasing it.
func ApplyRepairSession(
	ctx context.Context,
	planned Result,
	runCtx RunContext,
	opts Options,
) (Result, *FixExecution, error) {
	if !opts.Fix {
		return planned, nil, fmt.Errorf("repair session requires apply authority")
	}
	if opts.postApplyCheck != nil || opts.leaseHeldRepairPlanner != nil || opts.repairSessionPostcheck != nil {
		return planned, nil, fmt.Errorf("repair session orchestration is validate-owned")
	}
	if len(opts.ApplySelection) > 0 {
		// Both the generic and identifier/mixed engines select through
		// ApplyFixPlan; validating here also rejects a selection against an
		// empty plan before journal recovery can run.
		var reviewedActions []FixAction
		if planned.FixPlan != nil {
			reviewedActions = planned.FixPlan.Actions
		}
		if _, err := selectRepairActions(reviewedActions, opts); err != nil {
			return planned, nil, err
		}
	}

	pending, err := DetectPendingRepairJournals(runCtx)
	if err != nil {
		return planned, nil, fmt.Errorf("inspect repair journals: %w", err)
	}
	hasReviewedWork := planned.FixPlan != nil && (len(planned.FixPlan.Actions) > 0 || len(planned.FixPlan.Operations) > 0)
	if !hasReviewedWork && len(planned.RepairJournals) == 0 && len(pending) == 0 {
		return planned, nil, nil
	}
	if opts.PostApplyRefresher == nil {
		return planned, nil, fmt.Errorf("repair session requires a post-apply refresher before mutation or recovery")
	}
	if err := validateRepairSessionRoots(runCtx); err != nil {
		return planned, nil, err
	}

	opts.RunContext = &runCtx
	opts.Checks = mergeRepairRecoveryChecks(
		repairSessionCompletedChecks(planned),
		append(append([]RepairJournalEvidence(nil), planned.RepairJournals...), pending...),
	)
	var postcheck *Result
	var execution *FixExecution
	if planned.FixPlan != nil && planned.FixPlan.RequiresLeaseHeldReplan {
		payload, payloadErr := repairSessionIdentifierPayload(planned)
		if payloadErr != nil {
			return planned, nil, payloadErr
		}
		opts.repairSessionPostcheck = func(result Result) {
			captured := result
			postcheck = &captured
		}
		execution, err = ApplyIdentifierRepairPlan(
			ctx, runCtx, planned.FixPlan, payload.Assembly, payload.Bindings, opts,
		)
	} else {
		opts.postApplyCheck = repairSessionPostcheck(runCtx, opts, &postcheck, ontologyEditSessionPlan(planned.FixPlan))
		execution, err = ApplyFixPlan(ctx, runCtx, planned.FixPlan, opts)
		if execution != nil && postcheck != nil {
			mergePostApplyRemainingEvidence(execution, *postcheck)
		}
	}

	result := planned
	if postcheck != nil {
		result = *postcheck
	} else if err != nil {
		// Without an in-lease prepared postcheck, the reviewed plan may no
		// longer describe current bytes. Keep execution/replan evidence, but do
		// not expose stale actions as if they were still executable.
		result.FixPlan = nil
	}
	result.FixExecution = execution
	attachRepairSessionIdentifierTimings(&result, execution)
	journals, journalErr := DetectPendingRepairJournals(runCtx)
	if journalErr == nil {
		result.RepairJournals = journals
	}
	result.NextActions = BuildNextActions(result)
	if err == nil && execution != nil && execution.AppliedTransactions > 0 && postcheck == nil {
		err = fmt.Errorf("repair session mutated vault state without a prepared postcheck result")
	}
	return result, execution, errors.Join(err, journalErr)
}

func validateRepairSessionRoots(runCtx RunContext) error {
	configuredRoot := strings.TrimSpace(runCtx.VaultDef.BasePath())
	requestedRoot := strings.TrimSpace(runCtx.VaultPath)
	if configuredRoot == "" || requestedRoot == "" {
		return fmt.Errorf("repair session requires one resolved vault root")
	}
	configured, err := paths.NewVaultPaths(configuredRoot)
	if err != nil || configured.Root() == "" {
		return fmt.Errorf("resolve repair vault definition root: %w", err)
	}
	requested, err := paths.NewVaultPaths(requestedRoot)
	if err != nil || requested.Root() == "" {
		return fmt.Errorf("resolve repair run-context root: %w", err)
	}
	if !paths.CaseEqual(configured.Root(), requested.Root()) {
		return fmt.Errorf("repair run context roots disagree")
	}
	return nil
}

func repairSessionCompletedChecks(planned Result) []string {
	checks := make([]string, 0, len(planned.Checks))
	for _, result := range planned.Checks {
		if name := strings.TrimSpace(result.Name); name != "" {
			checks = append(checks, name)
		}
	}
	return sortedUnique(checks)
}

func repairSessionIdentifierPayload(planned Result) (*identifierRepairPayload, error) {
	var payload *identifierRepairPayload
	for index := range planned.Checks {
		candidate := planned.Checks[index].identifierRepair
		if candidate == nil || candidate.Assembly == nil {
			continue
		}
		if payload != nil {
			return nil, fmt.Errorf("reviewed result contains multiple identifier repair payloads")
		}
		payload = candidate
	}
	if payload == nil {
		return nil, fmt.Errorf("reviewed identifier repair result lost its sealed assembly or action bindings; replan")
	}
	return payload, nil
}

// ontologyEditSessionPlan reports whether plan only saves browser edits. Such a
// save postchecks the notes it wrote; the runtime's background validation
// refresh covers the rest of the vault without holding the index lock.
func ontologyEditSessionPlan(plan *FixPlan) bool {
	if plan == nil || len(plan.Actions) == 0 {
		return false
	}
	for _, action := range plan.Actions {
		if action.Kind != FixKindOntologyEditSessionCommit {
			return false
		}
	}
	return true
}

func repairSessionPostcheck(runCtx RunContext, opts Options, captured **Result, scopeToAffectedPaths bool) repairPostApplyCheck {
	baseChecks := append([]string(nil), opts.Checks...)
	return func(ctx context.Context, lease *IndexLockLease, runtime *ontology.Runtime, scope repairPostApplyScope) error {
		if err := lease.RequireHeldForVault(runCtx.VaultPath); err != nil {
			return err
		}
		checks := sortedUnique(append(baseChecks, scope.Checks...))
		resolved, err := ResolveChecks(checks)
		if err != nil {
			return fmt.Errorf("resolve repair postchecks: %w", err)
		}
		if registryNeedsOntologyRuntime(resolved) && runtime == nil {
			return fmt.Errorf("post-apply refresher did not provide its prepared ontology runtime")
		}
		postcheckOpts := opts
		postcheckOpts.Checks = checks
		postcheckOpts.Fix = false
		postcheckOpts.Confirm = nil
		postcheckOpts.PostApplyRefresher = nil
		postcheckOpts.leaseHeldRepairPlanner = nil
		postcheckOpts.postApplyCheck = nil
		postcheckOpts.repairSessionPostcheck = nil
		postcheckOpts.postApplyJournalValidation = true
		if scopeToAffectedPaths {
			postcheckOpts.postcheckPaths = scope.AffectedPaths
		}
		postcheckOpts.RunContext = &runCtx
		result, _, err := RunSuiteOncePrepared(ctx, postcheckOpts, runtime, nil)
		if err != nil {
			return fmt.Errorf("post-apply validation: %w", err)
		}
		copy := result
		*captured = &copy
		if result.ErrorCount > 0 {
			return fmt.Errorf(
				"post-apply validation reported %d check error(s): %s",
				result.ErrorCount,
				repairPostcheckErrors(result),
			)
		}
		return nil
	}
}

func attachRepairSessionIdentifierTimings(result *Result, execution *FixExecution) {
	if result == nil || result.IdentifierReconciliation == nil || execution == nil || execution.IdentifierReconciliationTimings == nil {
		return
	}
	reconciliation := *result.IdentifierReconciliation
	reconciliation.Diagnostics.Timings.Apply = execution.IdentifierReconciliationTimings.Apply
	reconciliation.Diagnostics.Timings.PostValidation = execution.IdentifierReconciliationTimings.PostValidation
	result.IdentifierReconciliation = &reconciliation
}
