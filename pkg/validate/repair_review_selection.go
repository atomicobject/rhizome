package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	initdiff "github.com/atomicobject/rhizome/pkg/app/cli/init/diff"
)

// RepairPreviewFile is a read-only projection of one selected repair path.
// It is presentation evidence only; ApplyRepairSession still rechecks the
// authoritative operations and their source preconditions before writing.
type RepairPreviewFile struct {
	Path         string              `json:"path"`
	OriginalPath string              `json:"originalPath,omitempty"`
	Kind         RepairOperationKind `json:"kind"`
	BeforeHash   string              `json:"beforeHash,omitempty"`
	AfterHash    string              `json:"afterHash,omitempty"`
	Diff         string              `json:"diff"`
}

type repairReviewSelection struct {
	result               Result
	actionIDs            []string
	transactionIDs       []string
	affectedPaths        []string
	selectionFingerprint string
	preview              []RepairPreviewFile
	confirmations        []RepairReviewConfirmation
}

func buildRepairReviewSelection(
	result Result,
	runCtx RunContext,
	generation int64,
	planFingerprint string,
	actionIDs []string,
) (repairReviewSelection, error) {
	if result.FixPlan == nil {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorAuthorityUnavailable, "validation result has no repair plan")
	}
	providedFingerprint := strings.TrimSpace(result.FixPlan.Fingerprint)
	verified, err := FinalizeRepairPlan(*result.FixPlan)
	if err != nil {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorAuthorityUnavailable, fmt.Sprintf("validate repair plan: %v", err))
	}
	if providedFingerprint == "" || providedFingerprint != verified.Fingerprint || strings.TrimSpace(planFingerprint) != verified.Fingerprint {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorPlanFingerprintMismatch, "repair plan fingerprint does not match live validation authority")
	}
	if verified.RequiresLeaseHeldReplan {
		if _, err := repairSessionIdentifierPayload(result); err != nil {
			return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorAuthorityUnavailable, err.Error())
		}
	}

	selected := sortedUnique(actionIDs)
	if len(selected) == 0 {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorActionNotFound, "select at least one repair action")
	}
	actionsByID := make(map[string]FixAction, len(verified.Actions))
	for _, action := range verified.Actions {
		actionsByID[action.ID] = action
	}
	for _, actionID := range selected {
		action, ok := actionsByID[actionID]
		if !ok {
			return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorActionNotFound, fmt.Sprintf("repair action %q is not part of the live plan", actionID))
		}
		if action.Safety == FixSafetyAgent || len(action.OperationIDs) == 0 {
			return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorActionUnavailable, fmt.Sprintf("repair action %q is not executable by the canonical repair engine", actionID))
		}
	}
	confirmations, err := requiredRepairReviewConfirmations(selected, actionsByID)
	if err != nil {
		return repairReviewSelection{}, err
	}

	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	operationsByID := make(map[string]RepairOperation, len(verified.Operations))
	for _, operation := range verified.Operations {
		operationsByID[operation.ID] = operation
	}
	var selectedTransactions []RepairTransaction
	var required []string
	for _, transaction := range verified.Transactions {
		var transactionActions []string
		for _, operationID := range transaction.OperationIDs {
			transactionActions = append(transactionActions, operationsByID[operationID].ActionIDs...)
		}
		transactionActions = sortedUnique(transactionActions)
		touched := false
		for _, id := range transactionActions {
			if _, ok := selectedSet[id]; ok {
				touched = true
				break
			}
		}
		if !touched {
			continue
		}
		selectedTransactions = append(selectedTransactions, transaction)
		for _, id := range transactionActions {
			if _, ok := selectedSet[id]; !ok {
				required = append(required, transactionActions...)
				break
			}
		}
	}
	if len(required) > 0 {
		reviewErr := newRepairReviewError(RepairReviewErrorTransactionIncomplete, "selection omits actions required by a connected repair transaction")
		reviewErr.RequiredActionIDs = sortedUnique(required)
		return repairReviewSelection{}, reviewErr
	}
	if verified.RequiresLeaseHeldReplan && len(selected) != len(verified.Actions) {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorAuthorityUnavailable, "partial identifier selection cannot preserve sealed repair authority; select the complete identifier plan")
	}

	var subsetActions []FixAction
	for _, action := range verified.Actions {
		if _, ok := selectedSet[action.ID]; ok {
			subsetActions = append(subsetActions, action)
		}
	}
	var subsetOperations []RepairOperation
	var transactionIDs, affectedPaths []string
	for _, transaction := range selectedTransactions {
		transactionIDs = append(transactionIDs, transaction.ID)
		affectedPaths = append(affectedPaths, transaction.AffectedPaths...)
		for _, operationID := range transaction.OperationIDs {
			subsetOperations = append(subsetOperations, operationsByID[operationID])
		}
	}
	subset := RepairPlan{
		AuthorityFingerprint:    verified.AuthorityFingerprint,
		RequiresLeaseHeldReplan: verified.RequiresLeaseHeldReplan,
		Actions:                 subsetActions, Operations: subsetOperations,
	}
	subset, err = FinalizeRepairPlan(subset)
	if err != nil {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorAuthorityUnavailable, fmt.Sprintf("finalize selected repair plan: %v", err))
	}
	recountRepairPlan(&subset)
	selectedResult := cloneRepairReviewResult(result)
	selectedResult.FixPlan = &subset
	selectedResult = cloneRepairReviewResult(selectedResult)
	preview, err := previewRepairOperations(runCtx, subset.Operations)
	if err != nil {
		return repairReviewSelection{}, newRepairReviewError(RepairReviewErrorRevalidationRequired, err.Error())
	}
	fingerprint, err := repairReviewSelectionFingerprint(generation, verified.Fingerprint, selected, transactionIDs)
	if err != nil {
		return repairReviewSelection{}, err
	}
	return repairReviewSelection{
		result: selectedResult, actionIDs: selected,
		transactionIDs: sortedUnique(transactionIDs), affectedPaths: sortedUnique(affectedPaths),
		selectionFingerprint: fingerprint, preview: preview, confirmations: confirmations,
	}, nil
}

