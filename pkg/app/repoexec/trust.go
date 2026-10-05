package repoexec

func TrustRequirement(args []string, cwd, exe string) (string, bool, error) {
	if isRepositoryTrustControlInvocation(args) || isExplicitExternalBinaryManagerMigration(args) {
		return "", false, nil
	}
	plan, err := Select(cwd, exe)
	if err != nil {
		return "", false, err
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

// isRepositoryTrustControlInvocation covers commands that stay on the invoking
// executable and run no repository code: trust control, and `desktop`, which
// only hands a path to the desktop app.
func isRepositoryTrustControlInvocation(args []string) bool {
	switch FirstCommandArg(args) {
	case "trust", "untrust", "desktop":
		return true
	default:
		return false
	}
}
