---
summary: "Installed Windows and Linux desktop acceptance, including test-key upgrades and removal."
reference-kind: guide
last-verified: 2026-11-06
---

# Windows and Linux desktop checks

CI owns native builds, process tests, package inventory, hashes, and signature verification. Drew owns the installed GUI checks below. A green CI run alone does not complete native acceptance. Record each result in the [desktop effort](../efforts/2026-11-05-17-50-windows-linux-desktop.md), with the candidate commit and artifact hash.

## Install a candidate

Use Windows 11 x64 or Ubuntu 24.04 LTS x64 GNOME. On Ubuntu, run the checks once in Wayland and once in an X11 session on the same host. Record the OS version, session type, GPU, app version, package path, and SHA256. A separate OS test account keeps installation registration and credentials separate; `RHIZOME_DESKTOP_DATA_DIR` isolates only library and window state.

The Desktop portability workflow retains `desktop-portability-unsigned-windows-x64` and `desktop-portability-unsigned-linux-x64`. These packages use version `0.0.0` and cannot demonstrate updates. Their default team API origin comes from the checked-in release configuration, so they can be used for browser sign-in checks. Confirm the selected origin in `EVIDENCE.txt`; older artifacts may still use a synthetic origin that cannot complete sign-in. Windows candidates lack production Authenticode signing; the release pipeline rejects them for public publication.

On Windows, run the NSIS installer for the current user and open its installed shortcut. Confirm the WebView2 bootstrap succeeds if the runtime is absent; without network access, its failure should explain the missing prerequisite. Full offline installation is outside this first support set.

On Ubuntu, move the AppImage to a durable location before launching:

```sh
mkdir -p ~/Applications
mv /path/to/Rhizome-linux-x64.AppImage ~/Applications/Rhizome.AppImage
chmod +x ~/Applications/Rhizome.AppImage
~/Applications/Rhizome.AppImage
```

