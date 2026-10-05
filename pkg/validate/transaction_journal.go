package validate

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
	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
)

const repairJournalVersion = namespaceadmission.RepairVersion

const (
	repairJournalPrepared       = "prepared"
	repairJournalCommitted      = "committed"
	repairJournalRestored       = "restored"
	repairJournalCleanupPending = "cleanup_pending"
)

const repairJournalCleanupPrefix = ".cleanup-"

// RepairJournalEvidence exposes an interrupted transaction without mutating it.
type RepairJournalEvidence struct {
	TransactionID   string   `json:"transactionId"`
	PlanFingerprint string   `json:"planFingerprint,omitempty"`
	State           string   `json:"state"`
	AffectedPaths   []string `json:"affectedPaths,omitempty"`
	RequiredChecks  []string `json:"requiredChecks,omitempty"`
}

type repairJournalManifest struct {
	Version         int                      `json:"version"`
	TransactionID   string                   `json:"transactionId"`
	PlanFingerprint string                   `json:"planFingerprint"`
	CreatedAt       string                   `json:"createdAt"`
	OperationIDs    []string                 `json:"operationIds,omitempty"`
	ActionIDs       []string                 `json:"actionIds,omitempty"`
	IssueKeys       []string                 `json:"issueKeys,omitempty"`
	Changed         []string                 `json:"changed,omitempty"`
	Renamed         []PathRename             `json:"renamed,omitempty"`
	Deleted         []string                 `json:"deleted,omitempty"`
	Checks          []string                 `json:"checks,omitempty"`
	OwnedArtifacts  []string                 `json:"ownedArtifacts,omitempty"`
	Entries         []repairJournalEntry     `json:"entries"`
	Namespace       *namespaceJournalPurpose `json:"namespace,omitempty"`
}

