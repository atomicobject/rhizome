# Desktop starter recovery

The runtime page carries persistent starter readiness and recovery across navigation. Its verified page report can request `openRepositorySettings`; the native host supplies the current pane repository and worktree, then opens the existing repository Settings tab. It accepts no page-supplied repository identity.

`RHIZOME_DESKTOP_STARTUP_TRACE` enables elapsed startup milestones on stderr, from main entry through installation preparation and both WebViews. The Windows portability fixture enables it and records hidden owned windows as well as visible windows, so a delayed first window can be localized without changing the timeout.

# Repository upgrades

Pin offers and repository Settings open `UpgradeSheet` before changing a worktree. `upgrade-plan` previews the bundled release's generated files. Upgrade advances the pin, clears the worktree executable override, applies `settings-apply` with empty choices, then reopens the selected worktree or restarts its running runtime. A retry resumes the failed step and never advances an already saved pin again, including a pin saved before `pin-advance` reported an error. File changes remain uncommitted; the result's `commit` paths supply the copyable Git command, written for PowerShell on Windows.

A ready, trusted, configured worktree without an advance offer gets a background `settings-report`. Pending writes produce the toolbar's files update offer, which uses the same sheet without installation or restart. Checks are serialized per worktree, silent on failure, and repeated after a successful apply.

# macOS app-owned updates

[`updater.rs`](src-tauri/src/updater.rs) keeps the download, prepare-update, import/sign-in cancellation, window recovery, and restart-marker lifecycle. Its macOS installation boundary rechecks `Update.verify`, then calls [`updater/macos.rs`](src-tauri/src/updater/macos.rs). Other platforms continue to call `Update.install` directly.

The adapter validates the entire archive before considering a fallback. It extracts outside the installed bundle and requires the announced version, configured identifier, and an Apple-anchored requirement for the running app's signing team. The fast path requires a user-owned, writable real bundle and `Contents`, a clean root, a valid current signature, and same-volume staging. It atomically exchanges only `Contents`, verifies the result, and rolls back on failure. Failed rollback moves the old `Contents` to a unique private directory under `~/Library/Application Support/<identifier>/recovery/` and reports its exact path. If that move fails, it keeps the staging copy and warns that macOS may delete temporary storage. A successful swap never falls through to the privileged installer. EPERM is logged separately from EACCES. Bundle-root xattrs survive; touch and cleanup cannot turn a committed installation into a reported failure.

The full-bundle Tauri fallback handles root-owned or otherwise ineligible installs after replacement validation. The old installer stages a user-owned replacement, so the one-authorized-update transition needs no chown migration. Signed standard-account A to B to C acceptance is mandatory before readiness; see [the distribution contract](../docs/specs/technical/desktop-distribution-and-updates.md#updater).
