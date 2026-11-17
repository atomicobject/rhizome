---
type: EffortNote
id: EFF-2026-11-17-10-45
aliases: [EFF-2026-11-17-10-45]
name: Filesystem-aware path casing
created-at: 2026-11-17T15:43:06Z
status: active
summary: Repair casing differences across discovery, schema selection, and file references without collapsing distinct files.
governing-specs:
  - "[[filesystem-aware-path-casing]]"
---

# Filesystem-aware path casing

## Scope

Implement the approved filesystem comparison policy in the fresh `fix/filesystem-aware-path-casing` worktree. Preserve the repaired external AO KB vault and the unrelated primary-checkout web artifact.

## Spec Set (Frozen)

- [[filesystem-aware-path-casing|SPEC-0141]], revision 2026-11-17, requirements R1 through R7.

## Stories In Scope (Frozen)

- [[filesystem-aware-path-casing#^SPEC-0141-US1|SPEC-0141.US1]]
- [[filesystem-aware-path-casing#Requirements]]

## Spec Coverage Checklist

- [x] R1: observed filesystem behavior selects the comparison policy.
- [x] R2: includes, selectors, and specificity agree.
- [x] R3: explicit references return actual indexed spelling.
- [x] R4: sensitive filesystems retain distinct files and ambiguity.
- [x] R5: type names remain case-sensitive.
- [x] R6: built-CLI reproduction and focused behavior tests pass.
- [x] R7: existing indexes rebuild affected materialization automatically.

## Plan

Drew explicitly approved the direction in the task brief on 2026-11-17 and invoked full-send for implementation, proof, independent review, and merge. No current-user identity is configured in this worktree, so `plan-approved-by` is absent; this paragraph records the actual chat authorization without inventing configured identity.

1. Capture the failure with the built CLI in an isolated copy of the shipped AO KB fixture. Compare small filesystem-policy designs against existing ownership boundaries.
2. Implement a shared filesystem policy in `pkg/paths`, integrate discovery, schema matching and precedence, and canonical reference lookup. Add focused regression coverage and the required materialization invalidation. Update owning subsystem notes and the changelog in the same change.
3. Run focused tests and the same CLI fixture. Run `make check-full`, fresh integration coverage, Markdown validation, independent sol-loop review, and local Greptile until the current head is 5/5 with no unresolved findings.
4. Close this effort on the branch, open the PR, verify current-head CI and mergeability, merge through normal protection, and follow the development installation policy after landing.

## Original Intended Delivery

AO KB setup works when schema path capitalization differs from the same on-disk file on an insensitive filesystem. Stored paths and output keep their real spelling, and sensitive filesystems retain distinct files.

## Actual Delivered

Implemented root-scoped filesystem case detection for discovery and schema paths, consistent selector precedence, and shared native-spelling resolution for explicit node and GraphQL references. Stored identities retain their original spelling. Materialization version 29 and the schema witness refresh earlier classifications.

The filesystem probe also handles relative roots and distinguishes separate hardlink directory entries. AO KB verification tracks successful requested refs while allowing returned paths to use canonical spelling.

## Execution Notes

- Sol review round 1 found S1, web readiness checking authored casing before canonicalization, and S2, per-host membership reads in batched resolution. Focused regressions reproduced HTTP 503 for a valid case alias and metadata reads increasing from 1 to 25. Both pass after native spelling is applied before readiness and explicit hosts are checked in batches. Committed and overlay batching are covered.
- The initial `make check-full` was stopped to apply those review findings. No successful full-gate result is claimed for that run.

- Built the baseline CLI at `969e2c2b61d7f093f26b9f2ea6d3e1e2ca90331a`. The isolated `/tmp/rzm-casing-proof` fixture, with `MOCs` physically renamed to `mocs`, reproduced `ao_index_projection_incomplete: MOCs/riders.md: node ref did not resolve`. The transcript is `/tmp/casing-before.log`; the baseline executable is `/tmp/rzm-casing-baseline`.
- Added the built-CLI regression `TestAOKBIndexAcceptsFilesystemCaseAliases`; its fresh pre-fix run failed with the same projection error.
- Mounted a disposable case-sensitive APFS volume and confirmed `Case.md` and `case.md` have different contents.
- Two design sketches and an independent judge selected a root-scoped comparison value with verified native spelling for concrete paths. Unknown detection stays exact; filesystem comparison does not reuse Unicode-normalizing logical link keys.
- AO KB verification matched returned canonical paths against authored request spelling. It now verifies successful batch items by `requestedRef`; the node still returns its canonical path. A focused Node test covers this contract.
- The fresh worktree initially lacked a Rhizome index. Built and indexed it, then loaded live EffortNote and ProductSpec authoring guides and allocated both identifiers through the CLI.

### Final local verification

- Final code revision: `ba8ae708c2439464065363a7fa3c965dcefc1f75`, following the clean rebase onto `83f0e0e0`.
- `make check` passed. The code portion of `make verify INTEGRATION_PACKAGES='./tests/integration/... -count=1' MIXED_INTEGRATION_PACKAGES='./pkg/vault/cache ./pkg/anchors ./pkg/validate -count=1'` passed, including race-enabled unit tests and fresh integration tests. Browser launch initially failed because the pinned Playwright executables were missing; after `npx playwright install chromium webkit`, `make web-e2e` passed all 339 tests: 166 workspace/WebKit, 92 AO KB, and 81 Agentic Engineering.
- The final fresh-fixture comparison used the old built CLI and the final built CLI on the same source fixture. The old CLI returned `ao_index_projection_incomplete`; the new CLI returned `indexReady: true`, exposed `_moc.md`, `mocs/riders.md`, and `mocs/routes.md` as MapOfContent nodes, and preserved the original MOC bytes. A post-binding `init` reconciliation followed by `init --check` exited 0. Evidence: `/tmp/casing-final-proof.json` and the fixture logs it identifies.
- Verbose tests on the mounted case-sensitive APFS volume reported exact matching and passed native-spelling, distinct-file/hardlink, schema, node/query, discovery, and structural-parity checks. Evidence: `/tmp/casing-sensitive-final-verbose.log`. The CLI case-alias regression runs on filesystems that actually support aliases; Linux still exercises the exact policy and distinct-file coverage.
- Sol approved after four rounds. S1 fixed web readiness, S2 fixed Scope batching, S3 fixed a test assertion, and the fourth round approved G1's GraphQL batch prefetch fix. G1's regression showed 2 versus 26 metadata reads before the fix and constant reads afterward. Greptile's second review scored the complete committed patch 5/5 with no comments. Model usage was not exposed by the delegate reports.
- The structural golden test retains every structural expectation but compares schema witness fields with the current schema hash, since the witness now incorporates the filesystem policy. Its focused test passed on both APFS modes.

## Deviations

- Comparison policy is captured per vault root. Mixed-sensitivity mounts or directory settings inside a single vault are not modeled for globs; concrete references still verify each path component. Unicode glob matching uses simple lowercase mapping without normalization or full case folding. Unknown root behavior stays exact.
- Rootless legacy `CaseKey` users in watchers and transactions retain their existing behavior. This change moves vault discovery, schema matching, and source ownership to the root-scoped policy and canonicalizes explicit read references.
- The legacy schemaVersion 5 proof fixture reports the product model as not applicable, as expected. Its starter, knowledge, bindings, and index readiness pass.
- The first sensitive-volume run filled the small test image during Go linking. The corrected run compiled on the main disk and executed tests with their fixture TMPDIR on the sensitive volume.

## Closure Checklist

- [x] Approved scope delivered or deviations recorded.
- [x] Required local verification and independent review passed.
- [ ] Current-head Greptile is 5/5 and CI is green.
- [ ] Delivery PR and merge evidence recorded.

## Compounding Follow-ups

None yet.

## Status

Implementation verified on `fix/filesystem-aware-path-casing`. PR CI and normal merge remain release gates; the effort stays active until their evidence is recorded.
