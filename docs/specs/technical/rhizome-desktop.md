---
type: TechnicalSpec
id: SPEC-0113
aliases: [SPEC-0113]
spec-status: active
last-updated: 2026-10-04
summary: "A desktop app presents each Rhizome repository as one unit with switchable worktrees, starts the selected worktree's configured runtime with visible loading states, and manages the user installation."
---

# Rhizome desktop

## Summary

Rhizome Desktop is a Tauri application for working in local Rhizome repositories. Every window has a collapsible sidebar of connected repositories, a toolbar with the sidebar toggle and a worktree selector, and a content area that shows the web UI served by the selected worktree's own configured Rhizome executable. A repository is one unit; its Git worktrees are alternative views of it. Selecting a worktree that has no Rhizome database seeds one from the repository's primary worktree before its runtime starts.

The desktop application owns the repository library, window layout, and worktree selection. Rhizome retains executable selection, configuration, indexing, database seeding, and runtime ownership. The governing install and runtime contracts remain [[version-pinned-install-and-update]] and [[vault-runtime-coordination]].

## Goals

- Add, remember, switch between, and remove local repositories without a terminal or a separate launch screen.
- Treat a repository's Git worktrees as views of one repository: detect them, mark ones the user has not opened, and switch the content to the selected worktree's runtime.
- Start a worktree without a database from a copy of the primary worktree's index, using copy-on-write cloning where the host supports it.
- Show what the app is waiting for while a runtime prepares, starts, and loads, and show the workspace as soon as it is available.
- Respect repository pins, development executables, explicit external binary ownership, and per-worktree trust.
- Support several windows at once and restore each window's layout and selection across sessions.
- Offer a visible global installation and update workflow without changing repository pins.
- Produce a locally buildable macOS application with testable native integration boundaries.

## Non-Goals

Publishing or signing a public release, cloud hosting, synchronizing the library between machines, changing the repository web UI, creating or deleting Git worktrees, and replacing external package managers are outside this delivery.

The app requires the Rhizome release that ships it, or a newer one, in every worktree it opens. It relies on that release's copy-on-write seeding and on UI event streams counting as runtime activity, and it carries no compatibility behavior for older runtimes.

## Requirements

### Repository library

The app MUST persist repositories in user application data. A Git repository's identity MUST be its canonical common Git directory, so every worktree of one repository resolves to the same entry and adding any of its worktrees selects the existing entry. A Rhizome folder below its Git working tree's root MUST be identified by the common Git directory and its relative path, and its worktrees are that path in each Git worktree. A folder outside Git MUST be a repository with exactly one worktree. Git records no location for the main checkout of a repository created with `--separate-git-dir` unless `core.worktree` is set, so the app MAY list only linked worktrees when such a repository is added from a linked worktree. Library paths MUST be canonical, and unavailable repositories or worktrees MUST remain listed with a recoverable error. Removing a repository MUST only forget its library entry. Adding a repository MUST NOT execute repository code. A library written by an earlier version of the app MUST migrate without losing saved folders, executables, or recency; folders that are worktrees of one repository MUST merge into that repository. A corrupt or unsupported library file MUST be preserved and reported.

### Worktrees

The app MUST discover a repository's worktrees from Git, excluding prunable worktrees whose directory is missing, and MUST refresh them when a window gains focus and periodically while a window is visible. A worktree first discovered after the repository was added, and not yet opened in the app, MUST show a new indicator until it is opened.

Each repository MUST have a primary worktree that seeds missing databases. By default it is the worktree that has the remote default branch checked out, otherwise the main working tree. The user MAY choose another primary worktree per repository.

Before starting a runtime in a configured worktree whose Rhizome database is missing, the app MUST seed it by running that worktree's selected Rhizome executable with `new-worktree <primary worktree>`, so the version that will serve the worktree performs the copy and catch-up indexing. Seeding MUST be skipped when the worktree is the primary, when the primary has no database, or when the worktree's executable lacks the command; the runtime then builds its index normally. A seeding failure MUST be shown with a retry action and an option to start without seeding.

### Windows and navigation

Every window MUST show the repository sidebar, a toolbar, and a content area; there is no separate launch screen. The sidebar MUST list repositories as vertical tabs, collapse and expand from a toolbar button, and offer adding a repository. The toolbar MUST include a worktree selector for the selected repository with the new indicator. Selecting a repository or worktree MUST switch only that window's content. Every window MUST reflect library changes made in any window, and an older library snapshot MUST NOT replace a newer one. The app MUST support multiple independent windows, and the same worktree MAY be open in several windows, sharing one runtime.

