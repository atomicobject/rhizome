package sourcetxn

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"strings"
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
	return syncDirectory(filepath.Dir(path))
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
	if err := publishRepairJournalManifest(tempPath, journalPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(journalPath))
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

func editFileFingerprint(path string) (string, bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return legacyFingerprint(string(content)), true, nil
}

func removeEditWriteJournal(journalPath string) error {
	if journalPath == "" {
		return nil
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(journalPath))
}

func legacyFingerprint(s string) string { return strings.TrimPrefix(Hash([]byte(s)), "sha256:") }
