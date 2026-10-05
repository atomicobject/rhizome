package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
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
	checks, err := attachStableRepairIssueKeys(checks)
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
	if check == nil || check.identifierRepair == nil || check.identifierRepair.Assembly == nil {
		return
	}
	payload := *check.identifierRepair
	payload.Bindings = append([]IdentifierRepairActionBinding(nil), payload.Bindings...)
	extraIssueKeysByCollisionAction := make(map[string][]string)
	redundantActions := make(map[string]struct{})
	for _, action := range check.Fixes {
		memberships := identifierAliasAppendMemberships(payload.Assembly, action)
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

func identifierAliasAppendMemberships(assembly *identifierreconcile.RepairAssembly, action FixAction) []string {
	if assembly == nil || action.Kind != FixKindAppendAlias || action.IssueCode != "identifier_not_in_aliases" {
		return nil
	}
	var memberships []string
	for _, edit := range action.Edits {
		if edit.Kind != FixKindAppendAlias || strings.TrimSpace(edit.NotePath) == "" || strings.TrimSpace(edit.Value) == "" {
			continue
		}
		for _, component := range assembly.Components {
			for _, intent := range component.Rewrites {
				if intent.Rewrite.Mode != reference.IdentifierRewritePreferredRekey ||
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
	var schema *ontology.Schema
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
		schema, err = ontology.LoadSchema(runCtx.VaultPath)
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

func attachStableRepairIssueKeys(checks []CheckResult) ([]CheckResult, error) {
	checks = append([]CheckResult(nil), checks...)
	for checkIndex := range checks {
		check := &checks[checkIndex]
		check.Issues = append([]Issue(nil), check.Issues...)
		evidenceIssues := append([]Issue(nil), check.fullIssues...)
		if len(evidenceIssues) == 0 {
			evidenceIssues = append(evidenceIssues, check.Issues...)
		}
		issuesByCode := map[string][]Issue{}
		var allKeys []string
		for issueIndex := range evidenceIssues {
			issue := &evidenceIssues[issueIndex]
			key, err := StableIssueKey(check.Name, *issue)
			if err != nil {
				return nil, err
			}
			issue.Key = key
			issuesByCode[issue.Code] = append(issuesByCode[issue.Code], *issue)
			allKeys = append(allKeys, key)
		}
		check.fullIssues = evidenceIssues
		for issueIndex := range check.Issues {
			if issueIndex < len(evidenceIssues) {
				check.Issues[issueIndex].Key = evidenceIssues[issueIndex].Key
			}
		}
		check.allIssueKeys = sortedUnique(allKeys)
		check.Fixes = append([]FixAction(nil), check.Fixes...)
		for actionIndex := range check.Fixes {
			action := &check.Fixes[actionIndex]
			if len(action.IssueKeys) != 0 {
				continue
			}
			action.IssueKeys = matchingRepairIssueKeys(*action, issuesByCode[action.IssueCode])
			if len(action.IssueKeys) == 0 && len(allKeys) == 1 {
				action.IssueKeys = append(action.IssueKeys, allKeys[0])
			}
			if len(action.IssueKeys) == 0 {
				action.IssueKeys = []string{fallbackActionIssueKey(*action)}
			}
		}
	}
	return checks, nil
}

func matchingRepairIssueKeys(action FixAction, issues []Issue) []string {
	var keys []string
	for _, issue := range issues {
		if actionMatchesRepairIssue(action, issue) {
			keys = append(keys, issue.Key)
		}
	}
	if len(keys) == 0 && len(issues) == 1 {
		keys = append(keys, issues[0].Key)
	}
	return sortedUnique(keys)
}

func actionMatchesRepairIssue(action FixAction, issue Issue) bool {
	actionPaths := append([]string(nil), action.AffectedPaths...)
	var targets, properties, blockIDs []string
	for _, edit := range action.Edits {
		actionPaths = append(actionPaths, edit.NotePath, edit.SourcePath)
		targets = append(targets, edit.OldTarget, edit.NewTarget, edit.Value)
		targets = append(targets, edit.Values...)
		properties = append(properties, edit.Property)
		blockIDs = append(blockIDs, edit.BlockID)
		targets = append(targets, edit.BlockID)
	}
	actionPaths = nonEmptyUnique(actionPaths)
	issuePaths := nonEmptyUnique([]string{issue.Path, issue.Source})
	if len(actionPaths) > 0 && len(issuePaths) > 0 && !stringSetsIntersect(actionPaths, issuePaths) {
		return false
	}
	targets = nonEmptyUnique(targets)
	if issue.Target != "" && len(targets) > 0 && !containsString(targets, issue.Target) {
		return false
	}
	properties = nonEmptyUnique(properties)
	fieldNamedByActionID := issue.Field != "" && strings.HasSuffix(action.ID, ":"+issue.Field)
	if issue.Field != "" && len(properties) > 0 &&
		!containsStringFold(properties, issue.Field) && !fieldNamedByActionID {
		return false
	}
	blockIDs = nonEmptyUnique(blockIDs)
	if issueBlockID := structuredIssueString(issue, "blockId"); issueBlockID != "" &&
		len(blockIDs) > 0 && !containsString(blockIDs, issueBlockID) {
		return false
	}
	return len(actionPaths) > 0 || len(targets) > 0 || len(properties) > 0 || len(blockIDs) > 0
}

func structuredIssueString(issue Issue, field string) string {
	if len(issue.Data) == 0 {
		return ""
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(issue.Data, &data); err != nil {
		return ""
	}
	var value string
	if err := json.Unmarshal(data[field], &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func nonEmptyUnique(values []string) []string {
	var result []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return sortedUnique(result)
}

func stringSetsIntersect(left, right []string) bool {
	for _, value := range left {
		if containsString(right, value) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func fallbackActionIssueKey(action FixAction) string {
	payload, _ := json.Marshal(struct {
		Check string    `json:"check"`
		Code  string    `json:"code"`
		Kind  string    `json:"kind"`
		Edits []FixEdit `json:"edits"`
	}{Check: action.Check, Code: action.IssueCode, Kind: action.Kind, Edits: action.Edits})
	return "issue:action:v1:" + strings.TrimPrefix(SourceHash(payload), "sha256:")
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

type plainRepairEdit struct {
	action FixAction
	edit   FixEdit
}

func planPlainRepairGroups(runCtx RunContext, edits []plainRepairEdit) ([]RepairOperation, error) {
	byPathAction := map[string]map[string][]plainRepairEdit{}
	for _, item := range edits {
		path := strings.TrimSpace(item.edit.NotePath)
		if path == "" {
			path = strings.TrimSpace(item.edit.SourcePath)
		}
		if path == "" {
			return nil, fmt.Errorf("repair action %s edit %s has no source path", item.action.ID, item.edit.Kind)
		}
		if byPathAction[path] == nil {
			byPathAction[path] = map[string][]plainRepairEdit{}
		}
		byPathAction[path][item.action.ID] = append(byPathAction[path][item.action.ID], item)
	}
	paths := make([]string, 0, len(byPathAction))
	for path := range byPathAction {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var operations []RepairOperation
	for _, path := range paths {
		abs, err := repairAbsPath(runCtx, path)
		if err != nil {
			return nil, err
		}
		before, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		actionIDs := make([]string, 0, len(byPathAction[path]))
		for actionID := range byPathAction[path] {
			actionIDs = append(actionIDs, actionID)
		}
		sort.Strings(actionIDs)
		for _, actionID := range actionIDs {
			items := byPathAction[path][actionID]
			after := string(before)
			for _, item := range items {
				if item.edit.Kind == FixKindRewriteLinkGroup {
					if rewritten, _, ok := applyBlockFragmentRewrite(
						runCtx, path, after, item.edit.OldTarget, item.edit.NewTarget,
					); ok {
						after = rewritten
					} else {
						after, err = ApplyFixEditInMemory(after, item.edit)
					}
				} else {
					after, err = ApplyFixEditInMemory(after, item.edit)
				}
				if err != nil {
					return nil, fmt.Errorf("plan repair action %s: %w", item.action.ID, err)
				}
			}
			if after == string(before) {
				continue
			}
			action := items[0].action
			operation := wholeFileRepairOperation(
				repairOperationID(actionID, path, "write"), action, path, before, []byte(after),
			)
			operation.Expected = []ExpectedText{minimalRepairExpectedText(before, []byte(after))}
			operation.IssueKeys = append([]string(nil), action.IssueKeys...)
			operation.LifecycleClaims = lifecycleClaimsForPlainEdits(before, items)
			operation.Lifecycle = ClassifyLifecycleEdit(LifecycleEdit{
				Before: before, After: []byte(after), Claims: operation.LifecycleClaims,
			})
			operations = append(operations, operation)
		}
	}
	return operations, nil
}

func lifecycleClaimsForPlainEdits(before []byte, items []plainRepairEdit) []LifecycleClaim {
	var claims []LifecycleClaim
	for _, item := range items {
		edit := item.edit
		switch edit.Kind {
		case FixKindRewriteLinkTarget:
			if edit.StartByte < 0 || edit.EndByte < edit.StartByte || edit.EndByte > len(before) {
				continue
			}
			claims = append(claims, LifecycleClaim{
				Kind: LifecycleEditBrokenLink, StartByte: edit.StartByte, EndByte: edit.EndByte,
				ExpectedText: string(before[edit.StartByte:edit.EndByte]), Replacement: edit.Value,
			})
		case FixKindRewriteLinkGroup:
			claims = append(claims, brokenLinkGroupLifecycleClaims(before, edit.OldTarget, edit.NewTarget)...)
		case FixKindSetFrontmatter:
			if !strings.EqualFold(strings.TrimSpace(edit.Property), "status") ||
				!strings.EqualFold(strings.TrimSpace(edit.Value), "archived") {
				continue
			}
			start, end, ok := frontmatterScalarRange(before, "status")
			if ok {
				claims = append(claims, LifecycleClaim{
					Kind: LifecycleEditArchiveStatus, StartByte: start, EndByte: end,
					ExpectedText: string(before[start:end]), Replacement: edit.Value,
				})
			}
		}
	}
	return claims
}

func brokenLinkGroupLifecycleClaims(before []byte, oldTarget, newTarget string) []LifecycleClaim {
	if oldTarget == "" || newTarget == "" {
		return nil
	}
	var claims []LifecycleClaim
	for cursor := 0; cursor < len(before); {
		offset := strings.Index(string(before[cursor:]), oldTarget)
		if offset < 0 {
			break
		}
		start := cursor + offset
		claim := LifecycleClaim{
			Kind: LifecycleEditBrokenLink, StartByte: start, EndByte: start + len(oldTarget),
			ExpectedText: oldTarget, Replacement: newTarget,
		}
		if validateBrokenLinkClaim(before, claim) == nil {
			claims = append(claims, claim)
		}
		cursor = claim.EndByte
	}
	return claims
}

func minimalRepairExpectedText(before, after []byte) ExpectedText {
	start := 0
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	beforeEnd, afterEnd := len(before), len(after)
	for beforeEnd > start && afterEnd > start && before[beforeEnd-1] == after[afterEnd-1] {
		beforeEnd--
		afterEnd--
	}
	return ExpectedText{
		StartByte: start, EndByte: beforeEnd,
		Text: string(before[start:beforeEnd]), Replacement: string(after[start:afterEnd]),
	}
}

func wholeFileRepairOperation(
	id string,
	action FixAction,
	path string,
	before, after []byte,
) RepairOperation {
	return RepairOperation{
		ID: id, ActionID: action.ID, IssueKey: action.IssueKeys[0], Kind: RepairOperationWrite,
		Path: path, SourceHash: SourceHash(before), Content: append([]byte(nil), after...),
		Expected:  []ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before), Replacement: string(after)}},
		Lifecycle: ClassifyLifecycleEdit(LifecycleEdit{Before: before, After: after}),
	}
}

func repairOperationID(actionID, path, kind string) string {
	digest := strings.TrimPrefix(SourceHash([]byte(actionID+"\x00"+path+"\x00"+kind)), "sha256:")
	return "operation:v1:" + digest
}
