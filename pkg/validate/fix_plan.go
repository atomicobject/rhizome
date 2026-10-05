package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// RepairPlan is the deterministic, immutable input to repair application.
// FixPlan remains an alias while existing check builders migrate to operations.
type RepairPlan struct {
	Fingerprint             string              `json:"fingerprint,omitempty"`
	AuthorityFingerprint    string              `json:"authorityFingerprint,omitempty"`
	RequiresLeaseHeldReplan bool                `json:"requiresLeaseHeldReplan,omitempty"`
	TotalCount              int                 `json:"totalCount"`
	SafeCount               int                 `json:"safeCount"`
	ConfirmationCount       int                 `json:"confirmationCount"`
	AgentCount              int                 `json:"agentCount"`
	IssueKeys               []string            `json:"issueKeys,omitempty"`
	Actions                 []FixAction         `json:"actions,omitempty"`
	Operations              []RepairOperation   `json:"operations,omitempty"`
	Transactions            []RepairTransaction `json:"transactions,omitempty"`
	FollowUps               []RepairFollowUp    `json:"followUps,omitempty"`
}

// FixPlan preserves the public validation result name during migration.
type FixPlan = RepairPlan

// BuildFixPlan aggregates fix actions from all check results into a plan.
func BuildFixPlan(checks []CheckResult) (*FixPlan, error) {
	var actions []FixAction
	for _, check := range checks {
		if len(check.Fixes) == 0 {
			continue
		}
		actions = append(actions, check.Fixes...)
	}
	if len(actions) == 0 {
		return nil, nil
	}
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].ID < actions[j].ID })
	plan := &FixPlan{Actions: actions}
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
	finalized, err := FinalizeRepairPlan(*plan)
	if err != nil {
		return nil, err
	}
	return &finalized, nil
}

// FinalizeRepairPlan is the sole constructor for executable repair plans. It
// validates stable identities and source preconditions, derives transaction
// membership, and computes the authoritative fingerprint.
func FinalizeRepairPlan(plan RepairPlan) (RepairPlan, error) {
	plan.Fingerprint = ""
	plan.AuthorityFingerprint = strings.TrimSpace(plan.AuthorityFingerprint)
	plan.Transactions = nil
	plan.IssueKeys = append([]string(nil), plan.IssueKeys...)
	plan.Actions = append([]FixAction(nil), plan.Actions...)
	plan.Operations = append([]RepairOperation(nil), plan.Operations...)
	followUps, err := canonicalRepairFollowUps(plan.FollowUps)
	if err != nil {
		return RepairPlan{}, err
	}
	plan.FollowUps = followUps

	actionIndex := make(map[string]int, len(plan.Actions))
	for i := range plan.Actions {
		action := &plan.Actions[i]
		if action.Check != "" {
			canonicalCheck, ok := CanonicalCheck(action.Check)
			if !ok {
				return RepairPlan{}, fmt.Errorf("repair action %q has unknown originating check %q", action.ID, action.Check)
			}
			action.Check = canonicalCheck
		}
		if action.ID == "" {
			key, err := StableActionKey(*action)
			if err != nil {
				return RepairPlan{}, err
			}
			action.ID = key
		}
		if _, exists := actionIndex[action.ID]; exists {
			return RepairPlan{}, fmt.Errorf("duplicate repair action id %q", action.ID)
		}
		actionIndex[action.ID] = i
		action.IssueKeys = sortedUnique(action.IssueKeys)
		action.OperationIDs = nil
		plan.IssueKeys = append(plan.IssueKeys, action.IssueKeys...)
	}
	plan.IssueKeys = sortedUnique(plan.IssueKeys)

	seenOperations := make(map[string]struct{}, len(plan.Operations))
	for i := range plan.Operations {
		canonical, err := canonicalRepairOperation(plan.Operations[i])
		if err != nil {
			return RepairPlan{}, err
		}
		if canonical.ID == "" {
			return RepairPlan{}, fmt.Errorf("repair operation id is required")
		}
		if canonical.DestinationState != nil {
			return RepairPlan{}, fmt.Errorf("repair operation %q requires the native namespace entrypoint", canonical.ID)
		}
		if _, exists := seenOperations[canonical.ID]; exists {
			return RepairPlan{}, fmt.Errorf("duplicate repair operation id %q", canonical.ID)
		}
		seenOperations[canonical.ID] = struct{}{}
		if canonical.SourceHash == "" {
			return RepairPlan{}, fmt.Errorf("repair operation %q source hash is required", canonical.ID)
		}
		if len(canonical.ActionIDs) == 0 {
			return RepairPlan{}, fmt.Errorf("repair operation %q requires action membership", canonical.ID)
		}
		if len(canonical.IssueKeys) == 0 {
			return RepairPlan{}, fmt.Errorf("repair operation %q requires issue membership", canonical.ID)
		}
		authorizedIssueKeys := make(map[string]struct{})
		var requiredIssueKeys []string
		for _, actionID := range canonical.ActionIDs {
			actionPosition, exists := actionIndex[actionID]
			if !exists {
				return RepairPlan{}, fmt.Errorf("repair operation %q references unknown action %q", canonical.ID, actionID)
			}
			if plan.Actions[actionPosition].Check == "" {
				return RepairPlan{}, fmt.Errorf("repair action %q requires an originating check", actionID)
			}
			for _, issueKey := range plan.Actions[actionPosition].IssueKeys {
				authorizedIssueKeys[issueKey] = struct{}{}
				requiredIssueKeys = append(requiredIssueKeys, issueKey)
			}
			plan.Actions[actionPosition].OperationIDs = append(plan.Actions[actionPosition].OperationIDs, canonical.ID)
			plan.Actions[actionPosition].AffectedPaths = append(
				plan.Actions[actionPosition].AffectedPaths, operationPaths(canonical)...,
			)
		}
		for _, issueKey := range canonical.IssueKeys {
			if _, authorized := authorizedIssueKeys[issueKey]; !authorized {
				return RepairPlan{}, fmt.Errorf("repair operation %q issue %q is not owned by an associated action", canonical.ID, issueKey)
			}
		}
		requiredIssueKeys = sortedUnique(requiredIssueKeys)
		if !slices.Equal(canonical.IssueKeys, requiredIssueKeys) {
			return RepairPlan{}, fmt.Errorf("repair operation %q issue membership must equal its associated actions", canonical.ID)
		}
		plan.Operations[i] = canonical
	}
	if len(plan.Operations) > 0 && len(plan.IssueKeys) == 0 {
		return RepairPlan{}, fmt.Errorf("executable repair plan requires stable issue keys")
	}

	sort.Slice(plan.Actions, func(i, j int) bool { return plan.Actions[i].ID < plan.Actions[j].ID })
	for i := range plan.Actions {
		plan.Actions[i].OperationIDs = sortedUnique(plan.Actions[i].OperationIDs)
		plan.Actions[i].AffectedPaths = sortedUnique(plan.Actions[i].AffectedPaths)
	}
	sort.Slice(plan.Operations, func(i, j int) bool { return plan.Operations[i].ID < plan.Operations[j].ID })
	transactions, err := GroupRepairTransactions(plan.Operations)
	if err != nil {
		return RepairPlan{}, err
	}
	plan.Transactions = transactions
	actionsByID := make(map[string]FixAction, len(plan.Actions))
	for _, action := range plan.Actions {
		actionsByID[action.ID] = action
	}
	operationsByID := make(map[string]RepairOperation, len(plan.Operations))
	for _, operation := range plan.Operations {
		operationsByID[operation.ID] = operation
	}
	for i := range plan.Transactions {
		var checks []string
		for _, operationID := range plan.Transactions[i].OperationIDs {
			checks = append(checks, operationsByID[operationID].RequiredChecks...)
			for _, actionID := range operationsByID[operationID].ActionIDs {
				if check := actionsByID[actionID].Check; check != "" {
					checks = append(checks, check)
				}
			}
		}
		plan.Transactions[i].Checks = sortedUnique(checks)
	}
	plan.Fingerprint, err = RepairPlanFingerprint(plan)
	if err != nil {
		return RepairPlan{}, err
	}
	return plan, nil
}

