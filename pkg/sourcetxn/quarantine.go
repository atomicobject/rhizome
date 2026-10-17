package sourcetxn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn/admission"
)

type quarantineRecord struct {
	Evidence  string   `json:"evidence"`
	Paths     []string `json:"affectedPaths,omitempty"`
	VaultWide bool     `json:"vaultWide"`
	Reason    string   `json:"reason"`
	// Evidence and its artifacts stay in place. The record retains their owner.
	Artifacts []string `json:"artifacts,omitempty"`
}

func quarantineRoot(root string) (string, error) {
	dir := filepath.Join(root, ".rhizome", "source-quarantine")
	if err := verifyEditRecoveryPath(root, filepath.Join(dir, "manifest.json"), true); err != nil {
		return "", err
	}
	return dir, nil
}
func quarantinePath(root, evidence string) (string, error) {
	dir, err := quarantineRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strings.TrimPrefix(Hash([]byte(evidence)), "sha256:")+".json"), nil
}
func recordQuarantine(root, evidence string, affected, artifacts []string, reason error) error {
	record := quarantineRecord{Evidence: evidence, Reason: reason.Error(), Artifacts: artifacts, VaultWide: len(affected) == 0}
	for _, p := range affected {
		clean, err := canonicalRepairPath(p)
		if err != nil || p == "" || clean != p {
			record.VaultWide = true
			record.Paths = nil
			break
		}
		record.Paths = append(record.Paths, p)
	}
	record.Paths = sortedUnique(record.Paths)
	target, err := quarantinePath(root, evidence)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".quarantine-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = temp.Close(); _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(append(content, '\n')); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := publishRepairJournalManifest(tempPath, target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}
func quarantineRecords(root string) ([]quarantineRecord, error) {
	dir, err := quarantineRoot(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []quarantineRecord
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unreadable source quarantine requires vault-wide review")
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var record quarantineRecord
		if err := json.Unmarshal(content, &record); err != nil || record.Evidence == "" {
			return nil, fmt.Errorf("unreadable source quarantine requires vault-wide review")
		}
		for _, p := range record.Paths {
			clean, err := canonicalRepairPath(p)
			if err != nil || clean != p || p == "" {
				return nil, fmt.Errorf("invalid source quarantine path requires vault-wide review")
			}
		}
		expected, err := quarantinePath(root, record.Evidence)
		if err != nil || expected != filepath.Join(dir, entry.Name()) {
			return nil, fmt.Errorf("invalid source quarantine identity requires vault-wide review")
		}
		if len(record.Paths) == 0 {
			record.VaultWide = true
		}
		out = append(out, record)
	}
	return out, nil
}
func isQuarantined(root, evidence string) bool {
	target, err := quarantinePath(root, evidence)
	if err != nil {
		return false
	}
	_, err = os.Lstat(target)
	return err == nil
}
func admissionOverlap(affected, blocked []string) bool {
	if len(affected) == 0 {
		return true
	}
	for _, a := range affected {
		for _, b := range blocked {
			if paths.LinkKey(a) == paths.LinkKey(b) {
				return true
			}
		}
	}
	return false
}

// CheckAdmission is the common read-only barrier for every source writer.
func CheckAdmission(root string, affected []string) error {
	vault, err := paths.NewVaultPaths(root)
	if err != nil {
		return err
	}
	root = vault.Root()
	if err := namespaceadmission.Check(root); err != nil {
		return err
	}
	records, err := quarantineRecords(root)
	if err != nil {
		return err
	}
	for _, record := range records {
		settled, err := settledEvidence(root, record.Evidence)
		if err != nil {
			return err
		}
		if settled {
			continue
		}
		if record.VaultWide || admissionOverlap(affected, record.Paths) {
			return fmt.Errorf("source recovery review required for %s; preserved artifacts: %s; %s", record.Evidence, strings.Join(record.Artifacts, ", "), record.Reason)
		}
	}
	// Pending repair and legacy evidence also owns its recorded paths, even
	// before startup has had an opportunity to classify it.
	journalRoot, err := repairJournalRoot(writerContext{VaultPath: root})
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(journalRoot)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		dir := filepath.Join(journalRoot, entry.Name())
		if isQuarantined(root, dir) {
			continue
		}
		_, cleanup, parseErr := namespaceadmission.ParseCleanupName(entry.Name())
		if parseErr != nil {
			return parseErr
		}
		if cleanup {
			continue
		}
		m, err := readRepairJournalManifest(dir)
		if err != nil {
			return fmt.Errorf("pending source recovery requires vault-wide review: %w", err)
		}
		settled, settleErr := sourceIsSettled(dir, m)
		if settleErr != nil {
			return settleErr
		}
		if settled {
			continue
		}
		if m.Namespace != nil && m.Namespace.SettledDecision != "" {
			decision, err := nativeJournalDecision(dir)
			if err != nil {
				return err
			}
			if (decision == repairJournalCommitted || decision == repairJournalRestored) && m.Namespace.SettledDecision == decision {
				// Native convergence uses current authored source after the durable
				// decision releases source and Git ownership.
				continue
			}
		}
		var blocked []string
		for _, e := range m.Entries {
			if !e.Internal {
				blocked = append(blocked, e.Path, e.OriginalPath)
			}
		}
		if admissionOverlap(affected, sortedUnique(blocked)) {
			return fmt.Errorf("pending source transaction %s requires recovery", m.TransactionID)
		}
	}
	legacy := filepath.Join(root, ".rhizome", "edit-journal")
	entries, err = os.ReadDir(legacy)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		file := filepath.Join(legacy, entry.Name())
		if isQuarantined(root, file) {
			continue
		}
		m, err := readEditWriteJournal(file)
		if err != nil {
			return fmt.Errorf("pending legacy recovery requires vault-wide review: %w", err)
		}
		var blocked []string
		for _, e := range m.Entries {
			blocked = append(blocked, e.NotePath)
		}
		if admissionOverlap(affected, blocked) {
			return fmt.Errorf("pending legacy transaction %s requires recovery", m.TransactionID)
		}
	}
	return nil
}
func CheckPending(root string) error { return CheckAdmission(root, nil) }

