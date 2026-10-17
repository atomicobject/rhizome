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
