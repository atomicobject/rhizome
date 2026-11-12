# Desktop bridge

`rzm desktop-bridge --state-dir <absolute directory>` is a hidden one-request
JSON command bundled with the Tauri app. It runs on the invoking executable,
bypassing repository delegation and diagnostics wiring. `Service` owns folder inspection, repository discovery, trust,
setup reports, initialization, index scope, seeding, opening, command-line
links, and repository pin advances. `protocol.go` bounds input and returns protocol 1 responses on both success and failure.

- `New` receives the invocation's explicit `config.Env`. Home, PATH, login-shell,
  Rhizome Dev app (`RZM_DEV_APP_RZM`), and update-manifest lookups use that
  environment. Portable installation ownership and CLI entry paths use the real
  OS account, independently of that environment. Pin installation and advance
  carry it into the in-process updater. Repository trust remains owned by the
  real operating-system account through `repositorytrust.DefaultStore`; an
  invocation home never redirects that authority. External process launch tools
  retain their process-owned environment; arbitrary lookup functions are not
  serialized into child processes.
- Inspect reads configuration and existing trust only. It canonicalizes the
  configured checkout, including aliases and subfolders, without probing
  repository executables, installing, granting trust, or writing configuration.
  A pin offer may probe the global executable in a neutral directory; a bundled
  bridge uses its own build version without probing.
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
- `repoexec.Select` determines executable authority, and `repoexec.Resolve`
  completes it into the target, trust root, pin install, and build rule that
  Open, Seed, Setup, and Scope act on. Trust precedes every
  repository-selected probe, download, and start. The Rhizome Dev app's `rzm`
  is app-owned, so selecting it needs no trust even in a pinned source
  checkout: desktop runs only serve, scope, and seeding with it, none of which
  act on the pin. (CLI dispatch keeps trust there, because `init` and `update`
  do.) A per-worktree executable the user chose takes precedence over a managed
  pin, external manager fallback, or global fallback on this Mac. Clearing it
  restores the repository selection. A managed override still requires the same
  canonical-checkout trust; it never installs or changes the pin. Open compares no build identity for a
  chosen executable, since it may be a shim. Only explicit initialization
  writes setup files; opening an unconfigured folder never initializes it.
- Verified running status also carries optional runtime version and provider-access reports. Constructed Voyage and last constructed Jev access are distinct from the next-construction decision in that runtime's environment. An older runtime without reports is unknown; desktop never substitutes its own credential report as actual repository routing.
- Setup-report runs `init --check --json`; initialize runs `init --json` with
  explicit choices and returns the untouched setup result plus post-run folder
  information. Both require an unconfigured folder, use Initialize's executable
  selection, and check trust before probing `init --help` for
  `--search-key-stdin`. After any initialization run, configuration that now
  exists causes trust to be recorded through Trust at the canonical root.
  A pin-only install failure remains a setup result; JSON error documents become
  `setup_failed` problems, including guidance when a key stays saved, or
  `setup_partial` when the folder is configured after the failed run, so the
  shell offers opening instead of a setup that can no longer run. A run that
  outlives its deadline reports that it timed out.
- The repository Settings tab always shows Rhizome version and local trust before
  reading settings. Discovery includes `pinnedVersion` even without a pin offer.
  Managed repositories can use the pin, the app's executable, or a file-picker
  choice. Source builds show their development authority without ineffective
  overrides. A missing settings capability offers a pin advance when available,
  or a local app-executable choice, and reloads after success. It shows no
  settings data when the selected executable cannot report it. Executable changes
  restart running worktrees and preserve the repository Settings tab.
- Settings-report and settings-apply change a set-up repository
  (SPEC-0118 US3): they require configuration and trust, prepare the
  worktree's selected executable, and probe `init --help` for "repository
  settings once set up" before running `init --check --json` or `init --json`
  with `--workflow`, `--agents`, `--search`, `--add-addon`, and
  `--remove-addon`, each only when its choice is present. They reject a key
  and setup choices; error documents become `settings_failed`, and a pin-only
  install failure after an apply remains a result.