type repairJournalEntry struct {
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

type recoveredRepairJournal struct {
	nativeCleanupDecision string
	decisionSynced        bool // In-memory proof; the settled decision remains writer admission.
	dir                   string
	manifest              repairJournalManifest
	state                 string
}

type repairJournalRecovery struct {
	native         []recoveredRepairJournal
	nativeCleanup  []recoveredRepairJournal
	committed      []recoveredRepairJournal
	rolledBack     []recoveredRepairJournal
	noMutation     []recoveredRepairJournal
	partial        []recoveredRepairJournal
	cleanupPending []recoveredRepairJournal
}

// DetectPendingRepairJournals reports durable repair state without recovering
// or deleting anything. Read-only validation and CI use this fail-closed seam.
func DetectPendingRepairJournals(runCtx RunContext) ([]RepairJournalEvidence, error) {
	journals, err := discoverRepairJournals(runCtx)
	if err != nil {
		return nil, err
	}
	evidence := make([]RepairJournalEvidence, 0, len(journals))
	for _, journal := range journals {
		paths := make([]string, 0, len(journal.manifest.Entries))
		for _, item := range journal.manifest.Entries {
			if item.Internal {
				continue
			}
			paths = append(paths, item.Path)
		}
		evidence = append(evidence, RepairJournalEvidence{
			TransactionID:   journal.manifest.TransactionID,
			PlanFingerprint: journal.manifest.PlanFingerprint,
			State:           journal.state, AffectedPaths: sortedUnique(paths),
			RequiredChecks: append([]string(nil), journal.manifest.Checks...),
		})
	}
	return evidence, nil
}

func discoverRepairJournals(runCtx RunContext) ([]recoveredRepairJournal, error) {
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
	var journals []recoveredRepairJournal
	for _, entry := range entries {
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
				expectedDigest := strings.TrimPrefix(SourceHash([]byte(manifest.TransactionID)), "sha256:")
				if digest != expectedDigest || (cleanupName.Decision != "" && (manifest.Namespace == nil || manifest.Namespace.SettledDecision != cleanupName.Decision)) || (cleanupName.Decision == "" && manifest.Namespace != nil) {
					manifest = repairJournalManifest{}
				} else {
					manifest = canonicalRepairJournalReportingManifest(manifest)
				}
			} else {
				manifest = repairJournalManifest{}
			}
			journals = append(journals, recoveredRepairJournal{
				dir: dir, manifest: manifest, state: repairJournalCleanupPending, nativeCleanupDecision: cleanupName.Decision,
			})
			continue
		}
		manifest, err := readRepairJournalManifest(dir)
		if err != nil {
			return nil, err
		}
		expectedName := strings.TrimPrefix(SourceHash([]byte(manifest.TransactionID)), "sha256:")
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
		journals = append(journals, recoveredRepairJournal{dir: dir, manifest: manifest, state: state})
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

func recoveryJournalSortKey(journal recoveredRepairJournal) string {
	if journal.manifest.TransactionID != "" {
		return journal.manifest.TransactionID
	}
	return journal.dir
}

func repairJournalRoot(runCtx RunContext) (string, error) {
	vaultRoot := strings.TrimSpace(runCtx.VaultPath)
	if vaultRoot == "" {
		vaultRoot = strings.TrimSpace(runCtx.VaultDef.BasePath())
	}
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

func repairJournalDir(runCtx RunContext, transactionID string) (string, error) {
	root, err := repairJournalRoot(runCtx)
	if err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(SourceHash([]byte(transactionID)), "sha256:")
	return filepath.Join(root, digest), nil
}

func writeRepairJournalManifest(dir string, manifest repairJournalManifest) error {
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

func readRepairJournalManifest(dir string) (repairJournalManifest, error) {
	encoded, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return repairJournalManifest{}, fmt.Errorf("read repair journal %s: %w", dir, err)
	}
	var manifest repairJournalManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return repairJournalManifest{}, fmt.Errorf("decode repair journal %s: %w", dir, err)
	}
	if manifest.TransactionID == "" || (manifest.Namespace == nil && manifest.Version != repairJournalVersion) ||
		(manifest.Namespace != nil && manifest.Version != namespaceadmission.NamespaceVersion) {
		return repairJournalManifest{}, fmt.Errorf("unsupported or incomplete repair journal %s", dir)
	}
	if err := validateNamespacePurpose(manifest); err != nil {
		return repairJournalManifest{}, err
	}
	return manifest, nil
}

func markRepairJournalCommitted(dir string) error {
	return writeSyncedFile(filepath.Join(dir, "COMMITTED"), []byte("committed\n"), 0o600)
}

func validateRepairJournalPaths(runCtx RunContext, manifest repairJournalManifest) error {
	if manifest.Namespace != nil {
		if err := validateNamespaceGitAuthority(runCtx, manifest); err != nil {
			return err
		}
	}
	shortID := strings.TrimPrefix(SourceHash([]byte(manifest.TransactionID)), "sha256:")[:12]
	for i, entry := range manifest.Entries {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return err
		}
		if entry.OriginalPath != "" {
			if _, err := repairAbsPath(runCtx, entry.OriginalPath); err != nil {
				return err
			}
		}
		expectedBackup := ""
		if entry.OriginalExists {
			expectedBackup = repairArtifactPath(abs, shortID, i, "backup")
		}
		expectedStage := ""
		if entry.FinalExists {
			expectedStage = repairArtifactPath(abs, shortID, i, "stage")
		}
		expectedCase := ""
		if entry.OriginalPath != "" && entry.OriginalPath != entry.Path {
			expectedCase = repairArtifactPath(abs, shortID, i, "case-original")
		}
		if entry.BackupPath != expectedBackup || entry.StagePath != expectedStage || entry.CasePath != expectedCase {
			return fmt.Errorf("journal artifact paths do not match affected path %s", entry.Path)
		}
		if entry.Internal {
			if err := validateRepairInternalEntryPath(runCtx, entry, abs); err != nil {
				return err
			}
		}
	}
	if err := validateRepairArtifactManifest(manifest); err != nil {
		return err
	}
	return validateRepairJournalDelta(manifest)
}

