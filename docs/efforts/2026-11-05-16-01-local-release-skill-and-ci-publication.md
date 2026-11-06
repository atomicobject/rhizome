---
type: EffortNote
id: EFF-2026-11-05-16-01
name: Local release skill and CI publication
created-at: 2026-11-05T21:01:42Z
status: complete
summary: Write reviewed release notes with local Codex or Claude, then build and publish approved release PRs in CI with minimal Mac work.
aliases:
  - EFF-2026-11-05-16-01
governing-specs:
  - "[[evidence-grounded-release-orchestration]]"
  - "[[desktop-distribution-and-updates]]"
---

# Local release skill and CI publication

## Scope

Implement the release flow the user approved in this conversation: a local Codex/Claude skill writes and revises the release narrative using the active subscription, updates and commits release files, and opens a release PR. Merging that reviewed PR starts deterministic CI builds, notarization, gateway verification and publication. Keep native macOS work on a single standard Apple Silicon runner and perform other work on Linux. Preserve private/internal local release support. Do not publish a real product release or deploy the gateway merely for acceptance testing.

## Spec Set (Frozen)

- [[evidence-grounded-release-orchestration|SPEC-0078]], as amended on 2026-11-05 for local editorial preparation and CI-owned public publication.
- [[desktop-distribution-and-updates|SPEC-0122]], public asset, signing, updater and orchestration requirements amended on 2026-11-05.

## Stories In Scope (Frozen)

