package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
)

type OpenResult struct {
	URL     string `json:"url"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
	Mode    string `json:"mode"`
	Spawned bool   `json:"spawned"`
}

func (s *Service) Open(ctx context.Context, req Request) (OpenResult, error) {
	info, plan, err := s.inspect(req.Folder)
	if err != nil {
		return OpenResult{}, err
	}
	if !info.Configured {
		return OpenResult{}, problem("not_configured", "This folder needs Rhizome setup before it can open.")
	}
	if info.TrustRequired {
		return OpenResult{}, problem("trust_required", "Confirm trust for this folder before running its selected Rhizome executable.")
	}
	if err := checkRecordedRuntimeIdentity(ctx, info.Path); err != nil {
		return OpenResult{}, err
	}
	if plan.Authority == "external" && req.Executable == "" {
		client, health, err := appruntime.LiveManifest(ctx, info.Path)
		if err == nil {
			return openResult(info.Path, appruntime.EnsureResult{Client: client, Health: health})
		}
		if !errors.Is(err, appruntime.ErrNoRuntime) {
			return OpenResult{}, err
		}
		return OpenResult{}, problem("external_executable_required", "Choose the Rhizome executable provided by this folder's external binary manager.")
	}
	target, err := s.prepare(ctx, info, plan, req)
	if err != nil {
		return OpenResult{}, err
	}
	version, err := repoexec.Version(ctx, target, info.Path)
	if err != nil {
		return OpenResult{}, problem("missing_executable", err.Error())
	}
	help, err := repoexec.Probe(ctx, target, info.Path, "serve", "--help")
	if err != nil || !strings.Contains(help, "--headless") {
		return OpenResult{}, problem("runtime_error", "This Rhizome version does not support desktop runtimes. Update the folder's selected Rhizome version to one with `serve --headless`, or select a current development build.")
	}
	build := appruntime.BuildID(version, target)
	if plan.Authority == "external" {
		build = ""
	}
	result, err := s.ensure(ctx, appruntime.EnsureOptions{VaultPath: info.Path, Executable: target, BuildID: build, Replace: req.Restart, Autostart: true, Wait: true})
	if err != nil {
		return OpenResult{}, problem("runtime_error", err.Error())
	}
	if build != "" && result.Health.BuildID != build {
		return OpenResult{}, problem("runtime_error", "The running Rhizome build does not match the selected executable.")
	}
	return openResult(info.Path, result)
}

func (s *Service) prepare(ctx context.Context, info FolderInfo, plan repoexec.Plan, req Request) (string, error) {
	target := plan.Target
	switch plan.Authority {
	case "managed":
		if err := s.ensurePinned(ctx, appupdate.EnsureOptions{CfgDir: plan.ConfigDir, Config: plan.Config}); err != nil {
			return "", problem("install_error", fmt.Sprintf("Could not prepare pinned Rhizome %s: %v", plan.PinnedVersion, err))
		}
	case "external":
		target = req.Executable
		if target == "" {
			return "", problem("external_executable_required", "Choose this folder's externally managed Rhizome executable.")
		}
	case "global":
		var err error
		target, err = s.globalExecutable(req.GlobalExecutable)
		if err != nil {
			return "", err
		}
	}
	if err := executable(target); err != nil {
		return "", err
	}
	return target, nil
}

func executable(path string) error {
	if !filepath.IsAbs(path) {
		return problem("missing_executable", "Choose an absolute path to the Rhizome executable, or install global Rhizome.")
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() || (runtime.GOOS != "windows" && stat.Mode()&0o111 == 0) {
		return problem("missing_executable", fmt.Sprintf("Rhizome executable is missing or cannot run: %s", path))
	}
	return nil
}

func runtimeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Port() == "" {
		return "", problem("runtime_error", "Rhizome did not provide a valid local runtime address.")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", problem("runtime_error", "Rhizome runtime must use a loopback IP address.")
	}
	return "http://" + u.Host, nil
}

func checkRecordedRuntimeIdentity(ctx context.Context, folder string) error {
	m, err := appruntime.ReadManifest(folder)
	if err != nil {
		return nil
	}
	if _, err = runtimeOrigin(m.HTTPURL); err != nil {
		return err
	}
	if !repoexec.SamePath(m.VaultPath, folder) {
		return problem("runtime_error", "The runtime manifest belongs to another folder.")
	}
	client, health, err := appruntime.LiveManifest(ctx, folder)
	if err == nil {
		_, err = openResult(folder, appruntime.EnsureResult{Client: client, Health: health})
		return err
	}
	return nil
}

func openResult(folder string, result appruntime.EnsureResult) (OpenResult, error) {
	if result.Client == nil {
		return OpenResult{}, problem("runtime_error", "Rhizome did not become ready.")
	}
	origin, err := runtimeOrigin(result.Client.Manifest.HTTPURL)
	if err != nil {
		return OpenResult{}, err
	}
	health := result.Health
	if !repoexec.SamePath(health.VaultPath, folder) || health.PID <= 0 || (health.Mode != appruntime.ModeAttached && health.Mode != appruntime.ModeHeadless) {
		return OpenResult{}, problem("runtime_error", "The running Rhizome identity does not match this folder.")
	}
	return OpenResult{URL: origin, Version: health.Version, PID: health.PID, Mode: string(health.Mode), Spawned: result.Spawned}, nil
}

func (s *Service) Initialize(ctx context.Context, req Request) (FolderInfo, error) {
	info, plan, err := s.inspect(req.Folder)
	if err != nil {
		return info, err
	}
	if info.Configured {
		return info, problem("invalid_request", "This folder is already configured for Rhizome.")
	}
	if info.TrustRequired {
		return info, problem("trust_required", "Confirm trust before running this folder's selected Rhizome executable.")
	}
	target, err := s.prepare(ctx, info, plan, req)
	if err != nil {
		return info, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, target, "init", "--path", info.Path)
	cmd.Dir = info.Path
	cmd.Env = append(os.Environ(), "RZM_REPO_DELEGATED=1", "RZM_SKIP_REPO_DELEGATE=1")
	if err := cmd.Run(); err != nil {
		return info, problem("runtime_error", fmt.Sprintf("Rhizome setup failed: %v", err))
	}
	return s.Inspect(info.Path)
}
