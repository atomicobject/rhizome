package appbundle

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func nativePortableBaseDirectory() (string, error) {
	root, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Rhizome", "desktop"), nil
}
