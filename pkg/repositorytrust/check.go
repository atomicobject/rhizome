package repositorytrust

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Untrusted checks the repository holding dir, using the main checkout for
// linked worktrees and the common Git directory for bare repositories.
// A folder outside Git is checked as its own checkout.
func Untrusted(ctx context.Context, dir, what string, trusted func(string) (bool, error)) string {
	checkout := dir
	git := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
		out, err := cmd.Output()
		return strings.TrimRight(string(out), "\n"), err
	}
	if common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		common = filepath.Clean(common)
		if resolved, err := filepath.EvalSymlinks(common); err == nil {
			common = resolved
		}
		checkout = common
		bare, _ := git("", "--git-dir="+common, "rev-parse", "--is-bare-repository")
		if bare != "true" && filepath.Base(common) == ".git" {
			checkout = filepath.Dir(common)
		}
	}
	return DenialReason(checkout, what, trusted)
}

// DenialReason checks an already-resolved checkout without running Git again.
func DenialReason(checkout, what string, trusted func(string) (bool, error)) string {
	ok, err := trusted(checkout)
	if err != nil {
		return "repository trust could not be read: " + err.Error()
	}
	if !ok {
		return "this repository is not trusted; run `rzm trust` in " + checkout + " to let its " + what + " run"
	}
	return ""
}
