package ontology

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type pendingWrite struct {
	notePath                   string
	absPath                    string
	backup                     string
	temp                       string
	expectedCurrentFingerprint string
	oldFingerprint             string
	newFingerprint             string
	mode                       os.FileMode
}

type writeConflictError struct {
	notePath string
	expected string
	current  string
}

type publicationWarningError struct{ err error }

func (e publicationWarningError) Error() string { return e.err.Error() }
func (e publicationWarningError) Unwrap() error { return e.err }

func (e writeConflictError) Error() string {
	return fmt.Sprintf("source changed before replacement for %s: expected %s, current %s", e.notePath, e.expected, e.current)
}

var editWriteMu sync.Mutex

func writeStatesAtomically(states map[string]*documentState, vaultPaths *paths.VaultPaths, transactionID string, completion *CommitCompletionArtifact, vaultWriteLeaseHeld ...bool) error {
	leaseHeld := len(vaultWriteLeaseHeld) > 0 && vaultWriteLeaseHeld[0]
	releaseLeases, err := acquireEditWriteLocks(vaultPaths, states, leaseHeld)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLeases() }()

	editWriteMu.Lock()
	defer editWriteMu.Unlock()
	if err := recoverEditWriteJournals(vaultPaths); err != nil {
		return err
	}

	items := make([]pendingWrite, 0, len(states))
	cleanupPrepared := func(removeBackups bool) error {
		var cleanupErrs []error
		for _, item := range items {
			if item.temp != "" {
				if err := os.Remove(item.temp); err != nil && !errors.Is(err, os.ErrNotExist) {
					cleanupErrs = append(cleanupErrs, err)
				}
			}
			if removeBackups && item.backup != "" {
				if err := os.Remove(item.backup); err != nil && !errors.Is(err, os.ErrNotExist) {
					cleanupErrs = append(cleanupErrs, err)
				}
			}
		}
		return errors.Join(cleanupErrs...)
	}

	for _, state := range states {
		if state == nil {
			continue
		}
		canonicalPath, err := paths.CleanNotePath(state.notePath)
		if err != nil {
			_ = cleanupPrepared(true)
			return err
		}
		abs, err := vaultPaths.AbsNote(canonicalPath)
		if err != nil {
			_ = cleanupPrepared(true)
			return err
		}
		absPath := abs.String()
		dir := filepath.Dir(absPath)
		base := filepath.Base(absPath)
		resolvedPath, err := filepath.EvalSymlinks(absPath)
		resolvedRoot, rootErr := filepath.EvalSymlinks(vaultPaths.Root())
		expectedResolved := filepath.Join(resolvedRoot, filepath.FromSlash(canonicalPath.String()))
		if err != nil || rootErr != nil || filepath.Clean(resolvedPath) != filepath.Clean(expectedResolved) {
			_ = cleanupPrepared(true)
			if err != nil {
				return fmt.Errorf("resolve edit path %s: %w", state.notePath, err)
			}
			if rootErr != nil {
				return fmt.Errorf("resolve edit vault root: %w", rootErr)
			}
			return fmt.Errorf("edit path %s uses an unsupported symbolic-link alias", state.notePath)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			_ = cleanupPrepared(true)
			return err
		}
		if editFileHasMultipleLinks(info) {
			_ = cleanupPrepared(true)
			return fmt.Errorf("edit path %s has multiple hard links and cannot be replaced safely", state.notePath)
		}
		tempFile, err := os.CreateTemp(dir, base+".rhizome-write-*")
		if err != nil {
			_ = cleanupPrepared(true)
			return err
		}
		tempPath := tempFile.Name()
		if err := tempFile.Chmod(info.Mode().Perm()); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
			_ = cleanupPrepared(true)
			return err
		}
		if _, err := tempFile.WriteString(state.content); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
			_ = cleanupPrepared(true)
			return err
		}
		if err := tempFile.Sync(); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
			_ = cleanupPrepared(true)
			return err
		}
		if err := tempFile.Close(); err != nil {
			_ = os.Remove(tempPath)
			_ = cleanupPrepared(true)
			return err
		}
		backupFile, err := os.CreateTemp(dir, "."+base+".rhizome-backup-*")
		if err != nil {
			_ = os.Remove(tempPath)
			_ = cleanupPrepared(true)
			return err
		}
		backupPath := backupFile.Name()
		if err := backupFile.Close(); err != nil {
			_ = os.Remove(tempPath)
			_ = os.Remove(backupPath)
			_ = cleanupPrepared(true)
			return err
		}
		items = append(items, pendingWrite{
			notePath:                   state.notePath,
			absPath:                    absPath,
			backup:                     backupPath,
			temp:                       tempPath,
			expectedCurrentFingerprint: state.expectedCurrentFingerprint,
			oldFingerprint:             state.expectedCurrentFingerprint,
			newFingerprint:             hashText(state.content),
			mode:                       info.Mode().Perm(),
		})
	}

	preparedCompletion, err := prepareEditCompletionArtifact(vaultPaths, completion)
	if err != nil {
		return errors.Join(err, cleanupPrepared(true))
	}
	journalPath, err := prepareEditWriteJournal(vaultPaths, transactionID, items, preparedCompletion)
	if err != nil {
		return errors.Join(err, cleanupPrepared(true), cleanupEditCompletionArtifact(preparedCompletion, false))
	}

	committed := make([]pendingWrite, 0, len(items))
	cleanupAfterRollback := func(rollbackErr error) error {
		return cleanupPrepared(rollbackErr == nil)
	}
	rollback := func() error {
		return restoreAtomicWrites(committed)
	}

	for index := range items {
		item := &items[index]
		if item.expectedCurrentFingerprint != "" {
			content, err := os.ReadFile(item.absPath)
			if err != nil {
				rollbackErr := rollback()
				cleanupErr := cleanupAfterRollback(rollbackErr)
				return finishEditWriteFailure(journalPath, errors.Join(err, rollbackErr, cleanupErr), rollbackErr)
			}
			currentFingerprint := hashText(string(content))
			if currentFingerprint != item.expectedCurrentFingerprint {
				rollbackErr := rollback()
				cleanupErr := cleanupAfterRollback(rollbackErr)
				conflictErr := writeConflictError{
					notePath: item.notePath,
					expected: item.expectedCurrentFingerprint,
					current:  currentFingerprint,
				}
				if rollbackErr == nil && cleanupErr == nil {
					return finishEditWriteFailure(journalPath, conflictErr, nil)
				}
				return finishEditWriteFailure(journalPath, errors.Join(conflictErr, rollbackErr, cleanupErr), rollbackErr)
			}
		}
		if err := os.Chmod(item.temp, item.mode); err != nil {
			rollbackErr := rollback()
			cleanupErr := cleanupAfterRollback(rollbackErr)
			return finishEditWriteFailure(journalPath, errors.Join(err, rollbackErr, cleanupErr), rollbackErr)
		}
		if err := os.Rename(item.absPath, item.backup); err != nil {
			rollbackErr := rollback()
			cleanupErr := cleanupAfterRollback(rollbackErr)
			return finishEditWriteFailure(journalPath, errors.Join(err, rollbackErr, cleanupErr), rollbackErr)
		}
		if err := os.Rename(item.temp, item.absPath); err != nil {
			restoreErr := os.Rename(item.backup, item.absPath)
			rollbackErr := rollback()
			cleanupErr := cleanupAfterRollback(errors.Join(restoreErr, rollbackErr))
			return finishEditWriteFailure(journalPath, errors.Join(err, restoreErr, rollbackErr, cleanupErr), errors.Join(restoreErr, rollbackErr))
		}
		item.temp = ""
		committed = append(committed, *item)
		if err := syncEditDirectory(filepath.Dir(item.absPath)); err != nil {
			rollbackErr := rollback()
			cleanupErr := cleanupAfterRollback(rollbackErr)
			return finishEditWriteFailure(journalPath, errors.Join(err, rollbackErr, cleanupErr), rollbackErr)
		}
	}
	if err := publishEditCompletionArtifact(preparedCompletion); err != nil {
		rollbackErr := rollback()
		cleanupErr := errors.Join(cleanupAfterRollback(rollbackErr), cleanupEditCompletionArtifact(preparedCompletion, true))
		return finishEditWriteFailure(journalPath, errors.Join(err, rollbackErr, cleanupErr), rollbackErr)
	}
	if err := markEditWriteJournalCommitted(journalPath); err != nil {
		return publicationWarningError{err: fmt.Errorf("files were saved, but the recovery journal could not be finalized: %w", err)}
	}

	var cleanupErrs []error
	for index := range committed {
		if committed[index].backup == "" {
			continue
		}
		if err := os.Remove(committed[index].backup); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	for _, item := range items {
		if item.temp == "" {
			continue
		}
		if err := os.Remove(item.temp); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	if cleanupErr := errors.Join(cleanupErrs...); cleanupErr != nil {
		return publicationWarningError{err: fmt.Errorf("files were saved, but recovery artifact cleanup failed: %w", cleanupErr)}
	}
	if err := removeEditWriteJournal(journalPath); err != nil {
		return publicationWarningError{err: fmt.Errorf("files were saved, but the recovery journal could not be removed: %w", err)}
	}
	return nil
}

func restoreAtomicWrites(committed []pendingWrite) error {
	var restoreErrs []error
	for i := len(committed) - 1; i >= 0; i-- {
		item := committed[i]
		fingerprint, exists, err := editFileFingerprint(item.absPath)
		if err != nil {
			restoreErrs = append(restoreErrs, err)
			continue
		}
		if exists && fingerprint != item.newFingerprint {
			restoreErrs = append(restoreErrs, fmt.Errorf("refuse to roll back %s after its published content changed", item.notePath))
			continue
		}
		if err := os.Remove(item.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			restoreErrs = append(restoreErrs, err)
		}
		if err := os.Rename(item.backup, item.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			restoreErrs = append(restoreErrs, err)
			continue
		}
		if err := syncEditDirectory(filepath.Dir(item.absPath)); err != nil {
			restoreErrs = append(restoreErrs, err)
		}
	}
	return errors.Join(restoreErrs...)
}
