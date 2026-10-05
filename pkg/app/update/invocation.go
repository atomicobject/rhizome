package update

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Invocation identifies the executable handling an update and the repository
// target selected by the pin, if any. Paths remain logical install
// destinations; filesystem identity is used only to classify RepoScoped.
type Invocation struct {
	ExecutablePath string
	RepoTargetPath string
	RepoPinned     bool
	RepoScoped     bool
	RepoPinVersion string
}

// ResolveInvocation derives executable and repository target identity from the
// local pin. Location fields choose where a pinned binary lives; they do not
// create a repository target without rhizome.version.
func ResolveInvocation(executablePath, cfgDir string, cfg *obsidian.LocalConfig) (Invocation, error) {
	if strings.TrimSpace(executablePath) == "" {
		resolved, err := os.Executable()
		if err != nil {
			return Invocation{}, err
		}
		executablePath = resolved
	}
	executableAbs, err := filepath.Abs(executablePath)
	if err != nil {
		return Invocation{}, err
	}
	invocation := Invocation{ExecutablePath: filepath.Clean(executableAbs)}
	if cfg == nil {
		return invocation, nil
	}
	externallyManaged, err := cfg.Rhizome.UsesExternalBinaryManager()
	if err != nil {
		return Invocation{}, err
	}
	if externallyManaged {
		return invocation, nil
	}

	pin := NormalizeVersion(cfg.Rhizome.Version)
	if pin == "" {
		return invocation, nil
	}
	if strings.TrimSpace(cfgDir) == "" {
		return Invocation{}, fmt.Errorf("config directory is required for a pinned repository")
	}
	cfgDirAbs, err := filepath.Abs(cfgDir)
	if err != nil {
		return Invocation{}, err
	}
	target := invocationRepoTarget(filepath.Clean(cfgDirAbs), cfg)

	invocation.RepoTargetPath = target
	invocation.RepoPinned = true
	invocation.RepoPinVersion = pin
	invocation.RepoScoped = sameInvocationFile(invocation.ExecutablePath, target)
	return invocation, nil
}

func invocationRepoTarget(cfgDir string, cfg *obsidian.LocalConfig) string {
	if binaryPath := strings.TrimSpace(cfg.Rhizome.BinaryPath); binaryPath != "" {
		if filepath.IsAbs(binaryPath) {
			return filepath.Clean(binaryPath)
		}
		return filepath.Clean(filepath.Join(cfgDir, binaryPath))
	}

	binaryDir := strings.TrimSpace(cfg.Rhizome.BinaryDir)
	if binaryDir == "" {
		binaryDir = filepath.Join(".rhizome", "bin")
	}
	if !filepath.IsAbs(binaryDir) {
		binaryDir = filepath.Join(cfgDir, binaryDir)
	}
	return filepath.Clean(filepath.Join(binaryDir, CurrentPlatformDir(), executableName()))
}

func sameInvocationFile(a, b string) bool {
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	if aErr == nil && bErr == nil && os.SameFile(aInfo, bInfo) {
		return true
	}

	aResolved, aResolveErr := filepath.EvalSymlinks(a)
	bResolved, bResolveErr := filepath.EvalSymlinks(b)
	if aResolveErr == nil && bResolveErr == nil && sameInvocationPath(aResolved, bResolved) {
		return true
	}
	return sameInvocationPath(a, b)
}

func sameInvocationPath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
