package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func commitPreparedRepairTransaction(runCtx RunContext, prepared *preparedRepairTransaction, hooks *repairExecutionHooks) error {
	if err := validatePreparedProgress(runCtx, prepared, 0); err != nil {
		return err
	}
	for index, entry := range prepared.manifest.Entries {
		if hooks != nil && hooks.BeforeInstall != nil {
			if err := hooks.BeforeInstall(index, entry.Path); err != nil {
				return err
			}
		}
		if err := validatePreparedProgress(runCtx, prepared, index); err != nil {
			return err
		}
		if !entry.FinalExists {
			if err := os.Remove(filepath.FromSlash(mustEntryAbs(prepared.states, entry.Path))); err != nil && !os.IsNotExist(err) {
				return err
			}
			prepared.installed = index + 1
			if hooks != nil && hooks.AfterMutation != nil {
				if err := hooks.AfterMutation(index, entry.Path); err != nil {
					return err
				}
			}
			if err := syncDirectory(filepath.Dir(mustEntryAbs(prepared.states, entry.Path))); err != nil {
				return err
			}
		} else {
			abs := mustEntryAbs(prepared.states, entry.Path)
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return err
			}
			if entry.CasePath != "" {
				if err := linkOwnedRepairArtifact(prepared.dir, &prepared.manifest, abs, entry.CasePath); err != nil {
					return fmt.Errorf("stage case-only rename %s: %w", entry.Path, err)
				}
				if err := os.Remove(abs); err != nil {
					return fmt.Errorf("remove case-only rename source %s: %w", entry.Path, err)
				}
				prepared.installed = index + 1
				if hooks != nil && hooks.AfterMutation != nil {
					if err := hooks.AfterMutation(index, entry.Path); err != nil {
						return err
					}
				}
				if err := syncDirectory(filepath.Dir(abs)); err != nil {
					return err
				}
			}
			if err := replaceFile(entry.StagePath, abs); err != nil {
				return fmt.Errorf("install repair path %s: %w", entry.Path, err)
			}
			if entry.CasePath == "" {
				prepared.installed = index + 1
				if hooks != nil && hooks.AfterMutation != nil {
					if err := hooks.AfterMutation(index, entry.Path); err != nil {
						return err
					}
				}
			}
			if entry.CasePath != "" {
				if err := validateRepairFileSnapshot(
					entry.CasePath, entry.Path+" case artifact", true, entry.OriginalHash, entry.OriginalMode,
				); err != nil {
					return err
				}
				if err := os.Remove(entry.CasePath); err != nil && !os.IsNotExist(err) {
					return err
				}
				if err := syncDirectory(filepath.Dir(abs)); err != nil {
					return err
				}
			}
		}
		prepared.installed = index + 1
		if hooks != nil && hooks.AfterInstall != nil {
			if err := hooks.AfterInstall(index, entry.Path); err != nil {
				return err
			}
		}
	}
	if prepared.manifest.Namespace != nil {
		if err := validatePreparedProgress(runCtx, prepared, len(prepared.manifest.Entries)); err != nil {
			return err
		}
		if err := publishNamespaceGitIndex(runCtx, prepared); err != nil {
			return err
		}
		if hooks != nil && hooks.AfterNamespaceIndexPublication != nil {
			if err := hooks.AfterNamespaceIndexPublication(); err != nil {
				return err
			}
		}
		if err := verifyCommittedRepairJournal(runCtx, prepared.manifest); err != nil {
			return err
		}
		if err := verifyNamespaceGitFinal(runCtx, prepared.manifest); err != nil {
			return err
		}
		if err := markNamespaceDecision(prepared.dir, "COMMITTED", []byte("committed\n"), hooks); err != nil {
			return namespaceDecisionSyncError{err: err}
		}
		return nil
	}
	return markRepairJournalCommitted(prepared.dir)
}

type repairCommitPreconditionError struct{ reason string }

func (e repairCommitPreconditionError) Error() string { return e.reason }

