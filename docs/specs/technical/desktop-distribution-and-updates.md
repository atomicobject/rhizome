---
type: TechnicalSpec
id: SPEC-0122
summary: "Every public Rhizome release ships a macOS desktop app that updates itself, carries the matching rzm, offers that rzm on the system PATH while the app is installed, and offers to advance a repository's older pin."
spec-status: active
last-updated: 2026-11-13
aliases:
  - SPEC-0122
---

# Desktop distribution and updates

## Summary

[[rhizome-desktop|Rhizome Desktop]] is built from source today. It has no signed release, no update mechanism, and a version (`0.1.0`) that is unrelated to the Rhizome it runs. It also depends on a separately installed `rzm`: its bridge selects a repository's pinned executable or a global one, and its global management writes `~/.local/bin/rzm` without putting it on the PATH. This spec makes the desktop app the primary way to install Rhizome on macOS.

Every public release publishes the app next to the CLI archives, in the same version as the release. Public desktop builds use Developer ID signing with hardened runtime, Apple notarization, and stapled tickets. Checksums and the updater signature provide additional integrity verification. The app contains that release's `rzm`. It updates itself from the GitHub release feed through Tauri's updater. It offers to make its `rzm` the system `rzm` through a link that stops working when the app is deleted. Folders it sets up pin its version. When a repository's pin is older than the app, it offers to advance the pin and leaves the change in the working tree for the user to commit. The README gives a coding agent enough instructions to install the app without help.

The contracts this builds on are [[version-pinned-install-and-update|SPEC-0033]] (pins, `rzm update` modes, release artifacts), [[rhizome-desktop|SPEC-0113]] (executable selection, trust, global installation), [[desktop-repository-setup|SPEC-0118]] (setup through `rzm init --json`), [[evidence-grounded-release-orchestration|SPEC-0078]] (the release orchestrator), and [[open-source-preparation|SPEC-0107]] (public artifact rules).

## Goals

- One release train: the CLI archives, the desktop app, and the app's update feed come from one tagged GitHub release and share its version.
- A first install on macOS takes at most one documented Gatekeeper step, and later releases reach installed apps without a manual download.
- While the app is installed, `rzm` resolves on the login-shell PATH that terminals and agent apps inherit, and it runs the app's version. After the app is deleted, the app's `rzm` no longer runs.
- A repository the app sets up pins the app's Rhizome version through the existing pin, with no new pinning mechanism.
- A repository whose pin is older than the app gets a visible, dismissible offer to advance the pin. It is never advanced silently.
- `rzm update` keeps working when `rzm` is the app's: the app performs the global update, and repository pins and binaries update as they do today.
- A coding agent can follow the README to download, install, and verify the app.

## Non-Goals

- Windows and Linux desktop builds, and an Intel (x86_64) macOS build. macOS 26 is the last release for Intel Macs; the CLI archives keep serving Intel and other platforms.
- Sparkle. Tauri's first-party updater covers the same signed-feed model without embedding an Objective-C framework.
- Delta updates, beta or nightly update channels, and staged rollouts.
- Desktop artifacts in internal (private S3) releases. Normal public publication is owned by the approved release-PR CI flow; private/internal releases remain local and CLI-only.
- A background agent that removes the PATH link when the app is deleted. The link stops working by construction; the app repairs or removes it when it runs.
- Committing, pushing, or opening pull requests for pin changes, and rerunning `rzm init` to refresh generated files after a pin advance. Refreshing stays in the terminal per SPEC-0118.
- Changing `install-rzm.sh`, its `/etc/paths.d/rhizome` registration, or the Homebrew formula.

## User Stories

### US1 - Every release ships the desktop app

- id:: ^SPEC-0122-US1
- summary:: Merging a reviewed public release PR has CI publish the desktop app and its update feed with the same version as the CLI, or report a recoverable failure.
- status:: draft

#### Acceptance Criteria

- A public release's GitHub assets include `Rhizome-macos-arm64.dmg`, the updater archive `Rhizome-macos-arm64.app.tar.gz` with its `.sig`, and `latest.json`, next to the existing CLI assets.
  verification:: release dry run lists the assets; a real release is inspected with `gh release view`
