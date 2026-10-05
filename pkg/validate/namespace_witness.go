package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func verifyNativeDestinationState(operation RepairOperation, source, destination *repairFileState) error {
	witness := operation.DestinationState
	switch witness.Kind {
	case RepairDestinationAbsent:
		if destination.originalExists {
			return fmt.Errorf("destination witness changed at %s: destination appeared", destination.rel)
		}
	case RepairDestinationOccupied:
		if !destination.originalExists || SourceHash(destination.originalContent) != witness.Hash ||
			!repairModeMatches(destination.originalMode, witness.Mode) {
			return fmt.Errorf("destination witness changed at %s", destination.rel)
		}
		sourceInfo, sourceErr := os.Lstat(source.abs)
		destinationInfo, destinationErr := os.Lstat(destination.abs)
		if sourceErr != nil || destinationErr != nil || os.SameFile(sourceInfo, destinationInfo) {
			return fmt.Errorf("occupied destination is the source file: %s", destination.rel)
		}
	case RepairDestinationCaseOnly:
		if !destination.originalExists || source.rel == destination.rel ||
			filepath.Dir(source.abs) != filepath.Dir(destination.abs) ||
			!strings.EqualFold(filepath.Base(source.abs), filepath.Base(destination.abs)) {
			return fmt.Errorf("case-only destination witness changed at %s", destination.rel)
		}
		sourceInfo, sourceErr := os.Lstat(source.abs)
		destinationInfo, destinationErr := os.Lstat(destination.abs)
		if sourceErr != nil || destinationErr != nil || !os.SameFile(sourceInfo, destinationInfo) {
			return fmt.Errorf("case-only destination witness changed at %s", destination.rel)
		}
		entries, err := os.ReadDir(filepath.Dir(source.abs))
		if err != nil {
			return err
		}
		matches := 0
		for _, entry := range entries {
			if strings.EqualFold(entry.Name(), filepath.Base(source.abs)) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("case-only destination has multiple directory entries: %s", destination.rel)
		}
	default:
		return fmt.Errorf("invalid native destination witness at %s", destination.rel)
	}
	return nil
}

func preflightJournalRenameEndpoints(manifest repairJournalManifest, rename PathRename, sourceAbs, destinationAbs string) error {
	if manifest.Namespace != nil {
		var source, destination *repairJournalEntry
		for i := range manifest.Entries {
			entry := &manifest.Entries[i]
			if entry.Path == rename.From {
				source = entry
			}
			if entry.Path == rename.To {
				destination = entry
			}
		}
		// Independent occupied originals permit a no-op rollback. This proves
		// snapshot state, not provenance, and never removes a recreated source.
		if source != nil && destination != nil && source.OriginalExists && destination.OriginalExists &&
			destination.OriginalPath == destination.Path &&
			validateRepairFileSnapshot(sourceAbs, source.Path, true, source.OriginalHash, source.OriginalMode) == nil &&
			validateRepairFileSnapshot(destinationAbs, destination.Path, true, destination.OriginalHash, destination.OriginalMode) == nil {
			return nil
		}
	}
	return preflightRepairRenameEndpoints(rename, sourceAbs, destinationAbs, os.Lstat)
}

func verifyNamespaceOriginalFiles(runCtx RunContext, manifest repairJournalManifest) error {
	for _, entry := range manifest.Entries {
		rel := entry.Path
		if entry.OriginalPath != "" {
			rel = entry.OriginalPath
		}
		abs, err := repairAbsPath(runCtx, rel)
		if err != nil {
			return err
		}
		if err := validateRepairFileSnapshot(abs, rel, entry.OriginalExists, entry.OriginalHash, entry.OriginalMode); err != nil {
			return err
		}
	}
	return nil
}