func validatePreparedProgress(runCtx RunContext, prepared *preparedRepairTransaction, installed int) error {
	if err := validateRepairJournalPaths(runCtx, prepared.manifest); err != nil {
		return repairCommitPreconditionError{reason: err.Error()}
	}
	for index, entry := range prepared.manifest.Entries {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return repairCommitPreconditionError{reason: err.Error()}
		}
		if filepath.Clean(abs) != filepath.Clean(mustEntryAbs(prepared.states, entry.Path)) {
			return repairCommitPreconditionError{reason: fmt.Sprintf("repair path changed after prepare: %s", entry.Path)}
		}
		expectExists, expectHash, expectMode := entry.OriginalExists, entry.OriginalHash, entry.OriginalMode
		if index < installed {
			expectExists, expectHash, expectMode = entry.FinalExists, entry.FinalHash, entry.FinalMode
		}
		if err := validateRepairFileSnapshot(abs, entry.Path, expectExists, expectHash, expectMode); err != nil {
			return repairCommitPreconditionError{reason: err.Error()}
		}
		if entry.BackupPath != "" {
			if !repairArtifactIsOwned(prepared.manifest, entry.BackupPath) {
				return repairCommitPreconditionError{reason: fmt.Sprintf("repair backup ownership is absent: %s", entry.Path)}
			}
			if err := validateRepairFileSnapshot(entry.BackupPath, entry.Path+" backup", true, entry.OriginalHash, entry.OriginalMode); err != nil {
				return repairCommitPreconditionError{reason: err.Error()}
			}
		}
		if index >= installed && entry.StagePath != "" {
			if !repairArtifactIsOwned(prepared.manifest, entry.StagePath) {
				return repairCommitPreconditionError{reason: fmt.Sprintf("repair stage ownership is absent: %s", entry.Path)}
			}
			if err := validateRepairFileSnapshot(entry.StagePath, entry.Path+" stage", true, entry.FinalHash, entry.FinalMode); err != nil {
				return repairCommitPreconditionError{reason: err.Error()}
			}
		}
	}
	return nil
}

func validateRepairFileSnapshot(path, label string, exists bool, hash string, mode uint32) error {
	info, err := os.Lstat(path)
	if !exists {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("repair path appeared after prepare: %s", label)
	}
	if err != nil {
		return fmt.Errorf("repair path changed after prepare: %s: %w", label, err)
	}
	if !info.Mode().IsRegular() || !repairModeMatches(info.Mode(), mode) {
		return fmt.Errorf("repair path mode changed after prepare: %s", label)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if SourceHash(content) != hash {
		return fmt.Errorf("repair path content changed after prepare: %s", label)
	}
	return nil
}

type repairRollbackState struct {
	entry  repairJournalEntry
	abs    string
	action string
}

type repairRollbackPlan struct {
	states []repairRollbackState
}

func buildRepairRollbackPlan(runCtx RunContext, manifest repairJournalManifest) (repairRollbackPlan, error) {
	if err := validateRepairArtifactOwnership(manifest); err != nil {
		return repairRollbackPlan{}, err
	}
	for _, rename := range manifest.Renamed {
		sourceAbs, err := repairAbsPath(runCtx, rename.From)
		if err != nil {
			return repairRollbackPlan{}, err
		}
		destinationAbs, err := repairAbsPath(runCtx, rename.To)
		if err != nil {
			return repairRollbackPlan{}, err
		}
		if err := preflightJournalRenameEndpoints(manifest, rename, sourceAbs, destinationAbs); err != nil {
			return repairRollbackPlan{}, err
		}
	}
	states := make([]repairRollbackState, 0, len(manifest.Entries))
	// Validate the complete rollback before mutating any path. This prevents a
	// later conflict from turning recovery itself into a partial transaction.
	for _, entry := range manifest.Entries {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return repairRollbackPlan{}, err
		}
		state := repairRollbackState{entry: entry, abs: abs}
		if entry.OriginalPath != "" && entry.OriginalPath != entry.Path {
			originalAbs, originalErr := repairAbsPath(runCtx, entry.OriginalPath)
			if originalErr != nil {
				return repairRollbackPlan{}, originalErr
			}
			originalInfo, originalStatErr := os.Lstat(originalAbs)
			destinationInfo, destinationErr := os.Lstat(abs)
			if originalStatErr != nil && !os.IsNotExist(originalStatErr) {
				return repairRollbackPlan{}, originalStatErr
			}
			if destinationErr != nil && !os.IsNotExist(destinationErr) {
				return repairRollbackPlan{}, destinationErr
			}
			if originalStatErr == nil {
				destinationSame := destinationErr == nil && os.SameFile(originalInfo, destinationInfo)
				if destinationErr == nil && !destinationSame {
					return repairRollbackPlan{}, fmt.Errorf(
						"rollback blocked because rename source %s reappeared as a distinct file",
						entry.OriginalPath,
					)
				}
				if originalErr := validateRepairFileSnapshot(
					originalAbs, entry.OriginalPath, true, entry.OriginalHash, entry.OriginalMode,
				); originalErr != nil {
					return repairRollbackPlan{}, fmt.Errorf(
						"rollback blocked because rename source %s changed after the crash: %w",
						entry.OriginalPath, originalErr,
					)
				}
				state.action = "none"
				if destinationSame {
					state.action = "restore_case"
				}
				states = append(states, state)
				continue
			}
		}
		info, statErr := os.Lstat(abs)
		currentExists := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return repairRollbackPlan{}, statErr
		}
		var currentHash string
		if currentExists {
			if !info.Mode().IsRegular() {
				return repairRollbackPlan{}, fmt.Errorf("rollback path %s is not a regular file", entry.Path)
			}
			content, err := os.ReadFile(abs)
			if err != nil {
				return repairRollbackPlan{}, err
			}
			currentHash = SourceHash(content)
		}
		if entry.OriginalExists && entry.BackupPath != "" {
			backup, backupErr := os.ReadFile(entry.BackupPath)
			if backupErr == nil {
				if !repairArtifactIsOwned(manifest, entry.BackupPath) {
					return repairRollbackPlan{}, fmt.Errorf("rollback backup ownership is absent for %s", entry.Path)
				}
				if SourceHash(backup) != entry.OriginalHash {
					return repairRollbackPlan{}, fmt.Errorf("rollback backup for %s does not match journal hash", entry.Path)
				}
			} else if !os.IsNotExist(backupErr) {
				return repairRollbackPlan{}, fmt.Errorf("read rollback backup for %s: %w", entry.Path, backupErr)
			}
		}
		currentOriginal := entry.OriginalExists && currentExists &&
			currentHash == entry.OriginalHash && repairModeMatches(info.Mode(), entry.OriginalMode) &&
			(entry.OriginalPath == "" || entry.OriginalPath == entry.Path)
		currentFinal := entry.FinalExists && currentExists &&
			currentHash == entry.FinalHash && repairModeMatches(info.Mode(), entry.FinalMode)
		switch {
		case currentOriginal:
			state.action = "none"
		case !entry.OriginalExists && !currentExists:
			state.action = "none"
		case !entry.OriginalExists && currentFinal:
			state.action = "remove"
		case entry.OriginalExists && (!currentExists || currentFinal):
			if _, backupErr := os.Lstat(entry.BackupPath); backupErr != nil {
				return repairRollbackPlan{}, fmt.Errorf("read rollback backup for %s: artifact is absent", entry.Path)
			}
			state.action = "restore"
		default:
			return repairRollbackPlan{}, fmt.Errorf("rollback blocked because %s changed after the crash", entry.Path)
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].entry.FinalExists != states[j].entry.FinalExists {
			return !states[i].entry.FinalExists
		}
		return states[i].entry.Path < states[j].entry.Path
	})
	return repairRollbackPlan{states: states}, nil
}

