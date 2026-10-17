package validate

import (
	"context"

	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	identifierrewrite "github.com/atomicobject/rhizome/pkg/validate/identifierreconcile/rewrite"

	"strings"
)

// BuildRepairPlan converts validation findings into immutable, source-backed
// operations. It is read-only; the returned plan is the only canonical input
// to transactional apply.
func BuildRepairPlan(ctx context.Context, runCtx RunContext, checks []CheckResult) (*RepairPlan, error) {
	return buildRepairPlan(ctx, runCtx, checks, NewOntologyOperationAdapter())
}

func buildRepairPlan(
	ctx context.Context,
	runCtx RunContext,
	checks []CheckResult,
	previewer OntologyOperationPreviewer,
) (*RepairPlan, error) {
	checks, err := prepareIdentifierRepairChecks(ctx, runCtx, checks)
	if err != nil {
		return nil, err
	}
	checks, err = attachStableRepairIssueKeys(checks)
	if err != nil {
		return nil, err
	}
	for checkIndex := range checks {
		foldIdentifierAliasMirrorActions(&checks[checkIndex])
	}
	identifierPlans := make([]*RepairPlan, 0, 1)
	identifierActionIDs := make(map[string]struct{})
	for checkIndex := range checks {
		payload := checks[checkIndex].identifierRepair
		if payload == nil || payload.Assembly == nil {
			continue
		}
		identifierPlan, buildErr := BuildIdentifierRepairPlan(ctx, runCtx, payload.Assembly, payload.Bindings)
		if buildErr != nil {
			return nil, buildErr
		}
		identifierPlans = append(identifierPlans, identifierPlan)
		for _, action := range identifierPlan.Actions {
			identifierActionIDs[action.ID] = struct{}{}
		}
	}
	if len(identifierPlans) > 0 {
		for checkIndex := range checks {
			filtered := make([]FixAction, 0, len(checks[checkIndex].Fixes))
			for _, action := range checks[checkIndex].Fixes {
				if _, owned := identifierActionIDs[action.ID]; !owned {
					filtered = append(filtered, action)
				}
			}
			checks[checkIndex].Fixes = filtered
		}
	}
	actionPlan, err := BuildFixPlan(checks)
	if err != nil {
		return actionPlan, err
	}
	var genericPlan *RepairPlan
	if actionPlan != nil {
		genericPlan, err = buildRepairPlanFromActions(ctx, runCtx, actionPlan.Actions, previewer)
		if err != nil {
			return nil, err
		}
	}
	return mergeRepairPlans(genericPlan, identifierPlans)
}

func foldIdentifierAliasMirrorActions(check *CheckResult) {
	if check == nil || check.identifierRepair == nil {
		return
	}
	payload := *check.identifierRepair
	payload.Bindings = append([]IdentifierRepairActionBinding(nil), payload.Bindings...)
	extraIssueKeysByCollisionAction := make(map[string][]string)
	redundantActions := make(map[string]struct{})
	for _, action := range check.Fixes {
		memberships := identifierAliasAppendMemberships(payload, action)
		if len(memberships) == 0 {
			continue
		}
		redundantActions[action.ID] = struct{}{}
		for bindingIndex := range payload.Bindings {
			binding := &payload.Bindings[bindingIndex]
			if !containsString(memberships, binding.MembershipKey) {
				continue
			}
			binding.Action.IssueKeys = sortedUnique(append(binding.Action.IssueKeys, action.IssueKeys...))
			extraIssueKeysByCollisionAction[binding.Action.ID] = append(extraIssueKeysByCollisionAction[binding.Action.ID], action.IssueKeys...)
		}
	}
	if len(redundantActions) == 0 {
		return
	}
	filtered := make([]FixAction, 0, len(check.Fixes)-len(redundantActions))
	for _, action := range check.Fixes {
		if _, redundant := redundantActions[action.ID]; redundant {
			continue
		}
		if extra := extraIssueKeysByCollisionAction[action.ID]; len(extra) > 0 {
			action.IssueKeys = sortedUnique(append(action.IssueKeys, extra...))
		}
		filtered = append(filtered, action)
	}
	check.Fixes = filtered
	check.identifierRepair = &payload
}

