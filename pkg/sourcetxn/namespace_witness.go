package sourcetxn

import (
	"os"
)

func preflightJournalRenameEndpoints(manifest Manifest, rename Rename, sourceAbs, destinationAbs string) error {
	if manifest.Namespace != nil {
		var source, destination *Entry
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

func verifyNamespaceOriginalFiles(runCtx writerContext, manifest Manifest) error {
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