- Scope and scope-edit require configuration and trust, prepare the worktree's
  selected executable, and probe `index scope --help` for `--remove-rule` before
  running `index scope --json`. JSON documents pass through; error documents
  become `scope_failed`. Unsupported setup or scope contracts return
  `setup_unsupported`. Native actions share the per-worktree lock and saved
  repository/current-worktree checks with Trust.
- Search keys are accepted only by initialize, travel in request JSON and child
  stdin, and never enter argv, environment, results, or diagnostics. The native
  Secret type redacts Debug and Display. The bridge rejects a result that echoes
  the key and redacts failure messages. Reports time out after two minutes,
  initialization after four, and scope after one.
- A managed pin whose release the server reports missing fails Open with
  `pinned_version_unavailable`, distinct from a retryable `install_error`, so
  the shell leads with the existing pin advance instead of a retry. An
  installed pin too old for `serve --headless` fails with
  `pinned_version_unsupported`, which the shell treats the same way.
- Open probes the selected version and headless capability from the checkout,
  then calls `runtime.Ensure` with its exact executable and build identity.
  Existing manifest and health folder identity must agree; returned origins use
  literal loopback IPs. Control tokens never enter JSON responses.
- Stop shuts down the folder's runtime with `runtime.StopVault`, the code
  behind `rzm stop`: graceful shutdown, then termination of a headless runtime
  that ignores it; an attached runtime is reported, not killed. Like Open, it
  first requires the manifest and health to name this folder, so a foreign
  manifest never receives a shutdown. It runs no repository executable, so it
  needs no trust.
- External managers may supply a verified existing runtime or a user-selected
  absolute executable or shim. Without either, Open runs the global executable,
  as the command line runs whichever `rzm` was invoked; it asks for a choice
  only when none exists. Runtime status carries the manifest's `executable`,
  and the shell's "External rzm" toolbar item names it and explains the
  fallback. Their manager owns build identity, so desktop
  does not derive a replacement build ID from a shim file. Desktop never
  downloads or updates their installations. Global overrides require a native
  Rhizome binary, not an external manager shim.
- Without an explicit global override, an app-owned bridge selects its own
  resolved executable before searching existing CLI installations. A bridge is
  app-owned inside a macOS app or a Windows/Linux image, or in a local build when
  `rhizome-desktop` sits beside it. Global status
  probes from a neutral directory. App updates belong to the native updater;
  terminal `rzm update` hands that global target to the app and keeps repository
  updates independent.
- On Windows/Linux the native host materializes the bundled `rzm` into a verified
  immutable image before any bridge, auth helper, or runtime starts. One account-owned
  registration records its durable GUI target and current image. Rust owns record
  publication under the installation lock and rejects stale generations; Go reads
  and validates it. App updates select a new image without replacing a running
  executable. Old images are retained; cleanup must first prove active-use safety.
  Repository settings and dotenv never choose the registration root. New public
  CLI entry invocations refuse a missing or disabled GUI owner. Retained images
  remain executable for independent runtime helpers and restarts after removal.
  `appbundle` protects the registration, images, entry, and registered
  GUI targets from repository pin replacement even from a standalone CLI.
  Existing regular destinations use native canonical filenames, including Windows
  short names; lookup failures stop planning. Separate final symlink and hardlink
  entries remain replaceable without changing the installed app.
- macOS command-line integration owns `/usr/local/bin/rzm` only when it points to
  a Rhizome app's `rzm`. A missing app's link is recognized by its structural
  `*.app/Contents/.../rzm` path because deleted metadata cannot prove identity.
  Mutations try without privileges first, then use one administrator prompt on a
  permission error. Windows instead installs a stable dispatcher and its owned
  user PATH directory; Linux atomically links `~/.local/bin/rzm` to the current
  image and refreshes an existing owned link after an update. Both use the real
  account's directories and preserve foreign files, links, and PATH entries.
  Dev installations refuse CLI mutations. Full desktop integration removal is a
  native-shell action: unlink the owned entry, remove the owned Linux desktop
  launcher, then write a disabled generation. Ordinary CLI unlink does not disable
  the installation. The Linux launcher points directly to the durable AppImage;
  exact generated bytes, identity, and the registered app target prove ownership.
  Foreign, edited, and symlink launcher files are preserved. Its content-addressed
  icon remains with user data.
