package sourcetxn

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn/admission"
)

const repairJournalVersion = namespaceadmission.RepairVersion

const (
	repairJournalPrepared       = "prepared"
	repairJournalCommitted      = "committed"
	repairJournalRestored       = "restored"
	repairJournalCleanupPending = "cleanup_pending"
)

const repairJournalCleanupPrefix = ".cleanup-"

// JournalEvidence exposes an interrupted transaction without mutating it.
type JournalEvidence struct {
	TransactionID   string   `json:"transactionId"`
	PlanFingerprint string   `json:"planFingerprint,omitempty"`
	State           string   `json:"state"`
	AffectedPaths   []string `json:"affectedPaths,omitempty"`
	RequiredChecks  []string `json:"requiredChecks,omitempty"`
	EvidencePath    string   `json:"evidencePath,omitempty"`
	Artifacts       []string `json:"artifacts,omitempty"`
}

type Manifest struct {
	Source          bool              `json:"source,omitempty"`
	Version         int               `json:"version"`
	TransactionID   string            `json:"transactionId"`
	PlanFingerprint string            `json:"planFingerprint"`
	CreatedAt       string            `json:"createdAt"`
	OperationIDs    []string          `json:"operationIds,omitempty"`
	ActionIDs       []string          `json:"actionIds,omitempty"`
	IssueKeys       []string          `json:"issueKeys,omitempty"`
	Changed         []string          `json:"changed,omitempty"`
	Renamed         []Rename          `json:"renamed,omitempty"`
	Deleted         []string          `json:"deleted,omitempty"`
	Checks          []string          `json:"checks,omitempty"`
	OwnedArtifacts  []string          `json:"ownedArtifacts,omitempty"`
	Entries         []Entry           `json:"entries"`
	Namespace       *NamespacePurpose `json:"namespace,omitempty"`
}

