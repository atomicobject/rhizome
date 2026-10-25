package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/spf13/cobra"
)

// hookDeadlineEnv carries the invocation's deadline, in Unix nanoseconds,
// to the repository binary a hook hands off to, so the handoff does not
// restart the clock.
const hookDeadlineEnv = "RZM_HOOK_DEADLINE"

// IsHookInvocation reports whether args run the hook dispatcher, which takes
// an early path: no .env loading, binary bootstrap, update, or Cobra setup.
func IsHookInvocation(args []string) bool {
	return len(args) > 0 && args[0] == "hook"
}

// registerHookFamily lists `rzm hook` in help. Execute runs it on the early
// path before Cobra, so this command only documents it.
func registerHookFamily(rootCmd *cobra.Command) {
	rootCmd.AddCommand(&cobra.Command{
		Use:                "hook <claude|codex> <" + strings.Join(hook.HookPoints, "|") + ">",
		Short:              "Run starter hook handlers for an agent harness (rzm init --hooks on installs the entries)",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			runHook(args)
		},
	})
}

// hookDelegate selects the repository binary a hook hands off to; tests
// replace it.
var hookDelegate = hookDelegateTargetForProcess

// runHook is `rzm hook <harness> <event>` and `rzm hook --check`. It exits
// 0 whatever happens: a hook never blocks or fails the host's tool call. The
// deadline, its watchdog, and the panic guard start before the repository
// binary is selected, so selection counts against them too, and a handoff
// runs that binary as a supervised child under the same deadline.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC7]]
func runHook(args []string) (code int) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(os.Stderr, "rzm hook: %v\n", p)
			code = 0
		}
	}()
	if len(args) == 1 && args[0] == "--check" {
		fmt.Printf("rzm-hook %d %s\n", hook.ProtocolVersion, hook.GuardCapability)
		return 0
	}
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: rzm hook <claude|codex> <%s>\n", strings.Join(hook.HookPoints, "|"))
		return 0
	}
	deadline := hookDeadline(time.Now(), os.Getenv(hookDeadlineEnv))
	cwd, _ := os.Getwd()
	opts := hook.Options{
		Harness: args[0], Point: args[1], Stdin: os.Stdin, Stdout: os.Stdout, Cwd: cwd,
		Handlers: initactions.HookHandlers, Deadline: time.Until(deadline),
	}
	if opts.Deadline <= 0 {
		hook.Diagnose(opts, "the deadline %s inherited passed before rzm hook started", hookDeadlineEnv)
		return 0
	}
	// A last guard below the host's timeout, should file I/O hang past the
	// dispatcher's own deadline.
	guard := time.AfterFunc(opts.Deadline+300*time.Millisecond, func() { os.Exit(0) })
	defer guard.Stop()
	// A host that stops reading makes writes fail instead of killing rzm
	// before it records state.
	signal.Ignore(syscall.SIGPIPE)
	if target := hookDelegate(cwd); target != "" {
		env := append(os.Environ(), hookDeadlineEnv+"="+strconv.FormatInt(deadline.UnixNano(), 10), repoDelegatedEnv+"=1")
		opts.Deadline = time.Until(deadline)
		hook.Delegate(context.Background(), target, env, opts)
		return 0
	}
	if opts.Deadline = time.Until(deadline); opts.Deadline <= 0 {
		hook.Diagnose(opts, "the deadline passed while selecting the repository's rzm")
		return 0
	}
	hook.Run(context.Background(), opts)
	return 0
}

// hookDeadline is the invocation's deadline: the one a launcher handed off
// with, even one already past, unless it is malformed or further away than
// a fresh one; otherwise a fresh one.
func hookDeadline(now time.Time, inherited string) time.Time {
	fresh := now.Add(hook.Deadline)
	if ns, err := strconv.ParseInt(inherited, 10, 64); err == nil {
		if at := time.Unix(0, ns); !at.After(fresh) {
			return at
		}
	}
	return fresh
}

func hookDelegateTargetForProcess(cwd string) string {
	if repoDelegationDisabledAtStartup {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	store, err := repositorytrust.DefaultStore()
	if err != nil {
		return ""
	}
	return hookDelegateTarget(cwd, exe, store)
}

// hookDelegateTarget is the repository-selected rzm a hook hands off to, or
// "" to run in this binary. Unlike ordinary dispatch it never prompts,
// probes, or installs: only a trusted checkout's development build, or its
// managed pin whose version marker matches, qualifies.
func hookDelegateTarget(cwd, exe string, store repositorytrust.Store) string {
	checkout, required, err := repoexec.TrustRequirement([]string{"hook"}, cwd, exe)
	if err != nil || !required {
		return ""
	}
	if trusted, err := store.Trusted(checkout); err != nil || !trusted {
		return ""
	}
	plan, err := repoexec.Select(cwd, exe)
	if err != nil || plan.Target == "" {
		return ""
	}
	target, err := filepath.Abs(plan.Target)
	if err != nil {
		return ""
	}
	switch plan.Authority {
	case "development":
		if info, err := os.Stat(target); err != nil || info.IsDir() {
			return ""
		}
	case "managed":
		installed, ok := appupdate.ReadVersionMarker(target)
		if !ok || installed != appupdate.NormalizeVersion(plan.PinnedVersion) {
			return ""
		}
	default:
		return ""
	}
	return target
}
