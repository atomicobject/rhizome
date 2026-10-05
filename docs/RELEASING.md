# Releasing Rhizome

`make cut-release` remains the guided release entrypoint. It creates a bounded evidence snapshot and reviewed plan before any repository mutation, builds a release candidate, applies the version changes, publishes the canonical Git branches atomically, then publishes GitHub Release assets and the Homebrew formula.

## Prerequisites

- A clean `main` or `release` worktree with the intended commits already present.
- Git 2.38 or newer. Hotfix publication uses `git merge-tree --write-tree` without checking out `main`.
- `python3`, `codex`, GoReleaser v2, `gitleaks`, and `trash` available.
- Codex CLI authentication (`codex login status`).
- GitHub CLI authentication plus `RZM_RELEASE_GITHUB_TOKEN` for GoReleaser publication.
- `BREW_GITHUB_TOKEN` for publishing the Homebrew tap.

GitHub CLI enrichment is optional during planning. If it is missing or unavailable, local Git evidence is retained and the plan is marked degraded. Applying a degraded plan requires explicit confirmation or `--accept-degraded-evidence`.

## Canonical source preflight

Normal releases may start only from the exact local branch `main` or `release`. Before `cut-release` plans anything, it fetches the matching origin branch and requires:

- `HEAD` is attached to `main` or `release`, not detached or on another branch;
- `HEAD` exactly equals the freshly fetched `origin/main` or `origin/release`.

The candidate build and first apply additionally require a clean worktree. The first apply repeats the source check before repository mutation, so an origin advance during plan review or the candidate build stops the release. Ahead, behind, and diverged worktrees are all refused; update the branch normally, inspect the resulting range, and create a new plan when necessary. `make release-plan` and `make release-dry` remain read-only inspection surfaces, but they do not authorize a later apply from a stale or non-canonical branch.

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

Release processes discard provider, 1Password, and AWS credentials; only publishing subprocesses receive the required GitHub tokens, and only the team-key bundler receives team keys. Set `RZM_OP_ENVIRONMENT_ID` and `OP_ACCOUNT` explicitly when using the optional Environment wrapper. See [credential policy](engineering/secrets.md).

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
2. Candidate build.
3. Three-file apply, release commit, and local version tag.
4. Atomic, non-force publication of `origin/main` and `origin/release`.
5. Local GoReleaser publication and exact verification of the GitHub-created remote tag.
6. Internal releases only: private mirror publication (`make release-s3`), checkpointed as `s3_published` so resume retries only the mirror.
7. Completion.

The apply phase owns only `CHANGELOG.md`, `pkg/vault/version/version.go`, and `pkg/vault/changelog/CHANGELOG.md`.

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
- links the mirror into `rzm update` and the staged installer (`https://<bucket>.s3.amazonaws.com/releases`), in place of the GitHub API default;
- requires AWS access to the bucket (`aws login` or the release Environment), checked before apply;
- publishes the legacy `latest.json` and `.tbz` layout read by v0.50.x and older clients, and GitHub-release-shaped `releases/latest` and `releases/tags/<version>` documents read by current clients.

With `RZM_INTERNAL_S3_BUCKET` unset, the same commands produce a public release with no team keys and no mirror. Apply records which kind of release it is; resume refuses to continue if the setting changed. Apply also verifies AWS access, the three key inputs, and repository privacy before any repository mutation. `make release-s3` publishes only a `dist/` produced by the tagged, non-snapshot release build, and prereleases never move the root `latest` aliases. Do not use the GitHub Actions recovery workflow for internal releases: it has neither the bucket nor the keys. Resume locally instead.

Public builds require empty tracked bundle source and reject inherited Go compiler overlays. They set `GOENV=off` to prevent persisted Go settings from supplying an overlay, and discard stale private mirror settings. GoReleaser post-build hooks check each actual binary before archiving or uploading, and installer staging checks the script before upload. These checks reject the generated internal bundle marker and S3 mirror URL and require a redacted gitleaks scan. Internal artifacts bypass the public content checks only after verifying that the target repository is private. Keep public release machines separate from the private release 1Password Environment.

## Publication ownership and manual fallback

The local orchestrator is the sole publisher during a normal release. Do not push a tag to start GoReleaser: an automatic tag-triggered publisher can race the local process while GitHub is creating the same release.

The GitHub Actions release workflow is a manual, guarded recovery surface. Dispatch it only for an existing validated version tag when local resume cannot complete publication. Its read-only probe validates the tag and canonical topology first, and an existing GitHub release makes the write-capable GoReleaser job a no-op. API errors, invalid tags, and conflicting refs fail closed; any existing release, including a draft or partially populated release, is left untouched for maintainer inspection.

`make release` remains the lower-level GoReleaser command for an already-tagged `HEAD`; use `make cut-release` for normal releases.

## Public migration

The historical repository is retained as a private archive. This clean repository remains private until publication is separately approved. The [open-source preparation specification](specs/technical/open-source-preparation.md) records the source and artifact requirements. Preparation does not authorize running release commands or changing repository visibility. Anonymous installation must be verified against a real public release after publication.

The current configuration retains the existing Homebrew formula and requires GoReleaser v2. GoReleaser reports `brews` as deprecated; migrating to a cask later requires a coordinated tap migration, not just renaming the config key. See [GoReleaser migration guidance](https://goreleaser.com/resources/deprecations/#brews).