- Pin offers compare managed pins with the bundled build's `version.Version`,
  or the selected global executable's version. External managers, development
  overrides, and Rhizome source checkouts receive no offer. An advance must not
  lose the worktree's index: a bundled release build, which is the release a
  pin advance installs, checks the existing index
  read-only (`store.CheckReadable`, Intel schema only) and reports `incompatible` with a `reason`
  when a newer Rhizome wrote it; pin advance refuses anything but `advance`.
  The bundled build ships a credential store on every desktop platform, so
  sign-in carries over. A development build (which reports the last release's
  version but reads newer indexes) or another executable cannot vouch for that
  release, so its advance carries a `caution`. A missing pin with no advance offers app updates
  and a per-worktree executable instead of retry alone. Go always reports
  an applicable offer; Rust owns repository library persistence and dismissal.
  Pin advance requires existing checkout trust and invokes the in-process
  updater with `SetVersion: version.Version, Yes: true`. It changes the pin and
  managed binary without refreshing generated setup or workflow files. The
  native shell reopens the worktree through Open after a successful advance.
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
installed app, and launches it with `--open <path>`. macOS uses `open`; Windows/Linux
use the registered durable GUI executable. Launch hands off without waiting for
that GUI process to exit. The app discovers that path through Repository and picks the deepest
configured worktree containing it. Repository reports `subpath` for a vault
below its Git root, so the app can map a Git worktree root, or a folder beside
the vault, to that Git worktree's vault when the vault's repository is already
in the library.

Tests use temporary configuration, trust stores, executable scripts, local
release servers, and fixture repositories built with `git init` and
`git worktree add`. No test needs a personal vault or an installed global
Rhizome.

## Distribution bridge JSON contract

All requests contain `"protocol": 1` and one `"operation"`. The command accepts
one JSON document from stdin and writes one response to stdout. Success is
`{"protocol":1,"result":<value>}`. Failure is
`{"protocol":1,"result":<partial value>,"error":{"code":"...","message":"..."}}`
and exits nonzero. Parsing failures omit `result`. Unknown request fields and
requests larger than 1 MiB are rejected. Paths in the contracts below are
absolute. Optional fields are omitted rather than `null`.

### Inspect and folder information

Request: `{"protocol":1,"operation":"inspect","folder":"/repo"}`.

Result:

```json
{
  "id": "<canonical-path SHA256>",
  "path": "/repo",
  "name": "repo",
  "configured": true,
  "trusted": true,
  "trustRequired": false,
  "authority": "managed",
  "pinnedVersion": "v1.2.3",
  "pinOffer": {"state":"advance","pinned":"v1.2.3","available":"v1.3.0"}
}
```

`pinOffer` is optional on folder information wherever returned, including
Inspect and Trust. Repository results expose the same optional `pinOffer` on
each `worktrees[]` entry, copied from that worktree's inspection. Its `state` is
exactly `advance` when
the pin is older and can move, `incompatible` when it is older but the app's
Rhizome cannot read the index, or `app-older` when the pin is newer. Equal or
unparsable versions omit it. Optional `reason` explains `incompatible`, and
optional `caution` qualifies an `advance` the app could not check. There is no `none` state and no dismissal filtering in Go.
`executable` is an optional string for development overrides. Existing folder
fields keep their meanings, including empty `pinnedVersion` without a pin.

### Repository worktrees

Request: `{"protocol":1,"operation":"repository","folder":"/repo"}`.
An optional `"primary":"/repo/worktree"` selects the preferred worktree.
The result retains the existing repository fields and adds only the optional
`worktrees[].pinOffer`:

