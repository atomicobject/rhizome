package repoexec

// TrustRequirement reports the checkout whose trust CLI dispatch needs before
// args may run a repository-selected rzm. Dispatch acts on the pin (init,
// update, and bootstrap do), so a pinned checkout needs trust even when the
// Dev app's rzm runs.
func TrustRequirement(args []string, cwd, exe string) (string, bool, error) {
	if isRepositoryTrustControlInvocation(args) || isExplicitExternalBinaryManagerMigration(args) {
		return "", false, nil
	}
	plan, err := Select(cwd, exe)
	if err != nil {
		return "", false, err
	}
	root := Resolve(plan, Caller{Executable: exe, ActsOnPin: true}).TrustRoot
	if root == "" {
		return "", false, nil
	}
	if trust := plan.trustPlan(true); trust.Authority == "managed" && trust.PreferDevelopment && isRepoDelegateBypassInvocation(args) {
		return "", false, nil
	}
	return root, true, nil
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
