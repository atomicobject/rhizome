package sourcetxn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type repairRollbackState struct {
	entry    Entry
	Absolute string
	action   string
}

type rollbackPlan struct {
	Files []repairRollbackState
}

func preflightRepairRollback(runCtx writerContext, manifest Manifest) (rollbackPlan, error) {
	return buildRepairRollbackPlan(runCtx, manifest, len(manifest.Entries))
}

func buildRepairRollbackPlan(runCtx writerContext, manifest Manifest, installed int) (rollbackPlan, error) {
	if err := rejectSourceSettlement(runCtx, manifest); err != nil {
		return rollbackPlan{}, err
	}
	if err := validateRepairArtifactOwnership(manifest); err != nil {
		return rollbackPlan{}, err
	}
	for _, rename := range manifest.Renamed {
		sourceAbs, err := repairAbsPath(runCtx, rename.From)
		if err != nil {
			return rollbackPlan{}, err
		}
		destinationAbs, err := repairAbsPath(runCtx, rename.To)
		if err != nil {
			return rollbackPlan{}, err
		}
		if err := preflightJournalRenameEndpoints(manifest, rename, sourceAbs, destinationAbs); err != nil {
			return rollbackPlan{}, err
		}
	}
	states := make([]repairRollbackState, 0, len(manifest.Entries))
	// Validate the complete rollback before mutating any path. This prevents a
	// later conflict from turning recovery itself into a partial transaction.
	for _, entry := range manifest.Entries[:installed] {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return rollbackPlan{}, err
		}
		state := repairRollbackState{entry: entry, Absolute: abs}
		if entry.OriginalPath != "" && entry.OriginalPath != entry.Path {
			originalAbs, originalErr := repairAbsPath(runCtx, entry.OriginalPath)
			if originalErr != nil {
				return rollbackPlan{}, originalErr
			}
			originalInfo, originalStatErr := os.Lstat(originalAbs)
			destinationInfo, destinationErr := os.Lstat(abs)
			if originalStatErr != nil && !os.IsNotExist(originalStatErr) {
				return rollbackPlan{}, originalStatErr
			}
			if destinationErr != nil && !os.IsNotExist(destinationErr) {
				return rollbackPlan{}, destinationErr
			}
			if originalStatErr == nil {
				destinationSame := destinationErr == nil && os.SameFile(originalInfo, destinationInfo)
				if destinationErr == nil && !destinationSame {
					return rollbackPlan{}, fmt.Errorf(
						"rollback blocked because rename source %s reappeared as a distinct file",
						entry.OriginalPath,
					)
				}
				if originalErr := validateRepairFileSnapshot(
					originalAbs, entry.OriginalPath, true, entry.OriginalHash, entry.OriginalMode,
				); originalErr != nil {
					return rollbackPlan{}, fmt.Errorf(
						"rollback blocked because rename source %s changed before rollback: %w",
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
			return rollbackPlan{}, statErr
		}
		var currentHash string
		if currentExists {
			if !info.Mode().IsRegular() {
				return rollbackPlan{}, fmt.Errorf("rollback path %s is not a regular file", entry.Path)
			}
			content, err := os.ReadFile(abs)
			if err != nil {
				return rollbackPlan{}, err
			}
			currentHash = Hash(content)
		}
		if entry.OriginalExists && entry.BackupPath != "" {
			backup, backupErr := os.ReadFile(entry.BackupPath)
			if backupErr == nil {
				if !repairArtifactIsOwned(manifest, entry.BackupPath) {
					return rollbackPlan{}, fmt.Errorf("rollback backup ownership is absent for %s", entry.Path)
				}
				if Hash(backup) != entry.OriginalHash {
					return rollbackPlan{}, fmt.Errorf("rollback backup for %s does not match journal hash", entry.Path)
				}
			} else if !os.IsNotExist(backupErr) {
				return rollbackPlan{}, fmt.Errorf("read rollback backup for %s: %w", entry.Path, backupErr)
			}
		}
		currentOriginal := entry.OriginalExists && currentExists &&
			currentHash == entry.OriginalHash && RepairModeMatches(info.Mode(), entry.OriginalMode) &&
			(entry.OriginalPath == "" || entry.OriginalPath == entry.Path)
		currentFinal := entry.FinalExists && currentExists &&
			currentHash == entry.FinalHash && RepairModeMatches(info.Mode(), entry.FinalMode)
		switch {
		case currentOriginal:
			state.action = "none"
		case !entry.OriginalExists && !currentExists:
			state.action = "none"
		case !entry.OriginalExists && currentFinal:
			state.action = "remove"
		case entry.OriginalExists && (!currentExists || currentFinal):
			if _, backupErr := os.Lstat(entry.BackupPath); backupErr != nil {
				return rollbackPlan{}, fmt.Errorf("read rollback backup for %s: artifact is absent", entry.Path)
			}
			state.action = "restore"
		default:
			return rollbackPlan{}, fmt.Errorf("rollback blocked because %s changed before rollback", entry.Path)
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].entry.FinalExists != states[j].entry.FinalExists {
			return !states[i].entry.FinalExists
		}
		return states[i].entry.Path < states[j].entry.Path
	})
	return rollbackPlan{Files: states}, nil
}

func preflightRepairRenameEndpoints(
	rename Rename,
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
	runCtx writerContext,
	manifest Manifest,
	plan rollbackPlan,
	hookOptions ...*Hooks,
) (bool, error) {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return false, err
	}

	var rollbackErr error
	mutated := false
	hooks := firstRepairExecutionHooks(hookOptions)
	for i := len(plan.Files) - 1; i >= 0; i-- {
		state := plan.Files[i]
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
				err = os.Rename(state.Absolute, originalAbs)
			}
			rollbackErr = errors.Join(rollbackErr, err)
			if err == nil {
				rollbackErr = errors.Join(rollbackErr, syncDirectory(filepath.Dir(originalAbs)))
			}
		case "remove":
			mutated = true
			if err := os.Remove(state.Absolute); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
			rollbackErr = errors.Join(rollbackErr, syncDirectory(filepath.Dir(state.Absolute)))
		case "restore":
			mutated = true
			var err error
			destination := state.Absolute
			if state.entry.OriginalPath != "" && state.entry.OriginalPath != state.entry.Path {
				destination, err = repairAbsPath(runCtx, state.entry.OriginalPath)
				if err == nil {
					if _, statErr := os.Lstat(state.Absolute); statErr == nil {
						err = os.Remove(state.Absolute)
						if err == nil {
							err = syncDirectory(filepath.Dir(state.Absolute))
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

func firstRepairExecutionHooks(options []*Hooks) *Hooks {
	if len(options) == 0 {
		return nil
	}
	return options[0]
}

func validateRollbackStateBeforeMutation(
	runCtx writerContext,
	manifest Manifest,
	state repairRollbackState,
) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}
	if err := rejectSourceSettlement(runCtx, manifest); err != nil {
		return err
	}
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
		return validateRepairFileSnapshot(state.Absolute, entry.Path, true, entry.FinalHash, entry.FinalMode)
	case "restore_case":
		originalAbs, err := repairAbsPath(runCtx, entry.OriginalPath)
		if err != nil {
			return err
		}
		originalInfo, originalErr := os.Lstat(originalAbs)
		destinationInfo, destinationErr := os.Lstat(state.Absolute)
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
		if _, err := os.Lstat(state.Absolute); err == nil {
			if err := validateRepairFileSnapshot(state.Absolute, entry.Path, true, entry.FinalHash, entry.FinalMode); err != nil {
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