func identifierAliasAppendMemberships(payload identifierRepairPayload, action FixAction) []string {
	if action.Kind != FixKindAppendAlias || action.IssueCode != "identifier_not_in_aliases" {
		return nil
	}
	var memberships []string
	for _, edit := range action.Edits {
		if edit.Kind != FixKindAppendAlias || strings.TrimSpace(edit.NotePath) == "" || strings.TrimSpace(edit.Value) == "" {
			continue
		}
		if payload.preparation != nil {
			memberships = append(memberships, payload.preparation.aliasAppendMemberships(edit)...)
		}
		assembly := payload.Assembly
		if assembly == nil {
			continue
		}
		for _, component := range assembly.Components {
			for _, intent := range component.Rewrites {
				if intent.Rewrite.Mode != identifierrewrite.IdentifierRewritePreferredRekey ||
					intent.Rewrite.OldRef.NotePath != edit.NotePath ||
					identifierreconcile.IdentifierComparisonKey(intent.Rewrite.OldIdentifier) != identifierreconcile.IdentifierComparisonKey(edit.Value) {
					continue
				}
				memberships = append(memberships, component.MembershipKeys...)
			}
		}
	}
	return sortedUnique(memberships)
}

func mergeRepairPlans(generic *RepairPlan, identifierPlans []*RepairPlan) (*RepairPlan, error) {
	if generic == nil && len(identifierPlans) == 0 {
		return nil, nil
	}
	combined := RepairPlan{}
	if generic != nil {
		combined.Actions = append(combined.Actions, generic.Actions...)
		combined.Operations = append(combined.Operations, generic.Operations...)
		combined.FollowUps = append(combined.FollowUps, generic.FollowUps...)
		combined.AuthorityFingerprint = generic.AuthorityFingerprint
		combined.RequiresLeaseHeldReplan = generic.RequiresLeaseHeldReplan
	}
	for _, plan := range identifierPlans {
		if plan == nil {
			continue
		}
		if combined.AuthorityFingerprint != "" && plan.AuthorityFingerprint != "" && combined.AuthorityFingerprint != plan.AuthorityFingerprint {
			return nil, fmt.Errorf("cannot merge repair plans with different authority fingerprints")
		}
		if combined.AuthorityFingerprint == "" {
			combined.AuthorityFingerprint = plan.AuthorityFingerprint
		}
		combined.Actions = append(combined.Actions, plan.Actions...)
		combined.Operations = append(combined.Operations, plan.Operations...)
		combined.FollowUps = append(combined.FollowUps, plan.FollowUps...)
		combined.RequiresLeaseHeldReplan = combined.RequiresLeaseHeldReplan || plan.RequiresLeaseHeldReplan
	}
	combined.TotalCount = len(combined.Actions)
	for _, action := range combined.Actions {
		combined.IssueKeys = append(combined.IssueKeys, action.IssueKeys...)
		switch action.Safety {
		case FixSafetySafe:
			combined.SafeCount++
		case FixSafetyConfirm:
			combined.ConfirmationCount++
		case FixSafetyAgent:
			combined.AgentCount++
		}
	}
	finalized, err := FinalizeRepairPlan(combined)
	if err != nil {
		return nil, err
	}
	return &finalized, nil
}