- `checksums.txt` covers the DMG and the updater archive, and the app inside the DMG passes `codesign --verify --deep --strict` with its Developer ID signature, hardened runtime, and valid notarization ticket.
- The app's `CFBundleShortVersionString`, its bundled `rzm --version`, and the release tag agree.
- The candidate build builds the app, so a desktop toolchain failure stops release-ref mutation, gateway deployment and uploads. Preparation commits remain reviewable; publication retries preserve the approved source and complete only matching partial state.

### US2 - The installed app updates itself

- id:: ^SPEC-0122-US2
- summary:: A user with an older app sees that an update is ready and installs it with one action, without leaving work.
- status:: draft

#### Acceptance Criteria

- An app older than the latest public release shows an unobtrusive "Update ready" control within one check interval and installs the update on the user's restart action.
- "Check for Updates…" in the app menu checks immediately and reports "up to date", "update ready", or the failure.
- An update whose signature does not verify against the app's embedded public key is rejected and the installed app is unchanged.
- A failed check or download leaves the app fully usable and retries on the next interval.

### US3 - rzm is on the PATH while the app is installed

- id:: ^SPEC-0122-US3
- summary:: A user installs the command-line tool from the app once, and `rzm` then works in terminals and agent apps until the app is deleted.
- status:: draft

#### Acceptance Criteria

- After "Install command-line tool" and one administrator prompt, a new login shell resolves `rzm` to the app's executable and reports the app's version.
- After the app is moved to the Trash, `rzm` in a new shell no longer runs the app's executable.
- The app never overwrites or removes an `rzm` it did not create.
- When another `rzm` comes first on the login-shell PATH, the app names it instead of claiming the install succeeded.
- `rzm update` run through the link reports "up to date" when the app is current, and otherwise has the app install the newer release and reports the new version, in every `rzm update` mode that includes the global target.

### US4 - The app offers to advance an older repository pin

- id:: ^SPEC-0122-US4
- summary:: A user opening a repository pinned to an older Rhizome sees a quiet offer to update the pin, and accepting it leaves a reviewable change.
- status:: draft

#### Acceptance Criteria

- A worktree pinned to a version older than the app shows a dismissible indicator naming both versions; it never blocks opening.
- Accepting performs the app's `rzm update --set-version <app version>` for that worktree, then reopens it on the new version; the only tracked change is `rhizome.version` in `.rhizome/config.yml`.
- "Not now" hides the offer for that repository until a newer app version is installed.
- A pin newer than the app offers the app update instead of a downgrade.
- Externally managed worktrees, `devBinaryDir` worktrees, and Rhizome source checkouts show no offer.

### US5 - An agent installs the app from the README

- id:: ^SPEC-0122-US5
- summary:: A user tells a coding agent to install Rhizome from the README, and the agent installs and verifies the app, stopping only for the steps that need the user.
- status:: draft

#### Acceptance Criteria

- The README has an "Install with a coding agent" section with copyable commands that download the latest DMG from a stable URL with `curl`, verify it against the release's `checksums.txt`, copy the app to `/Applications`, and open it.
- The section names the one user action, approving the command-line tool prompt, and gives the verification command for after it.
- A fresh agent session given only the README URL completes the install on a clean macOS account.

## Requirements

### Release artifacts and orchestration

