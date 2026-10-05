# Repository executable selection

`Select` is the shared, inert authority decision for CLI dispatch and desktop.
It preserves external ownership, configured development builds, Rhizome source
builds, then managed pins, with the caller's global executable as fallback.

`TrustRequirement` identifies whether CLI dispatch needs canonical-checkout
trust. `DecisionFor` applies command-specific control-plane exceptions and
checks managed installations only after the caller has established trust.
`Bootstrap` calls the existing checksum-verified pinned installer.

The CLI owns prompting and process handoff in `cmd/repo_delegate*.go`. Desktop
owns its explicit trust interaction and uses `Select` directly. Informational
probes are bounded and disable both re-delegation guards; callers supply their
working directory so an external manager's shim resolves the selected folder.
