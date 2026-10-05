---
type: TechnicalSpec
summary: "Defines bounded release evidence, reviewable release plans, and resumable apply/build/publish orchestration for Rhizome releases."
id: SPEC-0078
spec-status: active
last-updated: 2026-09-22
aliases:
  - SPEC-0078
---

# Evidence-grounded release orchestration

## Summary

Rhizome releases must remain easy to initiate through `make cut-release` while producing trustworthy notes and recoverable delivery state for release ranges that are too large for full-diff prompting. Release preparation uses bounded, structured evidence from the curated unreleased changelog, completed effort outcomes and relevant deviations, merged pull-request descriptions, direct commits, changed paths, and diff statistics. Full repository diffs are verification inputs only and never become the primary release-note prompt.

The workflow separates read-only planning from repository mutation and artifact publishing. A fingerprinted release plan records the exact base tag and head commit, the evidence used, the generated draft, and the selected version so operators can review, dry-run, apply, and resume without regenerating or discarding repository state.

## Goals

- preserve `make cut-release` as the familiar interactive entry point
- produce release notes from bounded, auditable evidence for large release ranges
- treat effort `Actual Delivered` sections as delivery truth while excluding historical note churn
- make note generation, release mutation, artifact building, and publishing independently testable
- make failures resumable without deleting release commits, tags, or operator work
- publish canonical Git branches and tags from a freshly synchronized `main` or `release`
- keep model selection compatible with the authenticated Codex CLI

## Non-Goals

- replacing GoReleaser
- generating release notes from every changed effort note or every diff hunk
- making semantic-version selection fully automatic
- requiring GitHub access when local Git and curated changelog evidence are sufficient for a degraded plan
- changing the end-user installer/update contract defined by [[version-pinned-install-and-update|SPEC-0033]]

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
- summary:: Generate release notes and a version recommendation from a bounded, inspectable evidence packet.
- status:: satisfied

#### Acceptance Criteria

- Release-note generation consumes the structured evidence packet and returns schema-validated bump, rationale, release-note, and changelog fields. ^acceptancecriterion-5f4f2b2a
- The default release-note model is `gpt-5.6-luna` with medium reasoning, with an explicit environment override supported without changing source. ^acceptancecriterion-f0ec076b
- One global editorial pass produces at most seven themes, clusters related delivery evidence around user outcomes, and avoids duplicating curated entries already covered by source attribution.
- Model or authentication failure occurs before changelog, version, commit, tag, build, or publish mutation. ^acceptancecriterion-1250a727
- The bump recommendation remains advisory and the operator selects or supplies the release version. ^acceptancecriterion-00f9b7b1
- The generated plan preserves enough source attribution to explain which changelog entry, effort, pull request, or commit supports each draft theme. ^acceptancecriterion-8d09e8a5

### US3 - Apply and resume a release safely

- id:: ^SPEC-0078-US3
- summary:: Review, apply, build, publish, and resume a release without losing or silently changing repository state.
- status:: satisfied

#### Acceptance Criteria

- `make cut-release` orchestrates plan, preview, version selection, apply, build, and publish while retaining explicit phase boundaries. ^acceptancecriterion-126fd1f1
- Read-only planning and dry-run modes create no changelog, version, commit, tag, GitHub release mutation. ^acceptancecriterion-44c36cd3
- Apply refuses to continue when the current head, base, evidence fingerprint, or required clean-worktree precondition differs from the reviewed plan. ^acceptancecriterion-44d704db
- A persisted plan can resume build or publish after failure without regenerating notes or duplicating changelog entries. ^acceptancecriterion-22e6576a
- Failed release phases preserve repository commits, tags, plans, and artifacts for inspection or retry and report the exact safe next command. ^acceptancecriterion-82455720
- Release orchestration and evidence modules remain independently testable and no code file exceeds the repository size guideline. ^acceptancecriterion-77631aaf

### US4 - Publish canonical release refs safely

- id:: ^SPEC-0078-US4
- summary:: Cut a release from the latest canonical `main` or `release` branch, then publish the release commit through both canonical lines and the selected version tag.
- status:: satisfied

#### Acceptance Criteria

- `make cut-release` accepts exactly `main` or `release`, fetches the matching `origin/<branch>`, and stops before planning, building, repository mutation, or publication unless `HEAD` equals that freshly fetched remote branch. ^acceptancecriterion-480087fe
- Before GoReleaser publication, the canonical branches publish atomically: a release cut from `main` advances `origin/main` and `origin/release` to the release commit; a release cut from `release` advances `origin/release` to the release commit and `origin/main` to a merge commit whose first parent is the latest fetched `origin/main` and whose second parent is the release commit. GoReleaser then creates the selected GitHub tag at the reachable release commit, and the release completes only after that tag and GitHub publication are verified. ^acceptancecriterion-5511bb00
- Git-ref publication is checkpointed and retry-safe: resume accepts matching local and remote branch topology, completes missing branch publication, revalidates checkpointed refs, and refuses conflicts or an uncheckpointed remote tag as ambiguous instead of overwriting or replaying publication. ^acceptancecriterion-bb6e5834
- The local orchestrator remains the sole normal GoReleaser publisher; a manually dispatched fallback must validate the selected tag and canonical topology, pin the validated commit across jobs, and skip duplicate GoReleaser publication when the release already exists. ^acceptancecriterion-55e3fc28