- The release MUST build the desktop app on Apple Silicon for `aarch64-apple-darwin`, bundling the exact `rzm` binary built for `darwin/arm64` and placed in the CLI archive. The candidate build MUST build the app too, so toolchain failures stop release refs, gateway deployment and uploads. Matching partial publication MUST be recoverable without changing approved source or notes.
- The app version MUST come from the release version at build time. `tauri.conf.json`, `Cargo.toml`, and `package.json` MUST NOT carry an independently maintained version.
- Public releases MUST Developer ID sign the bundle and every Mach-O in it with hardened runtime, and sign the final updater archive with the Tauri updater key. The final `checksums.txt` MUST include the DMG and updater archive.
- Public release builds MUST require Apple signing and notarization credentials, notarize and staple through Tauri, and verify the signing team, hardened runtime, stapling and Gatekeeper assessment before staging. Verification MUST inspect both the DMG app and the updater archive app. Development snapshots may use ad-hoc signing; an explicitly notarized candidate MUST use the same verification as publication.
- CI publication and recovery MUST preserve exact approved source, canonical release topology checks, completed-release idempotence and the gateway deployment gate. A separately selected candidate mode MUST build the selected source without deployment or publication and with read-only repository access.
- Asset names MUST be stable across versions so `https://github.com/atomicobject/rhizome/releases/latest/download/<asset>` always resolves to the newest stable release.
- `latest.json` MUST follow Tauri's static updater format with a `darwin-aarch64` entry whose URL points at the same release's updater archive.
- Normal local release preparation MUST need no Apple/updater credentials. Optional local signing loads explicitly scoped credentials from secure local storage. CI signing credentials MUST come from a protected GitHub release Environment; signing backups and notarization keys remain in restricted 1Password storage. CI MUST use an isolated temporary Keychain and protected temporary key files, with cleanup after success or failure. The updater key MUST reach only the protected Linux release coordinator's signing step and any explicitly requested signing probe; the Mac build job MUST NOT receive it ([SPEC-0134](windows-linux-desktop.md) owns the coordinator). Only the desktop signing/notarization subprocess may receive the Apple credentials. The updater signing probe MUST receive only updater signing credentials; verifiers and dependency installation MUST receive no credentials. Keys MUST NOT appear in Git, logs, or artifacts, per [credential policy](../../engineering/secrets.md).
- Every executable in the bundle MUST pass the public-artifact content checks the CLI binaries pass (`scripts/teamkeys/release.py --check-artifact`).
- A public release MUST refuse to publish the app until the committed updater public key is the release key rather than the placeholder. Each updater archive's signature MUST verify against that public key before the coordinator stages the feed. The signed candidate MUST verify that the signing key matches the committed public key before mutating release refs or uploading assets.
- Losing the updater private key strands installed apps. Its custody and backup MUST be documented in `docs/RELEASING.md`.

### Homebrew distribution

- Public CI MUST publish `Casks/rhizome-desktop.rb` in the existing Homebrew tap after verifying and publishing the release assets. Its version and SHA-256 MUST identify the same release's notarized DMG; no separate desktop build is required.
- The cask MUST require Apple Silicon and the minimum macOS version declared by the app, install `Rhizome.app`, link its bundled `Contents/MacOS/rzm` through Homebrew, and declare the app's built-in updates.
- The existing `rhizome` formula MUST remain the CLI-only option for Linux, Intel Macs and headless users. Installation guidance MUST prefer the desktop cask on supported Macs and explain switching from the formula without overwriting an existing executable.
- Publication recovery MUST inspect both generated recipes. An absent cask MUST be created only after proving it is absent; API/authentication failures MUST NOT be treated as absence. A partial tap update MUST resume only outstanding recipe changes after the fresh gateway gate.
- A cask installation MUST NOT require the app's separate command-line installation step. Direct DMG installations retain that step and its ownership safeguards.

### Updater

