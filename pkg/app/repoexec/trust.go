package repoexec

import (
	"os"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

func TrustRequirement(args []string, cwd, exe string) (string, bool, error) {
	if isRepositoryTrustControlInvocation(args) || isExplicitExternalBinaryManagerMigration(args) {
		return "", false, nil
	}
	plan, err := Select(cwd, exe)
	if err != nil {
		return "", false, err
	}
	if plan.DevApp && plan.PinnedVersion != "" {
		// WHY: init, update, and other commands act on the checkout's pin
		// whichever executable runs them, so a pinned checkout keeps the
		// trust its pinned binary requires, even when the Dev app runs.
		plan, err = selectWithoutDevApp(cwd, exe)
		if err != nil {
			return "", false, err
		}
	}
	root := plan.TrustRoot()
	if root == "" {
		return "", false, nil
	}
	if plan.Authority == "managed" && plan.PreferDevelopment && isRepoDelegateBypassInvocation(args) {
		return "", false, nil
	}
	targetAbs, err := absoluteExecutable(plan.Target)
	if err != nil {
		return "", false, err
	}
	exeAbs, err := absoluteExecutable(exe)
	if err != nil {
		return "", false, err
	}
	if SameExecutablePath(targetAbs, exeAbs) {
		return "", false, nil
	}
	return root, true, nil
}

func selectWithoutDevApp(cwd, exe string) (Plan, error) {
	home, _ := os.UserHomeDir()
	lookup := func(key string) (string, bool) {
		if key == DevAppEnv {
			return "", true
		}
		return os.LookupEnv(key)
	}
	return SelectWithEnv(vaultconfig.Env{HomeDir: home, LookupEnv: lookup}, cwd, exe)
}

// DevAppHandoff returns the installed Rhizome Dev app's rzm when dispatch
// hands args to it without checkout trust, or "". Callers use it once
// TrustRequirement reports no trust is needed. Resolving again is safe here,
// unlike for repository builds: the checkout never chooses this target.
func DevAppHandoff(args []string, cwd, exe string) string {
	if isRepositoryTrustControlInvocation(args) || isExplicitExternalBinaryManagerMigration(args) || isUpdateControlPlaneInvocation(args) {
		return ""
	}
	plan, err := Select(cwd, exe)
	if err != nil || !plan.DevApp || SameExecutablePath(plan.Target, exe) {
		return ""
	}
	return plan.Target
}

// isRepositoryTrustControlInvocation covers commands that stay on the invoking
// executable: trust control, user credentials, team sign-in, and desktop
// entrypoints. The desktop bridge applies explicit trust before running any
// repository-selected code.
func isRepositoryTrustControlInvocation(args []string) bool {
	switch FirstCommandArg(args) {
	case "trust", "untrust", "credentials", "auth", "desktop", "desktop-bridge":
		return true
	default:
		return false
	}
}
