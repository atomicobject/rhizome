---
type: EffortNote
id: EFF-2026-09-29-14-46
name: Runtime reboot recovery
created-at: 2026-09-29T18:46:38Z
status: active
summary: Recover dead-owner locks from a proven earlier local boot without taking locks from live or foreign owners.
aliases:
  - EFF-2026-09-29-14-46
---

# Runtime reboot recovery

## Scope

Repair stale runtime election, spawn, indexing, and startup-intent locks left after a local reboot. Keep the existing persisted lock format and guarded replacement protocol. Preserve live owners, foreign hosts, uncertain boot metadata, and Linux namespaces on the current boot. Changing foreground/background command behavior and publishing a release are outside this slice.

## Spec Set (Frozen)

- [[vault-runtime-coordination|SPEC-0104]], as checked out at `9409d04b05c0f597dd2391b56cbd9fa65ffe0342`.

## Stories In Scope (Frozen)

- [[vault-runtime-coordination#^SPEC-0104-US1]]: replace a stale runtime lock and start one usable runtime.
- [[vault-runtime-coordination#^SPEC-0104-US6]]: reclaim a lock left before a local reboot.

The story locators were verified through `story-acceptance-pack` with `requiresFix: false`. Individual criterion locators need source repair, so this effort freezes their durable parent stories instead. No spec source was changed.

## Spec Coverage Checklist

- [x] Recovery is shared by all callers of `indexlock.TryAcquireWithOptions`.
- [x] Exact hostname, valid boot identity, acquisition before this boot, and dead PID are required.
- [x] Reclaim rechecks the same owner and recovery predicate under the persistent OS guard.
- [x] Unit tests preserve live, foreign, malformed, and current-boot owners.
- [x] Real `start` and `index` commands recover runtime, spawn, and index locks in isolated vaults.
- [x] Documentation validation passes; the full gate was run and its unrelated failing test is recorded below.

## Plan

1. Reproduce post-reboot lock rejection in the shared acquisition primitive.
2. Add the smallest recovery predicate using existing platform boot identities and Linux kernel `btime`.
3. Verify guarded recovery and refusals, then exercise the real binary against temporary vaults.
4. Update the owning constraints and changelog, run repository gates, and build the local binary.

### Authorization

The user requested, "can we make rhizome detect and recover from this?", then said "Continue" after the implementation outline. This authorizes the recovery fix and its ordinary verification. No current user is configured in this vault, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

`rzm start` and automatic worker startup recover the dead-owner lock seen after the September 28 reboot, without manual deletion or a second runtime.

## Actual Delivered

The shared lock primitive recognizes earlier local boots on macOS, Windows, and Linux. Recovery requires the exact recorded hostname, well-formed boot metadata, acquisition before the current boot, and a dead PID. Linux obtains boot time from `/proc/stat` and rejects another namespace on the same boot. Guarded reclaim verifies that ownership has not changed. Unit and real-command regression tests pass on macOS; the lock package cross-builds for Linux and Windows.

## Execution Notes

- The regression failed before the fix with `ErrForeignRuntime` for both ordinary and process-lifetime locks.
- `go test -mod=vendor -race ./pkg/vault/indexlock -count=1` passed.
- `go test -mod=vendor -tags 'fts5 integration' ./tests/integration/runtime -run TestRuntimeRecoversLocksFromPreviousBoot -count=1` passed.
- `go test -mod=vendor -race -tags fts5 ./pkg/vault/indexlock/... ./pkg/app/runtime/... ./pkg/app/bootstrap/... ./pkg/app/cli/serve/... ./cmd/` passed.
- `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` passed with zero issues.
- `make build` passed. A separate smoke test ran `bin/darwin/rzm` against a synthetic pre-reboot lock, verified its published PID, and verified graceful shutdown released the lock. The personal vault selects this binary through `rhizome.devBinaryDir`.
- `make check-full` passed formatting, vet, credential checks, benchmark contracts, and the relevant race tests, but stopped on `TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates` in the unchanged identifier-reconciliation package. An isolated rerun also failed, and the same test failed in a clean archive of the original `9409d04b05c0f597dd2391b56cbd9fa65ffe0342` HEAD without this change, confirming a pre-existing failure.
- `make web-test integration-packages integration-mixed` passed all checks that the full-gate failure prevented, including the new runtime recovery test under the race detector.
- `GOOS=windows CGO_ENABLED=0 go build -mod=vendor ./pkg/vault/indexlock` and the equivalent Linux cross-build passed. Native Linux and Windows runtime behavior still needs CI evidence.
- Independent review found that short-hostname matching could conflate distinct DNS names. Recovery now requires exact hostname equality. Unsupported-platform test skips and the Windows acquisition-time fixture were also corrected.

## Deviations

A hostname change across a reboot, or a PID reused by a live process, retains manual recovery. Those ambiguous cases do not authorize taking a possibly live lock. Same-boot hostname suffix normalization is unchanged.

- 2026-10-04 (frozen-scope-drift acknowledged via [[2026-10-04-10-22-persistent-diagnostics]]): SPEC-0104's headless log location changes to `.rhizome/diagnostics`. The frozen reboot-recovery stories and lock ownership requirements are unchanged.
- 2026-10-04 (frozen-scope-drift acknowledged via EFF-2026-10-03-18-59) SPEC-0104 was amended so `rzm new-worktree` may copy the source database with a copy-on-write clone taken under the source's SQLite write lock, falling back to `VACUUM INTO`. Its lifecycle wording now names open UI event streams as runtime activity, which the runtime already promised. Neither change touches reboot recovery, lock reclaim, or hostname handling, so this effort's scope is unchanged.

## Closure Checklist

- [x] Regression reproduced before implementation.
- [x] Focused tests and independent review completed.
- [x] Owning context, subsystem notes, coordination runbook, and changelog updated.
- [x] Full gate attempted, its remaining targets run separately, documentation validated, and local binary built.
- [ ] Existing Git-provenance test failure resolved by its owning work.
- [ ] Landing PR or merge commit recorded.

## Compounding Follow-ups

None within this slice. Stronger machine and process identity could support recovery when hostnames change or PIDs are reused, if those cases become a practical problem.

## Status

Implementation, focused checks, integration suites, independent review, documentation validation, and local build are complete on `fix/runtime-reboot-recovery`. The full gate retains an unrelated identifier-reconciliation failure. This effort remains active pending resolution or accepted carry-forward of that failure and a landing PR or merge commit; no release was published.