- The app MUST use `tauri-plugin-updater` with the endpoint `https://github.com/atomicobject/rhizome/releases/latest/download/latest.json` and the release public key embedded at build time.
- The app MUST check at launch and then every 24 hours, download in the background, and install only on the user's restart action. It MUST NOT interrupt or restart on its own.
- Rhizome Dev and local development builds (version `0.0.0`) MUST NOT check for updates.
- Feed checks MUST settle within 30 seconds. Archive downloads MUST reject a 30-second connection or read stall and have a 30-minute overall limit, so progressing slow downloads can finish. Failure MUST release the update lock, permit retry, and preserve an already verified download.
- On macOS, an existing installation MUST upgrade in place at the same app and CLI-link paths with at most one further administrator authorization. Once the app bundle belongs to the user, later updates MUST atomically exchange only `Contents` without requiring write access to the enclosing `/Applications` directory. No migration may change ownership or permissions of `/Applications`.
- Before any installer fallback or installed mutation, the app MUST verify the updater signature, reject unsafe archive paths, escaping links, hardlinks, special files, and unsupported bundle-root entries, and verify the staged bundle's identifier, announced version, strict code signature, and Apple-anchored signing team requirement. Framework symlinks contained within `Contents` MUST remain supported.
- The macOS app-owned path MUST require a real, user-owned, writable app root and `Contents`, staging on the same filesystem outside the installed bundle, no root entries other than `Contents`, and a valid current strict signature. Ineligible installations, including root-owned bundles and damaged current resource seals, MUST use the existing full-bundle installer only after validating the replacement.
- A successful `Contents` swap MUST be followed by strict verification. Failure MUST atomically restore the old `Contents`; if restoration fails, the old copy MUST be retained with its recovery path reported. No privileged fallback may run after a successful swap. Unsupported operations and EACCES or EPERM may use the fallback only before mutation; EPERM MUST be logged distinctly for App Management diagnosis. Touching the bundle and cleanup after a verified commit MUST be best effort. App-root extended attributes MUST be preserved.
- Release readiness for this path MUST include a signed, packaged old-updater A to new-updater B to C sequence under a genuine standard GUI account, through the app and `rzm update`. The A to B transition permits one administrator authorization; B to C permits none. Evidence MUST cover cancellation, restored windows, unchanged CLI links, quarantine, signatures, notarization, and Gatekeeper. A protected-directory test under an administrator account does not satisfy this gate.


### Bundled rzm and the command-line link

- The app MUST contain the release's `rzm`. The bridge MUST use it as the global executable when no user override is set, so setup, and opening a folder that has no pin, run the app's version.
- The desktop bridge MUST be a hidden `rzm desktop-bridge` subcommand that runs on the invoking binary, so the bundle carries one Go executable and the bridge always matches the bundled `rzm`.
- "Install command-line tool" MUST create `/usr/local/bin/rzm` as a symbolic link to the bundled executable, creating `/usr/local/bin` when needed. It MUST try without privileges first and otherwise use one macOS administrator prompt, whose command MUST receive paths as arguments rather than interpolated script text and MUST re-check that the target is absent or a symbolic link before replacing it. `/usr/local/bin` is in `/etc/paths`, so every login shell finds it, and apps that inherit the login-shell PATH do too.
- The app MUST NOT replace or remove `/usr/local/bin/rzm` unless it is a symbolic link into a Rhizome app bundle. Otherwise it MUST report the existing file and leave it.
- At launch, when the link points into a Rhizome app bundle at a different path, such as after the app was moved, the app MUST offer to repair it. Settings MUST offer "Remove command-line tool".
- The app MUST report which `rzm` the user's login shell resolves (`$SHELL -lc 'command -v rzm'`) and its version. When another installation comes first, it MUST name it and say how to make the app's `rzm` win, such as `brew uninstall rhizome` for Homebrew's.
- Without the app, the link dangles, so `rzm` stops running the app's executable. That is the cleanup contract. Neither the app nor this spec promises to delete the link file after the app is deleted.
- Rhizome Dev MUST NOT offer or own the command-line link; the bridge MUST refuse link operations from a Dev bundle, not only the UI.
- The existing `~/.local/bin/rzm` management in Settings MUST be replaced by the bundled executable and the command-line link. The app MUST leave existing `~/.local/bin/rzm` files and `/etc/paths.d/rhizome` entries untouched.

### rzm running from an app bundle

- `rzm update` MUST resolve symbolic links in the running executable's path before deciding whether it is inside a `.app` bundle, so an invocation through `/usr/local/bin/rzm` counts. It MUST NOT write inside the bundle.
- When it is inside a release bundle, every mode that includes the global target (default and `--latest`, inside or outside a pinned repository) MUST hand the global target to the app:
  - It reads the app's update feed (`latest.json`), not the CLI release list, so it never promises a version the app cannot install.
  - When the bundle is current it reports "up to date".
  - Otherwise it launches its own bundle with the fixed `--install-update` argument through the same process-argument path as `rzm desktop`. It waits, with a timeout, until the bundle's version on disk changes, and reports the new version.