The app MUST restore each window's size, position, sidebar state, and selected repository and worktree after a restart, and MUST restore at least one window. Global installation settings and per-repository options MUST be reachable from every window.

Repository content MUST render in a webview separate from the app's own interface. That webview MUST NOT receive native desktop command permissions, MUST stay on the verified runtime origin and its isolated HTML viewer origins, and MUST open external HTTP links in the default browser.

### Opening a worktree

Opening MUST select the worktree's configured Rhizome instance through existing executable-selection behavior. External binary ownership MUST require an explicit external launcher or selected executable rather than silently substituting the global binary. Executable trust MUST remain explicit and per worktree, using Rhizome's canonical checkout trust store; the app MUST ask for trust in the content area rather than inherit it from another worktree. An unconfigured worktree MUST show setup guidance or an explicit initialization action.

The native controller MUST verify runtime identity and liveness before showing its URL. It MUST bind local starts to loopback and use dynamically selected ports. Repeated or concurrent selection of a worktree MUST converge on one runtime. A previously running runtime MUST remain owned by its existing lifecycle; closing a window or switching away MUST NOT terminate any runtime. Runtime control tokens MUST NOT reach the frontend or logs.

While a worktree opens, the content area MUST show the current step — checking the worktree, waiting for trust, seeding the index, starting Rhizome, or loading the workspace — and MUST keep the indicator until the repository page has loaded. Errors and timeouts MUST show the cause and a retry action. Switching to another worktree while one is opening MUST discard the earlier result so a slower open cannot replace the newer selection.

### Runtime presence

The app MUST show in the worktree selector whether a Rhizome runtime is running for each worktree, and in the sidebar whether any worktree of each repository has one, regardless of which process started it, and MUST notice runtimes started or stopped by other processes within seconds. Selecting a worktree without a running runtime MUST start one through the opening steps above.

While a worktree is displayed in any window, the app MUST keep its runtime available. The displayed Rhizome UI holds an event stream, and a headless runtime does not idle-exit while any UI event stream is open. When that runtime exits for any reason, the app MUST show that it is reconnecting and start the worktree's runtime again, and after three consecutive failed relaunches it MUST stop retrying and offer Retry. When another process replaces the runtime, for example `rzm start` taking over a headless runtime, the app MUST verify the replacement and follow it to its address. Worktrees that are not displayed keep Rhizome's normal runtime lifecycle. The app MUST NOT stop a runtime itself; opening goes through Rhizome's ensure, which replaces only a headless runtime whose build differs from the worktree's selected executable.

Externally managed launchers retain their manager's binary ownership. Their launcher file MUST NOT be used as the running executable's build identity. Desktop may reuse their verified worktree runtime without replacing it based on the launcher's file metadata.

### Opening from the terminal

`rzm desktop [path]` MUST open the folder containing `path` (default: the current directory) in the desktop app. The command MUST fail with guidance when the path is inside neither a configured Rhizome folder nor a Git working tree, so a worktree whose vault lives in a subfolder, or one that still needs setup, reaches the app; and MUST fail with an explanation when no desktop app is installed. It MUST run with the user's own `rzm` rather than a repository-pinned version, because it reads no repository index. It prefers a running app, then an installed Rhizome, then Rhizome Dev; `RZM_DESKTOP_APP` names a specific app bundle.

The app MUST add the containing repository when it is not already in the library, then select the worktree containing the path in its most recently focused window, creating a window when none is open, and bring that window to the front. A terminal selection MUST outrank any earlier pending selection in that window. The request reaches the app only through process arguments, never a URL scheme, so web content cannot trigger it. Terminal opening is macOS-only in this delivery.

### Global installation

The app MUST report the chosen global executable and provide installation when none exists. Managed installation and updates MUST reuse Rhizome's checksum verification and atomic replacement behavior. They MUST operate outside repository configuration, preserve pins, and avoid replacing a Homebrew or other externally managed executable. Network errors MUST leave the installed executable usable and offer retry.

### Verification

Tests MUST cover library persistence and migration, repository identity across worktrees, worktree discovery and new markers, primary worktree selection, runtime presence and relaunch of a displayed worktree, missing paths, executable ownership, runtime identity, stale-open discarding, failure reporting, and global update isolation. Native verification MUST exercise the packaged app with a real repository that has several worktrees: seeding a new worktree, switching worktrees and repositories, a runtime started and stopped from the terminal, two windows, and restoring layout and selection after relaunch. Build and test instructions MUST be documented alongside the desktop source.
