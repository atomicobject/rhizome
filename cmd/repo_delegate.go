package cmd

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"os"
	"os/exec"
)

const (
	repoDelegatedEnv        = "RZM_REPO_DELEGATED"
	repoSkipDelegateEnv     = "RZM_SKIP_REPO_DELEGATE"
	bootstrapReasonMissing  = repoexec.BootstrapReasonMissing
	bootstrapReasonMismatch = repoexec.BootstrapReasonMismatch
)

type repoDelegateDecision = repoexec.Decision

func repoDelegationBlockedByEnv() bool {
	return os.Getenv(repoDelegatedEnv) != "" || os.Getenv(repoSkipDelegateEnv) != ""
}
func repoDelegateDecisionFor(args []string, cwd, exe string) repoDelegateDecision {
	return repoexec.DecisionFor(args, cwd, exe)
}
func bootstrapRepoBinary(d repoDelegateDecision) error {
	return repoexec.Bootstrap(context.Background(), d, os.Stderr)
}
func findRhizomeSourceRoot(cwd string) (string, bool) { return repoexec.FindSourceRoot(cwd) }
func sameCleanPath(a, b string) bool                  { return repoexec.SamePath(a, b) }
func sameExecutablePath(a, b string) bool             { return repoexec.SameExecutablePath(a, b) }
func firstCommandArg(args []string) string            { return repoexec.FirstCommandArg(args) }
func repoPlatformDir() string                         { return repoexec.PlatformDir() }
func repoExecutableName() string                      { return repoexec.ExecutableName() }
func runRepoBinary(target string, args []string) int {
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), repoDelegatedEnv+"=1")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Whoops. There was an error delegating to repo Rhizome binary '%s': %v\n", target, err)
		return 1
	}
	return 0
}

func isUpdateControlPlaneInvocation(args []string) bool { return firstCommandArg(args) == "update" }
