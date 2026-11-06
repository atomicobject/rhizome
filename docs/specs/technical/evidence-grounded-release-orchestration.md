---
type: TechnicalSpec
summary: "Defines bounded release evidence, reviewable release plans, and resumable apply/build/publish orchestration for Rhizome releases."
id: SPEC-0078
spec-status: active
last-updated: 2026-11-06
aliases:
  - SPEC-0078
---

# Evidence-grounded release orchestration

## Summary

Public releases are prepared locally with the active Codex or Claude session and published in CI after a reviewed release PR merges. The agent collects bounded source evidence, investigates the important product changes, writes and revises the release narrative with the maintainer, selects the reviewed version, and commits release files on a temporary release-prep branch. No model API key or model execution is required in CI.

The reviewed narrative is committed at `docs/releases/<version>.md`. The root and embedded changelogs retain concise curated entries. A small preparation manifest records the exact source, previous tag, selected version and date. Merging that release PR approves CI to build and publish the immutable merge commit with those exact notes. Ordinary merges do not release or deploy anything.

CI performs tests, common frontend preparation, Linux/Windows builds, packaging, deployment gating and uploads on Linux. One standard Apple Silicon job performs native macOS compilation and desktop signing, notarization and verification. GoReleaser's open-source build configuration remains the compiler source of truth; a focused Linux publisher assembles the separately built artifacts. Private/internal releases retain the existing local resumable orchestrator and its separate credentials.

## Goals

- write useful release narratives through a local Codex or Claude subscription
- lead with verified changes to the user's product experience, especially across large release ranges
- retain bounded, auditable source evidence and preserve the maintainer's edits
- commit notes, changelog and version before approval through a release PR
- have one public CI publisher and one recovery path for that exact approved source
- keep macOS work limited to native Mac builds, signing, packaging and verification
- preserve canonical release refs, gateway verification, updater and installation contracts

## Non-Goals

- requiring a model API key in CI or choosing a model subscription for the maintainer
- generating release notes from every changed effort or every diff hunk
- making semantic-version selection fully automatic
- changing the end-user installer/update contract in [[version-pinned-install-and-update|SPEC-0033]]
- moving private S3 releases into public CI or replacing the GoReleaser compiler configuration

## User Stories

### US1 - Review bounded release evidence

- id:: ^SPEC-0078-US1
- summary:: Review a complete, bounded evidence packet for the exact release range before any repository mutation occurs.
- status:: satisfied

#### Acceptance Criteria

- The release plan records the exact base tag, base commit, head commit, and a deterministic evidence fingerprint. ^acceptancecriterion-05669bc4
- Evidence prioritizes curated `Unreleased` entries, shipped effort outcomes, merged pull-request summaries, and direct commits, with changed paths and statistics used for verification. ^acceptancecriterion-a6a4a496
- Effort evidence includes newly delivered or materially updated `Actual Delivered` content only when the associated delivery is present in the release range; active, planned, historical-normalization-only, and unrelated effort changes are excluded from shipped claims. ^acceptancecriterion-a257b28b
- Pull-request evidence handles squash merges, merge commits, stacked or integration pull requests, direct commits, and duplicate delivery descriptions without double-counting one delivery. ^acceptancecriterion-9267cead
- GitHub unavailability produces an explicit degraded local-evidence plan rather than silently omitting coverage or blocking read-only planning. ^acceptancecriterion-9a469c1a
- Full repository diffs are never placed in the release-note prompt; targeted hunks may be collected only to resolve an identified evidence ambiguity. ^acceptancecriterion-7a796433

### US2 - Generate trustworthy release notes

- id:: ^SPEC-0078-US2
- summary:: Use the active local agent to write and revise source-grounded release notes and a version recommendation.
- status:: satisfied

#### Acceptance Criteria

- Local preparation consumes bounded evidence and produces reviewed narrative notes, concise changelog entries and an explicitly selected version. ^acceptancecriterion-5f4f2b2a
- The active Codex or Claude session writes the narrative using its existing authentication; public preparation and CI require no separate model API key or nested model invocation. ^acceptancecriterion-f0ec076b
- Editorial review chooses the release headline and useful themes from actual product changes, scales detail to the release, and preserves human edits rather than imposing a bullet for every PR or a fixed theme count.
- Evidence and the proposed notes/version are reviewed before committing release files; writing a draft does not authorize deployment or publication. ^acceptancecriterion-1250a727
- The bump recommendation remains advisory and the operator selects or supplies the release version. ^acceptancecriterion-00f9b7b1
- The evidence preserves enough attribution to explain the claims, and the final reviewed notes are committed verbatim at a version-derived path. ^acceptancecriterion-8d09e8a5

