package sourcetxn

import (
	"fmt"
	"strings"
)

func validateRepairJournalPaths(runCtx writerContext, manifest Manifest) error {
	if manifest.Namespace != nil {
		if err := validateNamespaceGitAuthority(runCtx, manifest); err != nil {
			return err
		}
	}
	shortID := strings.TrimPrefix(Hash([]byte(manifest.TransactionID)), "sha256:")[:12]
	for i, entry := range manifest.Entries {
		abs, err := repairAbsPath(runCtx, entry.Path)
		if err != nil {
			return &SourceConflictError{Path: entry.Path, Err: err}
		}
		if entry.OriginalPath != "" {
			if _, err := repairAbsPath(runCtx, entry.OriginalPath); err != nil {
				return &SourceConflictError{Path: entry.OriginalPath, Err: err}
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
			return &SourceConflictError{Path: entry.Path, Err: fmt.Errorf("journal artifact paths do not match affected path %s", entry.Path)}
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
	return validateRepairJournalDelta(runCtx, manifest)
}

func validateRepairJournalDelta(runCtx writerContext, manifest Manifest) error {
	if manifest.Source && (manifest.Namespace != nil || len(manifest.Checks)+len(manifest.OperationIDs)+len(manifest.ActionIDs)+len(manifest.IssueKeys)+len(manifest.Renamed)+len(manifest.Deleted) > 0) {
		return fmt.Errorf("source journal has mixed repair authority")
	}
	if manifest.Namespace != nil {
		if err := validateNamespacePurpose(manifest); err != nil {
			return err
		}
	} else if !manifest.Source && len(manifest.Entries) > 0 && len(manifest.Checks) == 0 {
		return fmt.Errorf("journal has no originating checks")
	}
	if len(sortedUnique(manifest.Checks)) != len(manifest.Checks) {
		return fmt.Errorf("journal originating checks are not unique")
	}
	for _, check := range manifest.Checks {
		canonical, ok := runCtx.canonicalCheck(check)
		if !ok || canonical != check {
			return fmt.Errorf("journal originating check is not canonical: %s", check)
		}
	}
	if manifest.Namespace == nil {
		if err := validateRepairJournalMembership(manifest); err != nil {
			return err
		}
	}
	entries := make(map[string]Entry, len(manifest.Entries))
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
		if entry.OriginalMode != RepairModeBits(RepairFileMode(entry.OriginalMode)) ||
			entry.FinalMode != RepairModeBits(RepairFileMode(entry.FinalMode)) {
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

func validateRepairInternalEntryPath(runCtx writerContext, entry Entry, abs string) error {
	if entry.OriginalExists || entry.OriginalPath != entry.Path || !entry.FinalExists || entry.FinalMode != 0o600 {
		return fmt.Errorf("journal internal completion entry has invalid file State: %s", entry.Path)
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

func canonicalRepairJournalReportingManifest(runCtx writerContext, manifest Manifest) Manifest {
	var checks []string
	for _, check := range manifest.Checks {
		canonical, ok := runCtx.canonicalCheck(check)
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

func validateRepairJournalOperationMembership(manifest Manifest) error {
	if len(manifest.OperationIDs) == 0 {
		return nil
	}
	if strings.Join(sortedUnique(manifest.OperationIDs), "\x00") != strings.Join(manifest.OperationIDs, "\x00") {
		return fmt.Errorf("journal operation membership is not sorted and unique")
	}
	expectedID := "transaction:v1:" + strings.TrimPrefix(Hash([]byte(strings.Join(manifest.OperationIDs, "\x00"))), "sha256:")
	if manifest.TransactionID != expectedID {
		return fmt.Errorf("journal operation membership does not match transaction id")
	}
	return nil
}

func validateRepairJournalMembership(manifest Manifest) error {
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

func validateRepairJournalEntryDelta(manifest Manifest, entries map[string]Entry) error {
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
