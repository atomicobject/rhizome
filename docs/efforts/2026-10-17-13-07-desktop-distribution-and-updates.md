---
type: EffortNote
id: EFF-2026-10-17-13-07
aliases: [EFF-2026-10-17-13-07]
name: Desktop distribution and updates
created-at: 2026-10-17T17:08:37Z
status: active
summary: "Ship Rhizome Desktop with every public release: the app carries the release's rzm, updates itself through Tauri's updater, links rzm onto the PATH while installed, offers to advance older repository pins, and installs from README instructions an agent can follow."
governing-specs:
  - "[[desktop-distribution-and-updates]]"
---

# Desktop distribution and updates

## Scope

Deliver [[desktop-distribution-and-updates|SPEC-0122]] in one pull request, with the spec amendments it names for [[rhizome-desktop|SPEC-0113]] and [[version-pinned-install-and-update|SPEC-0033]]. Running `make cut-release` is not part of this effort; the first public release that carries the app is Drew's action. The effort closes when its pull request merges, and the criteria only a published release can meet become follow-up action items (decision 9). The same pull request closes the three earlier desktop efforts whose pull requests merged without closure: [[2026-10-03-18-59-rhizome-desktop|EFF-2026-10-03-18-59]], [[2026-10-06-08-07-desktop-repository-setup|EFF-2026-10-06-08-07]], and [[2026-10-07-17-15-desktop-unified-toolbar|EFF-2026-10-07-17-15]].

Excluded: SPEC-0122's non-goals, including Developer ID signing, notarization, Intel builds, a Homebrew cask, and desktop artifacts in internal releases.

## Spec Set (Frozen)

Frozen at plan approval on 2026-10-17:

- [[desktop-distribution-and-updates|SPEC-0122]], all requirements in the 2026-10-17 revision.

[[rhizome-desktop|SPEC-0113]] and [[version-pinned-install-and-update|SPEC-0033]] receive the amendments SPEC-0122's documentation requirements name. Both are frozen by the active [[2026-10-03-18-59-rhizome-desktop|EFF-2026-10-03-18-59]], which receives a frozen-scope-drift deviation.

## Stories In Scope (Frozen)

