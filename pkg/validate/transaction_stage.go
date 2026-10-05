package validate

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
)

type repairFileState struct {
	rel             string
	abs             string
	originalRel     string
	internal        bool
	omit            bool
	originalExists  bool
	originalContent []byte
	originalMode    os.FileMode
	finalExists     bool
	finalContent    []byte
	finalMode       os.FileMode
}

type preparedRepairTransaction struct {
	decisionSynced bool
	dir            string
	manifest       repairJournalManifest
	states         []repairFileState
	installed      int
}

type repairExecutionHooks struct {
	AfterJournalPublished                func(path string) error
	BeforeInstall                        func(index int, path string) error
	AfterMutation                        func(index int, path string) error
	AfterInstall                         func(index int, path string) error
	AfterCleanupBoundary                 func(boundary, path string) error
	BeforeRollbackMutation               func(index int, path string) error
	AfterNamespaceIndexPublication       func() error
	BeforeNamespaceDecisionDirectorySync func(marker string) error
}

var errSimulatedRepairInterruption = errors.New("simulated repair process interruption")

func prepareRepairTransaction(
	runCtx RunContext,
	planFingerprint string,
	transaction RepairTransaction,
	operations []RepairOperation,
	allowHistorical bool,
	hooks *repairExecutionHooks,
	completionArtifacts ...*RepairCompletionArtifact,
) (*preparedRepairTransaction, error) {
	if err := verifyRepairDestinationVacancies(runCtx, operations); err != nil {
		return nil, err
	}
	states, err := composeRepairFileStates(runCtx, operations)
	if err != nil {
		return nil, err
	}
	var completion *RepairCompletionArtifact
	if len(completionArtifacts) > 0 {
		completion = completionArtifacts[0]
	}
	if completion != nil {
		completionState, completionErr := prepareRepairCompletionState(runCtx, completion)
		if completionErr != nil {
			return nil, completionErr
		}
		if completionState != nil {
			states = append(states, *completionState)
			sortRepairFileStates(states)
		}
	}
	if !allowHistorical {
		if path, reason, protected := classifyStagedLifecycle(states, operations); protected {
			return nil, lifecycleProtectedError{path: path, reason: reason}
		}
	}
	return stageRepairFileStates(runCtx, planFingerprint, transaction, operations, states, nil, hooks)
}