func validateRepairJournalDelta(manifest repairJournalManifest) error {
	if manifest.Namespace != nil {
		if err := validateNamespacePurpose(manifest); err != nil {
			return err
		}
	} else if len(manifest.Entries) > 0 && len(manifest.Checks) == 0 {
		return fmt.Errorf("journal has no originating checks")
	}
	if len(sortedUnique(manifest.Checks)) != len(manifest.Checks) {
		return fmt.Errorf("journal originating checks are not unique")
	}
	for _, check := range manifest.Checks {
		canonical, ok := CanonicalCheck(check)
		if !ok || canonical != check {
			return fmt.Errorf("journal originating check is not canonical: %s", check)
		}
	}
	if manifest.Namespace == nil {
		if err := validateRepairJournalMembership(manifest); err != nil {
			return err
		}
	}
	entries := make(map[string]repairJournalEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		canonical, err := canonicalRepairPath(entry.Path)
		if err != nil || canonical != entry.Path {
			return fmt.Errorf("journal entry path is not canonical: %s", entry.Path)
		}
		if entry.OriginalPath != "" {
			original, originalErr := canonicalRepairPath(entry.OriginalPath)
			if originalErr != nil || original != entry.OriginalPath {
				return fmt.Errorf("journal original path is not canonical: %s", entry.OriginalPath)
			}
		}
		if entry.OriginalMode != repairModeBits(repairFileMode(entry.OriginalMode)) ||
			entry.FinalMode != repairModeBits(repairFileMode(entry.FinalMode)) {
			return fmt.Errorf("journal contains unsupported mode bits for %s", entry.Path)
		}
		if entry.OriginalExists != (entry.OriginalHash != "") || entry.FinalExists != (entry.FinalHash != "") {
			return fmt.Errorf("journal hash presence does not match file state for %s", entry.Path)
		}
		if _, exists := entries[entry.Path]; exists {
			return fmt.Errorf("journal contains duplicate path %s", entry.Path)
		}
		entries[entry.Path] = entry
	}
	return validateRepairJournalEntryDelta(manifest, entries)
}

func validateRepairInternalEntryPath(runCtx RunContext, entry repairJournalEntry, abs string) error {
	if entry.OriginalExists || entry.OriginalPath != entry.Path || !entry.FinalExists || entry.FinalMode != 0o600 {
		return fmt.Errorf("journal internal completion entry has invalid file state: %s", entry.Path)
	}
	_, rel, err := repairCompletionTarget(runCtx, abs)
	if err != nil {
		return fmt.Errorf("journal internal completion entry path is invalid: %s: %w", entry.Path, err)
	}
	if rel != entry.Path {
		return fmt.Errorf("journal internal completion entry path is not canonical: %s", entry.Path)
	}
	return nil
}

func canonicalRepairJournalReportingManifest(manifest repairJournalManifest) repairJournalManifest {
	var checks []string
	for _, check := range manifest.Checks {
		canonical, ok := CanonicalCheck(check)
		if !ok {
			checks = nil
			break
		}
		checks = append(checks, canonical)
	}
	manifest.Checks = sortedUnique(checks)
	if err := validateRepairJournalOperationMembership(manifest); err != nil {
		manifest.OperationIDs = nil
	}
	// A cleanup tombstone is terminal namespace authority, but its mutable
	// best-effort manifest cannot authenticate semantic action/issue ownership.
	// Apply restores those identities only from an exact reviewed plan match.
	manifest.ActionIDs = nil
	manifest.IssueKeys = nil
	return manifest
}

func validateRepairJournalOperationMembership(manifest repairJournalManifest) error {
	if len(manifest.OperationIDs) == 0 {
		return nil
	}
	if strings.Join(sortedUnique(manifest.OperationIDs), "\x00") != strings.Join(manifest.OperationIDs, "\x00") {
		return fmt.Errorf("journal operation membership is not sorted and unique")
	}
	expectedID := "transaction:v1:" + strings.TrimPrefix(SourceHash([]byte(strings.Join(manifest.OperationIDs, "\x00"))), "sha256:")
	if manifest.TransactionID != expectedID {
		return fmt.Errorf("journal operation membership does not match transaction id")
	}
	return nil
}

