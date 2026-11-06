# Releasing Rhizome

Prepare public releases with the repository's `rhizome-release` skill in a local Codex or Claude session. Review and commit the narrative, changelog and version in a release PR. Merging that PR authorizes GitHub Actions to build, notarize, deploy the pinned gateway source and publish the release and Homebrew update. The active coding subscription performs the writing; CI needs no model API key.

`make cut-release` points public maintainers to this preparation flow. The older local publisher remains available for private internal releases only.

## Prerequisites

Local public preparation requires Git, Python 3, Go for the evidence contract parser, an authenticated `gh` CLI, and a Codex or Claude session with this repository's skill available. Start from a clean checkout at freshly fetched `main`. No Apple credentials, publishing token, Xcode, Rust toolchain or Mac are needed to write and commit the release files.

CI requires the protected GitHub `release` Environment, the signing secrets listed under [Developer ID signing and notarization](#developer-id-signing-and-notarization), and permission for its publisher to update the canonical release refs, dispatch the gateway workflow and publish GitHub assets. The gateway workflow retains its own production environment and AWS authorization.

Private internal releases retain their local toolchain and credentials; see [Internal releases](#internal-releases).

## Canonical source preflight

`prepare_release.py evidence` fetches `origin/main` and version tags, then requires a clean checkout whose `HEAD` equals fresh `origin/main`. The reviewed evidence identifies an exact source SHA and prior version tag. `prepare` repeats that check before creating `release-prep/<version>` and applying the reviewed files.

The release PR changes exactly these five files:

- `CHANGELOG.md`, the concise user-facing release summary;
- `pkg/vault/changelog/CHANGELOG.md`, its identical embedded copy;
- `pkg/vault/version/version.go`, the compiled version;
- `.github/release-plan.json`, the source, prior tag, version, date and schema version;
- `docs/releases/<version>.md`, the full narrative that GitHub will publish verbatim.

If main advances before the PR merges, refresh the evidence and reconcile the existing prose with the new source. Preserve edits already made to the release PR. The CI selection job verifies the reviewed head, accepted merge tree, source relationship and five-file boundary before signing starts. Stale, forked, unmerged or source-changing release PRs are refused. After approval, all jobs use the immutable accepted merge SHA even if main advances later.

### First release in a repository

An empty prior tag means first publication in this clean repository. Evidence includes the root commit and later delivery units; do not invent a tag or assume every existing feature is new. Existing changelog history and the source version still establish the product baseline. The chosen version must exceed both the source version and the prior tag when one exists.

The existing changelog through `v0.50.5` records pre-public releases whose tags are absent from this repository. Compare that documented experience with the current product and state any historical evidence gap. If a prior release tag appears during preparation, refresh the release instead of retaining an empty baseline.

## Evidence and note generation

The helper collects first-parent delivery units, curated `Unreleased` entries, verified completed efforts, merged PR descriptions, commits, paths and statistics into source-bound evidence. It writes an index, bounded packets and a full audit under ignored `.release/` storage. GitHub enrichment uses the maintainer's existing `gh` login; unavailable evidence is reported as a gap.

The active Codex or Claude session reads the compact coverage overview first, then follows the relevant detail packets and sources behind important claims. For a large release, inspect the previous documented product experience and the current implementation. Organize the narrative around what users can do and how their workflow changes. A desktop app or redesigned workbench can be the headline when the evidence supports it, regardless of which smaller changes dominate commit counts.

Keep the full narrative in `.release/release-notes.md` and a shorter set of changelog bullets in `.release/changelog.md`. Review the narrative and version with the maintainer and preserve their edits. The helper applies those drafts deterministically; it does not start another model or rewrite the notes. Once prepared, `docs/releases/<version>.md` is canonical and can be edited directly in the release PR.

## Normal workflow

Invoke `rhizome-release` in Codex or Claude. The skill handles evidence, editorial review and PR preparation. Its underlying commands are:

```sh
python3 scripts/release/prepare_release.py evidence
python3 scripts/release/prepare_release.py prepare \
  --source <reviewed-source-sha> --version vX.Y.Z \
  --notes-file .release/release-notes.md \
  --changelog-file .release/changelog.md
python3 scripts/release/prepare_release.py check
```

Review the exact five-file diff, commit it, and open the `release-prep/<version>` PR. Editing the changelog later requires `python3 scripts/release/prepare_release.py sync`, followed by `check`. Opening or revising the PR does not authorize merging or publishing it.

Once the maintainer approves publication and merges the release PR, CI performs this sequence:

1. Linux validates the release intent and accepted source without signing secrets.
2. Linux builds the shared web and desktop frontend once and prepares the Mac credential scanner.
3. Linux builds the Linux and Windows CLI binaries. One Apple Silicon runner builds the native Darwin CLI binaries, Tauri app and DMG, signs and notarizes them, verifies distribution copies and removes temporary signing credentials.
4. Linux assembles and verifies the complete asset bundle, checksums and source/version manifest.
5. The protected Linux publisher atomically advances `release` and the version tag without force, deploys and verifies the exact gateway source, then publishes the committed notes, assets and Homebrew formula and desktop cask update.

The Mac job performs native compilation, bundling, signing and verification. Frontend compilation, cross-platform CLI builds, final archive assembly and publication run on Linux. Pushing an ordinary feature branch, merging an ordinary PR or pushing a tag does not start this public release flow.

## Gateway deployment before publication

Supported client publishers wait for a successful gateway deployment before public GitHub or private S3 uploads. Ordinary pushes to main, local preparation and candidate-only CI do not deploy the gateway. Client publication is never an acceptance test to run without authorization.

For a public release, gateway source equals the accepted release merge commit at `origin/release`. A private internal release selects the public `atomicobject/rhizome` release commit and retains that selection across retries. Its committed server Docker inputs must match the public source by mode, type and Git object hash. Missing inputs, differences or unreviewed Docker context reads block publication.

The trusted gateway workflow runs on public main, preserving production environment and OIDC restrictions. It tests and builds a separate immutable source checkout, tags the image with that SHA and verifies the deployment from trusted main. Private publishers receive no gateway AWS permissions. Dispatch carries only the public SHA and correlation ID.

The gate records the pinned source, fingerprint, request ID and exact workflow identity in a receipt. Failure, cancellation, timeout, ambiguous identity or API errors prevent uploads. Uncertain dispatches reconcile the saved request. Canonical refs are checked again after the wait. Only a live publisher and its descendants reuse a verified receipt during an uninterrupted invocation; recovery performs fresh deployment verification when publication remains necessary.

## Desktop app

Every public release includes the macOS desktop app from the same accepted source and version ([SPEC-0122](specs/technical/desktop-distribution-and-updates.md)). GoReleaser builds the native CLI binaries; its arm64 Darwin hook, `build_desktop.py`, wraps that exact `rzm` in the Tauri app using `desktop/src-tauri/tauri.release.conf.json`.

CI restores the source-bound frontend produced on Linux and avoids compiling it again on the Mac. Native compilation receives no signing credentials. Bundling receives only the required Apple and updater credentials. Verification checks Developer ID signatures, hardened runtime, stapled notarization tickets and Gatekeeper approval for the app, mounted DMG and updater archive. The final bundle includes:

- `Rhizome-macos-arm64.dmg`;
- `Rhizome-macos-arm64.app.tar.gz` and its `.sig`;
- `latest.json`, pointing at this release's signed archive;
- five CLI archives for Darwin arm64/amd64, Linux arm64/amd64 and Windows amd64;
- the installer, its checksum, `checksums.txt` and `release-manifest.json`.

A missing desktop asset or failed signature, notarization or content check stops publication. All release binaries compile the deployed team API origin from `.goreleaser.yml` into `pkg/teamapi/session.DefaultOrigin`; see [the team API runbook](../services/team-api/RUNBOOK.md) if it changes. Internal releases remain CLI only.

### Developer ID signing and notarization

Keep the encrypted `.p12` backup, export password and notarization `.p8` key in a restricted 1Password vault. The `.p12` must include the private key; the public `.cer` alone cannot restore signing. A local signing identity can live in the release Mac's login Keychain. Never commit credential bundles, even encrypted ones.

Configure the protected GitHub Environment named `release` to permit trusted main. Add required reviewers if the team wants a separate release-job approval. A premerge candidate needs an explicitly approved temporary branch exception; remove it after verification. Store these encrypted Environment secrets using protected input:

| Secret | Purpose |
| --- | --- |
| `APPLE_SIGNING_IDENTITY` | Full Developer ID Application identity and Team ID |
| `APPLE_API_KEY` | App Store Connect team key ID |
| `APPLE_API_ISSUER` | App Store Connect issuer ID |
| `APPLE_API_PRIVATE_KEY` | Notarization `.p8` contents |
| `APPLE_CERTIFICATE` | Base64-encoded `.p12` backup |
| `APPLE_CERTIFICATE_PASSWORD` | `.p12` export password |
| `TAURI_SIGNING_PRIVATE_KEY` | Updater signing key matching the committed public key |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Updater key password |
| `BREW_GITHUB_TOKEN` | Homebrew tap publication token |

Base64 is encoding, so `APPLE_CERTIFICATE` remains a secret. GitHub supplies the publisher's repository token separately. CI needs no provider or model keys. Routine local release preparation needs none of these release secrets and does not prompt 1Password.

CI imports the identity and Apple intermediate certificate into a temporary Keychain. Notarization keys and signing handoffs use owner-only temporary files. The general build environment contains a protected file reference, and only bundling/notarization receives the relevant credentials. Cleanup restores the Keychain search list and removes temporary files and keys even after failure; runner destruction is the final boundary after forced termination.

Tauri notarizes and staples the app before producing its updater archive; the hook separately submits and staples the outer DMG. Only ticket-service network failures identified as stapler error 68 with an NSURLErrorDomain diagnostic receive bounded retries. Invalid signatures or tickets fail immediately.

To test the full build without publishing, manually run the **Release** workflow on the reviewed source ref with `candidate_only=true`. It submits to Apple's notary service and retains the verified bundle for 30 days, but does not change release refs, deploy the gateway, create a GitHub Release or update Homebrew. The protected Environment's branch restriction still applies.

Local snapshots remain available for development. An ad-hoc snapshot needs no Apple credentials; explicit notarization uses `RZM_NOTARIZE_DESKTOP=1` and the Apple variables above, with the identity installed in the login Keychain. Optional `scripts/with-secrets release` can load a separately configured local release Environment. Configuring that local convenience is separate from CI readiness.

### Homebrew distribution

The protected Linux publisher maintains `rhizome.rb` and `Casks/rhizome-desktop.rb` in `atomicobject/homebrew-tap`. The formula installs the CLI on supported Mac/Linux platforms. The cask installs the already verified notarized DMG on Apple Silicon Macs, links the bundled `rzm`, and leaves built-in app updates enabled. No extra Mac build or credential is needed.

The cask uses the app's minimum macOS version (currently Monterey 12) and the exact release DMG checksum. The publisher proves whether each tap file exists before creating it, preserves unrelated files, and writes only changed recipes. Partial updates resume through the existing gateway-gated recovery; a complete release is a no-op only when both recipes match.

Desktop users should migrate from the CLI-only formula before installing the cask:

```sh
brew uninstall --formula atomicobject/tap/rhizome
brew install --cask atomicobject/tap/rhizome-desktop
```

Homebrew owns the cask's `rzm` link. The app's separate Settings > Install Command-Line Tool step is for direct DMG installations. The cask is created by the first successful public publication; candidate verification does not write the tap or publish an installable release URL.

### Updater signing key

Installed apps verify updates against the public key in `desktop/src-tauri/tauri.conf.json`. The private key and password belong in the restricted backup and protected CI Environment. The desktop hook rejects a placeholder public key and verifies the produced archive before staging it. Cargo and the pinned verifier receive no credentials; only the signing process receives the updater key.

The verifier under `scripts/release/updater-signature` builds offline with its locked dependencies into `.tmp/updater-signature`. Normal desktop dependency fetching supplies its Cargo cache.

To replace or recover the key before an app has shipped:

1. From `desktop/`, run `npx tauri signer generate -w <offline path>/rhizome-updater.key` and supply the password interactively.
2. Save the private key and password in restricted backups, then update their protected CI secrets.
3. Commit the corresponding public key in `tauri.conf.json`.

Keep an offline copy of the private key and password apart from the active release Environment. Losing the key prevents installed apps from verifying future updates. After distribution, rotation requires a release signed with the old key that embeds the new public key, followed by switching to the new private key.

## Canonical branch topology

Public releases are prepared against fresh main and approved through a release PR. The accepted merge SHA identifies the exact release source. After all builds pass, CI advances `origin/release` and `refs/tags/<version>` to that source in one atomic, non-force push. Main already contains it through the reviewed PR. Conflicting branches or tags stop publication without replacement.

Private legacy hotfixes retain the local orchestrator's topology: a release cut from main advances both canonical branches; a release-branch hotfix keeps its tag on the hotfix commit and integrates it into fresh main with a synthetic merge. Git 2.38 or newer is required for that local `merge-tree --write-tree` flow.

## Phase commands and resume

If the final public publisher fails, retry only the failed jobs in the original workflow run:

```sh
gh run rerun <original-run-id> --failed
```

Retain the original run and its verified artifact. The final bundle is retained for 30 days. A complete rebuild can produce different signed/archive bytes even at the same source; it cannot replace already uploaded assets or serve as publication recovery. Do not rerun all jobs to recover publication. After artifact expiry, stop for artifact recovery and reconciliation rather than rebuilding over a partial release.

The publisher also retains its gateway receipt for 30 days and restores it on retry. An uncertain or pending dispatch reconciles the original request. If the previous publisher started but its receipt was lost, retry stops for deployment reconciliation before any new dispatch. A publisher that was skipped because an earlier build failed may start normally after that build is repaired.

The publisher verifies existing remote notes, source, asset names and hashes against that same bundle. A matching partial draft resumes missing uploads; conflicting assets or notes fail without overwrite. A complete matching release is a no-op when both the Homebrew CLI formula and desktop cask also match. If only Homebrew remains, the retry performs fresh gateway and topology verification before its update. Never edit notes or move tags to make recovery pass.

The old `release-apply`, `release-publish`, `release-resume` and direct `make release` paths reject public publication. Read-only legacy planning/build commands remain available, but are not the public preparation workflow.

Private releases retain `.release/<base>-<head>/` checkpoints, or `first-release-<head>` when there is no tag. Failures retain state and print an exact retry command. Dry-run failures recommend only another dry run. Private phase commands accept the original reviewed base and head:

```sh
RELEASE_BASE=vX.Y.Z RELEASE_HEAD=<sha> make release-resume
```

Private checkpoints remain `applied`, `canonical_branches_published`, `github_published`, mirror completion and `published`. Resume validates the original plan, local tag and remote topology; uncertain or conflicting publication stops for inspection. Older run-state schemas require a new reviewed plan.

## Internal releases

Atomic Object users may continue using the private S3 mirror while its bridge is replaced. Internal releases run from the private archive and remain local through `./scripts/with-secrets release make cut-release`. They require the legacy release toolchain, authenticated Codex CLI, GoReleaser v2, gitleaks, Git 2.38 or newer, publishing tokens and AWS access.

`RZM_INTERNAL_S3_BUCKET` selects internal publication. It and `RZM_INTERNAL_S3_LEGACY_SUFFIX` belong only in the private release Environment. Internal source and target repository visibility must agree: a private repository cannot silently publish publicly, and a public repository cannot bundle internal keys.

Internal builds additionally require `ATOMIC_RHIZOME_KEY`, `VOYAGE_API_KEY` and `TYPESAFE_API_KEY`. `scripts/teamkeys/release.py` supplies the encrypted team bundle through a temporary Go overlay after verifying repository privacy. The tracked bundle source stays empty. Internal artifacts link the private mirror into the CLI and installer and retain legacy `.tbz`/`latest.json` layouts alongside current GitHub-shaped update documents. Prereleases never move root `latest` aliases.

Private apply verifies repository privacy, AWS access and key inputs before mutation; resume refuses a changed internal/public setting. `make release-s3` accepts only a tagged, non-snapshot build. Do not use the public Release workflow for internal publication; resume locally.

Public builds reject compiler overlays, disable persisted Go settings, remove stale mirror configuration and scan binaries and installers before upload. Public artifacts must contain neither team keys nor private mirror locations. Keep private release credentials separate from public build environments.

## Publication ownership and manual fallback

GitHub Actions owns public builds and publication after release-PR approval. A version-tag push is not a release trigger. Local scripts cannot race CI by publishing the same public release. Manual dispatch is reserved for explicit candidate verification. Publication recovery retries failed jobs in the original approved run with its original verified artifact, as described above.

The protected Mac job has read-only repository access. Complete bundle assembly also runs with read-only permissions. Only the final Linux publisher has repository write and gateway-dispatch permissions, and candidate mode skips that job. The Homebrew token reaches only the Homebrew operation.

## Public migration

The historical repository remains a private archive. Never merge that history into this public source repository. [The open-source preparation specification](specs/technical/open-source-preparation.md) records source and artifact requirements. Source publication and release-PR preparation do not authorize an actual product release.

GoReleaser v2 remains the compiler configuration. Public archives and both Homebrew recipes are assembled and published by the CI helpers so cross-platform work can stay on Linux. The private legacy flow retains its GoReleaser publication configuration.

## Release acceptance

Record the commit, version, platform, commands and observed results. Fixture tests establish helper behavior; CI establishes native build/signing evidence; public downloads and installed-app updates require separate production evidence. Never mark a check complete because its implementation exists.

Before authorizing publication:

1. Complete required release gates in [quality gates](engineering/quality-gates.md), including `make check-full`, desktop checks and the required independent review.
2. Run candidate-only CI on the intended implementation and verify the complete artifact set, matching app/CLI versions, Developer ID signatures, notarization tickets, updater signature, feed and checksums.
3. Install the candidate in a disposable location or account. Exercise Settings, command-line installation and login-shell resolution, repository setup and pin advancement while preserving the normal apps.
4. Test credential setup with a consenting account and synthetic failure fixtures. Verify import, cancellation, rotation and fresh-process use without recording secrets.

After the first authorized public release:

1. Download assets without GitHub authentication and verify checksums, feed version/URLs and the updater archive signature.
2. Download and install the DMG on a fresh macOS account, record Gatekeeper behavior, and verify the command-line link and behavior after deleting the disposable app.
3. Follow only the README from a fresh agent session. Install, initialize and index a disposable repository, complete retrieval, exercise both provider-key and no-key setup, and verify Homebrew installation.

After the next release, update an installed first-release app through Settings and `rzm update`. Verify signature checking, restart, restored windows, bundled CLI version, command-line link, independent repository pins and folder-access prompts. Production release-to-release updating remains unverified until that succeeds.