func preflightRepairRenameEndpoints(
	rename PathRename,
	sourceAbs string,
	destinationAbs string,
	lstat func(string) (os.FileInfo, error),
) error {
	sourceInfo, sourceErr := lstat(sourceAbs)
	if sourceErr != nil && !os.IsNotExist(sourceErr) {
		return fmt.Errorf("inspect rollback rename source %s: %w", rename.From, sourceErr)
	}
	destinationInfo, destinationErr := lstat(destinationAbs)
	if destinationErr != nil && !os.IsNotExist(destinationErr) {
		return fmt.Errorf("inspect rollback rename destination %s: %w", rename.To, destinationErr)
	}
	if sourceErr == nil && destinationErr == nil && !os.SameFile(sourceInfo, destinationInfo) {
		return fmt.Errorf(
			"rollback blocked because rename source %s reappeared as a distinct file",
			rename.From,
		)
	}
	return nil
}

func executeRepairRollbackPlan(
	runCtx RunContext,
	manifest repairJournalManifest,
	plan repairRollbackPlan,
	hookOptions ...*repairExecutionHooks,
) (bool, error) {
	var rollbackErr error
	mutated := false
	hooks := firstRepairExecutionHooks(hookOptions)
	for i := len(plan.states) - 1; i >= 0; i-- {
		state := plan.states[i]
		if hooks != nil && hooks.BeforeRollbackMutation != nil {
			if err := hooks.BeforeRollbackMutation(i, state.entry.Path); err != nil {
				return mutated, err
			}
		}
		if err := validateRollbackStateBeforeMutation(runCtx, manifest, state); err != nil {
			return mutated, err
		}
		switch state.action {
		case "none":
			continue
		case "restore_case":
			mutated = true
			originalAbs, err := repairAbsPath(runCtx, state.entry.OriginalPath)
			if err == nil {
				err = os.Rename(state.abs, originalAbs)
			}
			rollbackErr = errors.Join(rollbackErr, err)
			if err == nil {
				rollbackErr = errors.Join(rollbackErr, syncDirectory(filepath.Dir(originalAbs)))
			}
		case "remove":
			mutated = true
			if err := os.Remove(state.abs); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
			rollbackErr = errors.Join(rollbackErr, syncDirectory(filepath.Dir(state.abs)))
		case "restore":
			mutated = true
			var err error
			destination := state.abs
			if state.entry.OriginalPath != "" && state.entry.OriginalPath != state.entry.Path {
				destination, err = repairAbsPath(runCtx, state.entry.OriginalPath)
				if err == nil {
					if _, statErr := os.Lstat(state.abs); statErr == nil {
						err = os.Remove(state.abs)
						if err == nil {
							err = syncDirectory(filepath.Dir(state.abs))
						}
					} else if !os.IsNotExist(statErr) {
						err = statErr
					}
				}
			}
			if err == nil {
				err = replaceFile(state.entry.BackupPath, destination)
			}
			if state.entry.CasePath != "" {
				if _, statErr := os.Lstat(state.entry.CasePath); statErr == nil {
					if !repairArtifactIsOwned(manifest, state.entry.CasePath) {
						err = errors.Join(err, fmt.Errorf("case artifact ownership is absent for %s", state.entry.Path))
					} else if verifyErr := validateRepairFileSnapshot(
						state.entry.CasePath, state.entry.Path+" case artifact", true,
						state.entry.OriginalHash, state.entry.OriginalMode,
					); verifyErr != nil {
						err = errors.Join(err, verifyErr)
					} else if removeErr := os.Remove(state.entry.CasePath); removeErr != nil {
						err = errors.Join(err, removeErr)
					}
				} else if !os.IsNotExist(statErr) {
					err = errors.Join(err, statErr)
				}
			}
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return mutated, rollbackErr
}

func firstRepairExecutionHooks(options []*repairExecutionHooks) *repairExecutionHooks {
	if len(options) == 0 {
		return nil
	}
	return options[0]
}

func validateRollbackStateBeforeMutation(
	runCtx RunContext,
	manifest repairJournalManifest,
	state repairRollbackState,
) error {
	entry := state.entry
	if state.action != "none" {
		for _, rename := range manifest.Renamed {
			if entry.Path != rename.From && entry.Path != rename.To {
				continue
			}
			sourceAbs, err := repairAbsPath(runCtx, rename.From)
			if err != nil {
				return err
			}
			destinationAbs, err := repairAbsPath(runCtx, rename.To)
			if err != nil {
				return err
			}
			if err := preflightJournalRenameEndpoints(manifest, rename, sourceAbs, destinationAbs); err != nil {
				return err
			}
		}
	}
	switch state.action {
	case "none":
		return nil
	case "remove":
		return validateRepairFileSnapshot(state.abs, entry.Path, true, entry.FinalHash, entry.FinalMode)
	case "restore_case":
		originalAbs, err := repairAbsPath(runCtx, entry.OriginalPath)
		if err != nil {
			return err
		}
		originalInfo, originalErr := os.Lstat(originalAbs)
		destinationInfo, destinationErr := os.Lstat(state.abs)
		if originalErr != nil || destinationErr != nil || !os.SameFile(originalInfo, destinationInfo) {
			return fmt.Errorf("rollback case-rename precondition changed for %s", entry.Path)
		}
		return validateRepairFileSnapshot(originalAbs, entry.OriginalPath, true, entry.OriginalHash, entry.OriginalMode)
	case "restore":
		if entry.OriginalPath != "" && entry.OriginalPath != entry.Path {
			originalAbs, err := repairAbsPath(runCtx, entry.OriginalPath)
			if err != nil {
				return err
			}
			if _, err := os.Lstat(originalAbs); err == nil {
				return fmt.Errorf("rollback blocked because rename source %s reappeared as a distinct file", entry.OriginalPath)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		if _, err := os.Lstat(state.abs); err == nil {
			if err := validateRepairFileSnapshot(state.abs, entry.Path, true, entry.FinalHash, entry.FinalMode); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if !repairArtifactIsOwned(manifest, entry.BackupPath) {
			return fmt.Errorf("rollback backup ownership is absent for %s", entry.Path)
		}
		return validateRepairFileSnapshot(entry.BackupPath, entry.Path+" backup", true, entry.OriginalHash, entry.OriginalMode)
	default:
		return fmt.Errorf("unknown rollback action %q for %s", state.action, entry.Path)
	}
}

func rollbackRepairJournal(runCtx RunContext, manifest repairJournalManifest) (bool, error) {
	plan, err := buildRepairRollbackPlan(runCtx, manifest)
	if err != nil {
		return false, err
	}
	return executeRepairRollbackPlan(runCtx, manifest, plan)
}
