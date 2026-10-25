// Whether the rzm hook entries launch can run them, and apply guard rules.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC2]]
package init

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// checkHookSupport tells whether installed entries can run. It is a
// variable so tests need no second binary.
var checkHookSupport = selectedBinarySupportsHooks

// hookTrustStore is the repository trust store; tests replace it.
var hookTrustStore = repositorytrust.DefaultStore

// runningExecutable is os.Executable, replaceable in tests, where the test
// binary would otherwise count as a go run build.
var runningExecutable = os.Executable

// errNoGuard marks a binary that runs hook entries but does not apply guard
// rules, so the before-tool-call entry is left out and the others install.
var errNoGuard = errors.New("does not apply guard rules")

// selectedBinarySupportsHooks probes, with `rzm hook --check`, the rzm on
// PATH that every entry launches, and the binary that rzm selects for this
// repository and hands hooks off to. Selection and trust are judged from
// that launcher, not this binary, which may be a go run build that selects
// nothing. This binary needs no probe. The binary that runs the entries must
// also advertise the guard capability, or the error wraps errNoGuard.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC2]]
func selectedBinarySupportsHooks(root string, cfg repoconfig.LocalConfig) error {
	exe, err := runningExecutable()
	if err != nil {
		return err
	}
	launcher, err := exec.LookPath("rzm")
	if err != nil {
		return fmt.Errorf("hook entries run rzm from PATH, and there is no rzm on PATH")
	}
	hooks, guard := supportsHooks(root, launcher, exe)
	if !hooks {
		return fmt.Errorf("the rzm on PATH (%s) does not support rzm hook; update it with rzm update", launcher)
	}
	external, err := cfg.Rhizome.UsesExternalBinaryManager()
	if err != nil {
		return err
	}
	// A Rhizome source checkout selects its own bin/<os>/rzm even with no
	// devBinaryDir, so selection decides whether a second binary needs a check.
	plan, selectErr := repoexec.Select(root, launcher)
	development := selectErr == nil && plan.Authority == "development"
	// A pin, even one matching this binary, sends hooks to the pinned binary
	// whatever version the launcher is, so that binary is the one to check.
	pin := strings.TrimSpace(cfg.Rhizome.Version)
	if external || !development && strings.TrimSpace(cfg.Rhizome.DevBinaryDir) == "" && pin == "" {
		if !guard {
			return fmt.Errorf("the rzm on PATH (%s) %w, so the before-tool-call entry was not installed; update it with rzm update", launcher, errNoGuard)
		}
		return nil
	}
	// The selected binary is the checkout's own code: run it only for a
	// checkout the person trusts, as dispatch and the hook handoff do.
	if checkout, required, err := repoexec.TrustRequirement([]string{"hook"}, root, launcher); err != nil {
		return err
	} else if required {
		store, err := hookTrustStore()
		if err != nil {
			return err
		}
		if trusted, err := store.Trusted(checkout); err != nil {
			return err
		} else if !trusted {
			return fmt.Errorf("this checkout (%s) is not trusted to select a Rhizome executable, so its rzm was not checked; review it, run rzm trust there, then run rzm init again", checkout)
		}
	}
	target := launcher
	if selectErr == nil && plan.Target != "" {
		target = plan.Target
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("the selected rzm (%s) is not installed yet; run rzm init again once it is", target)
	}
	if hooks, guard = supportsHooks(root, target, exe); !hooks {
		return fmt.Errorf("the selected rzm (%s) does not support rzm hook; update it with rzm update", target)
	} else if !guard {
		return fmt.Errorf("the selected rzm (%s) %w, so the before-tool-call entry was not installed; update it with rzm update", target, errNoGuard)
	}
	return nil
}

// supportsHooks reports whether target answers `rzm hook --check`, and
// whether its answer advertises the guard capability.
func supportsHooks(root, target, exe string) (hooks, guard bool) {
	if repoexec.SameExecutablePath(target, exe) {
		return true, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := repoexec.Probe(ctx, target, root, "hook", "--check")
	fields := strings.Fields(out)
	if err != nil || len(fields) < 2 || fields[0] != "rzm-hook" {
		return false, false
	}
	return true, slices.Contains(fields[2:], hook.GuardCapability)
}