// RepairPlanFingerprint hashes canonical plan inputs. Runtime execution state
// and already-derived transactions are deliberately excluded.
func RepairPlanFingerprint(plan RepairPlan) (string, error) {
	issueKeys := sortedUnique(plan.IssueKeys)
	followUps, err := canonicalRepairFollowUps(plan.FollowUps)
	if err != nil {
		return "", err
	}
	type fingerprintAction struct {
		ID         string `json:"id"`
		PayloadKey string `json:"payloadKey"`
	}
	actions := make([]fingerprintAction, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		payloadKey, err := StableActionKey(action)
		if err != nil {
			return "", err
		}
		actions = append(actions, fingerprintAction{ID: action.ID, PayloadKey: payloadKey})
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].ID != actions[j].ID {
			return actions[i].ID < actions[j].ID
		}
		return actions[i].PayloadKey < actions[j].PayloadKey
	})
	operations := append([]RepairOperation(nil), plan.Operations...)
	for i := range operations {
		canonical, err := canonicalRepairOperation(operations[i])
		if err != nil {
			return "", err
		}
		operations[i] = canonical
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].ID != operations[j].ID {
			return operations[i].ID < operations[j].ID
		}
		return operations[i].Kind < operations[j].Kind
	})
	payload := struct {
		RequiresLeaseHeldReplan bool                `json:"requiresLeaseHeldReplan,omitempty"`
		AuthorityFingerprint    string              `json:"authorityFingerprint,omitempty"`
		IssueKeys               []string            `json:"issueKeys"`
		Actions                 []fingerprintAction `json:"actions"`
		Operations              []RepairOperation   `json:"operations"`
		FollowUps               []RepairFollowUp    `json:"followUps,omitempty"`
	}{
		RequiresLeaseHeldReplan: plan.RequiresLeaseHeldReplan,
		AuthorityFingerprint:    strings.TrimSpace(plan.AuthorityFingerprint),
		IssueKeys:               issueKeys,
		Actions:                 actions,
		Operations:              operations,
		FollowUps:               followUps,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "plan:v2:" + hex.EncodeToString(sum[:]), nil
}
