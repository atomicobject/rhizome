package sourcetxn

import (
	"fmt"
	"os"
	"path/filepath"
)

// The case-link window can precede durable ownership. Remove only the exact
// two-link inode pair while the live original is still present and unchanged.
// This never owns or deletes an arbitrary file at the expected artifact path.
func releaseCaseLinkPairs(run writerContext, m Manifest) error {
	for _, entry := range m.Entries {
		if entry.CasePath == "" {
			continue
		}
		artifact, err := os.Lstat(entry.CasePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		original, err := repairAbsPath(run, entry.OriginalPath)
		if err != nil {
			return err
		}
		live, err := os.Lstat(original)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !os.SameFile(artifact, live) {
			continue
		}
		links, err := fileLinkCount(original, live)
		if err != nil {
			return err
		}
		if links != 2 || !live.Mode().IsRegular() || !RepairModeMatches(live.Mode(), entry.OriginalMode) {
			return fmt.Errorf("case artifact has unproven link ownership: %s", entry.Path)
		}
		content, err := os.ReadFile(original)
		if err != nil {
			return err
		}
		if Hash(content) != entry.OriginalHash {
			return fmt.Errorf("case artifact source changed: %s", entry.Path)
		}
		if err := run.Lease.Validate(run.VaultPath); err != nil {
			return err
		}
		if err := os.Remove(entry.CasePath); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(entry.CasePath)); err != nil {
			return err
		}
	}
	return nil
}