func validateRepairJournalMembership(manifest repairJournalManifest) error {
	// Membership was added compatibly to journal v1. Older v1 manifests omit
	// all three lists and remain recoverable; newly written manifests persist
	// the complete, sorted transaction membership used for restart reporting.
	membershipLists := [][]string{manifest.OperationIDs, manifest.ActionIDs, manifest.IssueKeys}
	membershipPresent := 0
	for _, values := range membershipLists {
		if len(values) == 0 {
			continue
		}
		membershipPresent++
		if strings.Join(sortedUnique(values), "\x00") != strings.Join(values, "\x00") {
			return fmt.Errorf("journal repair membership is not sorted and unique")
		}
	}
	if membershipPresent != 0 && membershipPresent != len(membershipLists) {
		return fmt.Errorf("journal repair membership is incomplete")
	}
	if membershipPresent == len(membershipLists) {
		if err := validateRepairJournalOperationMembership(manifest); err != nil {
			return err
		}
	}
	return nil
}

func validateRepairJournalEntryDelta(manifest repairJournalManifest, entries map[string]repairJournalEntry) error {
	renameFrom, renameTo := map[string]string{}, map[string]string{}
	for _, rename := range manifest.Renamed {
		from, fromErr := canonicalRepairPath(rename.From)
		to, toErr := canonicalRepairPath(rename.To)
		if fromErr != nil || toErr != nil || from != rename.From || to != rename.To || from == "" || to == "" {
			return fmt.Errorf("journal rename is not canonical")
		}
		if renameFrom[from] != "" || renameTo[to] != "" {
			return fmt.Errorf("journal contains duplicate rename endpoints")
		}
		renameFrom[from], renameTo[to] = to, from
		source, sourceOK := entries[from]
		destination, destinationOK := entries[to]
		caseOnly := destinationOK && destination.OriginalPath == from &&
			(repairCollisionKey(from) == repairCollisionKey(to) ||
				(manifest.Namespace != nil && namespaceCaseCandidateKey(from) == namespaceCaseCandidateKey(to)))
		if caseOnly {
			source = destination
			sourceOK = true
		}
		if !sourceOK || !destinationOK || !source.OriginalExists || !destination.FinalExists {
			return fmt.Errorf("journal rename endpoints do not match entries")
		}
	}
	var changed, deleted []string
	for path, entry := range entries {
		if entry.Internal {
			continue
		}
		if entry.OriginalExists && !entry.FinalExists && renameFrom[path] == "" {
			deleted = append(deleted, path)
		}
		if !entry.FinalExists {
			continue
		}
		if sourcePath := renameTo[path]; sourcePath != "" {
			source := entries[sourcePath]
			if entry.OriginalPath == sourcePath {
				source = entry
			}
			if entry.FinalHash != source.OriginalHash {
				changed = append(changed, path)
			}
			continue
		}
		if !entry.OriginalExists || entry.FinalHash != entry.OriginalHash {
			changed = append(changed, path)
		}
	}
	if strings.Join(sortedUnique(changed), "\x00") != strings.Join(sortedUnique(manifest.Changed), "\x00") ||
		strings.Join(sortedUnique(deleted), "\x00") != strings.Join(sortedUnique(manifest.Deleted), "\x00") ||
		len(sortedUnique(manifest.Changed)) != len(manifest.Changed) ||
		len(sortedUnique(manifest.Deleted)) != len(manifest.Deleted) {
		return fmt.Errorf("journal refresh delta does not match entries")
	}
	return nil
}

func verifyCommittedRepairJournal(runCtx RunContext, manifest repairJournalManifest) error {
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
		if !info.Mode().IsRegular() || !repairModeMatches(info.Mode(), entry.FinalMode) {
			return fmt.Errorf("committed path %s mode changed", entry.Path)
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		if SourceHash(content) != entry.FinalHash {
			return fmt.Errorf("committed path %s content changed", entry.Path)
		}
	}
	return nil
}

func validateRepairJournalDir(runCtx RunContext, dir string, manifest repairJournalManifest) error {
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