func stageRepairFileStates(
	runCtx RunContext,
	planFingerprint string,
	transaction RepairTransaction,
	operations []RepairOperation,
	states []repairFileState,
	purpose *namespaceJournalPurpose,
	hooks *repairExecutionHooks,
) (*preparedRepairTransaction, error) {
	dir, err := repairJournalDir(runCtx, transaction.ID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("repair journal already exists for transaction %s", transaction.ID)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	shortID := strings.TrimPrefix(SourceHash([]byte(transaction.ID)), "sha256:")[:12]
	manifest := repairJournalManifest{
		Version: repairJournalVersion, Namespace: purpose,
		TransactionID: transaction.ID, PlanFingerprint: planFingerprint,
		OperationIDs: append([]string(nil), transaction.OperationIDs...),
		ActionIDs:    actionIDsForOperations(operations),
		IssueKeys:    issueKeysForOperations(operations),
		Checks:       append([]string(nil), transaction.Checks...),
		Entries:      make([]repairJournalEntry, 0, len(states)),
	}
	if purpose != nil {
		manifest.Version = namespaceadmission.NamespaceVersion
	}
	manifest.Changed, manifest.Renamed, manifest.Deleted = repairOperationDelta(operations)
	for i, state := range states {
		entry := repairJournalEntry{
			Path: state.rel, OriginalPath: state.originalRel, OriginalExists: state.originalExists,
			Internal:     state.internal,
			OriginalMode: repairModeBits(state.originalMode), FinalExists: state.finalExists,
			FinalMode: repairModeBits(state.finalMode),
		}
		if state.originalExists {
			entry.OriginalHash = SourceHash(state.originalContent)
			entry.BackupPath = repairArtifactPath(state.abs, shortID, i, "backup")
		}
		if state.finalExists {
			entry.FinalHash = SourceHash(state.finalContent)
			entry.StagePath = repairArtifactPath(state.abs, shortID, i, "stage")
		}
		if state.originalRel != "" && state.originalRel != state.rel {
			entry.CasePath = repairArtifactPath(state.abs, shortID, i, "case-original")
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	if err := writeRepairJournalManifest(dir, manifest); err != nil {
		return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
	}
	if hooks != nil && hooks.AfterJournalPublished != nil {
		if err := hooks.AfterJournalPublished(dir); err != nil {
			if errors.Is(err, errSimulatedRepairInterruption) {
				return nil, err
			}
			return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
		}
	}
	for i, state := range states {
		entry := manifest.Entries[i]
		if entry.BackupPath != "" {
			if err := createOwnedRepairArtifact(dir, &manifest, entry.BackupPath, state.originalContent, state.originalMode); err != nil {
				return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
			}
		}
		if entry.StagePath != "" {
			if err := createOwnedRepairArtifact(dir, &manifest, entry.StagePath, state.finalContent, state.finalMode); err != nil {
				return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
			}
		}
	}
	return &preparedRepairTransaction{dir: dir, manifest: manifest, states: states}, nil
}

func verifyRepairDestinationVacancies(runCtx RunContext, operations []RepairOperation) error {
	for _, operation := range operations {
		vacancy := operation.DestinationVacancy
		if vacancy == nil {
			continue
		}
		destinationDir := path.Dir(vacancy.DestinationPath)
		absDestination, err := repairAbsPath(runCtx, vacancy.DestinationPath)
		if err != nil {
			return repairDestinationVacancyError{destination: vacancy.DestinationPath, reason: err.Error()}
		}
		entries, err := os.ReadDir(filepath.Dir(absDestination))
		if err != nil {
			return repairDestinationVacancyError{destination: vacancy.DestinationPath, reason: err.Error()}
		}
		destinationBase := path.Base(vacancy.DestinationPath)
		for _, entry := range entries {
			candidatePath := entry.Name()
			if destinationDir != "." {
				candidatePath = path.Join(destinationDir, entry.Name())
			}
			if candidatePath == operation.Path {
				continue
			}
			switch {
			case vacancy.RequireExactVacancy && candidatePath == vacancy.DestinationPath:
				return repairDestinationVacancyError{destination: vacancy.DestinationPath, reason: "destination already exists"}
			case vacancy.RequirePortableCaseFoldVacancy && strings.EqualFold(entry.Name(), destinationBase):
				return repairDestinationVacancyError{destination: vacancy.DestinationPath, reason: fmt.Sprintf("portable sibling %s already exists", candidatePath)}
			}
		}
	}
	return nil
}

type repairDestinationVacancyError struct {
	destination string
	reason      string
}

func (e repairDestinationVacancyError) Error() string {
	return fmt.Sprintf("destination vacancy failed at %s: %s; replan validation fixes", e.destination, e.reason)
}

func cleanupFailedPrepare(
	runCtx RunContext,
	dir string,
	manifest repairJournalManifest,
	hooks *repairExecutionHooks,
) error {
	if manifest.Namespace != nil {
		if err := verifyNamespaceOriginalFiles(runCtx, manifest); err != nil {
			return err
		}
		decision, err := nativeJournalDecision(dir)
		if err != nil {
			return err
		}
		if decision == repairJournalPrepared {
			if err := markNamespaceRestored(dir); err != nil {
				return err
			}
		} else if decision != repairJournalRestored {
			return fmt.Errorf("native preparation cleanup has a published decision")
		}
		journal := recoveredRepairJournal{dir: dir, manifest: manifest, state: repairJournalRestored}
		if err := settleNamespaceGitRelease(runCtx, &journal); err != nil {
			return err
		}
		manifest = journal.manifest
	}
	return cleanupRepairJournal(runCtx, dir, manifest, hooks)
}

type lifecycleProtectedError struct {
	path   string
	reason string
}

func (e lifecycleProtectedError) Error() string {
	return "historical edit is protected: " + e.path + ": " + e.reason
}

func classifyStagedLifecycle(states []repairFileState, operations []RepairOperation) (string, string, bool) {
	stateByPath := make(map[string]repairFileState, len(states))
	for _, state := range states {
		stateByPath[state.rel] = state
		if state.originalRel != "" {
			stateByPath[state.originalRel] = state
		}
	}
	writesByPath := make(map[string][]RepairOperation)
	renameByPath := make(map[string]string)
	for _, operation := range operations {
		switch operation.Kind {
		case RepairOperationWrite:
			writesByPath[operation.Path] = append(writesByPath[operation.Path], operation)
		case RepairOperationRename:
			renameByPath[operation.Path] = operation.DestinationPath
			source := stateByPath[operation.Path]
			if isClosedEffortSource(source.originalContent) {
				return operation.Path, "closed effort paths cannot be renamed", true
			}
		case RepairOperationDelete:
			source := stateByPath[operation.Path]
			if isClosedEffortSource(source.originalContent) {
				return operation.Path, "closed effort paths cannot be deleted", true
			}
		}
	}
	for path, writes := range writesByPath {
		source := stateByPath[path]
		target := source
		if destination := renameByPath[path]; destination != "" {
			target = stateByPath[destination]
		}
		var claims []LifecycleClaim
		for _, operation := range writes {
			claims = append(claims, operation.LifecycleClaims...)
		}
		result := ClassifyLifecycleEdit(LifecycleEdit{
			Before: source.originalContent,
			After:  target.finalContent,
			Claims: claims,
		})
		if result.Decision == LifecycleProtected {
			return path, result.Reason, true
		}
	}
	return "", "", false
}

func isClosedEffortSource(content []byte) bool {
	result := ClassifyLifecycleEdit(LifecycleEdit{Before: content, After: content})
	return result.Decision != LifecycleNotHistorical
}

func composeRepairFileStates(runCtx RunContext, operations []RepairOperation) ([]repairFileState, error) {
	byPath := make(map[string]*repairFileState)
	stateFor := func(rel string, allowMissing bool) (*repairFileState, error) {
		if state, ok := byPath[rel]; ok {
			return state, nil
		}
		abs, err := repairAbsPath(runCtx, rel)
		if err != nil {
			return nil, err
		}
		state := &repairFileState{rel: rel, abs: abs, originalRel: rel}
		info, err := os.Lstat(abs)
		if os.IsNotExist(err) && allowMissing {
			byPath[rel] = state
			return state, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read repair source %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("repair path %s is not a regular file", rel)
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		state.originalExists = true
		state.originalContent = content
		state.originalMode = info.Mode()
		state.finalExists = true
		state.finalContent = append([]byte(nil), content...)
		state.finalMode = info.Mode()
		byPath[rel] = state
		return state, nil
	}

	writes := make(map[string][]RepairOperation)
	renameDestinations := make(map[string]string)
	for _, operation := range operations {
		source, err := stateFor(operation.Path, false)
		if err != nil {
			return nil, staleRepairError(operation, err.Error())
		}
		if SourceHash(source.originalContent) != operation.SourceHash {
			return nil, staleRepairError(operation, "source hash changed")
		}
		if operation.SourceMode != nil && !repairModeMatches(source.originalMode, *operation.SourceMode) {
			return nil, staleRepairError(operation, "source mode changed")
		}
		if err := verifyExpectedText(operation, source.originalContent); err != nil {
			return nil, err
		}
		switch operation.Kind {
		case RepairOperationWrite:
			writes[operation.Path] = append(writes[operation.Path], operation)
		case RepairOperationRename:
			destination, err := stateFor(operation.DestinationPath, true)
			if err != nil {
				return nil, err
			}
			if operation.DestinationState != nil {
				if err := verifyNativeDestinationState(operation, source, destination); err != nil {
					return nil, err
				}
			} else if destination.originalExists && repairCollisionKey(operation.Path) != repairCollisionKey(operation.DestinationPath) {
				return nil, fmt.Errorf("destination collision at %s", operation.DestinationPath)
			}
			caseOnly := operation.DestinationState != nil && operation.DestinationState.Kind == RepairDestinationCaseOnly
			if caseOnly || (operation.DestinationState == nil && destination.originalExists && repairCollisionKey(operation.Path) == repairCollisionKey(operation.DestinationPath)) {
				sourceInfo, sourceErr := os.Lstat(source.abs)
				destinationInfo, destinationErr := os.Lstat(destination.abs)
				if sourceErr == nil && destinationErr == nil && os.SameFile(sourceInfo, destinationInfo) {
					destination.originalRel = source.rel
					destination.originalContent = append([]byte(nil), source.originalContent...)
					destination.originalMode = source.originalMode
					source.omit = true
				} else {
					return nil, fmt.Errorf("destination collision at %s", operation.DestinationPath)
				}
			}
			source.finalExists = false
			renameDestinations[operation.Path] = operation.DestinationPath
			destination.finalExists = true
			destination.finalMode = source.originalMode
			destination.finalContent = append([]byte(nil), source.originalContent...)
			if operation.Content != nil {
				destination.finalContent = append([]byte(nil), operation.Content...)
			}
		case RepairOperationDelete:
			source.finalExists = false
		default:
			return nil, fmt.Errorf("unsupported repair operation %q", operation.Kind)
		}
	}
	for rel, operations := range writes {
		source := byPath[rel]
		content, err := composeRepairWrites(source.originalContent, operations)
		if err != nil {
			return nil, err
		}
		target := source
		if destinationPath := renameDestinations[rel]; destinationPath != "" {
			target = byPath[destinationPath]
			source.finalExists = false
		}
		target.finalExists = true
		target.finalContent = content
		target.finalMode = source.originalMode
	}
	states := make([]repairFileState, 0, len(byPath))
	for _, state := range byPath {
		if state.omit {
			continue
		}
		states = append(states, *state)
	}
	sortRepairFileStates(states)
	return states, nil
}

func sortRepairFileStates(states []repairFileState) {
	sort.Slice(states, func(i, j int) bool {
		if states[i].finalExists != states[j].finalExists {
			return !states[i].finalExists
		}
		return states[i].rel < states[j].rel
	})
}

func composeRepairWrites(original []byte, operations []RepairOperation) ([]byte, error) {
	if len(operations) == 1 && operations[0].Content != nil {
		return append([]byte(nil), operations[0].Content...), nil
	}
	type replacement struct {
		operationID string
		ExpectedText
	}
	var replacements []replacement
	for _, operation := range operations {
		for _, expected := range operation.Expected {
			replacements = append(replacements, replacement{operationID: operation.ID, ExpectedText: expected})
		}
	}
	sort.Slice(replacements, func(i, j int) bool {
		if replacements[i].StartByte != replacements[j].StartByte {
			return replacements[i].StartByte > replacements[j].StartByte
		}
		if replacements[i].EndByte != replacements[j].EndByte {
			return replacements[i].EndByte > replacements[j].EndByte
		}
		return replacements[i].operationID > replacements[j].operationID
	})
	updated := append([]byte(nil), original...)
	for _, replacement := range replacements {
		if replacement.EndByte > len(updated) {
			return nil, fmt.Errorf("stale repair operation %s: expected span is out of range", replacement.operationID)
		}
		updated = append(updated[:replacement.StartByte], append([]byte(replacement.Replacement), updated[replacement.EndByte:]...)...)
	}
	return updated, nil
}

func verifyExpectedText(operation RepairOperation, content []byte) error {
	for _, expected := range operation.Expected {
		if expected.EndByte > len(content) || string(content[expected.StartByte:expected.EndByte]) != expected.Text {
			return staleRepairError(operation, fmt.Sprintf("expected text mismatch at %d..%d", expected.StartByte, expected.EndByte))
		}
	}
	return nil
}

type repairStaleError struct{ message string }

func (e repairStaleError) Error() string { return e.message }

func staleRepairError(operation RepairOperation, reason string) error {
	return repairStaleError{message: fmt.Sprintf(
		"stale repair operation %s on %s: %s; replan validation fixes",
		operation.ID, operation.Path, reason,
	)}
}
