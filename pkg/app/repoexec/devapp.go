package repoexec

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// DevAppEnv names the rzm of an installed Rhizome Dev app. An empty value
// turns Dev app discovery off; unset means the standard install locations.
const DevAppEnv = "RZM_DEV_APP_RZM"

// DevAppExecutable returns the rzm bundled with an installed Rhizome Dev app,
// reading env's home directory and variables. A Rhizome source checkout
// without its own build runs it, so the Dev app and command-line rzm select
// the same build whatever else is installed.
func DevAppExecutable(env vaultconfig.Env) (string, bool) {
	if path, set := env.Lookup(DevAppEnv); set {
		if path == "" || !filepath.IsAbs(path) {
			return "", false
		}
		return path, executableFile(path)
	}
	for _, candidate := range devAppCandidates(env.HomeDir) {
		if executableFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func devAppCandidates(home string) []string {
	switch runtime.GOOS {
	case "darwin":
		dirs := []string{"/Applications"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Applications"))
		}
		var candidates []string
		for _, dir := range dirs {
			candidates = append(candidates, filepath.Join(dir, "Rhizome Dev.app", "Contents", "MacOS", ExecutableName()))
		}
		return candidates
	case "linux":
		if home == "" {
			return nil
		}
		// desktop/README.md documents this directory for a local Dev install.
		candidate := filepath.Join(home, ".local", "share", "rhizome-dev", "bin", ExecutableName())
		if !appbundle.BesideDesktop(candidate) {
			return nil
		}
		return []string{candidate}
	default:
		return nil
	}
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}