- [[desktop-distribution-and-updates#^SPEC-0122-US1|SPEC-0122.US1]] Every release ships the desktop app
- [[desktop-distribution-and-updates#^SPEC-0122-US2|SPEC-0122.US2]] The installed app updates itself
- [[desktop-distribution-and-updates#^SPEC-0122-US3|SPEC-0122.US3]] rzm is on the PATH while the app is installed
- [[desktop-distribution-and-updates#^SPEC-0122-US4|SPEC-0122.US4]] The app offers to advance an older repository pin
- [[desktop-distribution-and-updates#^SPEC-0122-US5|SPEC-0122.US5]] An agent installs the app from the README

## Spec Coverage Checklist

- [ ] [[desktop-distribution-and-updates#^SPEC-0122-US1|SPEC-0122.US1]]: release builds the app from the tag, publishes stable-named assets, `latest.json`, and checksums; fails before publication.
- [ ] [[desktop-distribution-and-updates#^SPEC-0122-US2|SPEC-0122.US2]]: updater checks, "Update ready" control, menu item, signature rejection, failure tolerance; off in Rhizome Dev.
- [ ] [[desktop-distribution-and-updates#^SPEC-0122-US3|SPEC-0122.US3]]: bundled `rzm`, `/usr/local/bin/rzm` link lifecycle, login-shell resolution report, `rzm update` hand-off to the app.
- [ ] [[desktop-distribution-and-updates#^SPEC-0122-US4|SPEC-0122.US4]]: version ordering, offer states, advance through `update --set-version`, reopen, dismissal.
- [ ] [[desktop-distribution-and-updates#^SPEC-0122-US5|SPEC-0122.US5]]: README human and agent install sections.
- [ ] Documentation and spec amendments; deviation on EFF-2026-10-03-18-59.
- [ ] Release-dependent criteria (real assets, a real update from one release to the next) recorded as follow-up action items for the first and second public releases.

## Plan

### Current gap

- **No bundled `rzm`.** The app ships one sidecar, `rhizome-desktop-bridge` (27 MB; `desktop/bridge/main.go`, 26 lines over `pkg/app/desktop`). `bridge.rs` spawns it per request. `globalExecutable` (`pkg/app/desktop/global.go`) searches `~/.local/bin`, `PATH`, `/opt/homebrew/bin`, and `/usr/local/bin`, so a fresh machine has no executable and setup cannot run. `InstallGlobal` writes `~/.local/bin/rzm` with no PATH registration.
- **No version.** `0.1.0` is hard-coded in `tauri.conf.json`, `Cargo.toml`, and `package.json`. There is no updater plugin, signing configuration, or release step. The CI desktop job builds an unsigned `.app` and discards it.
- **Release.** `release_cli.py` checkpoints `built → applied → canonical_branches_published → github_published → (s3_published) → published`. GoReleaser builds and publishes in one step and uploads `.tmp/goreleaser-extra/*` through `release.extra_files`. No public tag exists yet; `version.go` says `v0.50.5`.
- **Pins.** Setup already pins the running executable's version (`pkg/app/cli/init/rerun.go:94`). `rzm update --set-version` already rewrites a pin and installs the binary atomically. Go has no ordered version comparison. `pkg/app/update` would replace whatever `os.Executable()` resolves to, including a file inside an app bundle.

### Decisions

Settled with Drew on 2026-10-17 (recorded in SPEC-0122): ad-hoc signing for now, Tauri's updater, a `/usr/local/bin` symbolic link, Apple Silicon only. Routine choices fixed here so the batches share one contract:

1. **One Go executable.** The bridge becomes a hidden `rzm desktop-bridge` subcommand with the same flags and JSON protocol, and `desktop/bridge` is deleted. The bundle's only sidecar is `rzm`, so the bridge and the bundled CLI always share a version. The subcommand runs on the invoking binary (no repository delegation, no diagnostics wiring), like `desktop`.
2. **Bundled executable is the global executable.** When the bridge runs from inside an `.app` bundle and no user override is set, `globalExecutable` returns its own `os.Executable()`. The `~/.local/bin` install and update operations and their Settings controls are removed.
3. **Command-line link in Go.** `pkg/app/desktop/cli_link.go` classifies `/usr/local/bin/rzm` as absent, this app's link, another Rhizome bundle's link (moved app or other copy), a dangling Rhizome link, or foreign. It tries `os.Symlink` unprivileged first and prompts only on a permission error. The prompt runs `osascript -e 'on run argv' -e 'do shell script item 1 of argv with administrator privileges' -e 'end run' <shell text>`, so no path is interpolated into AppleScript. The shell text single-quotes fixed paths and re-checks `[ ! -e P ] || [ -L P ]` before `ln -sfn`, closing the gap between the unprivileged check and the privileged write. Link operations refuse when the bridge's own bundle is a Dev build. A test seam overrides the link directory and the privileged runner. It reports the login shell's resolution by running the user's `$SHELL -lc 'command -v rzm'` with a timeout. New bridge operations: `cli-link-status`, `cli-link-install`, `cli-link-remove`.
4. **Pin offer in Go.** `inspect` gains `pinOffer: {state: none|advance|app-older, pinned, available}`, computed by a small `vMAJOR.MINOR.PATCH[-pre]` comparator in `pkg/app/update` (no new dependency). A new `pin-advance` operation requires the worktree to be trusted and calls `appupdate.Run` in process with `SetVersion: version.Version, Yes: true`, as `InstallGlobal` already does. The fixture test asserts `git status` shows only `.rhizome/config.yml`; `SaveLocalConfig` also rewrites `.rhizome/workflows.yml`, so any spurious diff there is fixed in this batch. Dismissal lives in the desktop library entry as `pinOfferDismissed: <version>`, an optional field that needs no migration.
5. **`rzm update` hands the global target to the app.** `pkg/app/update` resolves the invocation path with `filepath.EvalSymlinks` and checks for `*.app/Contents/`, so `/usr/local/bin/rzm` counts. Without that, `replaceFile` would rename a real binary over the link on machines where `/usr/local/bin` is writable. In a bundle, the plan never contains a file-replacement global target; for that bundle, `BuildUpdatePlan` treats the invocation as repository-scoped. Instead, `Run` gains an app-owned global step:
   1. Fetch `latest.json`, the same endpoint the app's updater uses.
   2. When the bundle is current, report "up to date".
   3. Otherwise run `open -a <bundle> --args --install-update`.
   4. Poll the bundle's `Info.plist` `CFBundleShortVersionString` (default timeout 5 minutes) and report the new version, or a partial result with the download URL.

   The repository target runs as today, prompt included. A Dev bundle prints the `make desktop-install-dev` guidance. The `open` runner and the clock are test seams. On the app side, `main.rs` and the single-instance handler accept `--install-update` as a fixed flag and route it to the updater's install-and-relaunch path, which the shell's "Update ready" control also uses. `desktop-bridge` is added to `isRepositoryTrustControlInvocation` (`pkg/app/repoexec/trust.go`) and the diagnostics skip list (`cmd/diagnostics_wiring.go:61`).
6. **Version.** The three version fields become `0.0.0`, meaning a development build. Release builds pass the version through `tauri build --config`. Settings shows the bundled `rzm --version`.
7. **Updater configuration.** `tauri.conf.json` carries the updater endpoint and public key. `createUpdaterArtifacts` lives only in a new `tauri.release.conf.json`, so CI and `make desktop-build` need no key. Rust schedules checks (launch, then every 24 hours), downloads in the background, and emits `update` events to the shell. The shell shows the control and calls `install-update`, which installs and relaunches through `tauri-plugin-process`. Neither the updater nor the CLI link is registered when the bundle identifier ends in `.dev`. Updater permissions go to the shell webview only.
8. **Release placement: a GoReleaser post-build hook, no new checkpoint.** The `rzm-darwin` build in `.goreleaser.yml` gains a post hook, `scripts/release/build_desktop.py "{{ .Path }}" "{{ .Version }}"`. The hook runs only for the arm64 build of a public release. Its steps:
   1. Run the existing `teamkeys --check-artifact` check.
   2. Copy that exact binary into `src-tauri/binaries/rzm-aarch64-apple-darwin`.
   3. Run `tauri build --bundles app,dmg` with the release config and version.
   4. Run `codesign --verify --deep --strict`.
   5. Clear and rewrite its own stable-named outputs in `.tmp/goreleaser-extra/`.
   6. Write `latest.json` pointing at the tag's download URL.

   `release.extra_files` and `checksum.extra_files` pick up those outputs. The existing snapshot `built` phase runs the same hook before any repository change, which is the early failure US1 requires; it signs updater artifacts only when the key is present. The publish step sets `RZM_RELEASE_DESKTOP=1`, and with it set the hook fails, rather than skipping, when the key is missing, so a normal release cannot go out without the app. The GitHub Actions fallback does not set it and publishes CLI assets only, per SPEC-0122's non-goal. `run_clean.py` passes `TAURI_SIGNING_PRIVATE_KEY` and its password through, the same exposure class as `GITHUB_TOKEN` today.
9. **Closure timing.** The effort closes in its own pull request. The criteria only a published release can meet are verified locally against release-config builds. Their real-release checks become action items: assets, checksums, the Gatekeeper step, and the agent install on the first public release; the in-app update on the second. Drew chose this over keeping the effort open (2026-10-17).

### Prerequisite owned by Drew

Before merge: generate the updater key pair (`npx tauri signer generate`), store the private key and password in the release 1Password Environment, keep an offline backup, and share only the public key, which is committed to `tauri.conf.json`. I will provide a short guided script for this. Development and tests use a throwaway key generated in a temporary directory.

### Batches

| Batch | Outcome | Work | Exit evidence |
| --- | --- | --- | --- |
| 0. Feasibility spike | Know whether an ad-hoc-signed Tauri app in `/Applications` can replace itself through the updater on this macOS. | Throwaway: two ad-hoc builds (`0.0.1`, `0.0.2`) with a temporary key, a `latest.json` served from a local HTTP server, an endpoint override; install `0.0.1` to `/Applications` with `ditto` and trigger the update. Nothing is committed. | Updated bundle version and a clean relaunch, or the exact failure. A failure stops the effort for Drew's decision on Developer ID signing (SPEC-0122 Open Questions). |
| 1. Go: bridge, bundled executable, link, pins, refusal | `rzm` serves the desktop protocol, treats its own bundle as the global executable, manages the link, computes and applies pin offers, and refuses to overwrite itself inside an app. | `cmd/desktop_bridge.go`; delete `desktop/bridge`; delegation and diagnostics exemptions. `pkg/app/desktop`: `global.go` (decision 2), `cli_link.go`, `protocol.go` ops, `service.go` `pinOffer`, `pin-advance`, library field. `pkg/app/update`: comparator and app-bundle refusal. Update `pkg/app/desktop/CONTEXT.md` and `pkg/app/update/AGENTS.md`. Load `cli-subsystem` first. | Go tests: comparator table; offer states (older, equal, newer, unparsable, external, `devBinaryDir`, source checkout); link classification and planned privileged command for every state, through the seam, including Dev refusal; hand-off through a symlink fixture into a fake bundle for default and `--latest`, inside and outside a pinned fixture (current, newer, timeout, `open` failure, Dev bundle), with the repository half and its prompt unchanged; `desktop-bridge` exempt from delegation; `pin-advance` against a local release fixture server. `make check`. |
| 2. Desktop app | The app runs the bundled `rzm`, updates itself, and shows the link and pin controls. | `bridge.rs` spawns `rzm desktop-bridge`. `build-bridge.mjs` becomes `build-rzm.mjs`, building `rzm` with the web UI. `externalBin`, version fields, `tauri.release.conf.json`, `tauri-plugin-updater`, `tauri-plugin-process`, capabilities. Rust `updater.rs` (schedule, events, install and relaunch; off for Dev); `--install-update` argument at launch and through single-instance; "Check for Updates…" menu item. Shell: update control in the sidebar footer; Settings command-line tool section replacing global install; pin-offer chip with popover next to the worktree selector; reopen after advancing. `desktop/README.md`. Load `impeccable` for the two new controls. | `cargo test` for update-state transitions, the Dev guard, and `--install-update` parsing (ignored when no verified update exists). Vitest for the update control, Settings link states, and the pin chip (advance, not now, app older, failure stage). `make desktop-check`, `make desktop-build`. Shell browser pass with screenshots at 760 and 1280 widths. |
| 3. Release pipeline | `make release-dry` builds the app; `cut-release` publishes it with the CLI from the same binary. | `scripts/release/build_desktop.py` hook (decision 8), `latest.json` writer, `.goreleaser.yml` post hook plus extra and checksum files, `RZM_RELEASE_DESKTOP` in the publish command, `run_clean.py` pass-through, `scripts/with-secrets` mapping. `docs/RELEASING.md`: desktop step, credentials, key custody and backup. | Python tests: hook no-ops for amd64 and internal releases; fails without the key when `RZM_RELEASE_DESKTOP=1`; `latest.json` shape; output clearing; pass-through limited to the publishing subprocess. A local `make release-dry` producing the DMG, updater archive, and `latest.json`. |
| 4. Docs and spec amendments | Users and agents can install from the README; dependent specs match. | README: human install (DMG, Gatekeeper step through System Settings > Privacy & Security > Open Anyway, command-line tool) and "Install with a coding agent" (curl, checksum, `hdiutil`, `ditto`, `open`, verification, the one user step; `curl`, `hdiutil`, and `ditto` set no quarantine, so the agent path needs no Gatekeeper step), keeping Homebrew and the installer as CLI routes. `getting-started.md`. SPEC-0113 Non-Goals and Global installation; SPEC-0033 app hand-off for the global target; `last-updated` bumps; deviation on EFF-2026-10-03-18-59; `rzm agent validate frozen-scope-drift`. `CHANGELOG.md` Unreleased entry. | Validation (ontology, identifiers, broken links, frozen-scope drift) with the drift findings acknowledged. A fresh agent session, given only the README, walks the install against a local build served over HTTP, or the gap is recorded for the first release. |
| 4b. Earlier desktop efforts | EFF-2026-10-03-18-59, EFF-2026-10-06-08-07, and EFF-2026-10-07-17-15 are closed with their delivered truth. | Closure pass for each through `agentic-engineering finish`: actual delivery from their merged pull requests, deviations (including this effort's SPEC-0113 and SPEC-0033 amendments on EFF-2026-10-03-18-59), and closure checklists. Anything still owed, such as Drew's SPEC-0118 click-through, becomes an action item rather than holding the effort open. | Each effort `complete` with evidence; validation passes. |
| 5. Integration, native verification, review, PR | The packaged app works end to end on this machine, and the change is reviewed. | `make check`, `make desktop-check`, `make desktop-build`. Native run of a release-config build (throwaway key, version `0.0.1`) with `RHIZOME_DESKTOP_DATA_DIR` set to a temporary directory: setup pins the bundled version; the pin offer advances a fixture repository pinned older. Updater: a second build (`0.0.2`) served from a local `latest.json` through an endpoint override in a test build updates the first, once from the app control and once through `rzm update` via the bundled executable, inside and outside a pinned fixture. The link install and its Gatekeeper behavior need an admin prompt, so they are left to Drew's click-through, with a written checklist. Independent review (Claude Fable), accepted fixes, pull request with `link_pull_request`. | Gate output, native evidence or recorded gaps, review outcome, PR link, and Drew's click-through checklist. |

Batch 0 runs first and gates everything. After it, Batches 1, 3, and the README part of 4 can run in parallel once decision 1's subcommand name and decision 3's operation names are fixed (they are, above). Batch 2 needs Batch 1's protocol. Batch 5 needs everything. Delegation: Batch 1 and Batch 3 go to separate implementers in their own worktrees through T3 `delegate_task`; I integrate, own Batch 2's UI with `impeccable`, and run Batch 5.

Escalation: Drew decides Developer ID signing if Batch 0 fails, any change to what a release publishes beyond SPEC-0122's assets, any new privileged operation beyond the link, and the closure timing in decision 9. Asset names, UI placement within the stated surfaces, and test layering are routine.

## Plan Approval

Drew approved the plan in chat on 2026-10-17 (2026-10-17T17:28:17Z): "approved", in reply to the request to approve the plan, together with these answers: close the effort at merge (decision 9 revised), land before the first public release, generate the updater key after the bulk of the work (still before merge), and close the three earlier desktop efforts in this pull request (Batch 4b). The current-user configuration is absent, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

A Mac user downloads one DMG from the latest GitHub release, or has an agent do it, and gets an app that updates itself. One click and one password prompt put `rzm` on every login shell's PATH; deleting the app takes that `rzm` away. Folders set up in the app pin the app's Rhizome, and repositories with an older pin show a quiet offer to advance it. Every public release builds and publishes the app with the CLI from the same tag.

## Actual Delivered

Not started.

## Execution Notes

- 2026-10-17T17:08:37Z [decision] Drew chose unsigned (ad-hoc) distribution for now, Tauri's updater, a `/usr/local/bin` symbolic link, and Apple Silicon only, in reply to the four-question decision prompt.
- 2026-10-17T17:08:37Z [validation] Independent plan review (Claude Fable) approved with fixes, all taken: symlink resolution before the app-bundle check, repository half of `rzm update` kept inside pinned repositories, argument-passing privileged command with a re-check, Dev refusal in the bridge, Homebrew precedence copy, `workflows.yml` diff check, the real public key committed before merge, and a GoReleaser post-build hook instead of a new checkpoint. It named ad-hoc self-update in `/Applications` as the one feasibility risk, now Batch 0.
- 2026-10-17T17:08:37Z [decision] Drew asked that `rzm update` work gracefully, globally and per repository, rather than refuse inside the app. The global target is handed to the app through `--install-update`, and the repository target is unchanged. Spec and plan revised before approval.
- 2026-10-17T17:33:00Z [validation] Batch 0 passed on macOS 27.2 (Apple Silicon). A throwaway Tauri 2.12.1 app with `tauri-plugin-updater` 2.14.0, ad-hoc signed through `bundle.macOS.signingIdentity: "-"` (`flags=adhoc,runtime`, no Team ID), was installed to `/Applications` with `ditto`. It fetched a local `latest.json`, verified and installed 0.0.2, and relaunched as 0.0.2. No permission prompt appeared, and `codesign --verify --deep --strict` passed on the updated bundle. The spike app was removed afterward. Not covered, and left for Drew's click-through: a first install carrying the browser quarantine attribute, and an `/Applications` copy owned by another administrator.
- 2026-10-17T17:33:00Z [learning] `tauri::generate_context!` with plugin configuration requires `serde_json` as a direct dependency of the app crate.
- 2026-10-17T17:08:37Z [learning] SPEC-0121 is claimed by uncommitted session-memory work in worktree `t3-1405af06`; this spec took SPEC-0122 to avoid a collision the allocator cannot see.

## Deviations

None.

## Closure Checklist

- [ ] Quality gates: `make check`, `make desktop-check`, `make desktop-build`.
- [ ] Native verification and Drew's click-through checklist.
- [ ] Alignment check against SPEC-0122.
- [ ] Spec amendments, frozen-scope-drift acknowledgements, documentation, and CHANGELOG.
- [ ] Action items for the first and second public release checks.
- [ ] Earlier desktop efforts closed (Batch 4b).
- [ ] Compounding triage.

## Compounding Follow-ups

None yet.

## Status

Active. Batch 0 passed; Batches 1 and 3 are delegated, and Batch 2 is in progress.