type Entry struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"originalPath,omitempty"`
	Internal       bool   `json:"internal,omitempty"`
	OriginalExists bool   `json:"originalExists"`
	OriginalMode   uint32 `json:"originalMode,omitempty"`
	OriginalHash   string `json:"originalHash,omitempty"`
	FinalExists    bool   `json:"finalExists"`
	FinalMode      uint32 `json:"finalMode,omitempty"`
	FinalHash      string `json:"finalHash,omitempty"`
	BackupPath     string `json:"backupPath,omitempty"`
	StagePath      string `json:"stagePath,omitempty"`
	CasePath       string `json:"casePath,omitempty"`
}

type journal struct {
	NativeCleanupDecision string
	DecisionSynced        bool // In-memory proof; the settled decision remains writer admission.
	Directory             string
	Manifest              Manifest
	State                 string
}

type recovery struct {
	Native         []journal
	NativeCleanup  []journal
	Committed      []journal
	RolledBack     []journal
	NoMutation     []journal
	Partial        []journal
	CleanupPending []journal
}

// detectPendingRepairJournals reports durable repair state without recovering
// or deleting anything. Read-only validation and CI use this fail-closed seam.
func detectPendingRepairJournals(runCtx writerContext) ([]JournalEvidence, error) {
	journals, err := discoverRepairJournals(runCtx)
	if err != nil {
		return nil, err
	}
	evidence := make([]JournalEvidence, 0, len(journals))
	for _, journal := range journals {
		paths := make([]string, 0, len(journal.Manifest.Entries))
		for _, item := range journal.Manifest.Entries {
			if item.Internal {
				continue
			}
			paths = append(paths, item.Path)
		}
		evidence = append(evidence, JournalEvidence{
			TransactionID:   journal.Manifest.TransactionID,
			PlanFingerprint: journal.Manifest.PlanFingerprint,
			State:           journal.State, AffectedPaths: sortedUnique(paths),
			RequiredChecks: append([]string(nil), journal.Manifest.Checks...),
		})
	}

	records, recordErr := quarantineRecords(runCtx.VaultPath)
	if recordErr != nil {
		return nil, recordErr
	}
	for _, record := range records {
		id := record.Evidence
		journalRoot, rootErr := repairJournalRoot(runCtx)
		if rootErr == nil && filepath.Dir(record.Evidence) == journalRoot {
			if m, err := readRepairJournalManifest(record.Evidence); err == nil {
				id = m.TransactionID
			}
		} else if filepath.Dir(record.Evidence) == filepath.Join(runCtx.VaultPath, ".rhizome", "edit-journal") {
			if m, err := readEditWriteJournal(record.Evidence); err == nil {
				id = m.TransactionID
			}
		}
		state := "quarantined"
		if settled, err := settledEvidence(runCtx.VaultPath, record.Evidence); err != nil {
			return nil, err
		} else if settled {
			state = "source_settled"
		}
		evidence = append(evidence, JournalEvidence{TransactionID: id, State: state, AffectedPaths: record.Paths, EvidencePath: record.Evidence, Artifacts: append([]string(nil), record.Artifacts...)})
	}
	return evidence, nil
}

func discoverRepairJournals(runCtx writerContext) ([]journal, error) {
	root, err := repairJournalRoot(runCtx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read repair journal root: %w", err)
	}
	var journals []journal
	for _, entry := range entries {
		if runCtx.onlyDirectory != "" && filepath.Join(root, entry.Name()) != runCtx.onlyDirectory {
			continue
		}
		if !runCtx.includeQuarantined && isQuarantined(runCtx.VaultPath, filepath.Join(root, entry.Name())) {
			continue
		}
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected repair journal root entry %s", filepath.Join(root, entry.Name()))
		}
		dir := filepath.Join(root, entry.Name())
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("repair journal entry %s is not a direct directory", dir)
		}
		cleanupName, cleanup, cleanupErr := namespaceadmission.ParseCleanupName(entry.Name())
		if cleanupErr != nil {
			return nil, cleanupErr
		}
		if cleanup {
			digest := cleanupName.Digest
			// The detached directory name is the terminal authority. Its manifest
			// is best-effort reporting metadata only and must never make discovery
			// re-enter rollback or depend on mutable vault paths.
			manifest, manifestErr := readRepairJournalManifest(dir)
			if manifestErr == nil {
				expectedDigest := strings.TrimPrefix(Hash([]byte(manifest.TransactionID)), "sha256:")
				if digest != expectedDigest || (cleanupName.Decision != "" && (manifest.Namespace == nil || manifest.Namespace.SettledDecision != cleanupName.Decision)) || (cleanupName.Decision == "" && manifest.Namespace != nil) {
					manifest = Manifest{}
				} else {
					manifest = canonicalRepairJournalReportingManifest(runCtx, manifest)
				}
			} else {
				manifest = Manifest{}
			}
			journals = append(journals, journal{
				Directory: dir, Manifest: manifest, State: repairJournalCleanupPending, NativeCleanupDecision: cleanupName.Decision,
			})
			continue
		}
		manifest, err := readRepairJournalManifest(dir)
		if err != nil {
			return nil, err
		}
		expectedName := strings.TrimPrefix(Hash([]byte(manifest.TransactionID)), "sha256:")
		if entry.Name() != expectedName {
			return nil, fmt.Errorf("repair journal directory does not match transaction id %s", manifest.TransactionID)
		}
		if err := validateRepairJournalPaths(runCtx, manifest); err != nil {
			return nil, fmt.Errorf("validate repair journal %s: %w", manifest.TransactionID, err)
		}
		state := repairJournalPrepared
		if manifest.Namespace != nil {
			state, err = nativeJournalDecision(dir)
			if err != nil {
				return nil, err
			}
			if settled := manifest.Namespace.SettledDecision; settled != "" && settled != state {
				return nil, fmt.Errorf("native settled decision disagrees with marker")
			}
		} else if _, err := os.Stat(filepath.Join(dir, "COMMITTED")); err == nil {
			state = repairJournalCommitted
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat repair commit marker: %w", err)
		}
		journals = append(journals, journal{Directory: dir, Manifest: manifest, State: state})
	}
	sort.Slice(journals, func(i, j int) bool { return recoveryJournalSortKey(journals[i]) < recoveryJournalSortKey(journals[j]) })
	return journals, nil
}

func validRepairJournalDigest(digest string) bool {
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func recoveryJournalSortKey(journal journal) string {
	if journal.Manifest.TransactionID != "" {
		return journal.Manifest.TransactionID
	}
	return journal.Directory
}

func repairJournalRoot(runCtx writerContext) (string, error) {
	vaultRoot := strings.TrimSpace(runCtx.VaultPath)
	if vaultRoot == "" {
		return "", fmt.Errorf("resolve repair journal vault: vault root is required")
	}
	vaultPaths, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repair journal vault: %w", err)
	}
	if vaultPaths.Root() == "" {
		return "", fmt.Errorf("resolve repair journal vault: vault root is required")
	}
	current := filepath.FromSlash(vaultPaths.Root())
	for _, component := range []string{".rhizome", "repair-journal", "v1"} {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("repair journal path must be a direct vault directory: %s", current)
		}
	}
	return current, nil
}

func repairJournalDir(runCtx writerContext, transactionID string) (string, error) {
	root, err := repairJournalRoot(runCtx)
	if err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(Hash([]byte(transactionID)), "sha256:")
	return filepath.Join(root, digest), nil
}

func writeRepairJournalManifest(dir string, manifest Manifest) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create repair journal: %w", err)
	}
	if err := syncRepairJournalHierarchy(dir); err != nil {
		return fmt.Errorf("sync repair journal root: %w", err)
	}
	manifest.Version = repairJournalVersion
	if manifest.Namespace != nil {
		manifest.Version = namespaceadmission.NamespaceVersion
	}
	if manifest.CreatedAt == "" {
		manifest.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal repair journal: %w", err)
	}
	encoded = append(encoded, '\n')
	tmp := filepath.Join(dir, "manifest.json.tmp")
	if err := writeSyncedFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	if err := publishRepairJournalManifest(tmp, filepath.Join(dir, "manifest.json")); err != nil {
		return fmt.Errorf("publish repair journal: %w", err)
	}
	return syncDirectory(dir)
}

func syncRepairJournalHierarchy(dir string) error {
	current := filepath.Dir(dir)
	for i := 0; i < 3; i++ {
		if err := syncDirectory(current); err != nil {
			return err
		}
		current = filepath.Dir(current)
	}
	return nil
}

func readRepairJournalManifest(dir string) (Manifest, error) {
	encoded, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("read repair journal %s: %w", dir, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode repair journal %s: %w", dir, err)
	}
	if manifest.TransactionID == "" || (manifest.Namespace == nil && manifest.Version != repairJournalVersion) ||
		(manifest.Namespace != nil && manifest.Version != namespaceadmission.NamespaceVersion) {
		return Manifest{}, fmt.Errorf("unsupported or incomplete repair journal %s", dir)
	}
	if err := validateNamespacePurpose(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func markRepairJournalCommitted(dir string) error {
	return writeSyncedFile(filepath.Join(dir, "COMMITTED"), []byte("committed\n"), 0o600)
}

func verifyCommittedRepairJournal(runCtx writerContext, manifest Manifest) error {
	for _, entry := range manifest.Entries {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(abs)
		if !entry.FinalExists {
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("expected deleted path %s, found mode %s", entry.Path, info.Mode())
		}
		if err != nil {
			return fmt.Errorf("stat committed path %s: %w", entry.Path, err)
		}
		if !info.Mode().IsRegular() || !RepairModeMatches(info.Mode(), entry.FinalMode) {
			return fmt.Errorf("committed path %s mode changed", entry.Path)
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		if Hash(content) != entry.FinalHash {
			return fmt.Errorf("committed path %s content changed", entry.Path)
		}
	}
	return nil
}

func validateRepairJournalDir(runCtx writerContext, dir string, manifest Manifest) error {
	expectedDir, err := repairJournalDir(runCtx, manifest.TransactionID)
	if err != nil {
		return err
	}
	if filepath.Clean(dir) != filepath.Clean(expectedDir) {
		return fmt.Errorf("repair journal cleanup directory does not match transaction id")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("repair journal cleanup requires a direct directory: %s", dir)
	}
	return nil
}
