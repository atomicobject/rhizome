---
name: rhizome-release
description: Prepare a Rhizome release with reviewed narrative notes, changelog and version updates, and a release pull request. Use when preparing a release or revising its open release PR.
---

# Rhizome release

Write and revise the release in the active Codex or Claude session using its existing subscription. Read `docs/RELEASING.md` for publication and recovery rules. This skill belongs to this repository, not the downstream `rzm init` templates.

The output is a release PR containing the notes and version the maintainer reviewed. Its merge authorizes CI publication. Preparing a draft or opening the PR does not authorize merging it or deploying a product release.

## Establish the release story

From a clean checkout at current main, collect the source-bound evidence:

```sh
python3 scripts/release/prepare_release.py evidence
```

Read the returned `index.json`, then its `coverage-*.json` pages one at a time. These pages cover every delivery summary, curated-entry summary and source ID, with effort paths and pointers to full changed-file lists. Record the source SHA, previous tag, version floor and evidence gaps.

Choose likely release themes from that coverage before reading details. Follow the selected delivery's `packets` references as needed, one packet at a time. Each coverage page and optional detail packet is at most 24,000 characters. Do not read every detail packet into the session. Summaries are shortened; packet budget reports identify omitted or shortened details. Read the corresponding JSON pointer in `evidence.json` or the original source when a claim needs that detail. `detail_fallback_units` names deliveries whose reference lists alone exceed the packet budget; their complete source coverage and full evidence remain available. An absent version tag means first publication in this clean repository, not a reset of the product version or proof that every existing feature is new.

Use the evidence as a coverage index, then read the sources behind the important claims. For a large release, compare the previous documented product experience with the current one, inspect the relevant specifications and representative implementation, and follow completed effort outcomes. Use the maintainer's confirmed context when the historical checkout is unavailable, and state the remaining baseline gap.

Decide what a user can do now, what changed in their daily workflow, and what installation or migration requires. A new desktop app or redesigned workbench may be the release headline even when hundreds of smaller commits dominate the summaries. Verify that judgment against the release range and current product; choose each release's headline afresh.

## Draft and revise locally

Draft an opening that explains the release's main change, then the useful feature themes, noteworthy fixes, and concrete upgrade guidance. Scale detail to the release. Group changes by user outcome rather than producing one bullet per PR or imposing a fixed theme count. Distinguish shipped behavior from plans and experiments.

Show the narrative and recommend a version with a short reason. Reuse an already approved version and emphasis from the conversation. Incorporate the maintainer's edits without regenerating the whole story. Consult sources to check revised claims.

Keep two reviewed drafts under the ignored `.release/` directory:

- `release-notes.md`: the full narrative in Markdown, preserved verbatim for the GitHub release.
- `changelog.md`: a shorter set of `- ` entries for the root and embedded changelogs. Indent continuation lines.

The active session performs the editorial work. The helper collects evidence and applies files deterministically; it does not launch another coding agent or require a model API key in CI.

## Prepare the reviewed files

Use the exact source SHA from the evidence index and the reviewed version:

```sh
python3 scripts/release/prepare_release.py prepare \
  --source "$release_source" --version "$release_version" \
  --notes-file .release/release-notes.md \
  --changelog-file .release/changelog.md
python3 scripts/release/prepare_release.py check
```

`prepare` requires a clean checkout matching freshly fetched main and creates `release-prep/<version>`. It updates exactly five release files: `CHANGELOG.md`, its embedded copy, the compiled version, `.github/release-plan.json`, and `docs/releases/<version>.md`. Preserve unrelated work in its own checkout. The helper creates no commits, tags, deployment or publication.

Review the exact diff. When release-PR preparation is authorized, commit only those five files and open a PR through the user's existing `gh` login, applying the harness's PR skill when available. Describe the headline, selected version, source, verification and any evidence gaps. Link the PR through the harness when supported. The committed `docs/releases/<version>.md` is the final narrative; CI publishes its exact content without another editorial pass.

Before editing an existing release PR, fetch its current head so edits made in GitHub are preserved. Edit the committed narrative directly on that release-prep branch. When changing the root changelog, synchronize its embedded copy before checking:

```sh
python3 scripts/release/prepare_release.py sync
python3 scripts/release/prepare_release.py check
```

If main or the prior release tag changes before approval, refresh the evidence and reconcile the reviewed prose. Incorporate fresh main into the existing release-prep branch, preserve its human edits, and update the reviewed manifest source/base before running sync/check again. Use a separate clean checkout for fresh evidence if the release branch already contains prepared files. Keep changes relative to the new source within the same five-file boundary; do not silently broaden the release or force past the source check. After the release PR merges, CI stays pinned to that immutable release commit.

Report the release PR, version, what its merge triggers and any unfinished verification. Normal local preparation needs no Apple signing credentials. Merge or product publication needs the user's explicit authorization.
