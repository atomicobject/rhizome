package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
)

type CLILinkShell struct {
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	IsApp   bool   `json:"isApp"`
	Error   string `json:"error,omitempty"`
}

type CLILinkInfo struct {
	LinkPath  string       `json:"linkPath"`
	State     string       `json:"state"`
	Target    string       `json:"target,omitempty"`
	Shell     CLILinkShell `json:"shell"`
	rawTarget string
}

var cliLinkDirectory = "/usr/local/bin"
var cliLinkMkdir = os.MkdirAll
var cliLinkSymlink = os.Symlink
var cliLinkUnlink = os.Remove
var cliLinkRunCommand = func(ctx context.Context, name string, args ...string) (string, error) {
	return repoexec.Probe(ctx, name, "/", args...)
}
var cliLinkPrivileged = func(ctx context.Context, text string) error {
	out, err := exec.CommandContext(ctx, "osascript", cliLinkAdminArgs(text)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("administrator command: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func cliLinkAdminArgs(text string) []string {
	return []string{"-e", "on run argv", "-e", "do shell script item 1 of argv with administrator privileges", "-e", "end run", text}
}

func cliLinkBundle() (*appbundle.Info, error) {
	path, err := desktopExecutable()
	if err != nil {
		return nil, err
	}
	return appbundle.Resolve(path)
}

func cliLinkInspect(bundle *appbundle.Info) (CLILinkInfo, error) {
	info := CLILinkInfo{LinkPath: filepath.Join(cliLinkDirectory, "rzm"), State: "unsupported"}
	if bundle == nil || bundle.Dev || !bundle.Rhizome() {
		return info, nil
	}
	stat, err := os.Lstat(info.LinkPath)
	if errors.Is(err, os.ErrNotExist) {
		info.State = "absent"
		return info, nil
	}
	if err != nil {
		return info, err
	}
	info.State = "foreign"
	if stat.Mode()&os.ModeSymlink == 0 {
		return info, nil
	}
	target, err := os.Readlink(info.LinkPath)
	if err != nil {
		return info, err
	}
	info.rawTarget = target
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(info.LinkPath), target)
	}
	info.Target = filepath.Clean(target)
	root := appbundle.Root(info.Target)
	if root == "" || filepath.Base(info.Target) != "rzm" {
		return info, nil
	}
	resolved, err := filepath.EvalSymlinks(info.LinkPath)
	if errors.Is(err, os.ErrNotExist) {
		// A deleted bundle has no identifier left; dangling ownership uses its app/rzm path.
		info.State = "dangling"
		return info, nil
	}
	if err != nil {
		return info, err
	}
	targetBundle, err := appbundle.Resolve(resolved)
	if err != nil {
		return info, nil
	}
	if targetBundle == nil || !targetBundle.Rhizome() {
		return info, nil
	}
	info.State = "other-bundle"
	if resolved == bundle.ExecutablePath {
		info.State = "installed"
	}
	return info, nil
}

func cliLinkShell(ctx context.Context, bundle *appbundle.Info) CLILinkShell {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	result := CLILinkShell{}
	out, err := cliLinkRunCommand(ctx, shell, "-lc", "command -v rzm")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Path = strings.TrimSpace(out)
	if !filepath.IsAbs(result.Path) || strings.ContainsAny(result.Path, "\r\n") {
		result.Error = "The login shell did not resolve rzm to an absolute executable path."
		result.Path = ""
		return result
	}
	resolved, err := filepath.EvalSymlinks(result.Path)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.IsApp = bundle != nil && resolved == bundle.ExecutablePath
	out, err = cliLinkRunCommand(ctx, result.Path, "--version")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	fields := strings.Fields(out)
	for i, field := range fields {
		if field == "version" && i+1 < len(fields) {
			result.Version = fields[i+1]
			return result
		}
	}
	result.Error = "The login shell's rzm did not report a Rhizome version."
	return result
}

func (s *Service) CLILinkStatus(ctx context.Context) (CLILinkInfo, error) {
	bundle, err := cliLinkBundle()
	if err != nil {
		return CLILinkInfo{}, problem("link_failed", err.Error())
	}
	info, err := cliLinkInspect(bundle)
	info.Shell = cliLinkShell(ctx, bundle)
	if err != nil {
		return info, problem("link_failed", err.Error())
	}
	return info, nil
}

