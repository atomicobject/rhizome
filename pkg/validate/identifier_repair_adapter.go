package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// IdentifierRepairActionBinding binds one sealed collision membership to the
// consumer-owned action and issue identities used by the repair engine.
type IdentifierRepairActionBinding struct {
	MembershipKey string    `json:"membershipKey"`
	Action        FixAction `json:"action"`
}

// BuildIdentifierRepairPlan maps a sealed assembly for review. Apply remains
// fail-closed: identifier actions require the lease-held remap installed by
// ApplyIdentifierRepairPlan.
func BuildIdentifierRepairPlan(
	ctx context.Context,
	runCtx RunContext,
	assembly *identifierreconcile.RepairAssembly,
	bindings []IdentifierRepairActionBinding,
) (*RepairPlan, error) {
	if err := assembly.RevalidateCompleteSnapshot(ctx, runCtx.VaultDef); err != nil {
		return nil, err
	}
	return buildIdentifierRepairPlan(ctx, authoritativeIdentifierRunContext(runCtx), assembly, bindings)
}

// ApplyIdentifierRepairPlan applies a previously reviewed plan. The repair
// engine owns lock acquisition and invokes the authoritative mapper only after
// recovery preflight while its opaque lease remains held.
func ApplyIdentifierRepairPlan(
	ctx context.Context,
	runCtx RunContext,
	reviewed *RepairPlan,
	assembly *identifierreconcile.RepairAssembly,
	bindings []IdentifierRepairActionBinding,
	opts Options,
) (*FixExecution, error) {
	if reviewed == nil {
		return nil, fmt.Errorf("reviewed identifier repair plan is required")
	}
	canonicalReviewed, err := canonicalReviewedIdentifierRepairPlan(reviewed)
	if err != nil {
		return nil, err
	}
	reviewed = canonicalReviewed
	assemblySnapshot, err := assembly.ValidatedSnapshot()
	if err != nil {
		return nil, err
	}
	if reviewed.AuthorityFingerprint == "" || reviewed.AuthorityFingerprint != assemblySnapshot.Fingerprint {
		return nil, fmt.Errorf("reviewed identifier repair plan was built from a different identifier repair assembly; replan before applying")
	}
	if !reviewed.RequiresLeaseHeldReplan {
		return nil, fmt.Errorf("reviewed identifier repair plan lacks the engine-owned lease revalidation marker")
	}
	if opts.leaseHeldRepairPlanner != nil {
		return nil, fmt.Errorf("identifier repair lease planner is engine-owned")
	}
	if opts.PostApplyRefresher == nil {
		return nil, fmt.Errorf("identifier repair requires prepared refresh and held-lease postcheck orchestration")
	}
	if opts.postApplyCheck != nil {
		return nil, fmt.Errorf("identifier repair postcheck callback is engine-owned")
	}
	genericPlan, err := reviewedGenericRepairSubplan(reviewed, bindings)
	if err != nil {
		return nil, err
	}
	var reviewedChecks []string
	for _, transaction := range reviewed.Transactions {
		reviewedChecks = append(reviewedChecks, transaction.Checks...)
	}
	reviewedChecks = sortedUnique(reviewedChecks)
	var baseChecks []string
	if opts.repairSessionPostcheck != nil {
		baseChecks = append([]string(nil), opts.Checks...)
	}
	var postApplyResult *Result
	var postValidationDuration time.Duration
	opts.postApplyCheck = func(ctx context.Context, lease *IndexLockLease, runtime *ontology.Runtime, scope repairPostApplyScope) error {
		started := time.Now()
		defer func() {
			postValidationDuration += nonZeroDuration(time.Since(started))
		}()
		if err := lease.RequireHeldForVault(runCtx.VaultPath); err != nil {
			return err
		}
		checks := append(append([]string(nil), baseChecks...), scope.Checks...)
		if len(scope.Checks) == 0 {
			checks = append(checks, reviewedChecks...)
		}
		checks = sortedUnique(checks)
		resolvedChecks, err := ResolveChecks(checks)
		if err != nil {
			return fmt.Errorf("resolve identifier repair postchecks: %w", err)
		}
		if registryNeedsOntologyRuntime(resolvedChecks) && runtime == nil {
			return fmt.Errorf("identifier repair refresher did not provide prepared ontology runtime")
		}
		postcheckOpts := opts
		postcheckOpts.Checks = checks
		postcheckOpts.Fix = false
		postcheckOpts.Confirm = nil
		postcheckOpts.PostApplyRefresher = nil
		postcheckOpts.leaseHeldRepairPlanner = nil
		postcheckOpts.postApplyCheck = nil
		postcheckOpts.postApplyJournalValidation = true
		postcheckOpts.RunContext = &runCtx
		result, _, err := RunSuiteOncePrepared(ctx, postcheckOpts, runtime, nil)
		if err != nil {
			return fmt.Errorf("identifier repair post-apply validation: %w", err)
		}
		postApplyResult = &result
		if result.ErrorCount > 0 {
			return fmt.Errorf(
				"identifier repair post-apply validation reported %d check error(s): %s",
				result.ErrorCount,
				repairPostcheckErrors(result),
			)
		}
		return nil
	}
	opts.leaseHeldRepairPlanner = func(ctx context.Context, lease *IndexLockLease) (*FixPlan, error) {
		identifierPlan, err := buildIdentifierRepairPlanUnderLease(ctx, runCtx, lease, assembly, bindings)
		if err != nil {
			return nil, err
		}
		return mergeRepairPlans(genericPlan, []*RepairPlan{identifierPlan})
	}
	applyStarted := time.Now()
	execution, err := ApplyFixPlan(ctx, runCtx, reviewed, opts)
	if execution != nil {
		applyDuration := time.Since(applyStarted) - postValidationDuration
		execution.IdentifierReconciliationTimings = &identifierreconcile.StageTimings{
			Apply:          nonZeroDuration(applyDuration),
			PostValidation: postValidationDuration,
		}
	}
	if execution != nil && postApplyResult != nil {
		mergePostApplyRemainingEvidence(execution, *postApplyResult)
	}
	if postApplyResult != nil && opts.repairSessionPostcheck != nil {
		opts.repairSessionPostcheck(*postApplyResult)
	}
	return execution, err
}

