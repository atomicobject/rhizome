package appbundle

import (
	"os"
	"path/filepath"
	"runtime"
)

// DesktopExecutableName is the desktop app's own executable outside macOS,
// where the app has no bundle metadata and installs its rzm beside it.
func DesktopExecutableName() string {
	if runtime.GOOS == "windows" {
		return "rhizome-desktop.exe"
	}
	return "rhizome-desktop"
}

// BesideDesktop reports whether executable is the rzm a desktop app installed
// beside its own executable: the layout outside macOS, which has no
// Info.plist to read.
func BesideDesktop(executable string) bool {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(resolved), DesktopExecutableName()))
	return err == nil && info.Mode().IsRegular()
}
