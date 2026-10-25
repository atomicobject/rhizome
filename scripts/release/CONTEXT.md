## Bounded, resumable release planning and canonical publication

Git first-parent history defines delivery units; curated changelog text, verified completed efforts, PR descriptions, commits, paths, and stats enrich them without loading a raw diff. Generated themes carry their own source identifiers, and curated entries are deterministically retained in the reviewed plan. `release_cli.py` owns phase ordering and `.release/` checkpoints. Keep evidence/plan immutable, mutation retry-safe, and GitHub publication checkpointed.

With no `v[0-9]*` version tag, empty base tag/commit fields mean first publication in this repository. Collection includes the root commit using `diff-tree --root` without creating a Git object. State uses `first-release-<head>` and resume preserves the quoted empty base. A main release can create a missing release branch in its atomic push; hotfixes still require that branch. Version selection and stored-plan loading enforce a strict floor from the prior tag or the source version at the planned commit.

`cut-release` and the first apply accept only an attached `main` or `release` whose `HEAD` equals the freshly fetched matching `origin` branch. The apply boundary also revalidates base tag, head, fingerprint, and worktree. It accepts either a clean first apply or an `applying` retry whose three owned files match planned or deterministic target contents, and it reuses the checkpointed release date across retries. Dry-run failures must only recommend rerunning dry-run, never full resume.

After the release commit and local tag exist, canonical branches publish atomically and without force before GoReleaser. A `main` cut advances both branches to the release commit. A `release` hotfix keeps the tag on the release commit and advances `main` with a synthetic merge whose first parent is fresh `origin/main`; Git 2.38 or newer is required for `merge-tree --write-tree`. GoReleaser is the sole normal GitHub publisher and creates the remote tag at the now-reachable release commit; orchestration verifies that remote tag before completion.

Checkpoint order is `applied`, `canonical_branches_published`, `github_published`, then `published`. Resume accepts matching remote topology after an uncertain push, but conflicting branches or tags stop without overwrite. The Actions release workflow is a validated manual fallback only; never restore a pushed-tag publisher that can race local GoReleaser.

Run state has an independent schema version because checkpoint evolution must not invalidate immutable evidence or plans. Version 3 rejects legacy run state. Every publish/resume revalidates the exact local release commit/tag and already-checkpointed remote topology; a remote tag without a GitHub checkpoint is an ambiguous partial publication and must stop for inspection rather than rerun GoReleaser.

Interactive plan, dry-run, and cut-release flows present the generated notes and recommendation rationale before version selection. Minor or patch is ranked from generated scope evidence, option 1 is the default, and Enter must accept it without requiring the operator to type a version.

Release-note generation is one global editorial pass, not one bullet per delivery unit. It clusters related evidence into at most seven user-outcome themes, suppresses internal-only machinery, calls out migrations/default changes, and uses curated source coverage to prevent generated paraphrases from duplicating `Unreleased` entries. The default bounded packet is 620,000 characters; operator model and reasoning overrides remain explicit.

GitHub evidence uses the authenticated `gh` account. Publishing credentials are deliberately separate: `RZM_RELEASE_GITHUB_TOKEN` is required before apply and is exposed as `GITHUB_TOKEN` only to the local GoReleaser subprocess; `BREW_GITHUB_TOKEN` remains scoped to the Homebrew tap.

The desktop app is published by GoReleaser, not by a checkpoint. `build_desktop.py` is the `rzm-darwin` post-build hook: it builds the app around the arm64 binary in snapshots and when `release_publish.py` sets `RZM_RELEASE_DESKTOP=1` for a public release, and is a no-op otherwise (amd64, internal, Actions fallback). With that flag it fails without `TAURI_SIGNING_PRIVATE_KEY`, and `.goreleaser.yml` turns the four desktop asset globs into required literal paths. `run_clean.py` passes the signing key and password only to a `goreleaser` command; the hook strips publishing tokens before running Tauri. `verify_desktop_signing.py` verifies the actual archive against the committed public key before staging, and public apply verifies a freshly signed disposable probe before mutation. Only the signing subprocess receives the key; Cargo and the pinned verifier receive no credentials and run with bounded deadlines. The verifier builds offline into `.tmp/updater-signature`.

- **Entry points**: `release_cli.py`, `cut_release.sh`, `generate_notes.py`
- **Deep docs**: [Release process](../../docs/RELEASING.md), [[../../docs/specs/technical/evidence-grounded-release-orchestration|SPEC-0078]]

Workspace evidence is Git-snapshot evidence. `effort_evidence.py` reads an HTML EffortWorkspace entry and follows its explicit Markdown work-log link at the same revision; missing or invalid components produce diagnostics. `collect_evidence.py` collapses changed workspace components to one owning entry candidate. Completion and delivery proof retain the existing effort rules, including exclusion of post-closure-only changes. Governing specs must be discovered notes under the config and ignore rules at each snapshot, as well as projecting a SpecLike type. The canonical discovery helper uses the target and its ancestor policy files from Git; repositories without local config retain default Markdown discovery. Working-tree contents and directory proximity alone cannot supply lifecycle evidence.

Credentials come from the release process environment, optionally through `scripts/with-secrets`. GoReleaser receives the release-scoped GitHub token only in its subprocess, and Homebrew publication uses its separate tap token. Only internal releases bundle team keys, and only through `scripts/teamkeys/release.py`; see [internal releases](../../docs/RELEASING.md#internal-releases). See [credential policy](../../docs/engineering/secrets.md).

Public release subprocesses reject inherited Go overlays, disable persisted Go
settings, and remove stale private mirror settings. GoReleaser checks every built
binary before archiving/uploading and checks the staged installer before upload;
an internal bundle marker, mirror URL, failed credential scan, or missing scanner
blocks publication. Internal builds and artifacts require verified private
repository visibility and continue from the private archive during public migration.