// reviewedGenericRepairSubplan preserves the reviewed, fingerprinted generic
// portion of a mixed repair plan while the identifier portion is rebuilt from
// its sealed assembly under the held lease. Operations may not cross that
// authority boundary; an exact combined fingerprint comparison in ApplyFixPlan
// remains the final apply gate.
func reviewedGenericRepairSubplan(reviewed *RepairPlan, bindings []IdentifierRepairActionBinding) (*RepairPlan, error) {
	identifierActionIDs := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		actionID := strings.TrimSpace(binding.Action.ID)
		if actionID == "" {
			return nil, fmt.Errorf("identifier repair binding requires action identity")
		}
		identifierActionIDs[actionID] = struct{}{}
	}
	var actions []FixAction
	for _, action := range reviewed.Actions {
		if _, identifierOwned := identifierActionIDs[action.ID]; identifierOwned {
			continue
		}
		actions = append(actions, action)
	}
	var operations []RepairOperation
	for _, operation := range reviewed.Operations {
		actionIDs := sortedUnique(append(append([]string(nil), operation.ActionIDs...), operation.ActionID))
		var identifierOwned, genericOwned bool
		for _, actionID := range actionIDs {
			if _, ok := identifierActionIDs[actionID]; ok {
				identifierOwned = true
			} else {
				genericOwned = true
			}
		}
		if identifierOwned && genericOwned {
			return nil, fmt.Errorf("repair operation %q crosses identifier and generic authority", operation.ID)
		}
		if !identifierOwned {
			operations = append(operations, operation)
		}
	}
	if len(actions) == 0 && len(operations) == 0 {
		return nil, nil
	}
	generic := RepairPlan{Actions: actions, Operations: operations}
	for _, action := range actions {
		generic.IssueKeys = append(generic.IssueKeys, action.IssueKeys...)
		generic.TotalCount++
		switch action.Safety {
		case FixSafetySafe:
			generic.SafeCount++
		case FixSafetyConfirm:
			generic.ConfirmationCount++
		case FixSafetyAgent:
			generic.AgentCount++
		}
	}
	finalized, err := FinalizeRepairPlan(generic)
	if err != nil {
		return nil, err
	}
	return &finalized, nil
}

func canonicalReviewedIdentifierRepairPlan(reviewed *RepairPlan) (*RepairPlan, error) {
	if reviewed == nil {
		return nil, fmt.Errorf("reviewed identifier repair plan is required")
	}
	suppliedFingerprint := strings.TrimSpace(reviewed.Fingerprint)
	if suppliedFingerprint == "" {
		return nil, fmt.Errorf("reviewed identifier repair plan fingerprint is required")
	}
	canonical, err := FinalizeRepairPlan(*reviewed)
	if err != nil {
		return nil, err
	}
	if canonical.Fingerprint != suppliedFingerprint {
		return nil, fmt.Errorf("reviewed identifier repair plan fingerprint changed; replan before applying")
	}
	return &canonical, nil
}

func repairPostcheckErrors(result Result) string {
	var messages []string
	for _, check := range result.Checks {
		if strings.TrimSpace(check.Error) == "" {
			continue
		}
		messages = append(messages, check.Name+": "+check.Error)
	}
	if len(messages) == 0 {
		return "unknown check error"
	}
	sort.Strings(messages)
	return strings.Join(messages, "; ")
}

