package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	rzmpaths "github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// LaunchTarget resolves a user path to an absolute directory inside a Rhizome
// folder or a Git working tree. The app resolves the repository and worktree,
// including a vault in a subfolder, and offers setup for an unconfigured one.
func LaunchTarget(cwd, input string) (string, error) {
	abs := rzmpaths.ResolveSymlinks(rzmpaths.AbsFromInput(cwd, input).String()).String()
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	if _, _, err := obsidian.FindLocalConfigForDelegation(abs); err == nil {
		return abs, nil
	} else if !errors.Is(err, obsidian.ErrNoLocalConfig) {
		return "", err
	}
	if gitWorktreeRoot(abs) == "" {
		return "", fmt.Errorf("%s is not inside a Rhizome folder or Git repository; run `rzm init` there first", abs)
	}
	return abs, nil
}

// AppCandidates lists the installed app bundles `rzm desktop` considers, in
// preference order.
func AppCandidates(home string) []string {
	var candidates []string
	for _, name := range []string{"Rhizome.app", "Rhizome Dev.app"} {
		candidates = append(candidates, filepath.Join("/Applications", name))
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "Applications", name))
		}
	}
	return candidates
}

// FindApp chooses the app bundle to open: the override, else a running
// candidate, else the first installed one.
func FindApp(override string, candidates []string, running func(string) bool) (string, error) {
	if override != "" {
		if info, err := os.Stat(override); err != nil || !info.IsDir() || filepath.Ext(override) != ".app" {
			return "", fmt.Errorf("RZM_DESKTOP_APP must name an installed .app bundle, got %q", override)
		}
		return override, nil
	}
	var installed []string
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			installed = append(installed, candidate)
		}
	}
	if len(installed) == 0 {
		return "", errors.New("the Rhizome desktop app is not installed (looked for Rhizome.app and Rhizome Dev.app in /Applications and ~/Applications); install it, or set RZM_DESKTOP_APP to its bundle path")
	}
	for _, app := range installed {
		if running(app) {
			return app, nil
		}
	}
	return installed[0], nil
}

// AppRunning reports whether a process from the app bundle is running.
func AppRunning(app string) bool {
	return exec.Command("pgrep", "-f", filepath.Join(app, "Contents", "MacOS")+"/").Run() == nil
}

// Launch hands target to the app through process arguments, never a URL
// scheme, so web content cannot trigger it.
func Launch(ctx context.Context, app, target string) error {
	out, err := exec.CommandContext(ctx, "open", "-n", "-a", app, "--args", "--open", target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open %s: %v: %s", app, err, out)
	}
	return nil
}