```json
{
  "id": "<repository ID>",
  "root": "/repo",
  "name": "repo",
  "git": true,
  "defaultPrimary": "/repo",
  "primary": "/repo",
  "worktrees": [{
    "path": "/repo",
    "configured": true,
    "hasDatabase": false,
    "trusted": true,
    "trustRequired": false,
    "authority": "managed",
    "pinnedVersion": "v1.2.3",
    "pinOffer": {"state":"advance","pinned":"v1.2.3","available":"v1.3.0"}
  }]
}
```

Repository `defaultBranch` and `subpath` are optional strings. Worktree
`branch`, `head`, `authority`, `pinnedVersion`, `executable`, and `error` are optional strings;
`main` is an optional boolean omitted when false. Each worktree computes its
own offer, so linked worktrees with different pins may show different states.
The same shape appears under `status` results at
`repositories[].repository.worktrees[]`. Unavailable worktrees omit `pinOffer`.

### Global status

Request: `{"protocol":1,"operation":"global-status"}`. Optional
`"globalExecutable":"/absolute/rzm"` selects an explicit override.

Result: `{"path":"/Applications/Rhizome.app/Contents/MacOS/rzm","version":"v1.3.0","bundled":true,"bundledPath":"/Applications/Rhizome.app/Contents/MacOS/rzm"}`.
`bundledPath`, when present, always names this app's companion, even when `path`
is a global override. Repository Settings prefers it for The app's Rhizome.
`bundled` means the selected executable is owned by a recognized macOS bundle or
Windows/Linux installation image. With no executable found, the result is `{"path":"","version":"","bundled":false}`.
The `managedPath`, `installed`, and `canUpdate` fields and the `global-install`
and `global-update` operations have been removed. Executable probe failures
report `missing_executable`.

### Command-line link

Requests have no operation-specific fields:

```json
{"protocol":1,"operation":"cli-link-status"}
{"protocol":1,"operation":"cli-link-install"}
{"protocol":1,"operation":"cli-link-remove"}
```

All three return the same result shape, describing the state after a successful
mutation, or the last observed state on failure:

```json
{
  "linkPath": "/usr/local/bin/rzm",
  "state": "installed",
  "target": "/Applications/Rhizome.app/Contents/MacOS/rzm",
  "shell": {"path":"/usr/local/bin/rzm","version":"v1.3.0","isApp":true}
}
```

`state` is exactly `absent`, `installed`, `other-bundle`, `dangling`, `foreign`,
or `unsupported`. `installed` identifies this app's CLI entry. `other-bundle`
identifies another Rhizome app image. `dangling` targets a missing image or bundle.
`foreign` is any other entry. `unsupported` means the bridge is outside a Rhizome
release installation, including Dev. `target` identifies the resolved image;
Windows can return it for its regular dispatcher file as well as for a symlink.
`shell` is always present. macOS reports `$SHELL -lc 'command -v rzm'`, falling back
to `/bin/zsh`. Windows/Linux inspect absolute directories in the invocation's PATH.
Lookup and version probe share a five-second deadline. Its `path`, `version`, and
`error` strings are optional; `isApp` compares app ownership. A shell failure
appears in `shell.error` without failing link status or mutation. Another
installation winning PATH retains its path and version with `isApp:false`.

Install/repair accepts `absent`, `installed`, `other-bundle`, and `dangling`.
Remove accepts owned links and is idempotent when absent. Foreign entries
produce `link_foreign`. Dev mutation produces `link_dev_bundle`; other
unsupported mutation produces `link_unsupported`. Administrator cancellation
produces `link_cancelled`; other link failures produce `link_failed`. Status
itself succeeds with `unsupported` outside a release installation.

### Pin advance

Request: `{"protocol":1,"operation":"pin-advance","folder":"/repo"}`, like the other worktree operations.

Result: `{"pinnedVersion":"v1.3.0"}`. The result names the persisted pin after
success. The existing trust must already be granted; the operation never
prompts or grants trust. Errors include `trust_required`, `pin_unsupported`,
and `pin_advance_failed`; failure messages name the updater's failing stage
such as manifest, download, checksum, extract, smoke test, replace, marker,
or pin. A partial failure may have updated the binary before the pin save
failed, as the normal updater reports. The native caller reopens the worktree
through `open` after success. No Go operation or field persists dismissals.

