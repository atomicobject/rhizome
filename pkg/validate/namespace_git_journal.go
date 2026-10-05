package validate

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errNamespaceGitAdmission = errors.New("native Git exclusion unavailable before publication")

// Static authority is checked even for settled journals: cleanup may remove
// only transaction-bound snapshots, regardless of today's Git directory state.
func validateNamespaceGitAuthority(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	dir := filepath.Join(filepath.FromSlash(runCtx.VaultPath), ".git")
	if filepath.Clean(git.GitDir) != dir || git.IndexPath != filepath.Join(dir, "index") || git.LockPath != filepath.Join(dir, "index.lock") {
		return fmt.Errorf("native Git journal binding is outside canonical vault")
	}
	rollback, candidate := namespaceGitPaths(manifest)
	_, tokenErr := hex.DecodeString(git.Token)
	if git.Rollback.Path != rollback || git.Candidate.Path != candidate ||
		!validNamespaceHash(git.Rollback.Hash) || !validNamespaceHash(git.Candidate.Hash) ||
		git.Rollback.Mode != repairModeBits(repairFileMode(git.Rollback.Mode)) || git.Candidate.Mode != repairModeBits(repairFileMode(git.Candidate.Mode)) ||
		(git.RawOriginal.Exists && (!validNamespaceHash(git.RawOriginal.Hash) || git.RawOriginal.Mode != repairModeBits(repairFileMode(git.RawOriginal.Mode)))) ||
		(!git.RawOriginal.Exists && (git.RawOriginal.Hash != "" || git.RawOriginal.Mode != 0)) ||
		len(git.Token) != 32 || tokenErr != nil || git.LockHash != SourceHash(namespaceGitLockContent(git)) || git.LockMode != 0o600 {
		return fmt.Errorf("native Git journal witness is invalid")
	}
	return nil
}

func namespaceGitPaths(manifest repairJournalManifest) (string, string) {
	git := manifest.Namespace.Git
	short := strings.TrimPrefix(SourceHash([]byte(manifest.TransactionID)), "sha256:")[:12]
	return git.IndexPath + ".rzm-repair-" + short + ".rollback", git.IndexPath + ".rzm-repair-" + short + ".candidate"
}

// Bind again under live exclusion and before every replay action. Captured
// absolute paths alone do not authorize writes through a redirected .git.
func validateNamespaceGitBinding(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitAuthority(runCtx, manifest); err != nil {
		return err
	}
	dir := filepath.Join(filepath.FromSlash(runCtx.VaultPath), ".git")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("native Git directory binding changed")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(git.GitDir) || filepath.Clean(git.IndexPath) != filepath.Join(resolved, "index") || filepath.Clean(git.LockPath) != filepath.Join(resolved, "index.lock") {
		return fmt.Errorf("native Git index/lock binding changed")
	}
	return nil
}

func namespaceGitLockContent(git *namespaceGitJournal) []byte {
	return []byte("rhizome namespace " + git.Token + "\n")
}

func acquireNamespaceGitExclusion(runCtx RunContext, prepared *preparedRepairTransaction) error {
	git := prepared.manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitBinding(runCtx, prepared.manifest); err != nil {
		return err
	}
	if _, err := os.Lstat(git.LockPath); err == nil {
		if !repairArtifactIsOwned(prepared.manifest, git.LockPath) {
			return errNamespaceGitAdmission
		}
		if err := validateRepairFileSnapshot(git.LockPath, "native Git lock", true, git.LockHash, git.LockMode); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		// Recovery may acquire fresh exclusion only by exclusive creation. It
		// never adopts a lock left in the uncertain creation/ownership window.
		if err := createOwnedRepairArtifact(prepared.dir, &prepared.manifest, git.LockPath, namespaceGitLockContent(git), repairFileMode(git.LockMode)); err != nil {
			if errors.Is(err, os.ErrExist) {
				return errors.Join(errNamespaceGitAdmission, err)
			}
			return err
		}
	}
	return validateNamespaceGitBinding(runCtx, prepared.manifest)
}