func requiredRepairReviewConfirmations(selected []string, actionsByID map[string]FixAction) ([]RepairReviewConfirmation, error) {
	confirmations := make([]RepairReviewConfirmation, 0)
	for _, actionID := range selected {
		action := actionsByID[actionID]
		if action.Safety != FixSafetyConfirm {
			continue
		}
		candidates := sortedUnique(action.CandidatePaths)
		if len(candidates) > 1 {
			return nil, newRepairReviewError(RepairReviewErrorActionUnavailable, fmt.Sprintf("repair action %q has multiple candidates and no exact executable selection", actionID))
		}
		confirmation := RepairReviewConfirmation{
			ActionID: actionID, Question: strings.TrimSpace(action.Question),
			AffectedPaths: sortedUnique(action.AffectedPaths),
		}
		if len(candidates) == 1 {
			confirmation.CandidatePath = candidates[0]
		}
		confirmations = append(confirmations, confirmation)
	}
	return confirmations, nil
}

func repairReviewConfirmationsEqual(actual, required []RepairReviewConfirmation) bool {
	canonical := func(input []RepairReviewConfirmation) []RepairReviewConfirmation {
		if len(input) == 0 {
			return nil
		}
		out := cloneRepairReviewConfirmations(input)
		for i := range out {
			out[i].ActionID = strings.TrimSpace(out[i].ActionID)
			out[i].Question = strings.TrimSpace(out[i].Question)
			out[i].CandidatePath = strings.TrimSpace(out[i].CandidatePath)
			out[i].AffectedPaths = sortedUnique(out[i].AffectedPaths)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ActionID < out[j].ActionID })
		return out
	}
	left, right := canonical(actual), canonical(required)
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func recountRepairPlan(plan *RepairPlan) {
	plan.TotalCount = len(plan.Actions)
	plan.SafeCount = 0
	plan.ConfirmationCount = 0
	plan.AgentCount = 0
	for _, action := range plan.Actions {
		switch action.Safety {
		case FixSafetySafe:
			plan.SafeCount++
		case FixSafetyConfirm:
			plan.ConfirmationCount++
		case FixSafetyAgent:
			plan.AgentCount++
		}
	}
}

func previewRepairOperations(runCtx RunContext, operations []RepairOperation) ([]RepairPreviewFile, error) {
	states, err := composeRepairFileStates(runCtx, operations)
	if err != nil {
		return nil, err
	}
	statesByPath := make(map[string]repairFileState, len(states))
	renameSourceByDestination := make(map[string]string)
	renameSources := make(map[string]struct{})
	for _, state := range states {
		statesByPath[state.rel] = state
	}
	for _, operation := range operations {
		if operation.Kind == RepairOperationRename {
			renameSourceByDestination[operation.DestinationPath] = operation.Path
			renameSources[operation.Path] = struct{}{}
		}
	}
	previews := make([]RepairPreviewFile, 0, len(states))
	for _, state := range states {
		if _, renamed := renameSources[state.rel]; renamed {
			continue
		}
		before, after := state.originalContent, state.finalContent
		kind := RepairOperationWrite
		originalRel := state.originalRel
		originalExists := state.originalExists
		if sourcePath := renameSourceByDestination[state.rel]; sourcePath != "" {
			source := statesByPath[sourcePath]
			before = source.originalContent
			originalRel = sourcePath
			originalExists = source.originalExists
			kind = RepairOperationRename
		} else if originalExists && !state.finalExists {
			kind = RepairOperationDelete
			after = nil
		} else if !originalExists && state.finalExists {
			before = nil
		}
		diff, _, err := initdiff.GenerateUnifiedDiff(string(before), string(after), state.rel)
		if err != nil {
			return nil, fmt.Errorf("preview repair path %s: %w", state.rel, err)
		}
		preview := RepairPreviewFile{Path: state.rel, Kind: kind, Diff: diff}
		if originalRel != state.rel {
			preview.OriginalPath = originalRel
		}
		if originalExists {
			preview.BeforeHash = SourceHash(before)
		}
		if state.finalExists {
			preview.AfterHash = SourceHash(after)
		}
		previews = append(previews, preview)
	}
	sort.Slice(previews, func(i, j int) bool { return previews[i].Path < previews[j].Path })
	return previews, nil
}

func repairReviewSelectionFingerprint(generation int64, plan string, actions, transactions []string) (string, error) {
	payload, err := json.Marshal(struct {
		Generation   int64    `json:"generation"`
		Plan         string   `json:"plan"`
		Actions      []string `json:"actions"`
		Transactions []string `json:"transactions"`
	}{generation, plan, sortedUnique(actions), sortedUnique(transactions)})
	if err != nil {
		return "", fmt.Errorf("encode repair review selection: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "selection:v1:" + hex.EncodeToString(sum[:]), nil
}