- The app MUST treat `--install-update` as an explicit user request: check, verify the update's signature, install, and relaunch, restoring windows. It MUST ignore the argument when no newer verified update exists. No URL scheme or web content can send it.
- The repository target in those modes MUST proceed as SPEC-0033 defines, including the pin-advance prompt, independently of the global hand-off.
- `--pinned` and `--set-version` MUST be unaffected.
- When the hand-off cannot run or does not finish (no GUI session, app error, timeout), the command MUST report the global target as not updated with the release download URL, keep the repository result, and exit as a partial result.
- `rzm` inside a Rhizome Dev bundle MUST NOT hand off and MUST say the Dev app is updated with `make desktop-install-dev`.
- This amends SPEC-0033's global-target rules.

### Repository pin offers

- The app MUST compare a managed worktree's `rhizome.version` with its own version by semantic-version ordering. Equal or unparsable versions MUST show no offer.
- An older pin MUST show an offer that never blocks opening or interrupts the workspace. Accepting MUST perform the bundled Rhizome's `update --set-version <app version>` for that worktree without prompting, using the worktree's existing trust, and MUST then reopen the worktree through the normal open path so Rhizome's ensure replaces a headless runtime of the old build.
- A runtime the user started outside the app keeps running on the old build. The app MUST say it needs a restart instead of stopping it, per SPEC-0113.
- A failed advance MUST leave the pin and the repository binary as `rzm update` leaves them, and MUST show the failure stage it reports.
- Dismissal MUST be stored per repository with the dismissed app version.

### Documentation

- README: a human install section for the desktop cask and notarized DMG fallback, plus an "Install with a coding agent" section. Prefer the cask on supported Macs, retain the formula and installer as explicit CLI-only routes, and distinguish Homebrew-managed CLI links from manual DMG setup.
- SPEC-0113: remove "Publishing or signing a public release" from Non-Goals, and replace Global installation with the bundled executable and command-line link. SPEC-0033: add the app hand-off for the global target. Record a deviation or refreeze on any planned or active effort that froze them.
- `desktop/README.md`, `docs/reference/guides/getting-started.md`, `docs/RELEASING.md` (desktop phase, credentials, key custody), and a `CHANGELOG.md` entry.

### Verification

- Unit tests MUST cover version ordering and the offer states, the link decisions (absent, own link, foreign file, moved app, dangling), the `rzm update` hand-off in each mode (current, newer, timeout, no GUI session, Dev bundle, with and without a pinned repository), and release asset and `latest.json` generation.
- Native verification MUST include `rzm update` through the link updating the app, inside and outside a pinned repository.
- Native verification MUST install a real release DMG on a clean account twice: once downloaded with `curl` and once through a browser, following the README's Gatekeeper step and confirming the linked `rzm` runs. It MUST then install the command-line tool, update from the previous release through the updater, advance a fixture repository's pin, and delete the app and confirm `rzm` no longer runs it. The first public release cannot update from an earlier one; that check runs on the second.

## Decisions

- 2026-11-05: Add a desktop Homebrew cask using the existing notarized DMG and bundled CLI. Retain the CLI-only formula for other platforms and headless use.

- 2026-11-05, Drew Colthorp: require notarization for public desktop builds; support protected CI recovery and non-publishing candidates using the shared desktop build. Normal publication ownership remains local.

- 2026-10-17, Drew Colthorp: ship unsigned (ad-hoc) for now; Developer ID signing and notarization follow when an account is available.
- 2026-10-17, Drew Colthorp: Tauri's updater, not Sparkle.
- 2026-10-17, Drew Colthorp: `/usr/local/bin/rzm` symbolic link into the app bundle, not an `/etc/paths.d` entry. Apps launched by launchd without a login-shell PATH are not covered; changing launchd's PATH needs a restart and is out of scope.
- 2026-10-17, Drew Colthorp: Apple Silicon only.
- 2026-10-17, Drew Colthorp: this lands before the first public release, so that release includes the app.

## Open Questions

- Feasibility risk: macOS 13 and later restrict modifying app bundles in `/Applications`, keyed on the signing Team ID, which an ad-hoc build lacks. If an ad-hoc app cannot replace itself there, the updater cannot work unsigned and Developer ID signing moves into this delivery. The first implementation step tests this.
- Unsigned apps carry an ad-hoc signature that changes with every build, so macOS may ask again for folder-access permissions after an update. Native verification measures this; if it is disruptive, it raises the priority of Developer ID signing.
