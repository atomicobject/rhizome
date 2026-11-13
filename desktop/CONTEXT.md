# Desktop starter recovery

The runtime page carries persistent starter readiness and recovery across navigation. Its verified page report can request `openRepositorySettings`; the native host supplies the current pane repository and worktree, then opens the existing repository Settings tab. It accepts no page-supplied repository identity.

`RHIZOME_DESKTOP_STARTUP_TRACE` enables elapsed startup milestones on stderr, from main entry through installation preparation and both WebViews. The Windows portability fixture enables it and records hidden owned windows as well as visible windows, so a delayed first window can be localized without changing the timeout.

# Repository upgrades

Pin offers and repository Settings open `UpgradeSheet` before changing a worktree. `upgrade-plan` previews the bundled release's generated files. Upgrade advances the pin, clears the worktree executable override, applies `settings-apply` with empty choices, then reopens the selected worktree or restarts its running runtime. A retry resumes the failed step and never advances an already saved pin again, including a pin saved before `pin-advance` reported an error. File changes remain uncommitted; the result's `commit` paths supply the copyable Git command, written for PowerShell on Windows.

A ready, trusted, configured worktree without an advance offer gets a background `settings-report`. Pending writes produce the toolbar's files update offer, which uses the same sheet without installation or restart. Checks are serialized per worktree, silent on failure, and repeated after a successful apply.