### US3 - Apply and resume a release safely

- id:: ^SPEC-0078-US3
- summary:: Prepare a reviewed public release PR and recover CI publication without losing or silently changing its source or notes.
- status:: satisfied

#### Acceptance Criteria

- The local release skill prepares a release PR; public `make cut-release` guides the same preparation instead of publishing locally. ^acceptancecriterion-126fd1f1
- Read-only evidence and candidate modes create no release refs, gateway deployments or public uploads; preparing a release PR changes only the explicitly owned release files. ^acceptancecriterion-44c36cd3
- Preparation and eligibility checks refuse stale source, a changed previous tag, non-increasing version, dirty/unrelated work, or mismatched notes/version/changelog copies. ^acceptancecriterion-44d704db
- CI recovery consumes the same approved source and committed notes, reuses matching publication state, and refuses conflicting tags, releases or artifacts without overwrite. ^acceptancecriterion-22e6576a
- Failed release phases preserve repository commits, tags, plans, and artifacts for inspection or retry and report the exact safe next command. ^acceptancecriterion-82455720
- Release orchestration and evidence modules remain independently testable and no code file exceeds the repository size guideline. ^acceptancecriterion-77631aaf

### US4 - Publish canonical release refs safely

- id:: ^SPEC-0078-US4
- summary:: Publish only a validated merged public release PR, pin every job to its exact merge SHA, and retain canonical release topology.
- status:: satisfied

#### Acceptance Criteria

- Preparation starts from a clean checkout exactly matching freshly fetched `origin/main`; no version tag means an empty baseline and the existing source version remains the version floor. ^acceptancecriterion-480087fe
- A merged same-repository release-prep PR must contain a new valid manifest and only the allowed release files; its recorded source must equal main immediately before that merge. Unmerged, forked, ordinary, spoofed and stale release PRs cannot reach signing or publication. ^acceptancecriterion-5511bb00
- Every build and publication job consumes that immutable accepted merge SHA. After builds succeed, the publisher advances `release` without force, verifies canonical integration, gates uploads on the exact gateway source and revalidates refs after the wait. Conflicting refs are never replaced. ^acceptancecriterion-bb6e5834
- Public releases have one CI publisher. Local public commands route to reviewed preparation. Recovery retries the failed publisher in its original approved run with the retained original bundle and gateway receipt; manual dispatch otherwise serves non-publishing candidates, or starts a validated merged release PR whose merge started no release run and whose version is not yet tagged. ^acceptancecriterion-55e3fc28

## Requirements

### Must

