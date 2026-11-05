---
type: EffortNote
id: EFF-2026-11-05-10-45
name: Desktop signing and notarization
created-at: 2026-11-05T15:51:00Z
status: active
summary: Sign and notarize public desktop builds locally and in protected CI recovery.
aliases:
  - EFF-2026-11-05-10-45
governing-specs:
  - "[[desktop-distribution-and-updates]]"
---

# Desktop signing and notarization

## Scope

Implement the two-batch notarization plan approved by the user in chat on 2026-11-05. Include shared signing, verification, protected CI secrets and non-publishing acceptance. Publishing a release, merging, versioning and replacing Rhizome Dev are excluded.

## Spec Set (Frozen)

- [[desktop-distribution-and-updates|SPEC-0122]], release artifacts and orchestration requirements as amended on 2026-11-05 for the approved notarization plan.

## Stories In Scope (Frozen)

- [[desktop-distribution-and-updates#^SPEC-0122-US1|SPEC-0122.US1]], release signing, verification and CI recovery only.

## Spec Coverage Checklist

- [ ] Shared Developer ID signing, notarization, stapling and distribution verification.
- [ ] Protected CI signing and non-publishing candidate verification.
- [ ] Credential custody documentation and release contract updates.

## Plan

Approved in chat by the user's explicit `yes` response to the two-batch plan. Current-user identity is not configured in this vault, so no `plan-approved-by` identity is inferred.

### Batch 1: Shared notarized desktop build

Restore the configured worktree Rhizome binary, retrieve the owning release contracts, and create a new effort and focused specification amendment without reopening completed effort history. Record approval for the exact scope below once received.

Update desktop release configuration and scripts/release credential handling to select the Developer ID identity, pass only required Apple credentials to signing/notarization processes, and preserve isolation from provider and publishing credentials. Use Tauri's existing signing/notarization support; do not introduce a second bundler or updater system.

Local signing uses the installed Keychain identity. Notarization uses the release 1Password Environment, with the private API key materialized only as a protected temporary file for the signing process. Resolve non-secret 1Password source references from local configuration; never hard-code this machine's Downloads path. Keep the existing updater key and verification.

Public desktop publication fails if Apple credentials are missing, notarization is rejected, the signing team is wrong, or stapled-ticket/Gatekeeper validation fails. Ordinary development builds remain usable without release credentials. Verify the app from the DMG and the app contained in the updater archive, including the bundled rzm executable. Package and sign updater artifacts only after final app notarization/stapling; avoid modifying signed release artifacts afterward.

Exit evidence: focused Python tests for credential scoping, required credentials, failure handling, and staging order; make desktop-check and make desktop-build; a non-publishing signed candidate accepted by Apple; codesign, stapler and Gatekeeper checks; launch the packaged app with a disposable fixture; updater archive signature verification. Record exact commands and limits. Apple submission of this test candidate is in scope; publishing a release is not.

## Batch 2: Desktop publication through CI

Extend the existing manually dispatched recovery workflow to publish the same desktop assets alongside CLI assets for validated public release tags. Retain its existing-release no-op, canonical topology checks, exact release source, gateway gate, and single-publisher recovery semantics. Do not add a tag-triggered publisher that races the local orchestrator or move normal publication ownership in this change.

Use an explicit hosted Apple Silicon macOS runner and the existing project toolchain. Configure a release Environment with appropriate branch restrictions and approval settings supported by this repository. Supply the encrypted p12, export password, notarization API key, identifiers, and existing updater signing key through protected GitHub secrets. Keep 1Password as the recovery source. Secret transfer must use a value-suppressing path, never chat, command literals, logs, caches, or build artifacts.

Import the p12 and Apple intermediate into a temporary Keychain; write the p8 under runner temporary storage with owner-only permissions. Clean up temporary credentials and the Keychain even after failure or cancellation where the runner permits cleanup. Run the shared release build and validation, then publish using the existing GoReleaser ownership path.

Update the release spec, maintainer guide, credential policy where necessary, and changelog to reflect notarized desktop builds and CI recovery behavior.

Exit evidence: synthetic workflow/release contract tests, relevant repository checks, documentation and frozen-scope validation, full-diff independent review, and a non-publishing CI candidate build that signs, notarizes and verifies the packaged outputs. A real release dispatch or publication requires separate authorization. If GitHub secret administration or a non-publishing CI invocation needs a material decision beyond this plan, finish the code and surface that concrete blocker.



## Original Intended Delivery

A notarized desktop build shared by local releases and CI, with protected credential handling and actual candidate acceptance evidence before publication.

## Actual Delivered

Implementation and verification are in progress. No release has been published.

## Execution Notes

- 2026-11-05T15:51:00Z [decision] User approved the plan saved as rhizome-notarization-plan.md in temporary storage; its batches are copied above.
- 2026-11-05T15:51:00Z [validation] Apple accepted API authentication via a read-only submission-history request. The installed Developer ID identity matches the CSR and macOS reports it valid. No key contents were displayed.
- 2026-11-05T15:51:00Z [learning] Apple credentials pass through three separate release filters: the 1Password wrapper, GoReleaser environment adapter and desktop hook. Each requires explicit credential scope.
- 2026-11-05T15:51:00Z [validation] Restored the worktree binary with make build. A code-mode recipe request timed out under sandbox lock restrictions; elevated next-id allocation and ontology inspection succeeded.
- 2026-11-05T15:51:00Z [validation] Focused signing and credential tests pass. make desktop-check failed in an existing worktree-switching frontend test before native checks; this change does not touch that component.

- 2026-11-05T16:05:32Z [validation] A local candidate without updater artifacts passed Apple notarization, DMG stapling, signature/team/hardened-runtime checks, and Gatekeeper checks on the bundled app and the app inside the mounted DMG. Final updater-inclusive candidate remains in progress.
- 2026-11-05T16:05:32Z [validation] 48 focused release/signing tests and 9 secret-wrapper tests passed. Desktop native tests (110) and clippy passed after placing the development gateway sidecar. Standard make desktop-build passed. Default Rhizome validation passed with zero issues. Frozen-scope drift reports one unrelated existing issue on EFF-2026-10-03-18-33 / SPEC-0111; it is not modified.
- 2026-11-05T16:05:32Z [review] Independent review found three gaps: local publication discarded Apple credentials, Tauri did not staple the outer DMG, and compilation inherited signing credentials. Fixed all three and added regression checks. A second independent review is running.
- 2026-11-05T16:05:32Z [decision] User explicitly approved temporary release Environment access for implementation branch t3/ryzone-app-notarization after auto-review requested clearer approval. The branch policy will be removed after candidate verification.
- 2026-11-05T16:05:32Z [validation] Configured all eight Apple/updater secrets in the branch-restricted GitHub release Environment directly from 1Password without displaying values. The p12 backup contains the private key and validates with its stored password using OpenSSL legacy export support. No extra local Keychain copies were created.

## Deviations

None in approved delivery scope. CI uses candidate_only on the existing manual workflow so the candidate can be dispatched from an implementation branch; it neither deploys nor publishes.

## Compounding Follow-ups

No reusable shard proposed yet.

## Closure Checklist

- [ ] Local notarized candidate verified, including updater archive.
- [ ] Protected CI secrets configured and non-publishing candidate verified.
- [ ] Applicable checks and independent review completed.
- [ ] Documentation reconciled and change integrated through a reviewed PR.

## Status

Active. Candidate verification and operational setup remain in progress.