### Upgrade plan

Request: `{"protocol":1,"operation":"upgrade-plan","folder":"/repo"}`.

Result: the untouched `SetupPlan` JSON document from the executable that
reports the offered version (the bundled one in an app, otherwise the global
one) running `init --path /repo --check --json`, including `writes.summary`,
`writes.files`, and `writes.kept`. The pin change comes from Inspect's `pinOffer`.
Planning shares PinAdvance's managed-pin, non-source, existing-trust, and
`advance` offer checks. Errors are `pin_unsupported`, `trust_required`, or
`upgrade_plan_failed` for executable, process, JSON, or two-minute timeout
failures. Planning neither installs a binary nor changes the pin or agent files.

### Provider credential import

Settings groups account sign-in, independent Voyage/Jev route choices, manual key management and 1Password import as Provider access. Sign-in and imports do not alter route choices. `credential-status` additionally returns secret-free `providers` reports from the shared provider factory; these are the app's next-run defaults, not proof of a running repository's connection.

`provider-preference-set` accepts `provider` (`voyage` or `typesafe`) and `preference` (`server`, `key`, or empty to restore automatic). `provider-key-set` accepts `provider` and `key` on stdin JSON; `provider-key-clear` accepts only `provider`. Mutations return refreshed credential status. Server preference overrides all local keys and never falls back. Key writes replace or clear that provider's saved aliases together, preserving unrelated settings and remembered import references. Existing secret values are never returned for display.

Native mutations use the same app-wide global lock as setup and import, and broadcast a provider-access change so other windows refresh. Provider preference/key writes patch only selected values in the original YAML document, preserving unrelated and unknown fields including nested data. Saved configuration remains private and atomic; key values never enter arguments, diagnostics or results. Save errors remain visible across status refreshes. Runtime restart remains an explicit action and never terminates an attached runtime.

`credential-status` reads user-local source references and provider readiness,
without invoking 1Password or returning values. It returns
`{"available":true,"ready":["VOYAGE_API_KEY"],"source":{"account":"example.1password.com","environment":"aaaaaaaaaaaaaaaaaaaaaaaaaa","keys":["VOYAGE_API_KEY"]}}`.
`available` means the CLI executable was found, not that authentication or its
version has been checked. Native GUI discovery includes absolute PATH entries,
`~/.local/bin`, `/opt/homebrew/bin`, and `/usr/local/bin`.

`credential-import` accepts `source` in that shape plus a native `importId`.
The native shell creates an empty `credential-import-<importId>` marker with
mode `0600` in its explicitly private `0700` state directory. Missing or
unreadable markers cancel the import. The Go supervisor checks the marker
while importing and again before saving. The explicit setup operation runs
stable 1Password CLI 2.41+ with `op run --environment`, removes inherited
provider keys and aliases, and starts this executable's early
`credentials-capture` route. That route validates the complete supported-key
allowlist before writing any requested values to a private, bounded JSON pipe.
It bypasses dotenv, configuration, logging, and repository dispatch. Neither
raw output nor credentials enter bridge results or subprocess arguments.

Imports have a 90-second deadline and cancel the op process tree. All selected
keys must exist before one atomic `0600` user-config save also records the
non-secret source. Ordinary execution never invokes op. A `credential-cancel`
native action, request drop, timeout, or normal quit removes the marker; the
native shell leaves the Go supervisor alive long enough to cancel its children.
The UI refreshes saved status after errors or cancellation because cancellation
can race an already committed save. An abrupt app crash may leave an import
running until its deadline and may save credentials if authentication completes
before that deadline. Older binaries may drop source metadata when rewriting
user configuration; imported provider keys still use the existing env map.

Setup and credential import share the app-wide user-config lock across windows; setup acquires it before its worktree lock and holds it through the bridge call. Credential imports reject competing global operations immediately. Cancellation is scoped to the requesting native window; another window cannot cancel the active import. Status refreshes update readiness without overwriting edited source fields. Setup refreshes use the current choices and preserve a typed search key when an import fails or is canceled.
