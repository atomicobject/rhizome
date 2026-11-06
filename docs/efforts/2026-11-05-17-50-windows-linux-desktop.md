---
type: EffortNote
id: EFF-2026-11-05-17-50
name: Windows and Linux desktop support
created-at: 2026-11-05T22:55:46Z
status: active
summary: Implement the approved council plan for Windows and Linux desktop support, native identity, executable lifetime and release candidates.
aliases:
  - EFF-2026-11-05-17-50
governing-specs:
  - "[[windows-linux-desktop]]"
---

# Windows and Linux desktop support

## Scope

Implement the three-round council plan approved in this conversation by the user's instruction to do it and use subagents to divide the work. Start with Windows 11 x64 and Ubuntu 24.04 LTS x64. Preserve macOS, repository trust/pins/overrides and independent runtime ownership.

CI owns native builds and process checks; the user explicitly chose to perform installed GUI checks. The user confirmed there is no Windows production signing certificate/service. Deliver unsigned Windows candidates and test-key upgrade fixtures while keeping production publishing fail-closed. No signing-account creation, credential provisioning or release publication is authorized.

## Spec Set (Frozen)

- [[windows-linux-desktop|SPEC-0132]], the complete requirements-only platform contract authored with this effort. The approved council proposal at source baseline db91322a11310fd641002b977f4c08c08af18fe2 defines the implementation plan below.
- [[rhizome-desktop|SPEC-0113]] and [[desktop-distribution-and-updates|SPEC-0122]] remain linked dependencies for existing macOS and shared behavior; their historical Mac-only exclusions are not edited or reopened here.

## Stories In Scope (Frozen)