- [[evidence-grounded-release-orchestration#^SPEC-0078-US1|SPEC-0078.US1]], bounded evidence reused by the local skill.
- [[evidence-grounded-release-orchestration#^SPEC-0078-US2|SPEC-0078.US2]], subscription-based editorial preparation and reviewed committed notes.
- [[evidence-grounded-release-orchestration#^SPEC-0078-US3|SPEC-0078.US3]], preparation, validation and recovery.
- [[evidence-grounded-release-orchestration#^SPEC-0078-US4|SPEC-0078.US4]], single CI publisher and canonical refs.
- [[desktop-distribution-and-updates#^SPEC-0122-US1|SPEC-0122.US1]], same-version desktop/CLI artifacts and notarization in split build jobs.

## Spec Coverage Checklist

- [x] Local Codex/Claude release skill produces source-grounded narrative notes and a reviewed release PR without a CI model API key.
- [x] Deterministic preparation preserves edited notes, version floor, clean/source checks and release-file ownership.
- [x] Ordinary, forked, unmerged, stale or malformed release PRs never reach signing, deployment or publication.
- [x] Linux handles common assets, Linux/Windows builds, packaging and publication; Mac handles only native macOS builds and signing/verification.
- [x] Approved merge SHA, source-bound assets, canonical release refs, exact gateway gate and recoverable publication are preserved.
- [x] Private/internal local release support and public installation/updater/Homebrew contracts remain intact.

## Plan

The user approved the release-PR flow and instructed continuing after notarization, then clarified that writing belongs in a local Codex or Claude skill using the existing subscription. These chat instructions authorize the scope and the routine design decisions below; they do not authorize a real product release. Current-user identity is not configured, so no approver is inferred into metadata.

### Batch 1: Local reviewed preparation

Use the existing evidence collector and deterministic version/changelog renderers. Add a repository-local rhizome-release skill for both harnesses. The active agent investigates the actual product changes, drafts and revises a narrative with the maintainer, and prepares only approved release files. Narrative notes are committed at docs/releases/<version>.md; CHANGELOG.md retains concise curated entries and is mirrored into the embedded copy. A tracked .github/release-plan.json records source SHA, previous tag, selected version and release date. Preparation never builds, signs, creates release refs, deploys or publishes.

Expose evidence, prepare, sync and check helpers. Refuse stale or dirty source and non-increasing versions. Use the user's gh login for the release PR. Preserve reviewed prose when refreshing source. Exit: temporary-Git tests, exact edit preservation, first-release/no-tag coverage, skill validation and an independent forward-test with no external mutations.

### Batch 2: Single public CI publisher

Validate a merged same-repository release-prep PR on Linux, including the new reviewed manifest, allowed file set, exact source/PR/merge relationships, matching versions and changelog copies. Ordinary merges cannot publish. Pin the immutable approved merge SHA; refuse main movement before the release merge, and allow subsequent main work without substituting its source.

Build common frontend assets on Linux. Reuse GoReleaser OSS build IDs and flags for Linux/Windows binaries on Linux and Darwin binaries on Mac. Transfer prepared assets rather than rebuilding frontend output on Mac. Keep native Tauri compilation, packaging, signing, notarization, ticket/Gatekeeper/updater verification and temporary Keychain cleanup on Mac. Keep signing credentials out of compilation and publication/model credentials off Mac.

GoReleaser OSS cannot combine multiple runners' builds. Reuse its build configuration and implement focused Linux artifact assembly/publication rather than buying Pro or duplicating all build rules. Validate source-bound artifact manifests, produce existing archives/licenses/installer/desktop assets and checksums, advance release refs without force only after builds succeed, reuse the exact gateway deployment gate, revalidate refs afterward, upload complete verified assets and reviewed notes, then update the existing Homebrew tap. Keep one public publisher and one shared recovery path. Preserve internal/private local behavior.

Exit: event/merge/stale-source and artifact-tamper tests; failure ordering and credential scoping; preserved names/URLs/licenses; non-publishing split-job candidate; relevant repository checks, full diff review and Greptile 5/5. Candidate mode creates no release refs, gateway deployment or public uploads.

## Original Intended Delivery

Prepare a release through a local coding-agent subscription, review its notes/version in Git, and let CI release that exact approved commit with minimal macOS runner work.

## Actual Delivered

Delivered in [PR #94](https://github.com/atomicobject/rhizome/pull/94), with implementation and acceptance at 26b9fa787c936285158f9a7f98a39491018f59ae. The repository-local Codex/Claude release skill uses the active subscription to investigate changes, draft and revise the narrative, and prepare a reviewed five-file release PR. It keeps complete source coverage while exposing 24,000-character coverage/detail pages. Committed narrative notes are published verbatim, with no model API key in CI.

The CI pipeline validates approved release intent before credentials, builds shared frontends and Linux/Windows binaries on Linux, uses one standard Mac job for native Darwin/Tauri compilation and signing/notarization, and assembles/publishes on Linux. Source/version manifests, complete checksums, updater signatures, public-content checks and existing installation asset names are preserved. Public local publishers route to preparation; private internal release support remains.

Publication and recovery tests cover exact canonical refs, the shared gateway gate, partial drafts, completed-release idempotence, retained original bundles/receipts, conflicting state and credential scope. Live gateway deployment and public publication were deliberately excluded from acceptance; the first authorized release remains the live publication check.

All 395 release-helper tests passed in Linux CI, all 22 PR checks passed, and current implementation Greptile review scored 5/5 with zero comments. The non-publishing [split-job candidate](https://github.com/atomicobject/rhizome/actions/runs/42116493809) passed every build and verification job; the publisher was skipped. Downloaded native and final artifacts independently passed manifest/bundle verification. Temporary signing access was removed and the protected release Environment again permits only main.

## Execution Notes

- 2026-11-05 [closure] Candidate 42116493809 passed at 26b9fa787c936285158f9a7f98a39491018f59ae. Linux prepared common assets, built Linux/Windows binaries and assembled the complete bundle; the single Mac job compiled native assets, signed/notarized/verified both distribution copies and cleaned up its temporary Keychain. Publication was skipped. The candidate uses source version v0.50.5 only for verification and creates no release refs or public release.
- 2026-11-05 [verification] Downloaded the native worker output and complete 13-file verified bundle and ran the same strict source/version/hash/asset/feed/archive verifier locally. Both passed. Final release-manifest SHA-256: 9687a9d06230ff896d31ef8164994d5b31753c33c4fffc1f5aa419700ef7e049.
- 2026-11-05 [verification] All 22 PR checks passed at the implementation commit. Greptile current-head review scored 5/5 with no comments. Default/frozen-scope validation, repository content tests, actionlint, credential checks, skill validation, focused preparation/publisher tests and actual prepared-version CLI compilation passed. The final closure commit changes only this effort record; it does not invalidate the compiled candidate evidence.
- 2026-11-05 [security] Removed temporary Environment branch policy 65673367 after the Mac signing job completed; the verified policy list contains main only. The user's explicit access approval was scoped to non-publishing verification and is fully cleaned up.
- 2026-11-05 [closure] Closure drift pack resolved this EffortNote and both frozen specs without query errors. Reconciled the approved chat scope, implementation, independent reviews, actual candidate and local artifact verification. No implementation work remains; PR merge and an actual product release retain their separate authorization boundaries.
- 2026-11-05 [delivery] PR #93 merged as db91322a11310fd641002b977f4c08c08af18fe2 after all 21 applicable checks passed. Installed Rhizome Dev from that main commit, preserving unrelated generated web output in the primary checkout.
- 2026-11-05 [verification] Local full release-helper suite passed 386 tests. Focused selection/frontend/artifact/publisher tests passed; repository content tests, workflow actionlint, default validation and frozen-scope validation passed. Validation required running outside the sandbox for the existing macOS runtime identity check.
- 2026-11-05 [review] Independent review found that rebuilt notarized bytes cannot safely resume a partial upload. Recovery now retries the original failed publisher with its retained verified bundle, retained for 30 days, and restores its gateway receipt. A missing receipt after a started publisher blocks deployment pending reconciliation.
- 2026-11-05 [verification] Independent skill forward-test collected real clean-main evidence covering 81 delivery units and 358 curated entries, then drafted a desktop-led narrative and advisory v0.51.0 recommendation without a model API key or nested agent. The oversized evidence packet found by that test is being replaced with compact coverage and selective bounded details. These drafts are verification artifacts, not an approved product release.
- 2026-11-05 [verification] Final editorial projection of that actual history completed in 0.28 seconds with seven coverage pages and 81 optional detail packets, each at most 24,000 characters. All delivery summaries, curated summaries and source identities remain represented; full evidence remains available. Both harness skill validators and 18 preparation/intent tests passed. The recovery fixes passed 29 focused tests and independent re-review.
- 2026-11-05 [delivery] Implementation PR: https://github.com/atomicobject/rhizome/pull/94. The effort remains active until current-head review, ordinary CI and non-publishing split-job acceptance complete. Protected candidate access is requested only for this branch and will be removed after verification.
- 2026-11-05 [review] Greptile found a pre-existing abbreviated version renderer exposed by the new exact-content check: it removed Channel and Development, so a prepared release could not compile. The renderer now preserves the real package, with parity and compiled development/release-channel regression tests. An actual CLI build using an overlay of the rendered package succeeded and reported the synthetic prepared version.
- 2026-11-05 [verification] Candidate 42115208951 and ordinary credential CI both stopped on Linux before any Mac job because Tauri signing-test dependencies were not installed. Both workflows now install the locked desktop dependencies and fetch the pinned offline verifier dependencies before the full helper suite. No failed job was simply rerun.
- 2026-11-05 [verification] Reproduced the verifier dependency path with an empty temporary Cargo cache: locked fetch followed by offline locked build passed. Greptile's second review identified an unbounded shared gateway runner; the publisher now preserves the 120-second request limit, with a 600-second asset-upload limit and a regression check that timeout diagnostics do not expose subprocess output.
- 2026-11-05T21:01:42Z [decision] Started independent next-step preparation while the final Windows job for PR #93 is still running. Both repaired tests passed CI. PR #93's approved head remains unchanged; merge still requires all checks green.
- 2026-11-05T21:01:42Z [decision] The local skill uses the active session for editorial work, not a nested Codex call or a model API key in CI. Rich narrative notes have their own committed file; the existing changelog renderer remains suitable for concise summaries.
- 2026-11-05T21:01:42Z [learning] GoReleaser OSS supports per-ID builds but its multi-runner split/merge publication requires Pro. A focused Linux assembler preserves the current compiler configuration while moving Linux/Windows builds and uploads off Mac.

## Deviations

Independent preparation started during PR #93's final CI wait. Public recovery was narrowed to retrying the original failed publisher using retained artifacts, because rebuilding notarized packages can produce different bytes. Original bundles and gateway receipts are retained for 30 days; lost receipts after a started publisher stop for reconciliation. This preserves the approved source and avoids overwriting partial publication.

The first split-job candidate failed deterministically on missing Linux signing-test dependencies before any Mac job. The dependency ordering was fixed, then the new candidate passed. No flaky test was hidden by a rerun and no real publication or gateway deployment was used as acceptance.

## Compounding Follow-ups

No delivery-blocking follow-ups. The maintainer guide and release tooling context record the recovery, credential and minimal Mac boundaries; no separate context shard is proposed.

## Closure Checklist

- [x] Approved local skill and deterministic release preparation verified.
- [x] CI eligibility, split builds, signing and publication/recovery verified without a real release.
- [x] Documentation and governing contracts reconciled.
- [x] Applicable checks and independent review complete; delivery PR recorded.

## Status

Complete on the PR branch. Implementation, independent review, ordinary CI, non-publishing split-job acceptance, artifact verification and temporary-access cleanup are complete. PR #94 carries delivery and closure; merging it is separate from authorizing a product release.
