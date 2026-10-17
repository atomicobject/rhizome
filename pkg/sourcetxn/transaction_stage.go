package sourcetxn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"strings"
)

func stageRepairFileStates(runCtx writerContext, manifest Manifest, states []fileState, hooks *Hooks) (*preparedTransaction, error) {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return nil, err
	}
	var affected []string
	for _, state := range states {
		if !state.Internal {
			affected = append(affected, state.Path, state.OriginalPath)
		}
	}
	if err := CheckAdmission(runCtx.VaultPath, sortedUnique(affected)); err != nil {
		return nil, err
	}
	dir, err := repairJournalDir(runCtx, manifest.TransactionID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("repair journal already exists for transaction %s", manifest.TransactionID)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	shortID := strings.TrimPrefix(Hash([]byte(manifest.TransactionID)), "sha256:")[:12]
	for i, state := range states {
		entry := Entry{
			Path: state.Path, OriginalPath: state.OriginalPath, OriginalExists: state.OriginalExists,
			Internal:     state.Internal,
			OriginalMode: RepairModeBits(state.OriginalMode), FinalExists: state.FinalExists,
			FinalMode: RepairModeBits(state.FinalMode),
		}
		if state.OriginalExists {
			entry.OriginalHash = Hash(state.OriginalContent)
			entry.BackupPath = repairArtifactPath(state.Absolute, shortID, i, "backup")
		}
		if state.FinalExists {
			entry.FinalHash = Hash(state.FinalContent)
			entry.StagePath = repairArtifactPath(state.Absolute, shortID, i, "stage")
		}
		if state.OriginalPath != "" && state.OriginalPath != state.Path {
			entry.CasePath = repairArtifactPath(state.Absolute, shortID, i, "case-original")
		}
		manifest.Entries = append(manifest.Entries, entry)
	}

	if err := validateRepairJournalPaths(runCtx, manifest); err != nil {
		return nil, err
	}
	var identities []os.FileInfo
	for _, state := range states {
		abs, err := repairAbsPath(runCtx, state.Path)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(abs) != filepath.Clean(state.Absolute) {
			return nil, fmt.Errorf("prepared source path does not match vault authority: %s", state.Path)
		}
		if info, statErr := os.Lstat(abs); statErr == nil {
			for _, previous := range identities {
				if os.SameFile(previous, info) {
					return nil, fmt.Errorf("source states have duplicate filesystem path %s", state.Path)
				}
			}
			identities = append(identities, info)
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		if err := validateRepairFileSnapshot(abs, state.Path, state.OriginalExists, Hash(state.OriginalContent), RepairModeBits(state.OriginalMode)); err != nil {
			return nil, &SourceConflictError{Path: state.Path, Err: err}
		}
	}
	// Adjacent staging artifacts need their parent directories. Create them
	// only after every admission and original-source witness has passed.
	for _, state := range states {
		if !state.FinalExists {
			continue
		}
		if state.Internal {
			if err := ensureRepairCompletionParent(runCtx, state.Absolute, state.Path); err != nil {
				return nil, err
			}
		} else if err := ensureDirectory(runCtx, filepath.Dir(state.Absolute), 0755); err != nil {
			return nil, err
		}
	}
	if err := writeRepairJournalManifest(dir, manifest); err != nil {
		return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
	}
	if hooks != nil && hooks.AfterJournalPublished != nil {
		if err := hooks.AfterJournalPublished(dir); err != nil {
			if errors.Is(err, ErrSimulatedInterruption) {
				return nil, err
			}
			return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
		}
	}
	for i, state := range states {
		entry := manifest.Entries[i]
		if entry.BackupPath != "" {
			if err := createOwnedRepairArtifact(dir, &manifest, entry.BackupPath, state.OriginalContent, state.OriginalMode); err != nil {
				return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
			}
		}
		if entry.StagePath != "" {
			if err := createOwnedRepairArtifact(dir, &manifest, entry.StagePath, state.FinalContent, state.FinalMode); err != nil {
				return nil, errors.Join(err, cleanupFailedPrepare(runCtx, dir, manifest, hooks))
			}
		}
	}
	return &preparedTransaction{Directory: dir, Manifest: manifest, Files: states}, nil
}

func cleanupFailedPrepare(
	runCtx writerContext,
	dir string,
	manifest Manifest,
	hooks *Hooks,
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
		journal := journal{Directory: dir, Manifest: manifest, State: repairJournalRestored}
		if err := settleNamespaceGitRelease(runCtx, &journal); err != nil {
			return err
		}
		manifest = journal.Manifest
	}
	return cleanupRepairJournal(runCtx, dir, manifest, hooks)
}
