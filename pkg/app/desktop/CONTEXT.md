# Desktop bridge

`desktop/bridge/main.go` is a one-request JSON process bundled with the Tauri
app. `Service` owns folder inspection, repository discovery, trust,
initialization, seeding, opening, and global installation. `protocol.go` bounds
input and returns protocol 1 responses on both success and failure.

- Inspect reads configuration and existing trust only. It canonicalizes the
  configured checkout, including aliases and subfolders, without probing,
  installing, granting trust, or writing configuration.
- Repository discovery reads Git metadata files only and never runs `git` or
  repository code. A repository's identity is its canonical common Git
  directory; a folder outside Git is a one-worktree repository identified like
  Inspect. A vault configured below its Git root is a repository identified by
  the common directory plus that subpath, whose worktrees are the subpath in
  each Git worktree; one lacking it is unavailable. Its Root is the first
  worktree whose own configuration is that subfolder, so rediscovery from Root
  returns the same identity even when another worktree's copy of the
  subfolder is unconfigured. The main working tree is
  the parent of a `.git` common directory, else `core.worktree`, else the
  requested checkout when its `.git` file names the common directory (`git
  init --separate-git-dir`); Root is then that checkout, so rediscovery finds
  it again. Worktrees Git would prune (missing and unlocked) are omitted; a
  locked missing worktree, or one whose `.git` names another location, is
  listed with an error. The primary worktree is the requested override when it
  is still an available worktree, else the available worktree on
  `origin/HEAD`'s branch, else the first available one.
- Seed runs only after trust and configuration checks, and only when the
  worktree is not the primary, has no database, and the primary has one. It
  probes `new-worktree --help` (`seed_unsupported` when absent), then runs the
  worktree's selected executable as `new-worktree <primary>` from the worktree.
- Status reads files and probes loopback health endpoints only. One request
  may discover several repositories and probe their worktrees plus any listed
  folders, concurrently, each within one second. Running requires the same
  checks as Open: a loopback origin, a manifest and health for this folder,
  and a matching process and mode. A live process that holds the runtime but
  has not verified (an unresponsive manifest PID, or a live runtime lock
  owner, as during `rzm start` replacing a headless runtime) is starting, so
  the app never starts a competitor. Foreign or non-loopback manifests are
  stopped with an error and no origin. Results never contain control tokens.
- `repoexec.Select` determines executable authority. Trust precedes every
  repository-selected probe, download, and start. Only explicit initialization
  runs `init`; opening an unconfigured folder never initializes it.
- Open probes the selected version and headless capability from the checkout,
  then calls `runtime.Ensure` with its exact executable and build identity.
  Existing manifest and health folder identity must agree; returned origins use
  literal loopback IPs. Control tokens never enter JSON responses.
- External managers may supply a verified existing runtime or a user-selected
  absolute executable or shim. Their manager owns build identity, so desktop
  does not derive a replacement build ID from a shim file. Desktop never
  downloads or updates their installations. Global overrides require a native
  Rhizome binary, not an external manager shim.
- Global management owns only the operating-system user's `~/.local/bin/rzm`.
  An existing symlink at that path or its user-local parents prevents updates.
  A verified neutral temporary directory scopes update planning, and updater
  smoke tests run in the candidate's own directory. Neither mutates a repo pin.
- A runtime remains independently owned after the bridge and app exit. Existing
  runtime election, attach, replacement, cancellation, and idle policies apply.
  The app relaunches a displayed worktree's runtime through Open after Status
  reports it stopped twice in a row, at most 3 consecutive times without a
  loaded page, and follows a replacement to its verified origin. It replaces a
  runtime only on request: Open with `restart` replaces a live headless runtime
  once (an attached one is an error), and an Open whose development build
  changed replaces it through the normal build-mismatch rule. Worktrees under a
  developer override report that build as `executable`. The web UI's open event
  stream keeps a displayed headless runtime from idling out.

Opening from the terminal: `rzm desktop` is a thin command over `launch.go`,
which admits a path inside a Rhizome folder or Git working tree, chooses the
installed app, and launches it with `--open <path>`. The app discovers that path through Repository and picks the deepest
configured worktree containing it. Repository reports `subpath` for a vault
below its Git root, so the app can map a Git worktree root, or a folder beside
the vault, to that Git worktree's vault when the vault's repository is already
in the library.

Tests use temporary configuration, trust stores, executable scripts, local
release servers, and fixture repositories built with `git init` and
`git worktree add`. No test needs a personal vault or an installed global
Rhizome.
