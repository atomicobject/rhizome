package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var appCommandLineLinkPath = "/usr/local/bin/rzm"

// Invocation identifies the executable handling an update and the repository
// target selected by the pin, if any. Paths remain logical install
// destinations; filesystem identity is used only to classify RepoScoped.
type Invocation struct {
	ExecutablePath     string
	RepoTargetPath     string
	RepoTargetAppOwned bool
	RepoPinned         bool
	RepoScoped         bool
	RepoPinVersion     string
	Bundle             *appbundle.Info
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
	if cfg != nil {
		externallyManaged, err := cfg.Rhizome.UsesExternalBinaryManager()
		if err != nil {
			return Invocation{}, err
		}
		if externallyManaged {
			return invocation, nil
		}
	}
	bundle, err := appbundle.Resolve(executableAbs)
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve executable bundle: %w", err)
	}
	invocation.Bundle = bundle
	if cfg == nil {
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

	resolvedDir, err := resolveUpdateDirectory(filepath.Dir(target))
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve repository target directory: %w", err)
	}
	entry := filepath.Join(resolvedDir, filepath.Base(target))
	invocation.RepoTargetAppOwned = appbundle.Root(entry) != ""
	if bundle != nil {
		for _, appEntry := range []string{invocation.ExecutablePath, appCommandLineLinkPath} {
			entryDir, err := resolveUpdateDirectory(filepath.Dir(appEntry))
			if err != nil {
				return Invocation{}, fmt.Errorf("resolve app command directory: %w", err)
			}
			invocation.RepoTargetAppOwned = invocation.RepoTargetAppOwned || sameInvocationPath(entry, filepath.Join(entryDir, filepath.Base(appEntry)))
		}
	}
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

// Resolve only the destination directory: rename replaces a final symlink,
// whereas a symlink in its parent directories redirects the write itself.
func resolveUpdateDirectory(dir string) (string, error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) || filepath.Dir(dir) == dir {
		return "", err
	}
	parent, err := resolveUpdateDirectory(filepath.Dir(dir))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(dir)), nil
}