// An exclusive-creation refusal proves no live Git effect. Discard only owned
// preparation evidence, then prepare the existing filesystem fallback anew.
func abandonNativeGitPreparation(runCtx RunContext, prepared *preparedRepairTransaction) error {
	git := prepared.manifest.Namespace.Git
	if prepared.installed != 0 || repairArtifactIsOwned(prepared.manifest, git.LockPath) {
		return fmt.Errorf("cannot abandon published Git participation")
	}
	if err := verifyNamespaceOriginalFiles(runCtx, prepared.manifest); err != nil {
		return err
	}
	if err := validateNamespaceGitBinding(runCtx, prepared.manifest); err != nil {
		return err
	}
	for _, artifact := range []namespaceGitArtifact{git.Rollback, git.Candidate} {
		if !repairArtifactIsOwned(prepared.manifest, artifact.Path) {
			return fmt.Errorf("native fallback snapshot ownership is absent")
		}
		if err := validateRepairFileSnapshot(artifact.Path, "native fallback snapshot", true, artifact.Hash, artifact.Mode); err != nil {
			return err
		}
		if err := removeJustCreatedRepairArtifact(artifact.Path); err != nil {
			return err
		}
	}
	var retained []string
	for _, path := range prepared.manifest.OwnedArtifacts {
		if path != git.Rollback.Path && path != git.Candidate.Path {
			retained = append(retained, path)
		}
	}
	prepared.manifest.OwnedArtifacts = retained
	prepared.manifest.Namespace.Git, prepared.manifest.Namespace.GitMoves = nil, nil
	if err := writeRepairJournalManifest(prepared.dir, prepared.manifest); err != nil {
		return err
	}
	return cleanupFailedPrepare(runCtx, prepared.dir, prepared.manifest, nil)
}

func verifyNamespaceGitLock(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitBinding(runCtx, manifest); err != nil {
		return err
	}
	if manifest.Namespace.SettledDecision != "" || !repairArtifactIsOwned(manifest, git.LockPath) {
		return fmt.Errorf("native Git exclusion is not held by this journal")
	}
	return validateRepairFileSnapshot(git.LockPath, "native Git lock", true, git.LockHash, git.LockMode)
}

func verifyNamespaceGitOriginal(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
		return err
	}
	return validateRepairFileSnapshot(git.IndexPath, "native original Git index", git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode)
}

func publishNamespaceGitIndex(runCtx RunContext, prepared *preparedRepairTransaction) error {
	git := prepared.manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitOriginal(runCtx, prepared.manifest); err != nil {
		return err
	}
	if !repairArtifactIsOwned(prepared.manifest, git.Candidate.Path) {
		return fmt.Errorf("native Git candidate ownership is absent")
	}
	if err := validateRepairFileSnapshot(git.Candidate.Path, "native Git candidate", true, git.Candidate.Hash, git.Candidate.Mode); err != nil {
		return err
	}
	return replaceFile(git.Candidate.Path, git.IndexPath)
}

func verifyNamespaceGitFinal(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
		return err
	}
	return validateRepairFileSnapshot(git.IndexPath, "native final Git index", true, git.Candidate.Hash, git.Candidate.Mode)
}

func preflightNamespaceGitRollback(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitBinding(runCtx, manifest); err != nil {
		return err
	}
	if _, err := os.Lstat(git.LockPath); err == nil {
		if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if namespaceGitIndexMatches(git.IndexPath, git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode) || namespaceGitIndexMatches(git.IndexPath, true, git.Rollback.Hash, git.Rollback.Mode) {
		return nil
	}
	if !namespaceGitIndexMatches(git.IndexPath, true, git.Candidate.Hash, git.Candidate.Mode) {
		return fmt.Errorf("native Git index changed after publication")
	}
	if !repairArtifactIsOwned(manifest, git.Rollback.Path) {
		return fmt.Errorf("native Git rollback snapshot ownership is absent")
	}
	return validateRepairFileSnapshot(git.Rollback.Path, "native Git rollback snapshot", true, git.Rollback.Hash, git.Rollback.Mode)
}

func namespaceGitIndexMatches(path string, exists bool, hash string, mode uint32) bool {
	return validateRepairFileSnapshot(path, "native Git index", exists, hash, mode) == nil
}

func restoreNamespaceGitIndex(runCtx RunContext, prepared *preparedRepairTransaction) error {
	git := prepared.manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, prepared.manifest); err != nil {
		return err
	}
	if err := preflightNamespaceGitRollback(runCtx, prepared.manifest); err != nil {
		return err
	}
	if namespaceGitIndexMatches(git.IndexPath, git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode) || namespaceGitIndexMatches(git.IndexPath, true, git.Rollback.Hash, git.Rollback.Mode) {
		return nil
	}
	return replaceFile(git.Rollback.Path, git.IndexPath)
}

