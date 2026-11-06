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
	return paths, nil
}
