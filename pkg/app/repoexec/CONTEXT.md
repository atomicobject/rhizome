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
choose it, so an unpinned checkout needs no trust. A pinned one keeps the trust
its pinned binary requires, because `init` and `update` act on the pin
whichever binary runs them. `update` stays on the invoking executable so it
never replaces the app's `rzm`. Tests set `RZM_DEV_APP_RZM` empty so results do not depend on the
machine.

`TrustRequirement` identifies whether CLI dispatch needs canonical-checkout
trust. When it needs none, dispatch and hooks do not resolve a
repository build again; `DevAppHandoff` returns only a Dev app target, which
the checkout cannot choose. `DecisionFor` applies command-specific control-plane exceptions and
checks managed installations only after the caller has established trust.
`Bootstrap` calls the existing checksum-verified pinned installer.

The CLI owns prompting and process handoff in `cmd/repo_delegate*.go`. Desktop
owns its explicit trust interaction and uses `Select` directly. Informational
probes are bounded and disable both re-delegation guards; callers supply their
working directory so an external manager's shim resolves the selected folder.

`credentials`, `auth`, `desktop`, and `desktop-bridge` remain on the invoking executable and bypass
repository trust prompting and delegation. The bridge owns explicit trust
checks before any repository-selected probe, bootstrap, or runtime launch.
