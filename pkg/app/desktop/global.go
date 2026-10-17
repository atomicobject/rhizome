package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var desktopExecutable = os.Executable

type GlobalInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Bundled bool   `json:"bundled"`
}

func (s *Service) globalExecutable(override string) (string, error) {
	if override != "" {
		if err := executable(override); err != nil {
			return "", err
		}
		return override, nil
	}
	if own, err := desktopExecutable(); err == nil {
		bundle, err := appbundle.Resolve(own)
		if err != nil {
			return "", err
		}
		if bundle != nil {
			return bundle.ExecutablePath, nil
		}
	}
	paths := []string{filepath.Join(s.home, ".local", "bin", repoexec.ExecutableName())}
	if path, err := exec.LookPath(repoexec.ExecutableName()); err == nil && filepath.IsAbs(path) {
		paths = append(paths, path)
	}
	paths = append(paths, filepath.Join("/opt/homebrew/bin", repoexec.ExecutableName()), filepath.Join("/usr/local/bin", repoexec.ExecutableName()))
	for _, path := range paths {
		if executable(path) == nil {
			return path, nil
		}
	}
	return "", nil
}

func (s *Service) GlobalStatus(ctx context.Context, override string) (GlobalInfo, error) {
	info := GlobalInfo{}
	target, err := s.globalExecutable(override)
	if err != nil {
		return info, err
	}
	info.Path = target
	if target == "" {
		return info, nil
	}
	neutral, cleanup, err := s.neutralDir()
	if err != nil {
		return info, err
	}
	defer cleanup()
	info.Version, err = repoexec.Version(ctx, target, neutral)
	if err != nil {
		return info, problem("missing_executable", err.Error())
	}
	bundle, err := appbundle.Resolve(target)
	if err != nil {
		return info, err
	}
	info.Bundled = bundle != nil
	return info, nil
}

func (s *Service) neutralDir() (string, func(), error) {
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(s.stateDir, "operation-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	canonical, resolveErr := filepath.EvalSymlinks(dir)
	if resolveErr != nil {
		cleanup()
		return "", nil, resolveErr
	}
	dir = canonical
	_, _, err = obsidian.FindLocalConfigForDelegation(dir)
	if !errors.Is(err, obsidian.ErrNoLocalConfig) {
		cleanup()
		return "", nil, problem("invalid_request", "Desktop application data must be outside every configured Rhizome folder.")
	}
	return dir, cleanup, nil
}