func buildIdentifierRepairPlanUnderLease(
	ctx context.Context,
	runCtx RunContext,
	lease *IndexLockLease,
	assembly *identifierreconcile.RepairAssembly,
	bindings []IdentifierRepairActionBinding,
) (*RepairPlan, error) {
	if lease == nil {
		return nil, fmt.Errorf("identifier repair requires held index lease")
	}
	if err := lease.RequireHeldForVault(runCtx.VaultPath); err != nil {
		return nil, err
	}
	vaultRoot := strings.TrimSpace(runCtx.VaultDef.BasePath())
	if vaultRoot == "" {
		return nil, fmt.Errorf("identifier repair requires vault definition root")
	}
	if err := lease.RequireHeldForVault(vaultRoot); err != nil {
		return nil, fmt.Errorf("identifier repair run context roots disagree: %w", err)
	}
	if err := assembly.RevalidateCompleteSnapshot(ctx, runCtx.VaultDef); err != nil {
		return nil, err
	}
	return buildIdentifierRepairPlan(ctx, authoritativeIdentifierRunContext(runCtx), assembly, bindings)
}

func authoritativeIdentifierRunContext(runCtx RunContext) RunContext {
	runCtx.NoteReader = &obsidian.Note{}
	return runCtx
}

type identifierBindingSet struct {
	actions                    []FixAction
	actionIDsByMembership      map[string][]string
	issueKeysByMembership      map[string][]string
	requiredChecksByMembership map[string][]string
}

func bindIdentifierRepairActions(snapshot *identifierreconcile.RepairAssembly, bindings []IdentifierRepairActionBinding) (identifierBindingSet, error) {
	expected := make(map[string]identifierreconcile.CollisionRepairIntent, len(snapshot.Components))
	for _, component := range snapshot.Components {
		expected[component.MembershipKeys[0]] = component
	}
	byKey := make(map[string]IdentifierRepairActionBinding, len(bindings))
	for _, raw := range bindings {
		binding := raw
		binding.MembershipKey = strings.TrimSpace(binding.MembershipKey)
		binding.Action.ID = strings.TrimSpace(binding.Action.ID)
		binding.Action.IssueKeys = sortedUnique(binding.Action.IssueKeys)
		if _, ok := expected[binding.MembershipKey]; !ok {
			return identifierBindingSet{}, fmt.Errorf("extraneous identifier repair binding %q", binding.MembershipKey)
		}
		if _, duplicate := byKey[binding.MembershipKey]; duplicate {
			return identifierBindingSet{}, fmt.Errorf("duplicate identifier repair binding %q", binding.MembershipKey)
		}
		if binding.Action.ID == "" || strings.TrimSpace(binding.Action.Check) == "" || len(binding.Action.IssueKeys) == 0 {
			return identifierBindingSet{}, fmt.Errorf("identifier repair binding requires action, check, and issue identities")
		}
		canonicalCheck, ok := CanonicalCheck(binding.Action.Check)
		if !ok {
			return identifierBindingSet{}, fmt.Errorf("identifier repair binding %q has unknown check %q", binding.MembershipKey, binding.Action.Check)
		}
		binding.Action.Check = canonicalCheck
		if binding.Action.Safety != FixSafetySafe && binding.Action.Safety != FixSafetyConfirm && binding.Action.Safety != FixSafetyAgent {
			return identifierBindingSet{}, fmt.Errorf("identifier repair binding %q has invalid safety", binding.MembershipKey)
		}
		byKey[binding.MembershipKey] = binding
	}
	result := identifierBindingSet{
		actionIDsByMembership:      make(map[string][]string, len(expected)),
		issueKeysByMembership:      make(map[string][]string, len(expected)),
		requiredChecksByMembership: make(map[string][]string, len(expected)),
	}
	for _, component := range snapshot.Components {
		key := component.MembershipKeys[0]
		binding, ok := byKey[key]
		if !ok {
			return identifierBindingSet{}, fmt.Errorf("missing identifier repair binding %q", key)
		}
		primary := binding.Action
		result.actions = append(result.actions, primary)
		result.actionIDsByMembership[key] = append(result.actionIDsByMembership[key], primary.ID)
		result.issueKeysByMembership[key] = append(result.issueKeysByMembership[key], primary.IssueKeys...)
		for _, postcheck := range component.Postchecks {
			check, err := identifierPostcheckName(postcheck.Check)
			if err != nil {
				return identifierBindingSet{}, err
			}
			result.requiredChecksByMembership[key] = append(result.requiredChecksByMembership[key], check)
		}
		result.actionIDsByMembership[key] = sortedUnique(result.actionIDsByMembership[key])
		result.issueKeysByMembership[key] = sortedUnique(result.issueKeysByMembership[key])
		result.requiredChecksByMembership[key] = sortedUnique(result.requiredChecksByMembership[key])
	}
	sort.Slice(result.actions, func(i, j int) bool { return result.actions[i].ID < result.actions[j].ID })
	return result, nil
}

func identifierPostcheckName(check identifierreconcile.RepairPostcheck) (string, error) {
	switch check {
	case identifierreconcile.PostcheckIdentifiers:
		return CheckIdentifiers, nil
	case identifierreconcile.PostcheckOntology:
		return CheckOntology, nil
	case identifierreconcile.PostcheckBrokenLinks:
		return CheckBrokenLinks, nil
	case identifierreconcile.PostcheckFragileExternal:
		return CheckFragileExternal, nil
	default:
		return "", fmt.Errorf("unknown identifier repair postcheck %q", check)
	}
}
