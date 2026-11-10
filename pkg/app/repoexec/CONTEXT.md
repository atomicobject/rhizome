# Repository executable selection

`Select` is the shared, inert authority decision for CLI dispatch and desktop.
It preserves external ownership, configured development builds, Rhizome source
builds, then managed pins, with the caller's global executable as fallback. A
Rhizome source checkout without `bin/<goos>/rzm` selects an installed Rhizome
Dev app's `rzm` (`DevAppExecutable`: `RZM_DEV_APP_RZM`, else the standard
macOS or Linux install location, read from the caller's environment through
`SelectWithEnv`; `Select` reads the process, whose `RZM_DEV_APP_RZM` and
`HOME` `main` keeps out of repository dotenv) ahead of a pin, so the Dev app and a terminal
`rzm` run the same build. It is a development authority, but the checkout did not
choose it, so it needs no trust, pinned or not, unless the caller acts on the
pin. CLI dispatch does (`init` and `update` act on the pin whichever binary
runs them), so `TrustRequirement` keeps the trust a pinned checkout's binary
requires. `update` stays on the invoking executable so it
never replaces the app's `rzm`. Tests set `RZM_DEV_APP_RZM` empty so results do not depend on the
machine.

`Resolve` completes a plan for a `Caller` into an `Execution`. Both front ends
take trust from it, and desktop also acts on the rest; CLI dispatch still
chooses its target and bootstrap in `DecisionFor`. An `Execution` holds the exact target and why, whether it is the caller's own
executable (the invoked `rzm`, or the app's global one), the trust root, a
managed pin to install first, and whether the target's file identity is its build
(not for an external manager's executable or the user's choice, which may be
shims). A caller with `ActsOnPin` covers the pinned binary's trust even when
the Dev app's `rzm` runs; without an explicit override, a caller already
running the selected binary needs no trust. An explicit desktop worktree choice takes precedence over a managed pin or global
fallback and never installs the pin. Clearing it restores the repository selection.
Development authority keeps its source build. The repository's canonical-checkout
trust still applies when a managed pin is overridden, including by the app's own
executable. CLI callers supply no Chosen value, so CLI dispatch keeps its policy.
Trust is decided only in Resolve; a change to the target or install fields reaches
desktop, not CLI dispatch.

`TrustRequirement` identifies whether CLI dispatch needs canonical-checkout
trust, from `Resolve` with `ActsOnPin`. When it needs none, dispatch and hooks do not resolve a
repository build again; `DevAppHandoff` returns only a Dev app target, which
the checkout cannot choose. `DecisionFor` applies command-specific control-plane exceptions and
checks managed installations only after the caller has established trust.
`Bootstrap` calls the existing checksum-verified pinned installer.

The CLI owns prompting and process handoff in `cmd/repo_delegate*.go`. Desktop
owns its explicit trust interaction and resolves `Select`'s plan with
`Resolve`, supplying its global executable and the worktree's chosen one. Informational
probes are bounded and disable both re-delegation guards; callers supply their
working directory so an external manager's shim resolves the selected folder.
Probes run without a Windows console window; this does not detach them or change
their output, cancellation, or exit status.

`credentials`, `auth`, `desktop`, and `desktop-bridge` remain on the invoking executable and bypass
repository trust prompting and delegation. The bridge owns explicit trust
checks before any repository-selected probe, bootstrap, or runtime launch.
