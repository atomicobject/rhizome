package repoexec

// Caller describes the front end asking which rzm runs for a checkout.
type Caller struct {
	// Executable is the rzm the caller runs when the repository selects none:
	// the invoked rzm on the command line, the app's global rzm in desktop.
	Executable string
	// Chosen is an executable the user picked for this checkout (desktop).
	Chosen string
	// ActsOnPin marks a caller whose command may install or change the
	// checkout's pin whichever binary runs it, such as CLI dispatch with init
	// and update. Its trust then covers the pinned binary even when the Rhizome
	// Dev app's rzm runs.
	ActsOnPin bool
}

// Execution is the one decision CLI dispatch and desktop act on: what runs for
// a checkout, why, and what must hold first. Resolving it never probes,
// installs, or grants trust.
type Execution struct {
	Plan Plan
	// Target is the executable to run. For a managed pin it is the pinned
	// binary, which may still need installing.
	Target string
	// Reason says why Target runs, for display.
	Reason string
	// FromCaller marks a Target that is the caller's own Executable.
	FromCaller bool
	// TrustRoot is the checkout whose trust authorizes running Target, or ""
	// when nothing the repository selected runs.
	TrustRoot string
	// InstallPin means Target is a managed pin to install before running.
	InstallPin bool
	// ExactBuild means Target's file identity is its build. An external
	// manager's executable or the user's choice may be a shim.
	ExactBuild bool
}

// Resolve completes plan for caller.
func Resolve(plan Plan, caller Caller) Execution {
	e := Execution{Plan: plan, Target: plan.Target, ExactBuild: true}
	switch plan.Authority {
	case "external":
		e.ExactBuild = false
		e.Target, e.Reason = caller.Chosen, "chosen for this worktree"
		if e.Target == "" {
			e.Target, e.FromCaller = caller.Executable, true
			e.Reason = "the binary manager is external, so the invoked or global rzm runs"
		}
	case "global":
		e.Target, e.FromCaller, e.Reason = caller.Executable, true, "no repository selection"
		if caller.Chosen != "" {
			e.Target, e.FromCaller, e.ExactBuild, e.Reason = caller.Chosen, false, false, "chosen for this worktree"
		}
	case "managed":
		e.InstallPin = true
		e.Reason = "pinned to Rhizome " + plan.PinnedVersion
		if caller.Chosen != "" {
			e.Target, e.InstallPin, e.ExactBuild, e.Reason = caller.Chosen, false, false, "chosen for this worktree"
		}
	case "development":
		switch {
		case plan.DevApp:
			e.Reason = "a Rhizome source checkout without its own build runs the Rhizome Dev app's rzm"
		case plan.ConfiguredDevelopment:
			e.Reason = "rhizome.devBinaryDir selects a development build"
		default:
			e.Reason = "a Rhizome source checkout runs its own build"
		}
	}
	trust := plan.trustPlan(caller.ActsOnPin)
	e.TrustRoot = trust.trustRoot()
	if e.TrustRoot != "" && caller.Chosen == "" && caller.Executable != "" && SameExecutablePath(trust.Target, caller.Executable) {
		// Without an override, the caller already runs the selected binary.
		e.TrustRoot = ""
	}
	return e
}

// trustPlan is the plan a caller's trust must cover. A caller that acts on
// the pin covers the pinned binary, even when the Dev app's rzm runs.
func (p Plan) trustPlan(actsOnPin bool) Plan {
	if actsOnPin && p.DevApp && p.PinnedVersion != "" {
		p.Authority, p.Target, p.DevApp = "managed", p.PinTarget, false
	}
	return p
}
