package update

import (
	"os"
	"path/filepath"
	"strings"
)

// VersionMarkerName is the marker file written beside an installed repo-local
// binary recording the installed version (for example "v0.48.0\n"). Delegation
// compares this marker to the repo pin without spawning the binary.
const VersionMarkerName = ".version"

// VersionMarkerPath returns the marker path for a binary path.
func VersionMarkerPath(binaryPath string) string {
	return filepath.Join(filepath.Dir(binaryPath), VersionMarkerName)
}

// ReadVersionMarker reads and normalizes the version marker beside binaryPath.
// ok is false when the marker is missing, unreadable, or empty.
func ReadVersionMarker(binaryPath string) (version string, ok bool) {
	data, err := os.ReadFile(VersionMarkerPath(binaryPath))
	if err != nil {
		return "", false
	}
	version = NormalizeVersion(strings.TrimSpace(string(data)))
	if version == "" {
		return "", false
	}
	return version, true
}

// WriteVersionMarker atomically writes the normalized version marker beside
// binaryPath (stage to temp file + rename).
func WriteVersionMarker(binaryPath, version string) error {
	marker := VersionMarkerPath(binaryPath)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(marker), VersionMarkerName+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.WriteString(NormalizeVersion(version) + "\n")
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpPath)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if err := os.Rename(tmpPath, marker); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
