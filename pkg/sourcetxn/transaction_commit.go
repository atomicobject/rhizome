package sourcetxn

import (
	"fmt"
	"os"
	"path/filepath"
)

func commitPreparedRepairTransaction(runCtx writerContext, prepared *preparedTransaction, hooks *Hooks) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}

	if err := rejectSourceSettlement(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if err := validatePreparedProgress(runCtx, prepared, 0); err != nil {
		return err
	}
	for index, entry := range prepared.Manifest.Entries {
		if hooks != nil && hooks.BeforeInstall != nil {
			if err := hooks.BeforeInstall(index, entry.Path); err != nil {
				return err
			}
		}
		if err := validatePreparedProgress(runCtx, prepared, index); err != nil {
			return err
		}
		if !entry.FinalExists {
			if err := os.Remove(filepath.FromSlash(mustEntryAbs(prepared.Files, entry.Path))); err != nil && !os.IsNotExist(err) {
				return err
			}
			prepared.Installed = index + 1
			if hooks != nil && hooks.AfterMutation != nil {
				if err := hooks.AfterMutation(index, entry.Path); err != nil {
					return err
				}
			}
			if err := syncDirectory(filepath.Dir(mustEntryAbs(prepared.Files, entry.Path))); err != nil {
				return err
			}
		} else {
			abs := mustEntryAbs(prepared.Files, entry.Path)
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return err
			}
			if entry.CasePath != "" {
				if err := linkOwnedRepairArtifact(prepared.Directory, &prepared.Manifest, abs, entry.CasePath); err != nil {
					return fmt.Errorf("stage case-only rename %s: %w", entry.Path, err)
				}
				if err := os.Remove(abs); err != nil {
					return fmt.Errorf("remove case-only rename source %s: %w", entry.Path, err)
				}
				prepared.Installed = index + 1
				if hooks != nil && hooks.AfterMutation != nil {
					if err := hooks.AfterMutation(index, entry.Path); err != nil {
						return err
					}
				}
				if err := syncDirectory(filepath.Dir(abs)); err != nil {
					return err
				}
			}
			// shortcut: external writers can change an open inode after the witness check; require a cooperating editor protocol for stronger exclusion.
			published, installErr := replaceFileOutcome(entry.StagePath, abs, func() error {
				if hooks != nil && hooks.BeforeInstallDirectorySync != nil {
					return hooks.BeforeInstallDirectorySync(index, entry.Path)
				}
				return nil
			})
			if published {
				prepared.Installed = index + 1
			}
			if installErr != nil {
				// A missing, previously verified stage is also a publication
				// witness when a platform reports an uncertain move failure.
				if _, stageErr := os.Lstat(entry.StagePath); os.IsNotExist(stageErr) {
					prepared.Installed = index + 1
				}
				return fmt.Errorf("install repair path %s: %w", entry.Path, installErr)
			}
			if entry.CasePath == "" {
				prepared.Installed = index + 1
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
		prepared.Installed = index + 1
		if hooks != nil && hooks.AfterInstall != nil {
			if err := hooks.AfterInstall(index, entry.Path); err != nil {
				return err
			}
		}
	}
	if prepared.Manifest.Namespace != nil {
		if err := validatePreparedProgress(runCtx, prepared, len(prepared.Manifest.Entries)); err != nil {
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
		if err := verifyCommittedRepairJournal(runCtx, prepared.Manifest); err != nil {
			return err
		}
		if err := verifyNamespaceGitFinal(runCtx, prepared.Manifest); err != nil {
			return err
		}
		if err := markNamespaceDecision(prepared.Directory, "COMMITTED", []byte("committed\n"), hooks); err != nil {
			return NamespaceDecisionSyncError{Err: err}
		}
		return nil
	}
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}
	decisionErr := markRepairJournalCommitted(prepared.Directory)
	if decisionErr == nil && hooks != nil && hooks.afterCommitMarkerPublished != nil {
		decisionErr = hooks.afterCommitMarkerPublished()
	}
	if decisionErr != nil {
		if _, markerErr := os.Lstat(filepath.Join(prepared.Directory, "COMMITTED")); !os.IsNotExist(markerErr) {
			// The decision may survive a crash. Rolling back with this marker
			// present would let later recovery mistake partial rollback for a
			// committed source set.
			return commitDecisionUncertainError{Err: decisionErr}
		}
	}
	return decisionErr
}

type commitDecisionUncertainError struct{ Err error }

func (e commitDecisionUncertainError) Error() string {
	return "commit decision requires recovery: " + e.Err.Error()
}
func (e commitDecisionUncertainError) Unwrap() error { return e.Err }

type CommitPreconditionError struct {
	Path   string
	Reason string
}

func (e CommitPreconditionError) Error() string { return e.Reason }

func validatePreparedProgress(runCtx writerContext, prepared *preparedTransaction, installed int) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}
	if err := validateRepairJournalPaths(runCtx, prepared.Manifest); err != nil {
		if path := ConflictPath(err); path != "" {
			return CommitPreconditionError{Path: path, Reason: err.Error()}
		}
		return err
	}
	for index, entry := range prepared.Manifest.Entries {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return CommitPreconditionError{Path: entry.Path, Reason: err.Error()}
		}
		if filepath.Clean(abs) != filepath.Clean(mustEntryAbs(prepared.Files, entry.Path)) {
			return CommitPreconditionError{Path: entry.Path, Reason: fmt.Sprintf("repair path changed after prepare: %s", entry.Path)}
		}
		expectExists, expectHash, expectMode := entry.OriginalExists, entry.OriginalHash, entry.OriginalMode
		if index < installed {
			expectExists, expectHash, expectMode = entry.FinalExists, entry.FinalHash, entry.FinalMode
		}
		if err := validateRepairFileSnapshot(abs, entry.Path, expectExists, expectHash, expectMode); err != nil {
			return CommitPreconditionError{Path: entry.Path, Reason: err.Error()}
		}
		if entry.BackupPath != "" {
			if !repairArtifactIsOwned(prepared.Manifest, entry.BackupPath) {
				return CommitPreconditionError{Path: entry.Path, Reason: fmt.Sprintf("repair backup ownership is absent: %s", entry.Path)}
			}
			if err := validateRepairFileSnapshot(entry.BackupPath, entry.Path+" backup", true, entry.OriginalHash, entry.OriginalMode); err != nil {
				return CommitPreconditionError{Path: entry.Path, Reason: err.Error()}
			}
		}
		if index >= installed && entry.StagePath != "" {
			if !repairArtifactIsOwned(prepared.Manifest, entry.StagePath) {
				return CommitPreconditionError{Path: entry.Path, Reason: fmt.Sprintf("repair stage ownership is absent: %s", entry.Path)}
			}
			if err := validateRepairFileSnapshot(entry.StagePath, entry.Path+" stage", true, entry.FinalHash, entry.FinalMode); err != nil {
				return CommitPreconditionError{Path: entry.Path, Reason: err.Error()}
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
	if !info.Mode().IsRegular() || !RepairModeMatches(info.Mode(), mode) {
		return fmt.Errorf("repair path mode changed after prepare: %s", label)
	}
	linked, err := fileHasMultipleLinks(path, info)
	if err != nil {
		return err
	}
	if linked {
		return fmt.Errorf("repair path has multiple links: %s", label)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if Hash(content) != hash {
		return fmt.Errorf("repair path content changed after prepare: %s", label)
	}
	return nil
}