Requirements-only work: [[windows-linux-desktop#Requirements|SPEC-0132 requirements]]. No embedded user stories are invented.

## Spec Coverage Checklist

- [ ] Native supported installation and package ownership.
- [ ] Immutable app-owned images, stable CLI and independent runtime lifetime.
- [ ] Native credential storage and desktop/task integration.
- [ ] Safe running-image, app and pin updates.
- [ ] Complete coordinated build/signing/feed candidate flow.
- [ ] Native CI evidence, user GUI checklist and explicit publication obligations.

## Plan

The user approved the revised council plan after three review rounds: GPT-6.1 Sol 9/10 and Claude Opus 5.5 8.5/10, both approve. Current-user lookup returned missing_current_user, so no note-backed approver identity is inferred or written. The chat approval remains authority for covered work.

### Recommendation and support boundary

Extend the existing Tauri app and Go backend. Retain repository selection, trust, runtime and web UI contracts. Make platform-specific changes at their existing owners instead of introducing a broad platform framework.

Proposed first supported installations:
- Windows 11 x64, per-user NSIS, Evergreen WebView2, signed executables and installer.
- Ubuntu 24.04 LTS x64 GNOME, with Wayland and X11 acceptance. Prefer one user-owned AppImage installation with in-app updates, subject to the native lifetime gate below.

These are proposed support commitments, not observed compatibility. Windows 10, Ubuntu 22.04, other distributions/desktops, ARM, MSI, app stores and a tray are outside this first promise. Test unsupported-but-common systems later before expanding the published matrix. Build Linux on the oldest claimed runtime base and verify the bundled CGO CLI's dynamic dependencies as well as the GUI's.

Ship one Linux packaging/update model. If AppImage cannot meet the complete lifecycle with bounded work, choose .deb and package-manager-owned updates at Batch 1's exit. That is a recorded product/scope decision, not permission to silently disable updates. Inspect .deb sidecar destinations before adopting it, so the package does not unexpectedly claim an existing rzm command.

### What complete means

A clean supported machine can install, sign in, set up or open a repository, use its workspace and tasks, invoke the matching CLI from a new terminal, update the app and repository pins, and uninstall without losing repository/user data or modifying foreign installations. App exit and update preserve independently owned runtimes. The new release path retains the existing macOS behavior and publishing guarantees.

### Facts and uncertainty

Verified in current source:
- The app uses Tauri 2.12.1/wry 0.57.0 with separate shell, runtime-content and finder webviews. Desktop CI is macOS-only; Go CI already covers Linux/Windows.
- Non-macOS session.DefaultStore returns ErrStoreUnavailable. Managed login needs native storage, not just UI portability.
- App-owned executable identification, CLI installation, desktop launch and CLI-to-app update handoff depend on macOS bundles/tools.
- Background runtimes detach from their launching client and use its selected executable path.
- The current updater saves the window session after update.install; the pinned plugin exits on Windows inside installation.
- Release desktop staging and its updater feed currently contain only the Mac target.
- Public release packaging includes only rzm: tauri.release.conf.json replaces the development externalBin list with ["binaries/rzm"], and build_desktop.py always applies that profile. The team-api sidecar in tauri.conf.json is a development-only local gateway. Preserve that distinction in native profiles and assert the final release inventory; do not expand public scope to ship a local gateway.

Unverified concerns to reproduce:
- Tauri documents Windows deadlock risks for synchronous child-webview creation; menu paths use that pattern.
- AppImage mount lifetime can invalidate future access to its sidecar after GUI exit. Running executable mappings alone do not establish continued restart/helper/resource availability.
- Running Windows images can block update replacement. A rename-aside fix is a candidate, not a proven safe operation.
- Repeated Rust-to-Go bridge calls may create visible Windows console windows.

### Design decisions to settle early

#### Executable lifetime and installation identity

Preferred candidate: for Windows/Linux, materialize the bundled rzm as an immutable per-user runtime image identified by the release identity and content hash. It is an exact verified copy of the packaged executable, never independently downloaded or updated. Old runtimes retain their image; new launches use the current installed app's image. Do not run long-lived processes from a transient AppImage mount or from the mutable application installation.

All app-owned Go execution uses the immutable image, including short desktop-bridge calls, auth helpers and background runtime starts. Only the GUI runs from the replaceable app installation. Repository/user-selected executables and system tools retain their existing independent ownership. Development-only team-api is outside the public release inventory; its developer workflow must not be confused with public support.

There is no existing shared immutable binary cache in the inspected code. Reuse existing staging, checksum, locking and executable-selection primitives where applicable; keep this addition confined to installation/runtime ownership. Do not redirect repository pins, user-selected executables or external managers into it.

Do not expose a version directory directly as the permanent CLI entry. Use a stable user entry that resolves the current registered installation at invocation time, preserves arguments, stdio, exit status and Ctrl-C behavior, and runs the exact current image. Prove its Windows behavior rather than assuming a symlink is available. Old shells must not retain a permanently stale version-specific PATH entry. Uninstall removes only owned registration/entrypoints; a stale launcher must fail with useful guidance instead of using orphaned binaries.

Do not require an extra launcher process on Linux if an atomically swapped symlink into the current image satisfies the same contract. The app-owned image must validate its owning installation when invoked through that route, so deleting the outer AppImage does not leave a functioning orphan CLI. For Windows choose one mechanism after the lifetime experiment; preserve direct process exit and Ctrl-C behavior and ensure it does not introduce a new installer lock.

A packaged descriptor can establish identifier, channel, version and bundled executable hash at a fixed installation-relative location. A separate user registration records the durable installed application target needed for launch/update. Descriptor and registration describe installation; they do not authorize arbitrary replacement or command execution. Check fixed ownership boundaries, reject malformed/moved/conflicting registrations safely, and keep repository input out of identity resolution. The app updater remains the only updater for the app-owned runtime image.

Reuse compiled/package metadata for immutable identity where sufficient. Add a new packaged JSON descriptor only if the foundation experiment demonstrates information that cannot be obtained reliably; the mutable per-user installation registration remains distinct from immutable artifact identity.

Only an installation/update commit or explicit user selection may change the current registration. Ordinary reads/startup cannot silently switch it. Define an atomic generation/compare-and-swap check under the installation lock so an old process cannot restore an obsolete pointer. Test an old app writing after a newer update and intentional installation selection separately from version comparison.

Publish runtime images atomically under a per-user lock and verify existing entries before use. Never overwrite an executing image. Never collect an image while a process or selected installation still needs it; failed cleanup of a locked image is nonfatal. Define bounded cleanup using existing runtime discovery plus next-launch cleanup where feasible, without weakening lifetime guarantees.

The retention decision must cover short-lived CLI invocations and the selection-to-spawn interval, which runtime discovery alone cannot prove safe. At the foundation exit choose a simple reliable active-use mechanism or retain uncertain images; do not build a general garbage collector for this release. Include update/uninstall while the stable launcher and a long-running CLI command are active, so the launcher itself does not become the remaining replacement lock.

#### Native sign-in and desktop behavior

Implement Windows Credential Manager and Linux Secret Service behind the existing session.Store interface. Preserve per-origin identity, record version handling, cross-process refresh/locking and logout/revocation. Handle payload-size limits, absent or locked service, canceled unlock and write failure. No plaintext fallback. Use synthetic records/accounts in native tests.

Keep the existing loopback PKCE/state browser flow. Add protocol registration only if a demonstrated native failure requires it.

Support meaningful native menu/edit/clipboard/keyboard actions, dialogs, notifications, links and task terminal launch. Existing last-window-close exit behavior is acceptable on Windows/Linux. No tray or identical Dock-badge appearance is required. Starting tasks must preserve paths/arguments, including spaces, quotes, semicolons and Unicode, and report missing Git/agent/terminal dependencies clearly. Do not install agents automatically. Select and test one default terminal launch path per supported host, with a useful recovery if unavailable.

AppImage removal has an explicit owner: provide a single Remove desktop integration action in the existing Settings surface to remove only that installation's CLI entry, desktop registration and current pointer before the user deletes the file. Preserve user/repository data and active runtime images. Document direct deletion and stale-entry recovery. If .deb is selected instead, use its package-removal lifecycle and test user-integration cleanup without touching unrelated entries. Verify PATH visibility in a new terminal, new tab of an existing Windows Terminal, and a Linux session where ~/.local/bin did not exist at login; give actionable session-refresh guidance where necessary.

#### One release owner

Native jobs consume the exact canonical CLI artifact and build packages. They upload immutable candidate artifacts and manifests only. One existing release coordinator verifies and joins all targets, then publishes through the existing gated path. Preserve canonical source checks, the gateway gate, release authorization and resumable receipts; do not assume unchanged resume behavior after adding remote build dependencies.

Sign the Windows CLI before its canonical CLI archive is finalized. Do not re-sign that binary when bundling the app. Sign GUI payloads and installer before signing final updater bytes. Verify Authenticode and updater signatures separately. Resolve signing service/certificate eligibility early; a specific vendor is an option, not a dependency imposed by the plan. Signed software does not promise immediate SmartScreen reputation.

Updater-key custody stays with the protected release coordinator: native jobs receive no updater private key, and only the coordinator signs finalized updater payloads and emits the joined latest.json. The Windows signing job receives only its narrowly scoped Authenticode credential. Stage the signed CLI back into the canonical artifact flow before archive/package finalization. Package builders must not mutate/re-sign it; verify the extracted hash. Exercise the exact Tauri artifact-generation and detached-signing commands so separating custody does not omit a required updater archive.

One joined feed must retain every supported self-update platform in every public desktop release, including hotfixes. Missing-target refusal is a release gate. Upload versioned assets in a non-advertised candidate/draft state, verify retrieval and signatures, then advertise the release/feed. Account for GitHub's releases/latest selector: merely uploading latest.json last to an already-public new release does not preserve the old feed. Test the actual draft-to-public/advertisement sequence and failure recovery. Never point users at a partially populated release.

If anonymous download verification requires a public release, publish it with latest promotion disabled, verify its final public URLs, then promote it. Authenticated draft retrieval does not prove anonymous retrieval. Confirm the endpoint's actual selection behavior and the API sequence in a controlled release-assembly test before changing the public path.

After promotion, smoke-test anonymous releases/latest resolution, feed completeness, package URLs and signatures. If promotion exposes a broken feed, restore the prior latest release or return the faulty release to draft under the authorized release-recovery procedure, then verify the prior feed again. A later CLI-only release must not silently become latest and strand desktop update clients; preserve the complete-feed publication invariant for every release eligible for that endpoint.

### Batch 1: prove native feasibility and freeze the foundation

Scope: desktop window/bridge code, install/runtime paths, native toolchains and package prototypes.

Run bounded experiments on the named native hosts:
- Menu-triggered new windows; finder repeated open/close and focus; resize/DPI; native decorations/menu bar; clipboard, undo/redo and keyboard-only navigation; custom-view HTML and external links.
- AppImage launch/quit followed by runtime requests, runtime restart and CLI invocation. Compare direct sidecar execution with the immutable-image candidate; identify exactly what depends on the mounted bundle.
- Windows live-image replacement and installer update with app-started, CLI-started and separately attached runtimes. Test safe candidate strategies before choosing one.
- Repeated bridge calls with no unwanted console, and task launch with representative quoted paths.
- WebView2 bootstrap/failure, Linux FUSE prerequisites and relevant native graphics behavior. Record host/session/GPU coverage rather than claiming every GPU works.
- Windows signing availability and the actual package/sidecar layout.

Exit: executable lifetime, installation identity, CLI entrypoint, Linux package owner, supported hosts and Windows replacement strategy are concrete and documented. Confirm the designs with foundation review before dependent work. A failed experiment records a bounded alternative and its acceptance test; do not accumulate retries or change frameworks without evidence.

### Batch 2: deliver the complete installed workflow

Scope: pkg/app/desktop, appbundle/update/repoexec/runtime as needed, pkg/teamapi/session/login, desktop Rust integration, platform install hooks.

Implement the settled ownership and lifetime design, native credential stores and platform integration. Adapt global/update status and CLI handoff to the physical image versus installed-app identity. Preserve repository-selected paths, trust checks and independent app/repository update outcomes.

Reproduce updating a running pinned executable on Windows. Implement the smallest crash-safe replacement that passes the reproduction and rollback/failure tests. Do not assume rename-aside is safe: preserve the old runnable target on failed publication and retain truthful pin/version metadata. This fix belongs in the port where required; no unrelated rewrite of the updater.

Use CREATE_NO_WINDOW where appropriate for hidden child processes while retaining real terminals for interactive tasks. Verify untrusted content still cannot invoke privileged shell commands on every platform.

Exit: clean install, GUI/CLI operations, native login/refresh/logout, path handling, tasks and uninstall pass on the named hosts. CLI works with the GUI closed, old images can serve active runtimes, and foreign CLI/PATH entries remain untouched. User data and repository content survive removal.

### Batch 3: produce one verified multi-platform candidate

Scope: native CI, Tauri config/build hooks, GoReleaser, release manifests/coordinator and signing integration.

Reuse RZM_DESKTOP_PREBUILT_RZM and verify the embedded/extracted CLI bytes against the canonical artifact. Pin native build environments and verify Linux dependencies. Update macOS-only check scripts/gates without making macOS checks disappear.

Each platform manifest binds commit, version, target, relevant toolchains, package hashes, extracted CLI identity/hash and signing verification. Reject mismatched/stale manifests, bad signatures and incomplete platform sets before the advertisement boundary. Candidate preparation and inspection must not deploy/publish. Exercise interruption/resume across native build and aggregation phases.

Exit: all promised packages and one complete feed are assembled and verified offline/non-publicly; deliberate missing-target/hash/signature/source failures are rejected. Existing gateway and authorization protections remain effective.

### Batch 4: prove installed upgrades and finish the support contract

Pre-publication upgrade fixture: build two release-config candidates with explicit test versions N and N+1 and one narrow test overlay that replaces only the updater endpoint and public key. Use an ephemeral test signing key and a controlled local HTTPS fixture feed (or an explicitly fixture-only local transport setting), with no production credential. Retain packaging, install mode, payload layout, native signing mode and update logic. Record the permitted config differences and verify the production profile retains its production endpoint/key. This proves the native mechanism before any Windows/Linux release exists; it does not claim public-feed delivery.

Use installed release-config artifacts with disposable repositories and synthetic identity fixtures:
- Install, login, refresh, logout, trust/setup, indexing/search, custom views, tasks, terminal launch, multi-window/worktree and native privilege isolation.
- GUI quit with a live runtime, restart/crash recovery, CLI use after quit, unavailable dependency recovery.
- Signed N-to-N+1 update with live runtimes; session saved before Windows exits; new app/current CLI use N+1 while existing N runtimes remain valid. Invalid signature, wrong target/version, canceled/failed installer and incomplete download leave a recoverable installation.
- Pin advance with a running repository runtime on Windows, with truthful version/pin results after injected replacement failures.
- Uninstall, reinstall, stale registration, concurrent starts and cleanup. Preserve unrelated installs, user data and repositories.
- Existing macOS install/update and selected-runtime behavior.

If the recorded Linux decision selects .deb, replace its AppImage/Tauri upgrade cases with a signed package/repository verification and package-manager-owned upgrade test. Both the UI and rzm update must identify that owner. The self-update feed then covers only platforms that actually self-update.

At the first public Windows/Linux release verify anonymous download, install and production-feed lookup. The first actual public-feed N-to-N+1 upgrade can be observed only at the second such release. Keep that named check open with an owner and trigger rather than claiming the test-overlay upgrade completed it. Separate implementation/candidate readiness from these publication-triggered acceptance obligations in the effort record.

Run required focused and integrated gates from docs/engineering/quality-gates.md, extended for native Windows/Linux desktop jobs. Record exact build, OS/session and outcome per acceptance obligation. Compile-only and browser-only checks cannot stand in for native installation/upgrade evidence.

Update active specs and owning CONTEXT/subsystem notes, support/install/release guides, native gate documentation and changelog. Use a new effort for implementation; do not rewrite historical completed efforts. Formalize the plan and its foundation decisions before implementation approval.

Exit: every supported target has installed-workflow and signed-update evidence; outstanding public-release checks remain visibly open until observed. Publication is a separate authorized operation.

### Reconciliation of council recommendations

- Accept both reviewers' demand to settle execution lifetime, Linux update ownership and feed aggregation before dependent implementation.
- Accept Sol's native authentication workstream, missing from the initial draft.
- Reconcile executable advice through a single app-owned immutable image, not a separately updated CLI. Treat this as the candidate to prove in Batch 1.
- Reject unconditional claims that AppImage shutdown immediately kills an existing executable, or that Windows rename-aside necessarily works. Both require targeted native evidence.
- Prefer the narrower Windows 11/Ubuntu 24.04 support promise. Older hosts can be added with evidence rather than silently enlarging acceptance.
- Preserve Tauri and current repository/runtime contracts; exclude a new updater service, package manager, tray, universal terminal adapter and extra package formats.
- The second-sidecar concern is resolved by the public release overlay's explicit rzm-only inventory, not by adding team-api to public packaging. Add an inventory check and keep the local gateway a developer-only workflow.
- Accept the explicit candidate N-to-N+1 feed fixture, post-promotion recovery, all app-owned Go calls from runtime images, AppImage integration-removal ownership and coordinator-only updater-key custody.
- Do not adopt last-launch-wins registration: an older process must not roll back an installed update. Reuse existing single-instance handling while testing cross-version/CLI publication races.

## Original Intended Delivery

A complete Windows/Linux port of the existing application and release path, verified through native process and candidate-upgrade evidence, with user-owned GUI acceptance and production signing/publication prerequisites explicitly retained.

## Actual Delivered

Implementation is in progress on [PR #98](https://github.com/atomicobject/rhizome/pull/98). The branch contains native session stores, immutable app-owned executable images, CLI entrypoints and install/removal hooks, portable menus/tasks/clipboard, and coordinated release candidate tooling. None is advertised as a supported public release yet.

The [installed checks](../reference/desktop-platform-checks.md) identify the user-owned GUI acceptance cases. Native CI has produced verified Windows/Linux candidates and both two-version test-feed fixtures. Final Windows window-launch evidence and installed GUI acceptance remain open.

## Deviations

- User-owned installed GUI checks replace agent-operated GUI checks. CI still must provide native builds/process evidence; the GUI acceptance checklist remains open.
- No Windows production code-signing service exists. Unsigned/test-signed candidates are in scope; trusted public Windows publication remains blocked until credentials are provided. This is not a waiver of production signature verification.
- macOS retains its existing signing path. Manifests distinguish the canonical CLI hash from the signed bundled CLI hash there; Windows and Linux require exact canonical bytes in their packages.
- Main advanced during implementation to Go 1.27.3 and CI-owned public publication through reviewed release-preparation PRs. Preserve those accepted changes and the new Mac Homebrew cask. Replace the superseded local-public/nested-dispatch implementation with native jobs and one complete desktop feed in the existing CI publisher. The original plan remains the planning baseline; current release authorization policy governs implementation.

## Closure Checklist

- [ ] Approved requirements covered with actual evidence or recorded deviations.
- [ ] Native process, credential-store, window-launch and candidate upgrade checks pass.
- [ ] Required focused/integrated gates and independent full-diff review pass.
- [x] Installation, support, release, security and owning code documentation updated.
- [x] User GUI checklist supplied with exact candidate identities; acceptance status explicit.
- [ ] Production signing and first/second public-release checks have explicit owners/triggers.
- [ ] Effort closure matches the branch/PR's actual delivered scope.

## Compounding Follow-ups

Prefer existing OS publication primitives: the repository already has fileio.Replace for Windows cooperative replacement, which must be tested before designing another replacement mechanism. No compounding shard is authored in this effort.

## Status

Active. Source implementation is present and independent integration reviews have passed. Final CI, current-head review and user GUI acceptance remain open. Production Windows signing is not configured, and no release publication is authorized.

## Execution Notes

- 2026-11-05T22:55:46Z [decision] Recorded user approval of the council plan and permission to use subagents. The current user is not configured; no identity was inferred.
- 2026-11-05T22:55:46Z [decision] User selected CI for native builds/process checks and will handle installed GUI checks personally.
- 2026-11-05T22:55:46Z [blocker] User confirmed no Windows production signing certificate/service. Continue implementation and unsigned/test-key candidates; retain a hard production publishing gate.
- 2026-11-05T22:55:46Z [validation] Built this checkout's rzm and started session 32FPapg2D8dRDJbm. Ontology/identifier queries initially waited on the new checkout's structural index; the allocated identifiers and current authoring contracts were subsequently retrieved.
- 2026-11-05T22:55:46Z [learning] Existing pkg/fileio.Replace already provides Windows FileRenameInfoEx POSIX semantics. A native running-image probe compares it with ordinary rename before updater changes.

- 2026-11-05T23:01:11Z [validation] PR #98 opens with CI-only commit ef344dd2. Native baseline source excludes the local portability implementation, so failures can establish pre-fix behavior. The effort remains active; this draft PR is not ready to merge.
- 2026-11-06T00:02:31Z [decision] Use one per-channel registration with generation checks, exact content-addressed executable images, a stable Windows dispatcher and an atomically refreshed Linux CLI symlink. Retain uncertain images rather than introducing garbage collection. Rust alone owns installation registration; Go validates ownership and owns CLI entry mutation. New AppImage installs use a documented durable user path plus an owned applications-menu launcher.
- 2026-11-06T00:02:31Z [validation] Baseline run 42127929065 passed the Ubuntu 24.04 build and software-rendered X11 startup/New Window smoke test. Windows reached Rust tests and failed four Unix-path fixtures; those fixtures now use native absolute paths. These results concern the baseline source, not complete installed support.
- 2026-11-06T00:02:31Z [validation] Initial make check completed the Go unit suite successfully but failed formatting on an agent-created temporary vendoring helper. That helper was formatted; the full gate will run again before implementation commit.
- 2026-11-06T00:02:31Z [validation] Integrated make desktop-check passed frontend lint/typechecks/tests, 133 Rust tests, clippy with warnings denied, and app-instance script checks. The command used only the prebuilt rzm sidecar and a synthetic team API origin. This does not verify installed Windows/Linux behavior or browser sign-in.
- 2026-11-06T00:02:31Z [review] Installation review resolved stale Linux CLI links, aliased config roots, standalone-CLI protection of installed app targets, and GUI update-handoff lifetime. Release integration recheck approved deferred Homebrew publication, post-build source validation and credential separation at 9/10. Native review identified Windows path spelling and failed-update session recovery; fixes and native regression fixtures are being integrated.
- 2026-11-06T00:02:31Z [validation] Documentation validation and frozen-scope-drift both passed with zero issues. After migrating the remaining update-plan caller to the shared path comparator, make check passed, including 203 web test files and 1832 web tests. Native Windows-only process regressions still require CI execution.
- 2026-11-06T00:02:31Z [validation] The final desktop-check passed 134 Rust tests and clippy after terminal matching and failed-update recovery fixes. A full release-tooling run covered 405 tests and found two first-release fixture errors caused by the new native-preparation boundary; the updated fixture preserves ordering/resume assertions and all nine focused tests pass. The synthetic signing roundtrip is opt-in and has separate passing evidence; native package execution and publication are not inferred from it.
- 2026-11-06T00:21:00Z [integration] Committed implementation at 1861da163 and pushed PR #98. GitHub reported merge conflicts and did not start native CI. Fetched main at 9ffc1742c, including PRs #94, #97 and #100. Began integrating its public-release intent, Mac cask and toolchain changes. Stopped this checkout's obsolete full-check process group before source integration; that interrupted run is not a pass. Other checkouts' processes were preserved.
- 2026-11-06T00:21:00Z [review] Greptile scored 1861da163 4/5, identifying active Windows dispatcher removal and hidden dispatcher-child consoles. The former has a native regression awaiting CI; the latter is being fixed with console-aware child creation. A fresh review is required on the integrated head.
- 2026-11-06T01:21:19Z [integration] Resolved main's release-policy, cask and toolchain integration. Removed the superseded nested producer dispatcher and local public publication path. Same-run native producers feed the existing protected Mac updater coordinator, then the existing Linux public publisher. Candidate-only bundles retain unsigned Windows artifacts and the Mac candidate flow; publication explicitly refuses them.
- 2026-11-06T01:21:19Z [validation] Go 1.27.3 rebuild, make check, documentation validation and frozen-scope-drift pass. The installed linter's nested binary path was selected explicitly, and the inherited old GOROOT was unset for these commands. Windows dispatcher tests cross-compile after including the existing pinned registry package. Full release-tooling coverage found a Windows producer-routing bug; after fixing actual assembly routing, all 10 focused artifact tests, 51 publication/advertisement/tap tests and 10 workflow tests pass.
- 2026-11-06T01:21:19Z [validation] make desktop-check passes 137 Rust tests, frontend checks, clippy and app-instance checks. The finder uses Ctrl on Windows/Linux, including its separate webview; 69 focused UI tests cover modifiers and accessible labels. Restart markers now precede installer handoff and recover on failure. These checks do not claim a complete native NSIS upgrade.
- 2026-11-06T01:21:19Z [review] A fresh static integration review of the merged release pipeline passes at 5/5, separately from Greptile. Native execution, installed GUI behavior and live publication remain outside that review's evidence.
- 2026-11-06T01:51:58Z [validation] CI at 3be2880a2 passes Windows/Linux integration tests, web E2E and the macOS desktop check. Windows native tests reproduce active dispatcher removal failing with Access Denied; retirement now uses the existing POSIX rename primitive before changing PATH. Ctrl-C adds a direct-image control and resets inherited fixture suppression. Windows test binaries for file replacement, appbundle and repository trust now cross-compile; actual execution remains pending the next native run.
- 2026-11-06T01:51:58Z [review] Greptile scores 3be2880a2 4/5, finding the shared Linux CLI symlink unprotected during standalone updates. The fix recognizes owned links structurally, including dangling/orphaned links, while preserving foreign entries and repository-local replacement. A static delta recheck finds no further issue at 9/10. All five affected Go packages pass locally; the initial sandbox run could not bind its existing HTTP fixture and was repeated with that permission.
- 2026-11-06T01:51:58Z [validation] Native CI exposes a Windows UTF-8 metadata decoding failure, a Linux credential-fixture synchronization gap, and a Windows-only seek argument type error. Corrections preserve UTF-8 explicitly, serialize startup/item exports, and fix the argument type. Upgrade builds now retain bounded build-only diagnostics and use main's exact-version snapshot mechanism; the earlier Linux GoReleaser failure has no confirmed root cause until rerun. Thirteen focused Python checks pass.
- 2026-11-06T01:51:58Z [validation] Local check-full completes Go race/unit and benchmark checks but stops at four web tests. Two fixture defects reproduce deterministically: missing delayed record-pane GraphQL data and transcript readiness. Their fixes pass 55 focused tests, lint and typechecking. The unchanged trace and lazy-editor cases pass isolated at 586ms and 491ms, respectively; load-amplified timing remains the explanation to verify with the coordinated full suite. Integration stages did not run locally in that failed gate; native CI integration results are separate evidence.
- 2026-11-06T01:56:00Z [validation] The complete release-tooling suite passes 465 tests, with the opt-in native updater-signing experiment skipped. Its earlier separate signing roundtrip remains the relevant evidence for that experiment. Credential-policy CI also passes. Windows execution and the coordinated local full gate remain open.
- 2026-11-06T02:14:28Z [validation] Coordinated make check-full passes at ec2306ea1: race-enabled Go unit checks, benchmark contracts, all 1832 web tests and both integration stages. No timeout/retry increase was used. The trace/lazy-editor cases were left unchanged after isolated timing showed large margins and the coordinated suite passed.
- 2026-11-06T02:14:28Z [validation] Native ec2306ea1 confirms Windows dispatcher stdio/exit/Ctrl-C, consoleless probes and active-command integration removal. Ubuntu passes native Go and real Secret Service checks, then fails one of 139 Rust tests because GIO checks a percent-containing executable before field expansion. Windows exposes actual mapped-target denial, remaining significant-path identity loss and canonical notice checkout bytes. Corrections use a native final-path handle, canonical LF/POSIX notice output and an env-prefixed GNOME launcher; their next native execution remains required.
- 2026-11-06T02:14:28Z [decision] The Windows experiment disproves mapped-target replacement through both ordinary rename and fileio.Replace. Select the permitted recoverable-block alternative: retain the working file, pin and marker, explain repository-scoped stop/retry through the app or separate global CLI, and verify retry after that runtime exits. Do not introduce non-atomic two-move publication. Retiring the stable dispatcher remains a separate, natively passing operation.
- 2026-11-06T02:14:28Z [review] Greptile scores ec2306ea1 4/5, resolving its three prior findings and identifying runtime child launch after owner removal. A real retained-process/helper/successor fixture reproduces the failure, then passes after restricting owner validation to the public entry. Missing/disabled public entries remain refused. A static recheck identifies an indirect public-entry symlink bypass, which is being corrected before the next push.
- 2026-11-06T02:14:28Z [decision] Candidate builds now derive their default sign-in origin from the matching public values already checked into the canonical release definitions, through the existing HTTPS validator. Manual overrides remain available; normal development discovery and protected environment access are unchanged. This supplies usable browser-login candidates for the user's GUI checks.
- 2026-11-06T02:28:55Z [validation] CI at ec2306ea1 passes every main job except the two already identified Windows replacement/path cases; Linux race tests, both native integration jobs, web unit/E2E, macOS desktop and lint are green. The Linux upgrade diagnostic confirms an inherited Tauri TARGET prevents GoReleaser from selecting any binary. A real isolated GoReleaser experiment with installed Go 1.24 reproduces exit 1/no binary, then produces a Linux ELF at exact version 0.0.1 when TARGET is removed. The fixture drops it only for that child; production CI remains pinned to Go 1.27.3.
- 2026-11-06T02:28:55Z [validation] New delta checks pass: six affected Go packages with race enabled, Windows CGO cross-compilation, four desktop-entry Rust tests, six origin tests and fifteen focused Python tests. The corrected Go test environment keeps GOTMPDIR outside the repository because Go 1.27 uses it for test fixtures. A strengthened retained-image helper/successor test and absolute/relative shared-entry alias tests reproduce red before their fixes; the final appbundle race run passes. Desktop lint, documentation validation and frozen-scope checks pass. Native execution of the latest package and mapped-update cases remains pending the next CI run.
- 2026-11-06T02:28:55Z [review] Narrow native integration and installation rechecks approve the latest corrections at 9/10 with no remaining actionable source finding. The installation recheck explicitly resolves its shared-entry alias bypass. These static verdicts do not replace the next Greptile review or native acceptance.
- 2026-11-06T02:42:44Z [validation] Native Windows at 4d8ad5952 passes mapped-update Run/Ensure recovery, dispatcher lifetime, canonical raw path identities and directory-symlink cases. Its remaining trailing-space trust failure is in record decoding, not canonicalization: TrimSpace erases the saved final space. The exact writer-format comparison fixes it. A Go overlay of the old reader reproduces the new portable lifecycle test red; the fixed trust/paths race suite passes, and the Windows trust test cross-compiles.
- 2026-11-06T02:42:44Z [validation] The same Windows run exposes a 20-process refresh failure using only the synthetic fileStore. It now uses fileio.ReadFile/Replace to provide the native stores' atomic record semantics. A deterministic held-reader test verifies old/new snapshots and genuine deletion; focused and full session race tests plus Windows cross-compilation pass. Native Windows green remains pending. Production authentication, locks and native stores are unchanged; no sleep/retry/timeout was added.
- 2026-11-06T02:42:44Z [validation] The CI lint finding is resolved by moving the Windows-only needsVerbatimDOSPath helper into its owning Windows file. The exact make go-analysis gate reports zero issues. The normal local gate will run before committing this correction.
- 2026-11-06T02:42:44Z [review] Greptile completes 4d8ad5952 at 5/5 with zero comments, resolving all four earlier findings. Its first retrieval timed out; the completed result was recovered through the provided review identifier. A fresh review will cover the next committed correction.
- 2026-11-06T02:48:18Z [validation] The standard make check gate passes on the trust-reader, test-store and lint corrections, including all 1832 web tests. Focused session/trust/path race checks, Windows test cross-compilation and the exact static-analysis gate also pass. Native Windows must confirm the corrected multi-process fixture and trailing-space trust lifecycle.
- 2026-11-06T03:06:36Z [validation] The installed Linux upgrade fixture at 4d8ad5952 now reaches final package extraction but rejects a canonical CLI hash mismatch. This is separate from the corrected GoReleaser target selection. The failed artifact was not retained, so its precise byte changes are not measured. Pinned Tauri/linuxdeploy source identifies stripping and executable RPATH writes as packaging mutations.
- 2026-11-06T03:06:36Z [decision] Linux bundling disables stripping and uses the supported PATCHELF override to preserve only the exact canonical AppDir rzm. The temporary wrapper verifies its hash, skips only the known RPATH write, delegates truthful queries and all GUI/library operations, and refuses unexpected protected operations. Final extracted inventory/hash/dependency checks remain mandatory. Bounded public hash/ELF evidence is retained on failure. This may increase GUI/library package size; no separate CLI build or update owner is introduced.
- 2026-11-06T03:06:36Z [validation] Fifteen focused boundary tests and the full 468-test release suite pass, with the optional updater-signing experiment skipped. Real wrapper-process tests cover quoted paths, delegated writes/queries/exit codes, altered bytes and unexpected mutation rejection. A focused static integration review finds no actionable issue at 5/5. Full AppImage execution remains a native CI obligation.
- 2026-11-06T03:06:36Z [review] Greptile completes 46ee153f8 at 5/5 with zero comments. The forthcoming Linux packaging commit requires its own review and native build evidence.

- 2026-11-06T03:31:54Z [validation] Required checks pass at 46ee153f8, including Windows unit tests and lint. Its native Windows run passes Go, real Credential Manager, Rust and package inventory/dependency checks, then fails the raw visible-HWND count. Pinned single-instance 2.5.2 source creates a zero-size WS_EX_TOOLWINDOW with WS_VISIBLE for IPC. The smoke script now excludes tool windows, retains exact one-then-two application-window assertions, and records every raw visible HWND's class/title/style/dimensions. No app source or timeout/retry changed; Windows execution remains pending.
- 2026-11-06T03:31:54Z [validation] Windows two-version upgrade fixture succeeds: [artifact 13473812962](https://github.com/atomicobject/rhizome/actions/runs/42152766312/artifacts/13473812962), built from PR head 46ee153f8 through merge source 9eb084afed5a8f11393308aaf51ea4effca2c01d. Both downloaded installer hashes match their manifests: 0.0.1 afe2e12eae09f15aa014f70a7869485fb91ed045394abfb705bf1eba71f8f12e; 0.0.2 decb5d1c9fdb2fc622e514ab6810d657c21a861046c274936c7cf92687bdd399. These are unsigned installers with test-key updater signatures; the user owns installed GUI/upgrade acceptance.
- 2026-11-06T03:31:54Z [validation] Rebuilt the current Mac CLI, compiled the app, and produced an ad-hoc-signed 107.06 MiB release-profile worktree bundle with only rzm. Its packaged CLI runs successfully. The first make desktop-build attempt selected the development sidecar list because the CLI did not apply the environment-only overlay; rerunning with explicit release/config arguments succeeds. No app was installed in /Applications and production signing credentials were filtered.
- 2026-11-06T03:31:54Z [validation] Mac visual verification remains open: Computer Use returned cgWindowNotFound for the isolated app and Finder, while bundle-identifier selection was ambiguous across registered worktree bundles. The test process existed and wrote isolated session state, but no visible result was established. Only the recorded test PID was stopped after verifying its executable path; the installed Dev app and other worktrees were left alone. No alternate UI-automation mechanism was used.
- 2026-11-06T03:31:54Z [review] Greptile completes the Linux packaging commit 6c0676c3e at 5/5 with zero comments; its earlier API timeout was recovered through the supplied review identifier. Fresh native Linux package evidence is still required.

- 2026-11-06T03:50:14Z [validation] Required checks pass at 6c0676c3e. Its Linux native and upgrade jobs fail during final toolchain metadata, after the extraction/hash/dependency checks: the Tauri npm version command incorrectly runs from the repository root. A real local probe reproduces exit 254/package.json ENOENT there and exit 0/tauri-cli 2.12.1 from desktop/. The fix changes that probe's working directory; the byte-preservation shim and integrity gates are unchanged. Full native candidate completion still needs the next run.
- 2026-11-06T03:50:14Z [validation] Tool errors now include safe operation/exit status, with bounded sanitized diagnostics allowed only for credential-free AppImage bundling; signer, NSIS and credentialed output remain suppressed. Baseline native builds now retain evidence logs too. No mismatch log was expected for the prior metadata failure. Eighteen focused tests and the full 471-test release suite pass with the optional signing experiment skipped.
- 2026-11-06T03:50:14Z [review] Greptile completes cfc2bde35 at 5/5 with zero comments. The next Linux metadata correction requires its own review and native build confirmation.

- 2026-11-06T04:02:31Z [validation] All five required checks pass at cfc2bde35, but its separate native Windows smoke test now times out after Ctrl+N with one application window. The retained raw HWND evidence confirms the corrected initial count: one Tauri application window and two IPC/tool windows. It does not identify the foreground HWND. The smoke script now targets and verifies the classified application's exact HWND, records foreground details at every stage and saves a snapshot immediately before the shortcut. The native keyboard action, exact one-to-two assertion and existing deadlines remain unchanged; native execution of this correction is pending.
- 2026-11-06T04:02:31Z [review] A focused read-only source investigation finds no confirmed New Window deadlock: creation runs on spawn_blocking and builds the native frame before its child webviews. Correct keyboard delivery remains to be established on Windows. Greptile completes 5bcf880e0 at 5/5 with zero comments; its native Linux packaging and Windows smoke results are still pending.

- 2026-11-06T04:16:00Z [validation] Linux native CI passes at 5bcf880e0: native Go, real Secret Service, Rust, complete AppImage assembly, exact packaged CLI inventory/hash/dependency checks and unpackaged X11/Xvfb startup/New Window. Candidate [artifact 13476881036](https://github.com/atomicobject/rhizome/actions/runs/42159064463/artifacts/13476881036) records merge source 67189543b56ede000e936bd2be03c12400c64f29. Independently downloaded ZIP SHA256 1b2fd7787d3db28c82d18c1f94fd64ce160a0a05c85c7e367fb0fdefb9b7fd6c, AppImage 82ae066840ce1d900c418623a38f769cbd58e0f8d5f5a641ec628e7d5da8d600 and companion 596f65f34f7a7170e7bdaf4762bdfe05044ad7eb7ee2d01d400315e4e27e65bb match their manifests. Version 0.0.0 cannot demonstrate updates. The user has exact Windows/Linux candidate identities and installed GUI instructions; acceptance remains open. The older Linux upgrade fixture was canceled by the next push; current-head assembly remains pending.
- 2026-11-06T04:16:00Z [validation] All five required checks pass at 5bcf880e0, but its separate Web E2E job fails one HTML-note metadata Source-view assertion; 111 tests pass. Retained browser evidence shows the save refresh during the Source pointer action. A focused diagnosis is testing the layout/click race; a single passing local rerun is not accepted as a fix. Current-head Windows focus and Greptile checks remain pending.

- 2026-11-06T04:37:27Z [validation] Both native upgrade fixture jobs pass at 471e107bc. Linux [artifact 13476712947](https://github.com/atomicobject/rhizome/actions/runs/42160418427/artifacts/13476712947) records merge source 8b36f61da1c5581f865ee3d26626309729e1e64f, whose second parent is that PR head. Independently verified archive SHA256 a0ef8eace9989a77369c635a0c7bc866838179c69ea5ec76dd04ad09a55b6822, AppImages 0.0.1 aaf58011de81c93ccaae73a091f2bb039daf5a82db5b8b0997dd315de41759d2 and 0.0.2 eceb4db5f72ffa44447c8a823f062cead1a179761fad15cc1cbbbb0a4194260e, and both actually extracted CLI hashes match their manifests. Both updater signatures verify using the existing pinned Mac verifier, with no rebuild or Linux execution. Feed/source/sidecar inventories and absence of private-key files were checked. Exact Ubuntu instructions and verification records are supplied; installed Wayland/X11 and N-to-N+1 acceptance remain open.
- 2026-11-06T04:37:27Z [validation] A deterministic browser regression now reproduces the Source click loss while the actual HTML viewer enters its loading state between pointerdown and pointerup. After one cleanup correction, the old header fails solely on the missing Source panel; moving the variable-width actions before the view selector fixes that regression and the original metadata-save test (2 passed). A read-only review approves the change. Earlier save/watch timing probes passed and are not claimed as reproductions. The standard gate's new-test spacing findings are corrected; complete standard and browser verification are in progress.
- 2026-11-06T04:37:27Z [validation] Windows 471e107bc still fails after Ctrl+N. Retained before/after snapshots prove the exact same Tauri application HWND is foreground, falsifying IPC-window misfocus. A bounded after-failure input/menu diagnostic is being added; it cannot convert the original failure into a pass. Greptile scores this head 5/5 with one P2 AppImage-move launcher-repair finding; a focused source fix and regression are in progress.

- 2026-11-06T05:13:44Z [validation] Final local gates pass: make check, make check-full (race-enabled Go, benchmark contracts and both integration stages), all 1832 web unit tests and all 113 browser journeys. The HTML header regression fails on the old layout and passes after moving variable-width viewer actions before the view selector. The original metadata-saving journey also passes. No click retries or existing timeout increases were introduced.
- 2026-11-06T05:13:44Z [validation] The full gate initially exposed an existing repair-priority test publication gap: it counted a staged .json.tmp file beside a live peer as a finished renewal. The focused baseline fails 56/500 race iterations; filtering final published .json names and asserting that the peer remains active passes 500/500. Production lease code is unchanged. The subsequent complete make check-full passes, including the corrected test.
- 2026-11-06T05:13:44Z [review] The AppImage-move finding is repaired under the existing installation lock: update the owned launcher with its prior path before advancing an authorized relocation. Failed relocation retains the old path/generation for retry; initial/same-owner errors remain nonfatal and foreign launchers, disabled records and stale processes stay protected. Both relocation regressions fail before the fix and pass afterward; all 17 installation tests and Clippy pass. Native Linux execution awaits fresh CI.
- 2026-11-06T05:13:44Z [validation] make desktop-check passes 124 frontend and 140 Rust tests, formatting, Clippy and app-instance checks. Rebuilt the current CLI and ad-hoc release-profile Mac worktree bundle; its executable inventory is exactly rhizome-desktop and rzm, and the packaged CLI reports v0.50.5. No installation or publication occurred. Native Mac visual verification remains open for the previously recorded Computer Use limitation.
- 2026-11-06T05:13:44Z [diagnostic] The next Windows failure will retain its original Ctrl+N result and screenshot, then calibrate input against a separate ready, focused WinForms control and queue the real New Window menu command once. Evidence records independent marker/chord receipt, foreground HWNDs, and application counts before/after menu dispatch so an already-delayed window cannot count as menu success. This PowerShell diagnostic has static review/whitespace checks only; native CI must execute it. It does not establish the keyboard root cause or make the original failure pass.
- 2026-11-06T05:14:58Z [validation] ./scripts/rzm validate passes with 586 identifiers and zero issues; ./scripts/rzm validate frozen-scope-drift passes with zero issues. The installed-app guide now includes owned AppImage relocation/retry and disabled-registration checks. All source files are frozen for the focused commits and fresh native CI/review.

- 2026-11-06T06:15:51Z [review] Greptile completes 712419380 at 5/5 with zero comments, resolving all five prior findings. All five required checks, Linux native and both upgrade fixture jobs pass. Windows native completes process/store/Rust/package checks, then retains the original Ctrl+N failure.
- 2026-11-06T06:15:51Z [diagnostic] Verified Windows artifact 13480720792 archive SHA256 027f11c546c00dde6e93ffceb9c175c0adb740a0b6f882c14886945494756932. Its ready independent control receives both marker text and Ctrl+N with matching foreground HWNDs. The actual New Window menu command 1002 increases application windows from one to two and the app remains responsive. The initial app HWND is foreground, so general input delivery, menu dispatch and window creation work; app keyboard focus/host routing remains unresolved.
- 2026-11-06T06:15:51Z [diagnostic] The next bounded probe preserves the first failure and adds GUI-thread focus and native-menu readiness snapshots, one unchanged-focus key, one key after a guarded real shell click, and an explicitly artificial root-focus control. Each records before/after counts; added windows stop further attempts. Artificial input queues detach before the key, and their keyboard-state-reset limitation is recorded. The C# helper asserts Windows x64 structure layouts before native calls. Static review, whitespace checks and desktop lint pass; no local C#/PowerShell runtime exists, so compilation/execution remains CI evidence.
- 2026-11-06T06:15:51Z [research] Pinned Tauri routes translated accelerators to the message HWND, and [Wry issue 451](https://github.com/tauri-apps/wry/issues/451) documents WebView2 input bypassing the host queue. The proposed [host-input option](https://github.com/tauri-apps/wry/pull/1850) remains unmerged; tagged released sources do not supply that fix. Separately, this app creates a focused content webview and hides it without explicitly restoring shell focus. These are hypotheses for the remaining failure, not runtime conclusions. No application or dependency change was made.
- 2026-11-06T06:15:51Z [validation] The Windows run missed its Rust cache; the previous combined [cache action](https://raw.githubusercontent.com/actions/cache/v4/action.yml) saves only after overall success. Restore/save are now split so a successfully built/inspected candidate checkpoints the same cache before smoke. All remaining workflow fields, permissions, checks and deadlines are unchanged. Ten Python workflow tests, YAML parse and structural comparison pass; actual cache persistence remains for CI.
- 2026-11-06T06:15:51Z [validation] Both current upgrade fixtures bind merge source f92e6f4328ce824cb85ef42ce02325364e7fbc6c, whose second parent is 71241938065ccc55d220cd6ce3a7d5eaa20767b2. Independently verified ZIP/package hashes, manifest/feed identities and all four updater signatures. Linux artifact 13480352283 packages hash to 9e90f014009b2a5f23c07faaf9f88963a6294199ce2106ae999e2515c58b5ca7 (0.0.1) and 539e9380b78b4e715bd02f2ea0b902f60032f5e0a8dcd64ee073501df20a7cdc (0.0.2); actual extracted CLI/inventories also verify. Windows artifact 13479942569 installers hash to 03516f601afb30e57d40da7c8a70b5ebbaba4d9da643db1e8dba61d71e54faa9 and 85b70381b4aef489eee364624dd22b6c65dfc83a80be38df6ac7a8071edfa58d. Windows interior/extracted CLI verification remains CI attestation because local 7zz is absent. Source-pinned Windows/Ubuntu GUI guides are ready; installed acceptance remains open.

- 2026-11-06T07:12:01Z [diagnostic] Windows run 42174087027 at 38ebf2bd1 completes native process/store/Rust/package checks, saves its Rust cache, then fails the original Ctrl+N assertion. Verified artifact 13482289705 ZIP SHA256 3b0ec90610f29a7c45829ab4b0070cd31019aca09c9ec5ea55aa7e2100bf3d6a shows the New Window menu present before input. Ctrl+N also fails after a real click focuses the visible WebView2 child; direct native menu dispatch still creates a responsive second window. Waiting and initial focus alone are insufficient. The artificial focus experiment did not retain root focus, and its later independent input control is confounded by input-queue attachment, so neither establishes a new input conclusion. Greptile completes this head at 5/5 with zero comments.
- 2026-11-06T07:12:01Z [implementation] The Windows repair uses the pinned WebView2 controller keyboard callback for shell, content and finder, sharing enabled bindings and one dispatcher with native menus. The emitting window determines the action target; execution leaves the synchronous callback before creating webviews. Editing keys, Ctrl+H and Ctrl+M pass through. Windows uses F11 for full screen, menu-only Minimize, and omits the macOS Hide group. Frames appear after controller hooks are ready with shell focus; failed hooks remove unfinished views/windows for recovery. Native acceptance now requires genuine Ctrl+N transitions one-to-two immediately after launch and two-to-three after a real visible-child click, with exact counts and responsiveness. Temporary artificial focus and menu-dispatch diagnostics are removed. Native execution remains pending.
- 2026-11-06T07:18:00Z [validation] The keyboard patch passes 142 Rust tests using the rzm-only release override, macOS Clippy with all targets and warnings denied, and cargo fmt. Windows GNU cargo check and Clippy with all targets pass using the installed cross-target standard library and an empty sidecar list for typechecking only; no Windows execution or package verification is inferred. An independent read-only source review finds no remaining blocker after hook-failure cleanup. npm run check --prefix desktop passes typechecks, formatting, 124 frontend tests and six build-config checks. Ten release workflow tests and YAML/cache-prefix assertions pass. The cache retains its lockfile-hash primary key with a same-OS/architecture fallback. Disposable Mac app-instance checks and the unsigned rzm-only release-profile Mac bundle build pass. Existing Go/web evidence is unchanged; no Go or web source changed in this follow-up. Installed GUI behavior and the fresh native key-input checks remain open.