func verifyNamespaceGitRestored(runCtx RunContext, manifest repairJournalManifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
		return err
	}
	if namespaceGitIndexMatches(git.IndexPath, git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode) || namespaceGitIndexMatches(git.IndexPath, true, git.Rollback.Hash, git.Rollback.Mode) {
		return nil
	}
	return fmt.Errorf("native Git staging restoration is not verified")
}

func settleNamespaceGitRelease(runCtx RunContext, journal *recoveredRepairJournal) error {
	git := journal.manifest.Namespace.Git
	if journal.manifest.Namespace.SettledDecision != "" {
		if journal.manifest.Namespace.SettledDecision != journal.state {
			return fmt.Errorf("native settled decision disagrees with marker")
		}
		journal.decisionSynced = true
		return nil
	}
	if journal.state != repairJournalCommitted && journal.state != repairJournalRestored {
		return fmt.Errorf("native Git release requires a durable decision")
	}
	if err := syncNamespaceDecision(journal.dir, journal.state); err != nil {
		return err
	}
	journal.decisionSynced = true
	if git != nil {
		if err := validateNamespaceGitBinding(runCtx, journal.manifest); err != nil {
			return err
		}
		if _, err := os.Lstat(git.LockPath); err == nil {
			if err := verifyNamespaceGitLock(runCtx, journal.manifest); err != nil {
				return err
			}
			if err := os.Remove(git.LockPath); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := syncDirectory(filepath.Dir(git.LockPath)); err != nil {
			return err
		}
	}
	journal.manifest.Namespace.SettledDecision = journal.state
	if err := writeRepairJournalManifest(journal.dir, journal.manifest); err != nil {
		journal.manifest.Namespace.SettledDecision = ""
		return fmt.Errorf("persist native Git release: %w", err)
	}
	return nil
}

func nativeGitArtifactReleased(manifest repairJournalManifest, path string) bool {
	return manifest.Namespace != nil && manifest.Namespace.Git != nil && manifest.Namespace.SettledDecision != "" && path == manifest.Namespace.Git.LockPath
}

type namespaceDecisionSyncError struct{ err error }

func (e namespaceDecisionSyncError) Error() string {
	return "native decision synchronization failed: " + e.err.Error()
}
func (e namespaceDecisionSyncError) Unwrap() error { return e.err }

func restoreNamespacePrepared(runCtx RunContext, prepared *preparedRepairTransaction, hooks *repairExecutionHooks) error {
	if err := acquireNamespaceGitExclusion(runCtx, prepared); err != nil {
		return err
	}
	if err := preflightNamespaceGitRollback(runCtx, prepared.manifest); err != nil {
		return err
	}
	plan, err := buildRepairRollbackPlan(runCtx, prepared.manifest)
	if err != nil {
		return err
	}
	if _, err := executeRepairRollbackPlan(runCtx, prepared.manifest, plan, hooks); err != nil {
		return err
	}
	if err := restoreNamespaceGitIndex(runCtx, prepared); err != nil {
		return err
	}
	if err := verifyNamespaceOriginalFiles(runCtx, prepared.manifest); err != nil {
		return err
	}
	if err := verifyNamespaceGitRestored(runCtx, prepared.manifest); err != nil {
		return err
	}
	if err := markNamespaceRestored(prepared.dir, hooks); err != nil {
		return namespaceDecisionSyncError{err: err}
	}
	prepared.decisionSynced = true
	journal := recoveredRepairJournal{dir: prepared.dir, manifest: prepared.manifest, state: repairJournalRestored}
	err = settleNamespaceGitRelease(runCtx, &journal)
	prepared.manifest = journal.manifest
	return err
}
