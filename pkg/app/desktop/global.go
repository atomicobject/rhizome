package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type GlobalInfo struct {
	Path        string `json:"path"`
	ManagedPath string `json:"managedPath"`
	Version     string `json:"version"`
	Installed   bool   `json:"installed"`
	CanUpdate   bool   `json:"canUpdate"`
}

func (s *Service) managedPath() string {
	return filepath.Join(s.home, ".local", "bin", repoexec.ExecutableName())
}

func (s *Service) globalExecutable(override string) (string, error) {
	if override != "" {
		if err := executable(override); err != nil {
			return "", err
		}
		return override, nil
	}
	paths := []string{s.managedPath()}
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
	info := GlobalInfo{ManagedPath: s.managedPath()}
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
	info.Installed = true
	info.CanUpdate = filepath.Clean(target) == s.managedPath() && s.ownedTarget() == nil
	return info, nil
}

func (s *Service) ownedTarget() error {
	for path := s.managedPath(); path != filepath.Dir(s.home); path = filepath.Dir(path) {
		stat, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			return problem("install_error", fmt.Sprintf("Desktop cannot update a symlinked global installation: %s", path))
		}
		if path == s.home {
			break
		}
	}
	return nil
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

func (s *Service) InstallGlobal(ctx context.Context) (GlobalInfo, error) {
	info := GlobalInfo{Path: s.managedPath(), ManagedPath: s.managedPath()}
	neutral, cleanup, err := s.neutralDir()
	if err != nil {
		return info, err
	}
	defer cleanup()
	release, acquired, err := indexlock.TryAcquire(filepath.Join(s.stateDir, "global-update.lock"))
	if err != nil {
		return info, err
	}
	if !acquired {
		return info, problem("install_error", "Another global Rhizome installation is in progress. Try again when it finishes.")
	}
	defer func() { _ = release() }()
	if err := s.ownedTarget(); err != nil {
		return info, err
	}
	result, installErr := s.install(ctx, appupdate.Options{ExecutablePath: s.managedPath(), WorkDir: neutral, Yes: true, Latest: true})
	status, statusErr := s.GlobalStatus(ctx, s.managedPath())
	if statusErr == nil {
		info = status
	}
	if installErr != nil {
		message := "Global Rhizome installation failed."
		var stage *appupdate.StageError
		if errors.As(installErr, &stage) {
			switch stage.Stage {
			case appupdate.StageManifest:
				message = "Could not find a downloadable Rhizome release. Check the connection and published releases."
			case appupdate.StageDownload:
				message = "Could not download the Rhizome release. Check the connection and try again."
			case appupdate.StageChecksum:
				message = "The downloaded Rhizome release failed its checksum check. Try again."
			case appupdate.StageExtract, appupdate.StageSmokeTest:
				message = "The downloaded Rhizome release could not run on this computer."
			case appupdate.StageReplace:
				message = "Could not replace the global Rhizome executable. Check the installation folder permissions."
			}
		}
		if completed := result.Summaries(); len(completed) > 0 {
			message += " " + strings.Join(completed, ". ") + ". A later installation step failed."
		}
		return info, problem("install_error", message)
	}
	if statusErr != nil {
		return info, statusErr
	}
	return info, nil
}