- Evidence MUST prioritize curated Unreleased entries, verified delivered effort outcomes, merged PR context and direct commits. Each delivery appears once; unrelated or historical effort churn MUST NOT become shipped claims. Evidence remains bounded independently of raw diff size.
- The local skill MUST use the active Codex or Claude session for editorial judgment, investigate the headline changes and installation/migration implications, and preserve maintainer edits across revisions. Existing optional generation tools MAY remain for private/local uses, but public preparation MUST NOT depend on a particular harness/model or an API key in CI.
- Public preparation MUST pin freshly fetched main, the previous version tag or empty first-release baseline, and a strictly increasing explicitly selected version. It MUST preserve unrelated work and commit only the root changelog, embedded changelog, compiled version, version-derived notes file and preparation manifest on the release-prep branch.
- The committed narrative notes MUST be the source used verbatim in the GitHub release. CI MUST NOT regenerate or rewrite them. Changelog copies and the selected/compiled version MUST agree before approval. The manifest MUST contain only non-secret release intent, never credentials or local machine paths.
- The release-PR merge is publication approval. Publication MUST run from main, triggered by the merge's push of the release manifest, because the protected release environment admits only main. Eligibility MUST validate a merged same-repository PR into main, its reserved branch, newly changed manifest, exact reviewed source/head/merge relationship, allowed file set and version floor. Main movement before merge MUST require refreshed preparation. Later main movement MUST NOT substitute a new build source.
- Ordinary PR merges, malformed or stale release input, and candidate-only verification MUST NOT create release refs, dispatch a gateway deployment or upload public assets. Validation MUST run with read-only permissions before dependent jobs receive signing or publishing credentials.
- Linux MUST own common frontend builds, Linux/Windows CLI builds, final packaging/checksums, gateway verification, release uploads and the Homebrew update. A single standard Apple Silicon job MUST own native Darwin CLI builds and desktop compilation, signing/notarization and package verification. Already prepared frontend assets MUST be transferred to Mac rather than rebuilt there. The Mac job MUST receive no model, GitHub publishing or Homebrew token.
- Artifacts MUST be bound to their source/version and checked for the expected OS/architecture and required asset set before publication. Missing, extra or tampered inputs MUST fail before ref changes, deployment or uploads. Existing archive names, licenses, installer URLs, updater signatures/feed and Homebrew installation behavior MUST be preserved.
- Homebrew publication MUST update both the existing CLI formula and the Desktop cask from verified release hashes. Completed-release recovery MUST compare both recipes, safely create a missing cask, and gate any outstanding write without overwriting unrelated tap files.
- After all builds/verification succeed, the publisher MUST advance the canonical release ref without force and verify its integration into main. Tag creation MUST target the accepted release commit exactly; conflicting tags or refs MUST fail closed. Public publication MUST have one CI owner and a shared retry path, never competing local and CI publishers.
- Every supported GitHub or S3 publisher MUST verify a successful, exactly correlated gateway deployment before first artifact upload, and revalidate selected source and canonical topology after waiting. The existing deployment helper remains the sole AWS implementation. Ordinary main pushes, planning and candidate verification MUST NOT deploy.
- Public gateway source MUST equal the exact client release commit. Private/internal publication MUST retain automatic public-source selection, committed Docker-input agreement, trusted-main deployment controls, production/OIDC restrictions and request/run correlation. Existing resumable receipts MUST NOT let a stale successful deployment bypass fresh verification.
- Failed/cancelled/ambiguous builds, deployments or uploads MUST retain useful recovery state without deleting user work, overwriting conflicting releases or silently changing notes/source. Recovery MUST distinguish an exact completed publication from partial or conflicting state. A draft/partial release MUST not be treated as a fresh unrelated publication.
- Verified bundles and gateway receipts MUST be retained for 30 days. Publication recovery MUST reuse the original bundle instead of rebuilding signed bytes. Missing gateway state after a started publisher MUST stop for reconciliation; a retry MUST NOT silently dispatch a new deployment after an uncertain interrupted attempt.
- Signing credentials MUST remain outside Git/logs/artifacts, outside frontend/compiler steps and outside Linux publication processes. Only explicit signing processes receive them. The existing protected release Environment, temporary Keychain cleanup and public content checks remain required.
- Private/internal releases MAY continue using the existing local orchestrator. Its canonical main/release freshness, immutable plan, source-agreement, publishing-token scope, atomic canonical-ref updates, hotfix topology, checkpoints and S3 recovery rules MUST remain intact. Public local publication MUST instead direct the maintainer to the release skill/CI flow.

### Should

- The skill SHOULD show a proposed narrative and version for review before committing, then open the release PR through the maintainer's existing GitHub login.
- The helper SHOULD provide read-only evidence and explicit prepare/sync/check commands with clear recovery guidance.
- Read-only evidence SHOULD disclose degraded GitHub coverage; publication eligibility MUST remain fully verifiable.

## Edge Cases

- first publication has no version tags but the existing product version is already nonzero
- a large range's headline product change is obscured by many small commits or effort notes
- the maintainer edits narrative notes independently of concise changelog entries
- main or the previous release tag changes during preparation or before approval
- merge, squash or rebase integration must resolve one exact approved release snapshot
- an ordinary/forked/source-changing PR tries to resemble release preparation
- main advances after the release PR merges while CI builds the pinned source
- a native build, notarization or artifact check fails before publication
- canonical ref publication, gateway verification, upload or Homebrew update fails partway
- a retry encounters an exact completed release, an owned partial draft or a conflicting release/tag
- private/internal release behavior remains local and requires its existing isolated credentials

## Documentation Plan

- Keep the local release skill and maintainer guide aligned on draft review, committed notes, preparation checks, CI triggers, recovery and degraded evidence.
- Keep Makefile help, release-script context, CI permissions and the minimal-Mac build split aligned with the actual commands.
- Document evidence precedence, effort filtering, deduplication, plan fingerprinting, and model override behavior beside the release modules.
- Document exact source approval, stale preparation, canonical refs, gateway gating, retry/partial release behavior, the single public publisher and preserved private/internal flow.

## Open Questions

None for the initial implementation slice.