func (s *Service) CLILinkInstall(ctx context.Context) (CLILinkInfo, error) {
	return s.mutateCLILink(ctx, false)
}

func (s *Service) CLILinkRemove(ctx context.Context) (CLILinkInfo, error) {
	return s.mutateCLILink(ctx, true)
}

func cliLinkQuote(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'" }

func cliLinkCommand(info CLILinkInfo, target string, remove bool) string {
	link := cliLinkQuote(info.LinkPath)
	if remove {
		return "[ -L " + link + " ] && [ \"$(readlink " + link + ")\" = " + cliLinkQuote(info.rawTarget) + " ] && unlink " + link
	}
	// Under administrator privileges, replace only what inspection saw: nothing,
	// or the same link into a Rhizome bundle.
	unchanged := "[ ! -e " + link + " ] && [ ! -L " + link + " ]"
	if info.State != "absent" {
		unchanged = "[ -L " + link + " ] && [ \"$(readlink " + link + ")\" = " + cliLinkQuote(info.rawTarget) + " ]"
	}
	// Shell text for macOS: POSIX separators on every build platform.
	return "mkdir -p " + cliLinkQuote(path.Dir(info.LinkPath)) + " && " + unchanged + " && ln -sfn " + cliLinkQuote(target) + " " + link
}

func cliLinkUnlinkOwned(bundle *appbundle.Info, info CLILinkInfo) error {
	current, err := cliLinkInspect(bundle)
	if err != nil {
		return err
	}
	if current.State == "absent" {
		return nil
	}
	if current.State == "foreign" {
		return problem("link_foreign", "The command-line link changed to another installation. It was left in place.")
	}
	if current.rawTarget != info.rawTarget {
		return problem("link_failed", "The command-line link changed. Try again.")
	}
	return cliLinkUnlink(info.LinkPath)
}

func (s *Service) mutateCLILink(ctx context.Context, remove bool) (CLILinkInfo, error) {
	bundle, err := cliLinkBundle()
	if err != nil {
		return CLILinkInfo{}, problem("link_failed", err.Error())
	}
	info, err := cliLinkInspect(bundle)
	if err != nil {
		return info, problem("link_failed", err.Error())
	}
	info.Shell = cliLinkShell(ctx, bundle)
	if bundle != nil && bundle.Dev {
		return info, problem("link_dev_bundle", "Rhizome Dev does not own the command-line link.")
	}
	if info.State == "unsupported" {
		return info, problem("link_unsupported", "Install the command-line link from a Rhizome release app.")
	}
	if info.State == "foreign" {
		return info, problem("link_foreign", fmt.Sprintf("%s belongs to another installation. Leave it in place or remove it yourself first.", info.LinkPath))
	}
	if remove && info.State == "absent" || !remove && info.State == "installed" {
		return info, nil
	}
	if remove {
		err = cliLinkUnlinkOwned(bundle, info)
	} else {
		err = cliLinkMkdir(filepath.Dir(info.LinkPath), 0o755)
		if err == nil && info.State != "absent" {
			err = cliLinkUnlinkOwned(bundle, info)
		}
		if err == nil {
			err = cliLinkSymlink(bundle.ExecutablePath, info.LinkPath)
		}
	}
	if errors.Is(err, os.ErrPermission) {
		current, inspectErr := cliLinkInspect(bundle)
		if inspectErr != nil {
			return info, problem("link_failed", inspectErr.Error())
		}
		if current.State == "foreign" {
			return info, problem("link_foreign", "The command-line link changed to another installation. It was left in place.")
		}
		err = cliLinkPrivileged(ctx, cliLinkCommand(current, bundle.ExecutablePath, remove))
	}
	if err != nil {
		var failure *Problem
		if errors.As(err, &failure) {
			return info, err
		}
		if strings.Contains(err.Error(), "-128") {
			return info, problem("link_cancelled", "The administrator prompt was cancelled.")
		}
		return info, problem("link_failed", err.Error())
	}
	return s.CLILinkStatus(ctx)
}
