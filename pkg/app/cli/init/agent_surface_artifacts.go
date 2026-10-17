package init

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hasLegacyRhizomeFile reports whether the old generated root RHIZOME.md is
// present.
func hasLegacyRhizomeFile(projectRoot string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "RHIZOME.md"))
	return err == nil && strings.Contains(string(data), "# Rhizome (vault + agent CLI) guidelines")
}

func removeLegacyRhizomeFile(projectRoot string) (bool, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return false, nil
	}
	path := filepath.Join(projectRoot, "RHIZOME.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !strings.Contains(string(data), "# Rhizome (vault + agent CLI) guidelines") {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

// ensureDirExists creates a directory and all parent directories.
// If a parent path exists as a file (not a directory), it returns a descriptive error
// instead of the confusing "file exists" error from os.MkdirAll.
// If the target directory already exists (or is a symlink to a directory), it returns nil without error.
func ensureDirExists(dir string) error {
	// First check if a symlink exists at the target path (without following it)
	linkInfo, err := os.Lstat(dir)
	if err == nil {
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			// Symlink exists - check if it points to a valid directory
			targetInfo, err := os.Stat(dir)
			if err == nil && targetInfo.IsDir() {
				// Symlink points to a valid directory, nothing to do
				return nil
			}
			// Symlink exists but points to something invalid - skippable error
			return &SkippablePathError{
				Path:    dir,
				Message: fmt.Sprintf("%s is a symlink that does not resolve (target may not exist or is not a directory)", dir),
			}
		}
		// Not a symlink - check if it's a directory or file
		if linkInfo.IsDir() {
			// Directory already exists, nothing to do
			return nil
		}
		// Exists as a file
		return fmt.Errorf("cannot create directory %s: exists as a file", dir)
	}
	if !os.IsNotExist(err) {
		return err
	}

	// Walk up the path to find the first existing component.
	// Use Lstat to detect symlinks (including broken ones that point to non-existent targets).
	current := filepath.Dir(dir)
	for current != "" && current != "." && current != "/" {
		linkInfo, err := os.Lstat(current)
		if err == nil {
			if linkInfo.Mode()&os.ModeSymlink != 0 {
				// Parent is a symlink - check if it points to a valid directory
				targetInfo, err := os.Stat(current)
				if err != nil {
					// Symlink exists but target doesn't resolve (e.g., container volume mount path) - skippable
					return &SkippablePathError{
						Path:    current,
						Message: fmt.Sprintf("%s is a symlink that does not resolve (target may not exist)", current),
					}
				}
				if !targetInfo.IsDir() {
					// Symlink to non-directory - skippable
					return &SkippablePathError{
						Path:    current,
						Message: fmt.Sprintf("%s is a symlink to a non-directory", current),
					}
				}
				// Symlink to valid directory - we can create subdirectories inside it
				break
			}
			if linkInfo.IsDir() {
				// Regular directory exists, we can proceed
				break
			}
			// Exists as a file
			return fmt.Errorf("cannot create directory %s: %s exists as a file", dir, current)
		}
		if !os.IsNotExist(err) {
			return err
		}
		// Doesn't exist, check parent
		current = filepath.Dir(current)
	}

	return os.MkdirAll(dir, 0o755)
}

// cleanupStaleArtifacts removes rhizome-managed files from dir that are not in currentNames.
// It only removes files matching the rhizome- prefix (e.g., rhizome-onboard.md).
// Also cleans up legacy rhizome.* files (e.g., rhizome.onboard.md) from before the naming consolidation.
func cleanupStaleArtifacts(dir string, currentNames map[string]struct{}) ([]string, error) {
	var removed []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Check for current rhizome- prefix or legacy rhizome. prefix
		if !strings.HasPrefix(name, rhizomePrefix) && !strings.HasPrefix(name, "rhizome.") {
			continue
		}
		if _, ok := currentNames[name]; ok {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("remove stale artifact %s: %w", path, err)
		}
		removed = append(removed, name)
	}

	return removed, nil
}

func isRetiredCoreSkillName(name string) bool {
	for _, retired := range retiredCoreSkillNames {
		if name == retired {
			return true
		}
	}
	return false
}

func knownStarterSkillNames() (map[string]struct{}, error) {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, err
	}
	names := map[string]struct{}{}
	for starter := range registry {
		skills, err := loadStarterSkillTemplates(starter)
		if err != nil {
			return nil, err
		}
		for _, skill := range skills {
			names[skill.Name] = struct{}{}
		}
	}
	return names, nil
}

func isRetiredStarterSkillName(name string) bool {
	for _, retired := range retiredStarterSkillNames {
		if name == retired {
			return true
		}
	}
	return false
}
