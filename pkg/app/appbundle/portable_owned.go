package appbundle

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// RegisteredAppPaths protects installed files even when a standalone CLI runs
// the update. Disabled integrations retain their application ownership.
func RegisteredAppPaths() ([]string, error) {
	var paths []string
	for _, identifier := range []string{ReleaseIdentifier, DevIdentifier} {
		root, err := PortableRoot(identifier)
		if err != nil {
			return nil, err
		}
		if root == "" {
			continue
		}
		record, err := ReadInstallation(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		paths = append(paths, record.AppPath)
		if runtime.GOOS == "windows" {
			paths = append(paths, filepath.Join(filepath.Dir(record.AppPath), "rzm.exe"))
		}
	}
	if runtime.GOOS == "linux" {
		entry, err := EntryPath()
		if err != nil {
			return nil, err
		}
		owned, err := ownedImageEntry(entry)
		if err != nil {
			return nil, err
		}
		if owned {
			paths = append(paths, entry)
		}
	}
	return paths, nil
}

// An owned link remains protected when its image or owning app is missing.
// Check its target layout without requiring a live, enabled registration.
func ownedImageEntry(path string) (bool, error) {
	stat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if stat.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return ImageRoot(filepath.Clean(target)) != "", nil
}
