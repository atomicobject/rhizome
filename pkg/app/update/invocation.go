package update

import (
	"errors"
	"fmt"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/paths"
)

var appCommandLineLinkPath = "/usr/local/bin/rzm"
var registeredAppPaths = appbundle.RegisteredAppPaths
var canonicalAppEntryPath = paths.CanonicalExistingPath

// Invocation identifies the executable handling an update and the repository
// target selected by the pin, if any. Paths remain logical install
// destinations; filesystem identity classifies RepoScoped and app ownership.
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
// create a repository target without an exact rhizome.version.
func ResolveInvocation(executablePath, cfgDir string, cfg *repoconfig.LocalConfig) (Invocation, error) {
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
	if pin == "" || pin == "latest" {
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
	invocation.RepoTargetAppOwned = appbundle.OwnsPath(entry)
	appEntries, err := registeredAppPaths()
	if err != nil {
		return Invocation{}, fmt.Errorf("read desktop installation ownership: %w", err)
	}
	if bundle != nil {
		appEntries = append(appEntries, invocation.ExecutablePath)
	}
	// The command-line link belongs to the app whenever it resolves into a
	// Rhizome bundle, whichever rzm runs the update; a plain rzm there does not.
	if linked, err := appbundle.Resolve(appCommandLineLinkPath); err == nil && linked != nil && linked.Rhizome() {
		appEntries = append(appEntries, appCommandLineLinkPath)
	}
	for _, appEntry := range appEntries {
		entryDir, err := resolveUpdateDirectory(filepath.Dir(appEntry))
		if err != nil {
			return Invocation{}, fmt.Errorf("resolve app command directory: %w", err)
		}
		owned, err := sameAppOwnedEntry(entry, filepath.Join(entryDir, filepath.Base(appEntry)))
		if err != nil {
			return Invocation{}, fmt.Errorf("resolve app ownership: %w", err)
		}
		invocation.RepoTargetAppOwned = invocation.RepoTargetAppOwned || owned
	}
	invocation.RepoTargetPath = target
	invocation.RepoPinned = true
	invocation.RepoPinVersion = pin
	invocation.RepoScoped = sameInvocationFile(invocation.ExecutablePath, target)
	return invocation, nil
}

func sameAppOwnedEntry(destination, appEntry string) (bool, error) {
	if appbundle.SamePath(destination, appEntry) {
		return true, nil
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	// Resolve native filename aliases, but keep separate hardlink and final
	// symlink entries independent: replacing them leaves the app in place.
	destinationPath, err := canonicalAppEntryPath(destination)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	appPath, err := canonicalAppEntryPath(appEntry)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return appbundle.SamePath(destinationPath.String(), appPath.String()), nil
}

func findRepoConfig(env vaultconfig.Env, workDir string) (string, *repoconfig.LocalConfig, error) {
	if strings.TrimSpace(workDir) == "" {
		workDir = env.WorkingDir
	}
	if !filepath.IsAbs(workDir) {
		return "", nil, errors.New("update working directory must be absolute")
	}
	cfgDir, cfg, err := repoconfig.FindLocalConfig(workDir)
	if err != nil {
		if errors.Is(err, repoconfig.ErrNoLocalConfig) {
			return "", nil, nil
		}
		return "", nil, err
	}
	return cfgDir, cfg, nil
}

func invocationRepoTarget(cfgDir string, cfg *repoconfig.LocalConfig) string {
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
	if aResolveErr == nil && bResolveErr == nil && appbundle.SamePath(aResolved, bResolved) {
		return true
	}
	return appbundle.SamePath(a, b)
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