func quarantineRepair(run writerContext, dir string, cause error) error {
	var affected, artifacts []string
	// Decode only for visibility. Invalid confined journal authority makes the
	// barrier vault-wide instead of trusting a partial set of paths.
	m, err := readRepairJournalManifest(dir)
	if err == nil && validateRepairJournalPaths(run, m) == nil {
		for _, e := range m.Entries {
			if !e.Internal {
				affected = append(affected, e.Path, e.OriginalPath)
			}
		}
		for path := range expectedRepairArtifacts(m) {
			artifacts = append(artifacts, path)
		}
	}
	return recordQuarantine(run.VaultPath, dir, sortedUnique(affected), artifacts, cause)
}

func recoverLegacy(run writerContext) error {
	root := run.VaultPath
	dir := filepath.Join(root, ".rhizome", "edit-journal")
	if err := verifyEditRecoveryPath(root, filepath.Join(dir, "probe.json"), true); err != nil {
		return recordQuarantine(root, dir, nil, nil, err)
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	vault, err := paths.NewVaultPaths(root)
	if err != nil {
		return err
	}
	type pendingLegacy struct {
		path    string
		journal editWriteJournal
	}
	var pending []pendingLegacy
	quarantine := func(file string, m editWriteJournal, cause error) error {
		var affected, artifacts []string
		for _, e := range m.Entries {
			affected = append(affected, e.NotePath)
			artifacts = append(artifacts, e.TempPath, e.BackupPath)
		}
		if m.Completion != nil {
			artifacts = append(artifacts, m.Completion.TempPath)
		}
		return recordQuarantine(root, file, affected, artifacts, cause)
	}
	// Classify the complete legacy family before changing any source.
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		m, readErr := readEditWriteJournal(file)
		if readErr == nil {
			_, readErr = preflightEditRecovery(&vault, file, m)
		}
		if readErr != nil {
			if err := quarantine(file, m, readErr); err != nil {
				return errors.Join(readErr, err)
			}
			continue
		}
		if err := clearQuarantine(run, file); err != nil {
			return err
		}
		pending = append(pending, pendingLegacy{file, m})
	}
	records, err := quarantineRecords(root)
	if err != nil {
		return err
	}
	for _, item := range pending {
		var affected []string
		for _, entry := range item.journal.Entries {
			affected = append(affected, entry.NotePath)
		}
		blocked := false
		for _, record := range records {
			settled, err := settledEvidence(root, record.Evidence)
			if err != nil {
				return err
			}
			if settled {
				continue
			}
			if record.VaultWide || admissionOverlap(affected, record.Paths) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if err := run.Lease.Validate(root); err != nil {
			return err
		}
		if err := recoverEditWriteJournal(&vault, item.path, item.journal); err != nil {
			if err := quarantine(item.path, item.journal, err); err != nil {
				return err
			}
		}
	}
	return nil
}

// Quarantine constrains recovery too: unreadable evidence might own any path,
// so a valid-looking journal cannot authorize a conflicting rollback.
func outsideQuarantine(run writerContext, journals []journal) ([]journal, error) {
	records, err := quarantineRecords(run.VaultPath)
	if err != nil {
		return nil, err
	}
	var eligible []journal
	for _, journal := range journals {
		var affected []string
		for _, entry := range journal.Manifest.Entries {
			if !entry.Internal {
				affected = append(affected, entry.Path, entry.OriginalPath)
			}
		}
		blocked := false
		for _, record := range records {
			settled, err := settledEvidence(run.VaultPath, record.Evidence)
			if err != nil {
				return nil, err
			}
			if settled {
				continue
			}
			if record.VaultWide || admissionOverlap(sortedUnique(affected), record.Paths) {
				blocked = true
				break
			}
		}
		if !blocked {
			eligible = append(eligible, journal)
		}
	}
	return eligible, nil
}
