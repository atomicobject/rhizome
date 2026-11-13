---
type: EffortNote
id: EFF-2026-11-13-15-07
aliases: [EFF-2026-11-13-15-07]
name: Safe recovery of incomplete repository upgrades
created-at: 2026-11-13T20:07:42Z
status: complete
summary: Recover lost starter ownership and expose safe, reviewable file decisions so repository upgrades install Delivery.
governing-specs:
  - "[[docs/specs/technical/rhizome-modules]]"
  - "[[docs/specs/experience/desktop-repository-setup]]"
---

# Safe recovery of incomplete repository upgrades

## Scope

Repair upgrades that installed Delivery views while retaining old flat schemas. Recover provable ownership, preserve identifier allocation and local edits, and keep unresolved decisions reachable in the desktop. Publishing a release, changing waffles-app, and repairing its existing note validation issues are outside this implementation.

## Spec Set (Frozen)

- [[docs/specs/technical/rhizome-modules]] at 867bdb1ecddc89a0a648ddf00f75529ee4504b0a: migrate existing repositories without loss.
- [[docs/specs/experience/desktop-repository-setup]] at the same revision: review and apply repository updates through the selected executable.

## Stories In Scope (Frozen)

The parent specifications above define the bounded migration and repository update obligations. This effort does not select unrelated setup, indexing, or module-command work.

## Spec Coverage Checklist

- [x] Transfer verified legacy fingerprints before clearing them.
- [x] Recover missing ownership from exact raw-byte matches in bounded Git history.
- [x] Preserve sequential effort identifier contracts during schema replacement.
- [x] Present unresolved replacements as explicit take, keep, or defer decisions.
- [x] Show distinct diffs for distinct mirrored edits and protect unlisted local copies.
- [x] Bind desktop choices to the reviewed contents and reject stale snapshots before writes.
- [x] Complete combined verification and packaged-app inspection.

## Plan

Drew authorized fixing the reported upgrade issues and delegated needed design updates to Opus 5.5 high, with Sol 6.1 xhigh ultrafast implementation workers. Opus designed exact-baseline recovery, ordinary file replacement decisions, desktop review, and the later snapshot guard. Three workers implemented ownership, init recovery, and desktop changes; a fourth implemented snapshot binding. Independent Sol reviews identified mirror and stale-preview risks, which were corrected before final verification.

## Original Intended Delivery

An upgrade should deliver the current starter experience, including Delivery for complex-domain repositories, without silently losing edits or hiding incomplete migration work.

## Actual Delivered

The implementation recovers unchanged starter schemas, keeps edited or unproven content pending, and provides desktop diffs and choices. Taking a replacement preserves installed identifier contracts. Keeping it persists the decision. Desktop applies carry a snapshot of the reviewed file versions and group membership; stale reviews require a new review.

## Execution Notes

- 2026-11-13T20:07:42Z: Recorded the work after implementation; this is a retrospective execution record, not a claim that the effort existed before coding.
- A disposable waffles-app-shaped reproduction established that old sourceFingerprints were discarded, leaving byte-identical schemas classified as unrecorded. Exact ownership restoration installed Delivery and preserved sequential EFF allocation.
- The desktop worker verified that settings-apply refreshes Delivery in a running fixture without a restart.
- Independent review accepted the final mirror and snapshot protections. Focused Go, desktop, and Rust checks passed.
- The first full run was blocked by sandbox port and runtime-identity restrictions. The unrestricted run exposed a host-dependent missing-executable test; an explicit broken-executable fixture fixed that assumption. The next make check passed.

- Final gates passed: `make check`, `make desktop-deps`, `make desktop-check`, `make desktop-build`, `make build`, and `./scripts/rzm validate` (0 issues).
- The final CLI replay recovered schemas using committed legacy evidence after the working metadata was lost, retained sequential effort IDs, returned zero writes on rerun, and passed `rzm view validate` for 56 views.
- The built macOS app opened the recovered fixture with its bundled executable. After trusting the disposable fixture, the Delivery dashboard and navigation rendered successfully.

## Deviations

Workers requested as isolated tasks were supplied the same checkout. Their writes were coordinated, commits deferred, and final builds serialized. No live waffles-app files were changed. The pre-existing note validation issues are unrelated to the missing Delivery schema and remain outside scope.

## Closure Checklist

- [x] Required quality gates pass.
- [x] Implementation has independent review.
- [x] CLI and desktop subsystem documentation describe the new contract.
- [x] Final replay and packaged-app inspection recorded.

## Compounding Follow-ups

An installed executable on the host must not determine a missing-executable test result. The regression now supplies a deterministic broken executable. Legacy ownership evidence must migrate before its old storage is removed.

## Status

Implementation, independent review, and verification are complete on the feature branch. Publication and installation are separate release work. The live affected repository remains unchanged.
