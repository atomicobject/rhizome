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

The [installed checks](../reference/desktop-platform-checks.md) identify the user-owned GUI acceptance cases. Native CI must still verify the integrated implementation and produce both unsigned package candidates and two-version test-feed fixtures.

## Deviations

- User-owned installed GUI checks replace agent-operated GUI checks. CI still must provide native builds/process evidence; the GUI acceptance checklist remains open.
- No Windows production code-signing service exists. Unsigned/test-signed candidates are in scope; trusted public Windows publication remains blocked until credentials are provided. This is not a waiver of production signature verification.
- macOS retains its existing signing path. Manifests distinguish the canonical CLI hash from the signed bundled CLI hash there; Windows and Linux require exact canonical bytes in their packages.
- Main advanced during implementation to Go 1.27.3 and CI-owned public publication through reviewed release-preparation PRs. Preserve those accepted changes and the new Mac Homebrew cask. Replace the superseded local-public/nested-dispatch implementation with native jobs and one complete desktop feed in the existing CI publisher. The original plan remains the planning baseline; current release authorization policy governs implementation.

## Closure Checklist

- [ ] Approved requirements covered with actual evidence or recorded deviations.
- [ ] Native process, credential-store, window-launch and candidate upgrade checks pass.
- [ ] Required focused/integrated gates and independent full-diff review pass.
- [ ] Installation, support, release, security and owning code documentation updated.
- [ ] User GUI checklist supplied with exact candidate identities; acceptance status explicit.
- [ ] Production signing and first/second public-release checks have explicit owners/triggers.
- [ ] Effort closure matches the branch/PR's actual delivered scope.

## Compounding Follow-ups

Prefer existing OS publication primitives: the repository already has fileio.Replace for Windows cooperative replacement, which must be tested before designing another replacement mechanism. No compounding shard is authored in this effort.

## Status

Active. Source implementation and independent integration review are in progress. Native evidence and user GUI acceptance remain open. Production Windows signing is not configured, and no release publication is authorized.

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