Use the downloaded filename when it differs. Keep this path writable and stable for updates and CLI launch. After first launch, open Rhizome from the GNOME applications menu too. An edited or foreign launcher at `~/.local/share/applications/com.atomicobject.rhizome.desktop` must be preserved. If FUSE is missing, Ubuntu 24.04 uses `libfuse2t64`; see the [AppImage FUSE instructions](https://docs.appimage.org/user-guide/troubleshooting/fuse.html). Use the packaged AppImage for acceptance, rather than an extracted development executable.

Also quit and move the registered AppImage to another durable path, launch it there, and confirm the owned applications-menu launcher opens the new location. If a foreign launcher prevents repair, it must remain untouched and the app must report the integration error so the move can be retried. A disabled integration must stay disabled.

## Exercise the installed app

Use a disposable Git repository whose path includes spaces, an apostrophe, a semicolon, and a non-ASCII character. Keep one independently started attached runtime alongside an app-started headless runtime.

| Check | Required result |
| --- | --- |
| Repository setup | Add, trust, initialize, index, and open the fixture. Repository pins and explicit executable overrides retain their authority. |
| Windows and focus | Create windows from the native menu and Ctrl+N immediately after launch and after clicking the shell, content, or finder. Close and reopen finders. On Windows, test F11 entering/leaving full screen, menu Minimize, and editor Ctrl+H/Ctrl+M without hiding or minimizing the app. Disabled app commands must leave keys to the page. No deadlock, lost input, blank content, or child console flashes. |
| Normal editing | Keyboard-only navigation, undo/redo, Copy Page URL, clipboard paste, file drag/drop, search, and repository/worktree switching work. |
| Session recovery | Move and resize multiple windows, select different worktrees, quit, and relaunch. Layout and selection return. Closing the last window exits on Windows/Linux. |
| Team sign-in | Complete browser login, restart, refresh the session, and sign out. Canceling login leaves no running helper. Linux locked/unavailable Secret Service and canceled unlocking show a useful error without plaintext storage. |
| Tasks | Start and cancel a task with installed Git and the chosen agent. The correct directory and exact arguments reach PowerShell or GNOME Terminal. Missing Git, agent, or terminal reports a useful error. |
| Native boundary | Repository/custom-view content attempting a privileged `desktop_request` is rejected. Shell commands still work. Do this with a synthetic fixture, never by granting content a capability. |
| CLI installation | Install Command-Line Tool twice. A fresh terminal finds the app's CLI; `rzm --version`, `rzm desktop`, stdin/stdout/stderr, nonzero exit codes, and Ctrl-C work. A preexisting foreign `rzm` keeps its precedence and contents. |

Check PATH from an already open shell, a new shell, and a new tab in an already running Windows Terminal. A stale shell may need restarting; its reported resolution must be truthful. On Linux, a login that predates `~/.local/bin` may need that directory added to PATH or a new login.

## Test an installed upgrade

The native upgrade fixture builder produces versions `0.0.1` and `0.0.2` from one source revision. It changes only the test version, updater endpoint/key, and loopback HTTP permission. Packaging and native signing policy stay the same; Windows fixture packages remain unsigned. The updater private key is discarded after assembly.

Add the `desktop-upgrade-fixtures` label to a pull request, or run Desktop portability manually with `upgrade_fixtures: true` once the workflow is on the default branch. Download `desktop-upgrade-fixture-windows-x64` or `desktop-upgrade-fixture-linux-x64`. The extracted root contains `0.0.1/` and `0.0.2/` packages, a feed advertising `0.0.2`, public key, overlay, and `EVIDENCE.txt`. Each version has its candidate and assembly manifests. These use the production app identifier, so use a disposable OS account or VM. The default team API origin is the checked-in release origin; a manual run can override it with a nonsecret `team_api_origin`. Check `EVIDENCE.txt` for the actual origin before browser sign-in. In the extracted root, start the feed:

```sh
python3 -m http.server 9444 --bind 127.0.0.1
```

1. Install `0.0.1/Rhizome-windows-x64-setup.exe` or `0.0.1/Rhizome-linux-x64.AppImage`. On Linux copy it to the same durable AppImage path used above.
2. Install its CLI entry, open two windows, and start both the app-owned and independent runtimes. Keep a terminal command active through the stable CLI entry.
3. Choose Check for Updates, then Restart to Update. Leave the loopback server running. Confirm version `0.0.2`, restored windows, current CLI selection, and continued independent runtime operation. Repeated bridge/auth requests must not block installer replacement.
4. Quit the GUI. On Linux confirm the AppImage mount disappears and the copied runtime can still serve requests, restart, and spawn helpers. Reopen through `rzm desktop`.
5. On Windows, advance a repository pin while that repository's old runtime is running. Confirm success or a recoverable failure that preserves the previous working binary and truthful pin metadata. When Windows blocks the replacement, stop that repository's runtime and retry from the app's Update Pin action or its installed global CLI; the update must then complete. Running the repository binary itself would keep it locked during the retry. App-owned immutable images avoid this installation lock during app updates.
6. Test interrupted download, an invalid package signature, installer cancellation/failure, and a retry. The working installation and recoverable window session must remain available.

This proves the test feed only. The first upgrade through the production feed stays open until the second public Windows/Linux release; the release owner must record that result separately.

## Remove and recover

Choose Settings > Remove Desktop Integration before uninstalling Windows or deleting the AppImage. Verify the owned CLI entry, Linux applications-menu launcher, and Windows PATH directory are removed, foreign entries and user data survive, and an older running app cannot restore the disabled registration. An active runtime keeps its normal lifecycle.

Also test direct deletion/uninstall before that action. A stale CLI must refuse a missing app owner. Reinstall at the same path to use the removal action, or remove only the identified stale Rhizome entry and PATH directory. Reinstall and explicit CLI selection should recover without deleting repository data.

The Windows installer explicitly selects a newly installed or reinstalled app. Manual uninstall-before-upgrade removes its CLI entry; use Settings > Install Command-Line Tool again afterward. Updates initiated by the app preserve that entry.
