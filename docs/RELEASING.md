# Releasing Rhizome

`make cut-release` remains the guided release entrypoint. It creates a bounded evidence snapshot and reviewed plan before any repository mutation, builds a release candidate, applies the version changes, publishes the canonical Git branches atomically, deploys and verifies the release gateway source, then publishes GitHub Release assets, including the macOS desktop app, and the Homebrew formula.

## Prerequisites

- A clean `main` or `release` worktree with the intended commits already present.
- Git 2.38 or newer. Hotfix publication uses `git merge-tree --write-tree` without checking out `main`.
- `python3`, `codex`, GoReleaser v2, `gitleaks`, and `trash` available.
- Codex CLI authentication (`codex login status`).
- GitHub CLI authentication plus `RZM_RELEASE_GITHUB_TOKEN` for GoReleaser publication.
- `BREW_GITHUB_TOKEN` for publishing the Homebrew tap.
- An Apple Silicon Mac with the Xcode command line tools, Node.js, and a Rust toolchain from rustup (`cargo` on `PATH` or in `~/.cargo/bin`). The desktop npm dependencies are installed with `make desktop-deps` when `desktop/node_modules` is missing.
- `TAURI_SIGNING_PRIVATE_KEY` and `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` for public releases, from the release 1Password Environment. See [Desktop app](#desktop-app).

GitHub CLI enrichment is optional during planning. If it is missing or unavailable, local Git evidence is retained and the plan is marked degraded. Applying a degraded plan requires explicit confirmation or `--accept-degraded-evidence`.

## Canonical source preflight

Normal releases may start only from the exact local branch `main` or `release`. Before `cut-release` plans anything, it fetches the matching origin branch and requires:

- `HEAD` is attached to `main` or `release`, not detached or on another branch;
- `HEAD` exactly equals the freshly fetched `origin/main` or `origin/release`.

The candidate build and first apply additionally require a clean worktree. The first apply repeats the source check before repository mutation, so an origin advance during plan review or the candidate build stops the release. Ahead, behind, and diverged worktrees are all refused; update the branch normally, inspect the resulting range, and create a new plan when necessary. `make release-plan` and `make release-dry` remain read-only inspection surfaces, but they do not authorize a later apply from a stale or non-canonical branch.

### First release in a repository

When the repository has no version tags, the plan has empty `base_tag` and `base_commit` fields. Its evidence includes the root commit and every later commit up to the reviewed head. No baseline tag needs to be invented. Discovery considers tags matching `v[0-9]*`; a repository with version tags but no reachable release base is an error, not an implicit first release.

The existing changelog through `v0.50.5` records pre-public releases whose tags are absent from this clean repository. Version selection continues from the version stored in the planned source commit, not from zero: a minor recommendation yields `v0.51.0` and a patch yields `v0.50.6`. Every selected version, including a custom command-line version, `VERSION`, or a reused stored plan, must exceed that baseline. Later releases use their previous version tag as the floor.

First-release state is stored in `.release/first-release-<head>/`. The phase commands accept `RELEASE_BASE=` with the reviewed `RELEASE_HEAD`; printed Python resume commands quote the empty base as `--base ''`. Keep those reviewed inputs when resuming.

A release from `main` creates `origin/release` in the same atomic, non-force push that advances `origin/main`. A hotfix still requires an existing `release` branch. If another release tag appears after a first-release plan was prepared, create a new plan instead of reusing the empty baseline.

## Evidence and note generation

The release range is divided by first-parent commits. Each delivery unit is synthesized once from this trust order:

1. Curated entries under `CHANGELOG.md`'s `Unreleased` section.
2. Verified completed-effort `Actual Delivered` content from the same delivery unit.
3. Top-level merged PR descriptions, with commit-proven stacked PRs attached as support.
4. Direct commit messages, changed paths, and insertion/deletion totals.

Raw diffs are never sent to the model. The full evidence audit is persisted separately from a 620,000-character maximum generation packet. Generation defaults to `gpt-5.6-luna` with medium reasoning and schema-validated output. The editorial pass clusters the whole release into at most seven user-outcome themes instead of emitting one item per delivery unit; it suppresses internal-only machinery and highlights supported migrations or default changes. Each theme carries its own source identifiers. Curated `Unreleased` entries covered by generated themes are not repeated, while omitted curated entries are retained deterministically. Failures stop before mutation and never silently fall back to another model.

GitHub evidence and publication use separate credentials. Use
`./scripts/with-secrets release make cut-release` to load them from 1Password.
The launcher maps the Environment's `GITHUB_TOKEN` to `RZM_RELEASE_GITHUB_TOKEN`;
only the GoReleaser publish subprocess receives it as `GITHUB_TOKEN`. The Homebrew
token remains `BREW_GITHUB_TOKEN`. Do not store token values in `.env`.

Release processes discard provider, 1Password, and AWS credentials; only publishing subprocesses receive the required GitHub tokens. The updater signing key reaches only GoReleaser, its desktop build hook, and the public-apply Tauri signing probe; only the team-key bundler receives team keys. Set `OP_ACCOUNT`, and `RZM_OP_RELEASE_ENVIRONMENT_ID` to the release Environment (falling back to `RZM_OP_ENVIRONMENT_ID`), when using the optional Environment wrapper. See [credential policy](engineering/secrets.md).

Overrides:

```sh
RZM_RELEASE_MODEL=<model> RZM_RELEASE_REASONING_EFFORT=<effort> make release-plan
PREV_TAG=v0.49.0 VERSION=v0.50.0 FEEDBACK="Emphasize migration guidance" make release-plan
```

## Normal workflow

Inspect a read-only plan:

```sh
make release-plan
```

Run the same planning path plus a GoReleaser snapshot build, without changing files, committing, tagging, or publishing:

```sh
make release-dry
```

Both dry run and cut release display the generated release notes and recommendation rationale before version selection. Without a `VERSION` override, the model's minor or patch recommendation is option 1 in the version menu. Press Enter to accept it; the other increment and a custom version remain available.

Cut and publish the release:

```sh
make cut-release
```

The order is fixed:

1. Fresh canonical-source preflight, plan, preview, and version selection.
2. Candidate build, which builds the desktop app too.
3. Three-file apply, release commit, and local version tag.
4. Atomic, non-force publication of `origin/main` and `origin/release`.
5. Local GoReleaser publication and exact verification of the GitHub-created remote tag.
6. Internal releases only: private mirror publication (`make release-s3`), checkpointed as `s3_published` so resume retries only the mirror.
7. Completion.

The apply phase owns only `CHANGELOG.md`, `pkg/vault/version/version.go`, and `pkg/vault/changelog/CHANGELOG.md`.

## Gateway deployment before publication

Every supported client publisher waits for a successful gateway deployment before uploading artifacts: `make cut-release`, publish/resume, `make release`, `make release-s3`, and the manual Actions fallback. Gateway deployment no longer follows ordinary pushes to `main`. Planning, preflight and dry-run commands do not deploy anything.

For a public release, the server source is the validated client release commit at `origin/release`, including the exact hotfix commit. A private internal release automatically selects the public `atomicobject/rhizome` release commit and retains that selection across retries. Its committed server Docker inputs must match the public source by mode, type and Git object hash. Missing inputs, differences, or unreviewed Docker context reads block publication. Synchronize matching public gateway changes into the private release checkout before retrying; do not substitute a different SHA manually.

The trusted workflow still runs on public `main`, preserving its production environment and OIDC restrictions. It tests and builds a separate immutable source checkout, tags the image with that source SHA, and uses the deployment verifier from trusted main. The private publisher needs `gh` authorization to dispatch and read public Actions runs; it gains no gateway AWS access. No private source, repository location, mirror setting, provider key, or publishing token is sent in dispatch inputs.

The release run stores `gateway-receipt.json` beside its existing checkpoints. It contains the pinned public source and fingerprint, request ID, and matched workflow URL. The gate prints that URL promptly and waits up to four hours. Failure, cancellation, timeout, ambiguous identity or API errors prevent client uploads and preserve the existing resume command. Uncertain dispatches reconcile the saved request instead of blindly creating another deployment. Pending runs resume; an earlier terminal run requires a fresh attempt for the same saved source. Canonical release refs and the local tag are checked again after the wait.

Only a live publisher process and its descendants can reuse its verified receipt during one uninterrupted publish invocation. Reusing an older receipt through a direct command starts fresh deployment verification. GoReleaser and a remaining internal S3 upload therefore share one deployment during an uninterrupted run, while S3-only resume verifies again. Receipt arguments are internal handoffs, not skip controls.

The existing first-publication atomic push creates a missing `release` branch before this gate. The Actions fallback must be dispatched on public `main`; its write-capable publisher depends on the validated deployment. Do not cut or publish a client release merely to test the gate. Deployment-only acceptance can exercise a pinned public main ancestor before the first release branch exists; it does not authorize client publication.

## Desktop app

Every public release publishes the macOS desktop app built from the same tag ([SPEC-0122](specs/technical/desktop-distribution-and-updates.md)). GoReleaser's `rzm-darwin` build runs `scripts/release/build_desktop.py` as a post-build hook. For the `darwin/arm64` binary it builds the Tauri app in `desktop/` with `desktop/src-tauri/tauri.release.conf.json` and the release version, bundling that exact `rzm`. It then verifies Developer ID signatures, hardened runtime, stapled notarization tickets and Gatekeeper approval in the app, mounted DMG and updater archive before staging these assets in `.tmp/goreleaser-extra/`:

- `Rhizome-macos-arm64.dmg`, the installer;
- `Rhizome-macos-arm64.app.tar.gz` and its `.sig`, the signed updater archive;
- `latest.json`, the update feed the installed app reads from `releases/latest/download/latest.json`, pointing at this release's archive.

GoReleaser uploads all four and lists them in `checksums.txt`.

The candidate build, a GoReleaser snapshot, runs the same hook, so a broken desktop toolchain stops the release before any repository change. Without the signing key the snapshot still builds the app and DMG but skips the updater archive and `latest.json`. Publication sets `RZM_RELEASE_DESKTOP=1`. In that mode the hook fails when the key is missing, and GoReleaser fails when any of the four assets is absent, so a public release cannot go out without the app. Apply also refuses to start a public release without the key.

Public `make release` also enables desktop publication automatically and requires the signing key and desktop build tools listed in the prerequisites. Internal releases (`RZM_INTERNAL_S3_BUCKET` set) publish the CLI only. The manually dispatched GitHub Actions recovery workflow publishes the same notarized desktop assets as local publication. In those cases the hook clears any staged desktop assets left by an earlier build, and the desktop asset entries in `.goreleaser.yml` become globs that match nothing.

If the desktop build fails during publication, GoReleaser stops in its build phase, before it creates a GitHub release. Fix the cause, such as a missing Rust toolchain or key, and rerun the printed resume command. Resume continues from the recorded checkpoint and runs GoReleaser again.

### Developer ID signing and notarization

Public desktop publication requires Python 3.12 or newer for safe updater archive extraction, a valid Developer ID Application identity in the release Mac's login Keychain and notarization credentials. `RZM_RELEASE_DESKTOP=1` requires both notarization and updater signing. A local snapshot is ad-hoc signed unless `RZM_NOTARIZE_DESKTOP=1` explicitly requests notarization. Ordinary development requires no Apple credentials.

Store the encrypted `.p12` export, its password, and the notarization `.p8` private key in a restricted 1Password vault. The `.p12` must include the certificate and private key. The public `.cer` alone cannot restore signing. Keep 1Password backups after installing the identity in a release Mac's Keychain. Never store even encrypted credential bundles in Git.

Local releases load these variables from the separate release 1Password Environment through `scripts/with-secrets release`:

| Variable | Value |
| --- | --- |
| `APPLE_SIGNING_IDENTITY` | Full Developer ID Application identity, including the Team ID in parentheses |
| `APPLE_API_KEY` | App Store Connect team key ID |
| `APPLE_API_ISSUER` | App Store Connect issuer ID |
| `APPLE_API_PRIVATE_KEY` | Contents of the notarization `.p8` private key |

The GoReleaser adapter writes signing credentials to an owner-only temporary file and passes only its reference through the general build environment. The desktop hook reads that file, compiles without credentials, then runs Tauri bundling with signing credentials. The desktop hook writes `APPLE_API_PRIVATE_KEY` to an owner-only temporary file for Tauri, removes its contents from the child environment, and removes the file when Tauri exits. An explicitly supplied `APPLE_API_KEY_PATH` can instead reference an existing protected local file for candidate verification. The key is never printed. Apple credentials are forwarded only through GoReleaser to the notarizing desktop build, then removed before distribution verification and public-content scanning. Tauri notarizes and staples the app before producing the updater archive; the hook separately submits and staples the outer DMG. Signing failures stop staging; there is no unsigned fallback for public publication.

For CI, create a GitHub Environment named `release`, with selected-branch restrictions; add required reviewers if the team wants a separate approval for release jobs. Permit only trusted release source. Temporarily authorize the reviewed implementation branch for a premerge candidate, then remove that exception. Set these encrypted Environment secrets using protected secret input, never command literals or logs:

- The four Apple variables above.
- `APPLE_CERTIFICATE`: base64 encoding of the backed-up `.p12`, and `APPLE_CERTIFICATE_PASSWORD`: its export password. Base64 is an encoding, not encryption; treat it as a secret.
- `TAURI_SIGNING_PRIVATE_KEY` and `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`, using the existing updater key that matches the committed public key.
- `BREW_GITHUB_TOKEN` for recovery publication; the workflow supplies GitHub publication auth separately.

CI runs on Apple Silicon, imports the `.p12` and Apple's intermediate certificate into a temporary Keychain, and writes the notarization key under runner temporary storage. The cleanup step restores the Keychain search list and deletes temporary credentials even if the build fails. Temporary Keychain passwords do not appear in process arguments. These workflows never receive provider keys. Hosted runner destruction provides the final cleanup boundary if a job is forcibly terminated before cleanup.

To verify a candidate, select the manual GoReleaser fallback workflow, choose the reviewed source ref, set `candidate_only=true`, and provide any tag input. Confirm the candidate job, including distribution verification, succeeds. This submits software to Apple's notary service but does not create a GitHub release, advance version refs or deploy the gateway.

### Updater signing key

The installed app accepts an update only when its signature verifies against the public key committed in `desktop/src-tauri/tauri.conf.json`. The private key and its password live only in the release 1Password Environment as `TAURI_SIGNING_PRIVATE_KEY` and `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`. `./scripts/with-secrets release` loads them; `run_clean.py` passes them to GoReleaser and its desktop build, and public apply passes them to its Tauri signing probe. The verifier receives neither value. Never commit them, print them, or copy them into release notes or logs.

The release public key is committed in `tauri.conf.json`. The desktop hook refuses a placeholder key and verifies the generated archive's signature against that public key before staging any desktop assets. Public apply also signs a disposable probe and verifies it before changing release files or refs, catching a changed or mismatched key even if the candidate build skipped signing.

Verification uses `scripts/release/updater-signature`, a small helper pinned to the same `minisign-verify` version as the desktop updater. It builds with `cargo --offline --locked` into `.tmp/updater-signature`; the release machine therefore needs that pinned crate in its Cargo cache. A normal desktop dependency fetch supplies it. Cargo and the verifier receive no signing or provider credentials. Only the Tauri signing subprocess receives the updater key and password; subprocess output is suppressed on failure.

To replace or recover the release key before any app has shipped:

1. From `desktop/`, run `npx tauri signer generate -w <offline path>/rhizome-updater.key` and choose a password when it asks. Do not paste the key or password into a terminal command, chat, or log.
2. Add the key file's contents as `TAURI_SIGNING_PRIVATE_KEY` and the password as `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` in the release 1Password Environment.
3. Replace the placeholder with the contents of `rhizome-updater.key.pub` (the public key, safe to commit) and commit it.
4. Move the key file to the offline backup described below.

Losing the private key strands every installed app: it can never verify another update, and each user must download a new DMG by hand. Keep an offline backup of the key file and password, stored apart from the 1Password Environment. To rotate the key, publish one release signed with the old key that embeds the new public key, then switch the Environment to the new key. A leaked key lets anyone sign an update the app accepts; rotate immediately.

## Canonical branch topology

For a release cut from `main`, the release commit advances both `origin/main` and `origin/release`. The local version tag identifies that same commit.

For a hotfix cut from `release`, the release commit and version tag remain free of unreleased `main` work. The orchestrator fetches the latest `origin/main` and creates a merge commit without checking out `main`:

- first parent: the freshly fetched `origin/main` tip;
- second parent: the tagged release commit; and
- tree: the conflict-free merge of those commits.

It then atomically advances `origin/main` to that merge and `origin/release` to the release commit. A merge conflict or either remote branch moving before the push rejects the operation without updating either canonical branch. The release commit must remain an ancestor of the published main tip, preserving first-parent evidence and making later retry verification deterministic.

Canonical branches are intentionally published before GoReleaser. That makes the release commit reachable by GitHub before the Releases API creates the remote tag. GoReleaser targets the exact release commit; after publication, the orchestrator resolves the remote tag, including an annotated tag's peeled target, and refuses to continue unless it equals the release commit. The release script never force-pushes a conflicting branch or replaces a conflicting tag.

## Phase commands and resume

Reviewed artifacts live under the ignored `.release/<base>-<head>/` directory. Successful releases and successful dry runs trash that exact directory. Failures retain it and print an exact retry command. A failed dry run recommends another `dry-run` command; it never recommends `resume`, because resume may continue into apply and publication.

Run a persisted phase directly with the base tag and the original planned head SHA:

```sh
RELEASE_BASE=v0.49.0 RELEASE_HEAD=<sha> make release-build
RELEASE_BASE=v0.49.0 RELEASE_HEAD=<sha> make release-apply
RELEASE_BASE=v0.49.0 RELEASE_HEAD=<sha> make release-publish
RELEASE_BASE=v0.49.0 RELEASE_HEAD=<sha> make release-resume
```

For a deliberately accepted degraded plan:

```sh
RELEASE_BASE=v0.49.0 RELEASE_HEAD=<sha> ACCEPT_DEGRADED_EVIDENCE=1 make release-resume
```

Progress is checkpointed in this order: `applied`, `canonical_branches_published`, `github_published`, and `published`. If a branch push succeeds but its response or checkpoint is lost, resume queries the remote refs and accepts publication only when `origin/release` equals the release commit and that commit is contained in `origin/main`. A mismatched branch or tag stops for inspection rather than overwriting remote history.

Run-state schema version 3 makes this ordering fail closed; older in-flight state files must be replaced with a new reviewed plan. Resume revalidates the local release commit/tag, canonical branch topology, and GitHub-created remote tag even when their checkpoints already exist. If the remote tag exists but the GitHub checkpoint was never written, resume stops for inspection instead of risking duplicate or partial GoReleaser publication.

Resume verifies the GitHub-created tag and skips duplicate publication after a recorded successful GitHub release. A reviewed plan mismatch or moved base tag is refused. First apply requires a clean worktree; a partial apply retry accepts only release-owned files whose contents match either the planned commit or the deterministic reviewed output. The selected release date is checkpointed before file mutation and reused across retries. Use `--new-plan` only when intentionally replacing the prior plan.

## Internal releases

Atomic Object users may continue installing and updating from a private S3 mirror while the bridge is replaced; their binaries carry encrypted team keys. The bridge code may remain in public source, but internal releases run from the private archive. A release is internal when `RZM_INTERNAL_S3_BUCKET` is set. It and the legacy suffix for the oldest installers, `RZM_INTERNAL_S3_LEGACY_SUFFIX`, live only in the release 1Password Environment, so run releases through `./scripts/with-secrets release`. Neither may appear in Git; a gitleaks rule rejects staged legacy suffix aliases. Releases from a private repository must be internal and releases from a public repository must not be, so a release started without the Environment stops before any mutation instead of silently going out public.

An internal release additionally:

- requires `ATOMIC_RHIZOME_KEY`, `VOYAGE_API_KEY`, and `TYPESAFE_API_KEY`. `scripts/teamkeys/release.py` encrypts the keys into a temporary Go overlay for the publishing build only and refuses to run unless the GitHub repository is private, because GoReleaser also uploads the bundled binaries there;
- when `RZM_INTERNAL_TEAM_API_ORIGIN` is set in the private release environment, compiles that root HTTPS origin into `pkg/teamapi/session.DefaultOrigin` for team sign-in. The gateway must be deployed first; see [the team API runbook](../services/team-api/RUNBOOK.md). Public builds discard this variable and keep an empty default;
- links the mirror into `rzm update` and the staged installer (`https://<bucket>.s3.amazonaws.com/releases`), in place of the GitHub API default;
- requires AWS access to the bucket (`aws login` or the release Environment), checked before apply;
- publishes the legacy `latest.json` and `.tbz` layout read by v0.50.x and older clients, and GitHub-release-shaped `releases/latest` and `releases/tags/<version>` documents read by current clients.

With `RZM_INTERNAL_S3_BUCKET` unset, the same commands produce a public release with no team keys and no mirror. Apply records which kind of release it is; resume refuses to continue if the setting changed. Apply also verifies AWS access, the three key inputs, and repository privacy before any repository mutation. `make release-s3` publishes only a `dist/` produced by the tagged, non-snapshot release build, and prereleases never move the root `latest` aliases. Do not use the GitHub Actions recovery workflow for internal releases: it has neither the bucket nor the keys. Resume locally instead.

Public builds require empty tracked bundle source and reject inherited Go compiler overlays. They set `GOENV=off` to prevent persisted Go settings from supplying an overlay, and discard stale private mirror settings. GoReleaser post-build hooks check each actual binary before archiving or uploading, and installer staging checks the script before upload. These checks reject the generated internal bundle marker and S3 mirror URL and require a redacted gitleaks scan. Internal artifacts bypass the public content checks only after verifying that the target repository is private. Keep public release machines separate from the private release 1Password Environment.

## Publication ownership and manual fallback

The local orchestrator is the sole publisher during a normal release. Do not push a tag to start GoReleaser: an automatic tag-triggered publisher can race the local process while GitHub is creating the same release.

The GitHub Actions release workflow is a manual, guarded recovery surface. Dispatch it only for an existing validated version tag when local resume cannot complete publication. Its read-only probe validates the tag and canonical topology first, and an existing GitHub release makes the write-capable GoReleaser job a no-op. API errors, invalid tags, and conflicting refs fail closed; any existing release, including a draft or partially populated release, is left untouched for maintainer inspection.

`make release` is the gated lower-level publisher for an exact, clean release tag with matching canonical topology; use `make cut-release` for normal releases. Public publication also requires the desktop signing key and builds the desktop app through the same publisher.

The GitHub Actions workflow requires the protected `release` Environment and all signing secrets listed below. Recovery publication still requires an existing validated release tag and a verified gateway deployment. Select `candidate_only=true` to build and notarize the selected workflow ref without deployment or publication; the required tag input is ignored in candidate mode. Candidate mode has read-only repository permissions and does not upload artifacts.

## Public migration

The historical repository is retained as a private archive. This clean repository's source is public. The [open-source preparation specification](specs/technical/open-source-preparation.md) records the source and artifact requirements. Source publication does not establish that release downloads or installation work. Release preparation does not authorize publishing artifacts or changing repository visibility.

The current configuration retains the existing Homebrew formula and requires GoReleaser v2. GoReleaser reports `brews` as deprecated; migrating to a cask later requires a coordinated tap migration, not just renaming the config key. See [GoReleaser migration guidance](https://goreleaser.com/resources/deprecations/#brews).

## Release acceptance

Record the commit, tag, platform, commands, and observed result for each check. Local fixtures and CI establish preparation evidence; public downloads and installed-app updates need separate evidence. Do not mark a check complete because its implementation or test exists.

Before publication:

1. Run the required release gates in [quality gates](engineering/quality-gates.md), including `make check-full`, `make desktop-check`, and `make desktop-build`. Review the complete change set and finish the required independent review.
2. Run the release candidate build on Apple Silicon with the actual release configuration. Verify the app and bundled `rzm` versions match, the bundle signature is valid, and the DMG, updater archive, signature, feed, and checksums are staged. Confirm signing-key access without displaying or recording private values.
3. Install the candidate into a disposable location or account. Exercise Settings, the command-line installation prompt, login-shell resolution, repository setup, and pin advancement. Keep the normal Rhizome and Rhizome Dev installations intact.
4. Test AO credential setup with a consenting account: import the intended providers, cancel an import, repeat after rotation, and verify a subsequent fresh process uses the saved configuration. Missing CLI, denied access, timeout, and missing-provider cases must have actionable messages without secret values. Use synthetic fixtures for automated tests.

After the first public release:

1. Fetch all desktop assets and `checksums.txt` without GitHub authentication. Verify the DMG and updater archive checksums, the feed's version and archive URL, and the archive signature against the committed public key.
2. Download the DMG in a browser on a fresh macOS account. Record Gatekeeper behavior and complete the documented Open Anyway flow. Install the command-line tool, confirm a new login shell resolves the app's `rzm`, then verify deleting the disposable app leaves that link unable to run the app.
3. Follow only the README from a fresh agent session. Install, initialize a disposable repository, index it, and complete an agent retrieval. Test both provider-key setup and the no-key path. Verify Homebrew installation against the newly published formula.

After the next public release, update an installed first-release app through Settings and through `rzm update`. Confirm signature verification, restart, restored windows, the bundled CLI version, an unchanged command-line link, and independent repository pin advancement. Record whether folder-access prompts return. Until this passes, production release-to-release updating remains unverified.
