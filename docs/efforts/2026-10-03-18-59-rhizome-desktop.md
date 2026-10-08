---
type: EffortNote
id: EFF-2026-10-03-18-59
aliases: [EFF-2026-10-03-18-59]
name: Rhizome desktop application
created-at: 2026-10-03T22:59:47Z
status: active
summary: "Build and verify a Tauri desktop app that presents Rhizome repositories with switchable worktrees and starts each worktree's configured runtime."
governing-specs:
  - "[[rhizome-desktop]]"
  - "[[version-pinned-install-and-update]]"
  - "[[vault-runtime-coordination]]"
---

# Rhizome desktop application

## Scope

A macOS-first Tauri app with a persistent folder library, repository UI windows, automatic configured runtime startup, and global installation management. The user requested the feature, then explicitly instructed "Build it" in chat. This authorizes implementation and local verification. Publication and deployment are excluded.

On 2026-10-04, before publication, Drew Colthorp expanded this effort rather than opening a stacked one: repositories become single units with switchable Git worktrees, a missing worktree database is seeded from the primary worktree (copy-on-write where available, through `rzm new-worktree`), every window gains a collapsible repository sidebar and toolbar in place of the launch screen, window state and selection persist, and opening shows loading states until the workspace is available. The spec set was refrozen at that revision.

## Spec Set (Frozen)

- [[rhizome-desktop]] requirements, authored 2026-10-03 from the user's request and refrozen 2026-10-04 after the worktree, sidebar, and loading-state revision.
- [[version-pinned-install-and-update]] executable selection, external ownership, trust, and global update boundaries at the start of this effort.
- [[vault-runtime-coordination]] discovery, election, and process ownership boundaries, refrozen 2026-10-04 with the `rzm new-worktree` copy-on-write snapshot amendment.

## Stories In Scope (Frozen)

