package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
)

// main loads repository dotenv files after package initialization. Only the
// calling process may opt out of delegation; repository files cannot grant
// themselves an execution bypass.
var repoDelegationDisabledAtStartup = repoDelegationBlockedByEnv()

func maybeDelegateToRepoBinary(args []string) repoDelegateDecision {
	if repoDelegationDisabledAtStartup || IsOfflineDiagnosticsInvocation(args) {
		return repoDelegateDecision{}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return repoDelegateDecision{Err: err}
	}
	exe, err := os.Executable()
	if err != nil {
		return repoDelegateDecision{Err: err}
	}
	checkout, required, err := repoexec.TrustRequirement(args, cwd, exe)
	if err != nil {
		return repoDelegateDecision{Err: err}
	}
	if !required {
		// The preflight proved this invocation has no repository-selected handoff.
		// Do not resolve it again: repository files may change between checks.
		return repoDelegateDecision{}
	}
	store, err := repositorytrust.DefaultStore()
	if err != nil {
		return repoDelegateDecision{Err: err}
	}
	var confirm func(string) (bool, error)
	if isInteractive(os.Stdin) {
		confirm = repositoryTrustPrompt(os.Stdin, os.Stderr)
	}
	return repoDelegateDecisionForRequiredTrust(args, cwd, exe, checkout, store, confirm)
}

func repoDelegateDecisionForTrust(
	args []string,
	cwd string,
	exe string,
	store repositorytrust.Store,
	confirm func(string) (bool, error),
) repoDelegateDecision {
	if IsOfflineDiagnosticsInvocation(args) {
		return repoDelegateDecision{}
	}
	checkout, required, err := repoexec.TrustRequirement(args, cwd, exe)
	if err != nil {
		return repoDelegateDecision{Err: err}
	}
	if !required {
		return repoDelegateDecision{}
	}
	return repoDelegateDecisionForRequiredTrust(args, cwd, exe, checkout, store, confirm)
}

func repoDelegateDecisionForRequiredTrust(
	args []string,
	cwd string,
	exe string,
	checkout string,
	store repositorytrust.Store,
	confirm func(string) (bool, error),
) repoDelegateDecision {
	trusted, err := store.Trusted(checkout)
	if err != nil {
		return repoDelegateDecision{Err: err}
	}
	if !trusted && confirm != nil {
		trusted, err = confirm(checkout)
		if err != nil {
			return repoDelegateDecision{Err: err}
		}
		if trusted {
			if _, err := store.Trust(checkout); err != nil {
				return repoDelegateDecision{Err: err}
			}
		}
	}
	if !trusted {
		return repoDelegateDecision{Err: fmt.Errorf(
			"repository is not trusted to select a Rhizome executable: %s; review the checkout, then run `rzm trust` from that checkout",
			checkout,
		)}
	}
	return repoDelegateDecisionFor(args, cwd, exe)
}

func repositoryTrustPrompt(in io.Reader, out io.Writer) func(string) (bool, error) {
	reader := bufio.NewReader(in)
	return func(checkout string) (bool, error) {
		if _, err := fmt.Fprintf(out, "Trust this repository to select and run its Rhizome executable?\n  %s\n[y/N]: ", checkout); err != nil {
			return false, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes", nil
	}
}
