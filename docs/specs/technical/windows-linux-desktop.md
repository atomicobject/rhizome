---
type: TechnicalSpec
id: SPEC-0132
summary: Complete Windows and Linux desktop installation, native identity, executable ownership and safe multi-platform updates.
spec-status: active
last-updated: 2026-11-05
aliases:
  - SPEC-0132
---

# Windows and Linux desktop support

## Summary

Extend [[rhizome-desktop|Rhizome Desktop]] and [[desktop-distribution-and-updates|desktop distribution]] to Windows and Linux while preserving their macOS and repository-executable contracts. This spec owns the new platforms; their exclusion from the original Mac delivery is not a permanent platform prohibition.

The desktop application owns one matching bundled rzm and its update lifecycle. App-owned child executables must survive app shutdown and upgrades independently of replaceable installation paths. Repository pins, explicit executables, external binary managers and trust retain their current authority.

## Goals

- Provide a usable install, sign-in, repository setup, workspace/task, CLI and removal experience on the named supported hosts.
- Preserve independently owned runtimes through app exit and update without locking the Windows installation or relying on a mounted AppImage.
- Build all supported desktop targets from one canonical source/version and advertise only complete verified update feeds.
- Preserve macOS behavior and credential, trust, origin and publication boundaries.

## Non-Goals

Additional architectures/distributions, Windows 10, MSI, app stores, a tray, a universal terminal framework, independent CLI/image updates and public local-gateway packaging are excluded. Creating a signing account, provisioning production Windows credentials and publishing releases require separate authorization. A general executable cache or garbage collector is not required.

## Requirements

### Supported installations

The initial target MUST be Windows 11 x64 with a per-user NSIS installation and Ubuntu 24.04 LTS x64 GNOME. Ubuntu X11 and Wayland GUI behavior MUST be checked before claiming both. One AppImage installation/update model is preferred. A package-managed .deb fallback requires a recorded decision before dependent implementation and equivalent package-owner upgrade evidence.

The release configurations MUST package only rzm; team-api remains development-only. Candidate builds MUST be possible without production signing credentials. Production Windows publishing MUST fail when trusted code signing is unavailable.

### Installation and execution ownership

Before any app-owned Go process runs, the native application MUST materialize an exact verified copy of the bundled executable in an immutable per-user image. This includes bridge/auth helpers and runtime starts. Roots MUST come from the OS account/native folders, not repository configuration or dotenv-controlled home variables. Existing images MUST be verified before reuse.

Only installation/update or explicit user selection may change the current installation registration. Publication MUST be serialized and protect against an older process replacing newer or disabled state. Removal MUST retain enough generation state to prevent old-app resurrection. Uncertain active images MUST be retained.

A stable owned CLI entry MUST select the current installed image without changing repository selection. It MUST preserve arguments, cwd, streams, exit status and interactive interrupt behavior. It MUST NOT require a version-specific PATH update on each app upgrade. Removal or direct deletion of the owning app MUST prevent new invocations through that entry, while retained runtime images remain usable for independent runtime helpers and restarts.

App-owned images, entrypoints and metadata MUST be excluded from repository update replacement targets, including aliases and missing paths. User overrides, development executables and externally managed entries MUST retain their current behavior.

### Native identity and integration

Windows Credential Manager and Linux Secret Service MUST implement the existing session.Store contract without plaintext fallback. Native errors MUST distinguish missing records from unavailable/locked/denied storage; all operations MUST be bounded. Preserve per-origin records, record-version refusal, generation, cross-process refresh and logout behavior. Native tests MUST use isolated synthetic records, not existing user sessions.

The current loopback PKCE/state browser sign-in MUST be reused. Clipboard, native menus/shortcuts, window creation, task terminal launch and notifications MUST work on supported hosts. Hidden helper calls MUST not create visible consoles; interactive task terminals MUST remain interactive. Untrusted content webviews MUST retain no native command authority. Task paths/arguments MUST survive spaces, quotes, semicolons and Unicode, with useful dependency recovery.

AppImage installation MUST provide an explicit integration-removal action and direct-deletion recovery. All removal paths MUST preserve unrelated PATH entries, foreign installations, repositories and user data.

### Update and publication

Windows MUST save session state before the native updater can exit. App updates MUST preserve independently owned runtimes and fail recoverably if a required replacement cannot be performed. Repository pin advancement on Windows MUST be tested with its runtime executing; failed replacement MUST retain runnable files and truthful metadata. An in-use repository executable may require stopping that runtime and retrying through the app or its global CLI; the error MUST explain the recovery, and the retry MUST succeed after the lock is released.

Native jobs MUST produce immutable candidate artifacts/manifests without updater signing credentials or publication rights. One protected coordinator MUST verify source, version, target, sidecar inventory, canonical CLI bytes and signatures before creating the complete updater feed. The Windows CLI MUST be Authenticode-signed before canonical archiving and MUST NOT be re-signed during desktop bundling. Updater signatures MUST cover final bytes.

Every release eligible for the desktop latest endpoint MUST retain every supported self-update target. Candidate preparation MUST not deploy or publish. Public advertisement MUST follow full asset verification, anonymous URL checks where available, and a tested promotion/recovery sequence. Preserve the current canonical-source, gateway, authorization and resumable-publication contracts.

### Verification and delivery truth

CI MUST run native Windows/Linux builds and process-level lifecycle tests. Installed GUI checks are performed by the user, with explicit package/build identities and a concise checklist. Do not treat compile-only, Xvfb-only or browser-only checks as complete native GUI acceptance.

Before publication, two signed test-version candidates MUST exercise N-to-N+1 updates using only a narrowly documented endpoint/public-key overlay and synthetic signing key. Production profile endpoints/keys MUST remain unchanged. The first actual public-feed upgrade is a separate obligation triggered by the second public platform release, not evidence supplied by the test feed.

Record exact checks, findings and remaining prerequisites. Production Windows signing remains unavailable until configured; this blocks trusted publication, not implementation and unsigned candidate verification.
