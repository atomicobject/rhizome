package ontology

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

const editWriteJournalVersion = 1

type editWriteJournal struct {
	Version       int                         `json:"version"`
	TransactionID string                      `json:"transactionId"`
	Phase         string                      `json:"phase"`
	Entries       []editWriteJournalEntry     `json:"entries"`
	Completion    *editCompletionJournalEntry `json:"completion,omitempty"`
}

type editCompletionJournalEntry struct {
	TargetPath  string `json:"targetPath"`
	TempPath    string `json:"tempPath"`
	Fingerprint string `json:"fingerprint"`
}

type editWriteJournalEntry struct {
	NotePath       string `json:"notePath"`
	TempPath       string `json:"tempPath"`
	BackupPath     string `json:"backupPath"`
	OldFingerprint string `json:"oldFingerprint"`
	NewFingerprint string `json:"newFingerprint"`
	Mode           uint32 `json:"mode"`
}

func prepareEditWriteJournal(vaultPaths *paths.VaultPaths, transactionID string, items []pendingWrite, completion *editCompletionJournalEntry) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	if strings.TrimSpace(transactionID) == "" {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err != nil {
			transactionID = fmt.Sprintf("edit-%d", time.Now().UnixNano())
		} else {
			transactionID = "edit-" + hex.EncodeToString(buf)
		}
	}
	journal := editWriteJournal{Version: editWriteJournalVersion, TransactionID: transactionID, Phase: "prepared", Completion: completion}
	for _, item := range items {
		journal.Entries = append(journal.Entries, editWriteJournalEntry{
			NotePath: item.notePath, TempPath: item.temp, BackupPath: item.backup,
			OldFingerprint: item.oldFingerprint, NewFingerprint: item.newFingerprint, Mode: uint32(item.mode),
		})
	}
	dir := filepath.Join(vaultPaths.Root(), ".rhizome", "edit-journal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(transactionID))
	journalPath := filepath.Join(dir, hex.EncodeToString(digest[:])+".json")
	if _, err := os.Stat(journalPath); err == nil {
		return "", fmt.Errorf("edit transaction %q is already prepared", transactionID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return journalPath, writeEditWriteJournal(journalPath, journal)
}

func prepareEditCompletionArtifact(vaultPaths *paths.VaultPaths, artifact *CommitCompletionArtifact) (*editCompletionJournalEntry, error) {
	if artifact == nil {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Join(vaultPaths.Root(), ".rhizome", "edit-receipts"), 0o700); err != nil {
		return nil, err
	}
	target, err := verifiedEditCompletionTarget(vaultPaths, artifact.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(target); err == nil {
		return nil, fmt.Errorf("edit completion artifact already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".completion-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = removeIfPresent(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return nil, err
	}
	if _, err := temp.Write(artifact.Content); err != nil {
		return nil, err
	}
	if err := temp.Sync(); err != nil {
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	if err := syncEditDirectory(filepath.Dir(target)); err != nil {
		return nil, err
	}
	cleanup = false
	return &editCompletionJournalEntry{TargetPath: target, TempPath: tempPath, Fingerprint: hashText(string(artifact.Content))}, nil
}

func publishEditCompletionArtifact(completion *editCompletionJournalEntry) error {
	if completion == nil {
		return nil
	}
	if err := validateEditCompletionArtifactPaths(completion); err != nil {
		return err
	}
	if err := os.Rename(completion.TempPath, completion.TargetPath); err != nil {
		return err
	}
	completion.TempPath = ""
	return syncEditDirectory(filepath.Dir(completion.TargetPath))
}

func cleanupEditCompletionArtifact(completion *editCompletionJournalEntry, removeTarget bool) error {
	if completion == nil {
		return nil
	}
	if err := validateEditCompletionArtifactPaths(completion); err != nil {
		return err
	}
	var errs []error
	for _, path := range []string{completion.TempPath} {
		if path != "" {
			if err := removeIfPresent(path); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if removeTarget && completion.TargetPath != "" {
		fingerprint, exists, err := editFileFingerprint(completion.TargetPath)
		if err != nil {
			errs = append(errs, err)
		} else if exists && fingerprint == completion.Fingerprint {
			errs = append(errs, removeIfPresent(completion.TargetPath))
		} else if exists {
			errs = append(errs, fmt.Errorf("refuse to remove changed edit completion artifact"))
		}
	}
	return errors.Join(errs...)
}

func verifiedEditCompletionTarget(vaultPaths *paths.VaultPaths, target string) (string, error) {
	target = filepath.Clean(target)
	allowed, err := filepath.EvalSymlinks(filepath.Join(vaultPaths.Root(), ".rhizome", "edit-receipts"))
	if err != nil {
		return "", fmt.Errorf("resolve edit receipt directory: %w", err)
	}
	targetParent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", fmt.Errorf("resolve edit receipt target: %w", err)
	}
	target = filepath.Join(targetParent, filepath.Base(target))
	rel, err := filepath.Rel(allowed, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("edit completion artifact escaped the receipt directory")
	}
	return target, nil
}

func validateEditCompletionArtifactPaths(completion *editCompletionJournalEntry) error {
	if completion == nil {
		return nil
	}
	if strings.TrimSpace(completion.TargetPath) == "" {
		return fmt.Errorf("edit completion artifact has an empty target path")
	}
	if completion.TempPath == "" {
		return nil
	}
	targetDir := filepath.Clean(filepath.Dir(completion.TargetPath))
	temp := filepath.Clean(completion.TempPath)
	if filepath.Clean(filepath.Dir(temp)) != targetDir {
		return fmt.Errorf("edit completion temp artifact escaped its target directory")
	}
	if !strings.HasPrefix(filepath.Base(temp), ".completion-") {
		return fmt.Errorf("edit completion temp artifact has an invalid name")
	}
	if temp == filepath.Clean(completion.TargetPath) {
		return fmt.Errorf("edit completion temp artifact must differ from its target")
	}
	return nil
}

func removeIfPresent(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncEditDirectory(filepath.Dir(path))
}

func markEditWriteJournalCommitted(journalPath string) error {
	if journalPath == "" {
		return nil
	}
	journal, err := readEditWriteJournal(journalPath)
	if err != nil {
		return err
	}
	journal.Phase = "committed"
	return writeEditWriteJournal(journalPath, journal)
}

func writeEditWriteJournal(journalPath string, journal editWriteJournal) error {
	payload, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(journalPath), ".journal-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = removeIfPresent(tempPath)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, journalPath); err != nil {
		return err
	}
	return syncEditDirectory(filepath.Dir(journalPath))
}

func readEditWriteJournal(journalPath string) (editWriteJournal, error) {
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return editWriteJournal{}, err
	}
	var journal editWriteJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return editWriteJournal{}, fmt.Errorf("read edit journal %s: %w", journalPath, err)
	}
	if journal.Version != editWriteJournalVersion || len(journal.Entries) == 0 {
		return editWriteJournal{}, fmt.Errorf("unsupported edit journal %s", journalPath)
	}
	return journal, nil
}

// CheckNoPendingEditJournals is a read-only admission query. The native
// namespace owner calls it under its vault lease rather than nesting recovery.
func CheckNoPendingEditJournals(vaultPath string) error {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return fmt.Errorf("resolve edit journal vault: %w", err)
	}
	dir := filepath.Join(vaultPaths.Root(), ".rhizome", "edit-journal")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		journalPath := filepath.Join(dir, entry.Name())
		if _, err := readEditWriteJournal(journalPath); err != nil {
			return fmt.Errorf("pending edit recovery requires startup recovery: %w", err)
		}
		return fmt.Errorf("pending edit journal %s requires startup recovery before namespace mutation", entry.Name())
	}
	return nil
}

func recoverEditWriteJournals(vaultPaths *paths.VaultPaths) error {
	dir := filepath.Join(vaultPaths.Root(), ".rhizome", "edit-journal")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		journalPath := filepath.Join(dir, entry.Name())
		journal, err := readEditWriteJournal(journalPath)
		if err != nil {
			return err
		}
		if err := recoverEditWriteJournal(vaultPaths, journalPath, journal); err != nil {
			return err
		}
	}
	return nil
}

func recoverInterruptedEditWrites(vaultPaths *paths.VaultPaths, vaultWriteLeaseHeld ...bool) error {
	leaseHeld := len(vaultWriteLeaseHeld) > 0 && vaultWriteLeaseHeld[0]
	release, err := acquireEditWriteLocks(vaultPaths, nil, leaseHeld)
	if err != nil {
		return err
	}
	editWriteMu.Lock()
	recoverErr := recoverEditWriteJournals(vaultPaths)
	editWriteMu.Unlock()
	return errors.Join(recoverErr, release())
}

// RecoverInterruptedEdits restores or completes any journaled edit before a
// reader, cache, or index observes the vault after process startup.
func RecoverInterruptedEdits(vaultPath string) error {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return err
	}
	return recoverInterruptedEditWrites(&vaultPaths)
}

// RecoverInterruptedEditsContext is the startup form of recovery. The newly
// elected runtime waits for an earlier writer to release the vault lease, then
// holds that lease through journal replay. Cancellation ends the wait without
// inspecting or changing the journal.
func RecoverInterruptedEditsContext(ctx context.Context, vaultPath string) error {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return err
	}
	release, err := waitEditRecoveryWriteLease(ctx, &vaultPaths)
	if err != nil {
		return err
	}
	return errors.Join(recoverInterruptedEditWrites(&vaultPaths, true), release())
}

func editFileFingerprint(path string) (string, bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hashText(string(content)), true, nil
}

func finishEditWriteFailure(journalPath string, cause, rollbackErr error) error {
	if rollbackErr != nil {
		return editPublicationFailure{err: cause}
	}
	return errors.Join(cause, removeEditWriteJournal(journalPath))
}

type editPublicationFailure struct{ err error }

func (e editPublicationFailure) Error() string {
	return "edit publication failed and recovery evidence was preserved: " + e.err.Error()
}

func removeEditWriteJournal(journalPath string) error {
	if journalPath == "" {
		return nil
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncEditDirectory(filepath.Dir(journalPath))
}