This is a requirements-only slice. The requirements in [[rhizome-desktop#Requirements]] define the acceptance scope.

## Spec Coverage Checklist

- [x] Saved folders, canonical identity, and missing-folder recovery.
- [x] Configured executable selection and explicit trust.
- [x] Runtime startup, reuse, and isolated repository windows.
- [x] Global install/update controls with repository isolation.
- [x] Packaged application, relevant tests, native verification, and usage documentation.
- [ ] Repository identity across worktrees, library migration, and missing-path recovery.
- [ ] Worktree discovery, new indicators, primary worktree selection, and database seeding.
- [ ] `rzm new-worktree` copy-on-write snapshot with `VACUUM INTO` fallback.
- [ ] Sidebar and toolbar windows without a launch screen, multiple windows, and restored layout and selection.
- [ ] Isolated content webview, per-worktree trust in the content area, loading states, and stale-open discarding.
- [ ] Runtime presence for every worktree, external start and stop detection, and keeping displayed worktrees' runtimes alive.
- [ ] `rzm desktop` opens the current worktree in the most recently focused window and fails clearly without an installed app.
- [ ] Packaged application exercised with a multi-worktree repository, updated tests, and documentation.

## Plan Approval

The original delivery was authorized by Drew Colthorp's "Build it" instruction on 2026-10-03. The 2026-10-04 expansion plan below was approved by Drew Colthorp in chat on 2026-10-04 ("Approve as written"), together with Tauri's unstable multi-webview feature, per-worktree trust prompts, and the copy-on-write path in `rzm new-worktree`.

## Plan

1. Ground the executable, runtime, update, and frontend boundaries. Compare independent integration designs and record the selected ownership model.
2. Implement the native controller and Tauri launch screen against one typed interface. Preserve existing Rhizome runtime and binary-selection contracts.
3. Build the app, exercise saved-folder and real-runtime journeys, and test installation failure/success against local synthetic release fixtures.
4. Run applicable repository gates and an independent review, fix findings, and record the verified artifact and limitations.

Expansion (approved 2026-10-04):

5. Merge current `origin/main` and rerun the gates before further work.
6. Add the copy-on-write fast path to `rzm new-worktree`: clone the database and WAL under the source's SQLite write lock, falling back to `VACUUM INTO`, with a test that copies while another connection writes the source.
7. Model repositories by common Git directory with discovered worktrees, a primary worktree, new-worktree markers, and migration from the version 1 folder library.
8. Replace the launcher with sidebar-and-toolbar windows: a shell webview plus an isolated child content webview, multiple windows, and restored window layout and selection.
9. Open worktrees through a staged pipeline with loading states: inspect, trust, seed through the worktree's `rzm new-worktree`, ensure the runtime, then load the content and discard stale opens.
10. Keep displayed worktrees' runtimes alive (added 2026-10-04 at Drew Colthorp's request): count open UI event streams as runtime activity in Rhizome, show running state for every worktree, detect external starts and stops, relaunch a displayed worktree's runtime when it exits, and follow a replacement runtime.
11. Add `rzm desktop [path]` (requested by Drew Colthorp on 2026-10-04): the CLI checks the folder, finds the installed app, and hands it the path through process arguments; the app adds the repository if needed and selects the containing worktree in its most recently focused window.
12. Verify with unit tests, the packaged app on a real multi-worktree repository, a creation-to-load timing check, and an independent review, then update documentation.

The user's direct build instruction supplies execution authority for this bounded feature. No separate plan approval identity is fabricated; this worktree has no configured current Person.

## Original Intended Delivery

A local desktop app that lets the user select a saved repository and work in that repository's Rhizome UI without starting a server manually.

## Actual Delivered

Implemented the Tauri 2 application in `desktop/`, with a bundled one-request Go companion, persistent canonical folder registry, explicit trust and setup, repository windows, configured executable selection, and managed global installation controls. CLI and desktop now share inert executable selection through `pkg/app/repoexec`.

The source-built macOS app is `desktop/src-tauri/target/release/bundle/macos/Rhizome.app`. The existing Rhizome branding is retained. Make targets, a macOS CI job, usage documentation, subsystem constraints, and an Unreleased changelog entry accompany it.

The work is local on `t3code/evaluate-rhizome-desktop`, based on `2d7e0c3`. It has not been published, pushed, or merged. The effort remains active for the repository's PR/merge closure requirement; implementation and local acceptance are delivered.

## Execution Notes

- Initial inspection confirmed repository-aware executable delegation, version bootstrap, runtime manifest/probe, and global update services already exist.
- The worktree initially lacked a built launcher binary. `make build` completed and a Rhizome agent session now works. Initial indexed context was unavailable; direct code and normative specifications supplied grounding.
- Rust was installed with the minimal official toolchain without changing shell startup files.
- Design exploration compared Opus and Sol. The Grok lane failed authentication preflight and supplies no design evidence.

The selected design keeps executable selection, trust, installation, and runtime identity in Go, with a thin Rust window/persistence controller and a React launcher. Independent frontend and backend work followed a fixed JSON contract and was integrated and reviewed together.

Verification on macOS arm64:

- `make build`: passed with the current repository UI embedded.
- `make check-full`: passed, including race tests, integration tests, benchmark contracts, web checks, and web unit tests. A later check identified stale generated Greptile guidance after adding subsystem paths; regenerating it with `scripts/subsystem-guidance-map --greptile` fixed the gate, and the full check passed again.
- `make desktop-deps`: clean `npm ci` passed.
- `make desktop-check`: passed, including six frontend tests, nine Rust tests, TypeScript, lint/format, and Clippy.
- `make desktop-build`: produced the native app bundle. The final build includes only a localhost-scoped App Transport Security exception; it does not enable arbitrary HTTP hosts.
- Focused Go tests cover canonical identity, trust before execution/download, owned and external binary selection, protocol bounds, runtime origin/identity, update isolation, successful synthetic release installation, checksum failure preserving the installed binary, and subprocess pipe timeout behavior. Desktop, resolver, runtime, and update race suites passed.
- Packaged app: added two fixture folders, confirmed canonical paths and trust prompts, opened both configured runtimes, retained the folder list across Quit/relaunch, reused an existing runtime, and recovered the existing window after its fixture runtime was killed.
- Packaged app: HTML note preview rendered its content; a custom TSX view rendered and updated its button state. A custom view calling the top window's native command bridge received `Command desktop_request not allowed by ACL`.
- Packaged app: selected the current development binary under Installation and verified its path/version. Installation itself was exercised through local synthetic release fixtures without changing the real global binary.
- A real external wrapper fixture enforced repository cwd, started one runtime, reused the same PID on reopening, served the UI, and stopped cleanly.
- Independent review found two issues: external shim metadata incorrectly driving runtime replacement, and successful installation leaving an old executable selected. Both were repaired and re-reviewed with no unresolved findings.
- Comment review removed eight unnecessary new comment blocks and moved updater smoke-test cwd ownership into the updater. Existing policy comments moved with the CLI resolver were retained rather than changing unrelated dispatch structure.
- The Rhizome `closure-drift-pack` recipe resolved this effort and its frozen scope. `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` both passed with zero issues.

### 2026-10-04 expansion

- Merged `origin/main` (82 commits) as `70b2937`; conflicts were limited to the changelog and subsystem `last-verified` dates.
- `rzm new-worktree` copy-on-write: `sqliteutil.CloneInto` holds `BEGIN IMMEDIATE` on the source and clones the database and WAL; `copyRhizomeIndex` falls back to `VACUUM INTO`. A stress test clones twenty times while one connection commits ten-row transactions and another runs passive checkpoints; every clone passed `integrity_check` with whole transactions, three runs under `-race`. The real-binary integration test against a source with a live runtime reported a clone and caught up.
- Keep-alive: UI event-stream heartbeats now touch the idle timer. `TestHeadlessRuntimeStaysAliveUnderAnOpenUIEventStream` (25 s idle timeout, stream held 45 s) failed with the fix removed and passed with it.
- Desktop rounds 1 and 2 were delegated to Opus 5.5 (high) with fixed ownership boundaries. The packaged app was smoke-tested against fixture repositories: v1 library migration, seeding a new worktree through the real `rzm new-worktree`, new-worktree markers, session restore of two windows after quit, terminal `rzm start`/`rzm stop` reflected within five seconds, and relaunch of a displayed worktree after `rzm stop`.
- Gates on the integrated tree: `make build` and `make check-full` passed; `make desktop-check` (10 frontend tests, 26 Rust tests, Clippy) and `make desktop-build` passed.
- Native menus, keyboard shortcuts, ⌘W session removal, Dock reopen, and following a runtime to a new port could not be exercised in the GUI because this session has no accessibility permission; they have unit coverage and await a manual walkthrough.
- Review: Claude Fable judged the copy-on-write clone safe after tracing WAL restart, checkpoint, truncation, and close interleavings, and found a cleanup-before-rollback defer order, now fixed. GPT-6.1-Sol reported thirteen findings. Fixes: a symlinked source made the clone alias the source (now resolved first; regression fails without the fix); heartbeats could not hold a runtime under idle timeouts shorter than 20 s (UI streams now hold the idle tracker for their lifetime, verified with a 3 s timeout against the real binary); and eleven desktop ordering, supervision, discovery, and migration defects. Its verification round confirmed eleven and found three more (stale give-up, subfolder root, unordered library snapshots), all fixed with failing-first tests.
- Rhizome Dev: `make desktop-install-dev` builds a separately identified, DEV-badged app from the primary checkout on `main`, installs it, and restarts it. A forced run into a temporary directory verified installation and restart; the guard refused this worktree.
- `rzm desktop`: the CLI checks the folder, finds the installed app, and passes `--open <path>` through process arguments; it runs on the invoking `rzm` like `trust`. The packaged app selected the worktree when cold-started, switched a running app's focused window, and added an unknown repository.
- `./scripts/rzm validate` reported 0 issues. Frozen-scope drift initially flagged EFF-2026-09-29-14-46 for the SPEC-0104 amendment; a deviation there acknowledges it, and the check now passes.

## Deviations

Scope expansion on 2026-10-04 (frozen-scope-drift acknowledged for SPEC-0113 and SPEC-0104): Drew Colthorp added first-class worktrees, sidebar windows without a launch screen, persisted window state, and loading states before publication, and chose to expand this effort over a stacked one. SPEC-0113 was rewritten as one contract and SPEC-0104's `rzm new-worktree` snapshot rule was amended to permit a copy-on-write clone taken under the source's write lock. Both were refrozen here. Later the same day Drew added runtime presence and keep-alive for displayed worktrees; SPEC-0113 gained a Runtime presence section and was refrozen again. He then requested `rzm desktop`, and SPEC-0113 gained an "Opening from the terminal" section, refrozen again; his request is the approval for that step.

Grounding the keep-alive work found that the web UI's global and node event streams sent heartbeats without recording runtime activity. A headless runtime therefore idle-exited after an hour under an open but quiet UI, contrary to SPEC-0104's lifecycle rule that an open event stream is activity. Those heartbeats now record activity. This is a conformance fix, not a contract change.

External managers own runtime build identity, so desktop verifies folder/process identity without deriving a replacement ID from a wrapper script. This clarifies the existing external-ownership requirement.

Native HTML verification exposed a macOS ATS requirement. A localhost-only exception and tightly scoped frame navigation preserve the existing isolated HTML viewer. No web UI or runtime-content permissions were broadened.

The public release feed has no published release at verification time. Global install/update behavior is implemented and tested with local releases; real downloads depend on publishing a compatible release. Current development binaries work. Signing, notarization, and public distribution remain outside this delivery.

Pointer added on 2026-10-07 (frozen-scope-drift acknowledged for SPEC-0113): EFF-2026-10-06-08-07 added a pointer from the unconfigured-worktree requirement to [[desktop-repository-setup|SPEC-0118]], which owns the setup sheet and the What gets indexed page. The requirement itself is unchanged.

New-worktree marker on 2026-10-07 (frozen-scope-drift acknowledged for SPEC-0113): Drew reported that opening the rhizome repository left its new marker lit, because only the opened worktree was acknowledged while agent tools keep adding others. SPEC-0113 now says opening any of a repository's worktrees acknowledges every worktree it has at that moment, so the marker shows only worktrees that appeared since; the sidebar and toolbar show it as a muted count instead of a red dot that read as a second status.

## Closure Checklist

- [x] Requirements exercised on the actual application.
- [x] Focused and repository checks recorded.
- [x] Independent review findings resolved.
- [x] Documentation reflects implementation and remaining limits.

## Compounding Follow-ups

The macOS desktop CI job and documented native smoke checks preserve the discovered WebKit and installation boundaries. Windows/Linux packaging and signed distribution are future work, not validated by this macOS delivery.

## Status

The original delivery is implemented and verified locally. The 2026-10-04 expansion is approved and in progress. No release has been published.