func buildRepairPlanFromActions(
	ctx context.Context,
	runCtx RunContext,
	actions []FixAction,
	previewer OntologyOperationPreviewer,
) (*RepairPlan, error) {
	actions = append([]FixAction(nil), actions...)
	for i := range actions {
		if len(actions[i].IssueKeys) == 0 {
			actions[i].IssueKeys = []string{fallbackActionIssueKey(actions[i])}
		}
	}
	var operations []RepairOperation
	var schema *ontoschema.Schema
	var err error
	var plainEdits []plainRepairEdit
	var structuredEdits []structuredRepairEdit
	for i := range actions {
		action := &actions[i]
		special, remaining, handled, err := planExistingIdentifierBlockIDMigration(runCtx, *action)
		if err != nil {
			return nil, err
		}
		if handled && special.ID != "" {
			operations = append(operations, special)
		}
		plain, ontologyEdits := splitRepairEdits(remaining)
		for _, edit := range plain {
			plainEdits = append(plainEdits, plainRepairEdit{action: *action, edit: edit})
		}
		for _, edit := range ontologyEdits {
			structuredEdits = append(structuredEdits, structuredRepairEdit{action: *action, edit: edit})
		}
	}
	plainOperations, err := planPlainRepairGroups(runCtx, plainEdits)
	if err != nil {
		return nil, err
	}
	operations = append(operations, plainOperations...)
	if len(structuredEdits) > 0 {
		schema, err = ontoschema.LoadSchema(runCtx.VaultPath)
		if err != nil {
			return nil, err
		}
		structured, supplemental, err := planOntologyRepairGroups(
			ctx, runCtx, schema, previewer, structuredEdits,
		)
		if err != nil {
			return nil, err
		}
		operations = append(operations, structured...)
		operations = append(operations, supplemental...)
	}
	plan := RepairPlan{Actions: actions, Operations: operations}
	for _, action := range actions {
		plan.IssueKeys = append(plan.IssueKeys, action.IssueKeys...)
		plan.TotalCount++
		switch action.Safety {
		case FixSafetySafe:
			plan.SafeCount++
		case FixSafetyConfirm:
			plan.ConfirmationCount++
		case FixSafetyAgent:
			plan.AgentCount++
		}
	}
	finalized, err := FinalizeRepairPlan(plan)
	if err != nil {
		return nil, err
	}
	return &finalized, nil
}

func planExistingIdentifierBlockIDMigration(
	runCtx RunContext,
	action FixAction,
) (RepairOperation, []FixEdit, bool, error) {
	var ensure *FixEdit
	var removals []FixEdit
	var remaining []FixEdit
	for i := range action.Edits {
		edit := action.Edits[i]
		switch edit.Kind {
		case FixKindEnsureBlockID:
			if ensure == nil {
				copy := edit
				ensure = &copy
				continue
			}
		case FixKindRemoveBlockID:
			removals = append(removals, edit)
			continue
		}
		remaining = append(remaining, edit)
	}
	if ensure == nil || len(removals) == 0 {
		return RepairOperation{}, action.Edits, false, nil
	}
	before, abs, err := readNoteForFix(runCtx, ensure.NotePath)
	if err != nil {
		return RepairOperation{}, nil, false, err
	}
	_ = abs
	updated, changed, err := rewriteIdentifierFieldByBlockID(runCtx, ensure.NotePath, ensure.BlockID)
	if err != nil {
		return RepairOperation{}, nil, false, err
	}
	after := before
	if changed {
		after = updated.content
	}
	for _, removal := range removals {
		var removed bool
		after, removed = removeExactBlockIDLine(after, removal.BlockID)
		_ = removed
	}
	if after == before {
		return RepairOperation{}, remaining, true, nil
	}
	return wholeFileRepairOperation(
		repairOperationID(action.ID, ensure.NotePath, "identifier-block-id-migration"),
		action, ensure.NotePath, []byte(before), []byte(after),
	), remaining, true, nil
}

func splitRepairEdits(edits []FixEdit) ([]FixEdit, []FixEdit) {
	var plain, structured []FixEdit
	for _, edit := range edits {
		switch edit.Kind {
		case FixKindEnsureBlockID, FixKindUpgradeToBlockID, FixKindRemoveBlockID,
			FixKindOntologySetScalar, FixKindOntologySetLink, FixKindOntologyAddSection:
			structured = append(structured, edit)
		default:
			plain = append(plain, edit)
		}
	}
	return plain, structured
}