## Requirements

### Must

- Release planning MUST be a deterministic, read-only phase before release mutation.
- Evidence MUST retain source identifiers and MUST be bounded independently of raw diff size.
- Curated `Unreleased` entries MUST outrank generated narrative evidence.
- Completed effort `Actual Delivered` content MUST be eligible as authoritative delivery evidence only after its associated code or delivery commit is verified in the range.
- Effort `Deviations` MUST be included only when they qualify shipped behavior, compatibility, evidence quality, or known limitations.
- Pull requests and efforts describing the same delivery MUST be deduplicated into one release evidence unit.
- Direct commits not represented by an included pull request MUST remain visible to the release planner.
- Model invocation MUST use structured output validation and MUST NOT rely on delimiter-only parsing.
- Release plan application MUST verify its base/head/fingerprint preconditions immediately before mutation.
- A release mutation MUST start on exact branch `main` or `release` at the commit freshly fetched from its matching `origin` branch; detached, other-branch, ahead, behind, and diverged worktrees MUST be rejected before mutation.
- Release publication MUST require `RZM_RELEASE_GITHUB_TOKEN` before mutation and expose it as `GITHUB_TOKEN` only to GoReleaser, leaving `gh` evidence authentication independent.
- Git publication MUST atomically advance the applicable canonical branches before GoReleaser so GitHub can create the selected tag at a reachable commit, then verify the GitHub-created remote tag targets that release commit before continuing.
- A release cut from `main` MUST advance `origin/main` and `origin/release` to the release commit; a release cut from `release` MUST merge the release commit into the latest fetched `origin/main` and advance `origin/main` to that merge without including unreleased `main` work in the tagged release.
- A release-line merge into `main` MUST use the fetched `origin/main` tip as first parent and the release commit as second parent so future first-parent release evidence remains on the main line.
- GoReleaser MUST set GitHub `target_commitish` to the release commit and remain the only normal creator of the remote release tag.
- Retry MUST verify existing release refs before treating Git publication as complete and MUST NOT force-update conflicting remote refs or tags.
- Normal releases MUST have exactly one GoReleaser publication owner.
- Build and publish phases MUST consume the reviewed plan rather than rediscovering release scope.

### Should

- GitHub evidence collection SHOULD use the authenticated `gh` command surface and record a degraded-mode diagnostic when unavailable.
- Evidence collection SHOULD extract concise PR summary, root-cause, and user-impact sections instead of entire templated bodies where possible.
- Release-plan artifacts SHOULD be human-readable JSON stored in an ignored, stable location that survives a failed build.
- The CLI SHOULD present one safe resume command for each failed phase.

### May

- The planner MAY collect bounded targeted hunks for items whose narrative evidence conflicts or lacks user-impact detail.
- Operators MAY supply extra release-note guidance without changing the collected evidence or plan fingerprint inputs.

## Edge Cases

- a release range contains only direct commits
- a parent integration PR contains several child PRs whose merge commits are not ancestors of `main`
- an old completed effort is touched only by schema, formatting, or backport normalization
- an effort is complete but its delivery PR is not in the release range
- the existing `Unreleased` changelog already covers a PR or effort
- GitHub authentication or connectivity fails while local Git evidence remains available
- the worktree or HEAD changes after the plan preview
- note generation succeeds but build or GitHub publishing fails
- the release starts from a detached, non-canonical, ahead, behind, or diverged worktree
- the local or remote `release` branch or selected tag points at a conflicting commit during resume
- a release cut from `release` conflicts while merging the hotfix release commit into the latest `origin/main`
- `origin/main` advances while a release cut from `release` is being planned, built, or published
- canonical branches advance but GitHub tag/release creation fails
- a manually dispatched fallback release targets a missing, mismatched, or partially published GitHub release

## Documentation Plan

- Add a maintainer release guide describing plan, dry-run, cut, resume, and degraded-evidence behavior.
- Keep `Makefile` help and release-script module documentation aligned with the phase commands.
- Document evidence precedence, effort filtering, deduplication, plan fingerprinting, and model override behavior beside the release modules.
- Document canonical-branch freshness, staged Git-ref publication around GitHub-created tags, retry behavior, hotfix merge topology, and single-publisher ownership in the maintainer guide and release module context.

## Open Questions

None for the initial implementation slice.
