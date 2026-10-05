---
type: EffortNote
id: EFF-2026-10-04-03-00
aliases:
  - EFF-2026-10-04-03-00
name: Recoverable note namespace mutations
created-at: 2026-10-04T06:55:00Z
status: complete
summary: Publish native note moves, required backlink changes, and admitted Git staging through the existing repair transaction owner.
---

# Recoverable note namespace mutations

## Scope

This bounded unit belongs to the [foundational cleanup](2026-10-03-22-36-codebase-cleanup.md). A reproduced native move publishes its new name and some backlinks before a later write failure. The old source is then absent, an ordinary retry cannot finish, and startup has no journal to recover the operation.

Reuse the existing validation repair engine for the complete required mutation. Preserve CLI syntax, endpoint admission, format capabilities, protected text, basename ambiguity, staged versus dirty Git content, and optional code-reference behavior. All requested moves and required backlink writes form one transaction. This is recoverable publication under cooperative writer exclusion; arbitrary external readers can still observe intermediate filesystem states.

## Spec Set (Frozen)

The existing [CLI](../reference/subsystems/cli.md), [validation](../reference/subsystems/validate.md), [vault core](../reference/subsystems/vault-core.md), and [ontology](../reference/subsystems/ontology.md) contracts govern this repair. No new product feature or generic transaction framework is in scope.

## Stories In Scope (Frozen)

1. A planning or publication failure preserves or restores the original namespace, backlinks, overwritten destinations, and admitted Git staging when witnesses remain valid.
2. A crash retains one durable decision and sufficient owned evidence for repeated recovery. Unknown later edits produce a visible conflict without destructive recovery.
3. A committed move reports its decision even when projection refresh, cleanup, or optional follow-up fails. Recovery of an earlier request is never reported as success of the current request.

## Spec Coverage Checklist

- [x] Complete namespace and backlink planning precedes publication.
- [x] Filesystem and Git witnesses survive interruption and recovery.
- [x] Other authored-file writers respect pending namespace evidence.
- [x] Refresh uses the existing projection owner under the held lease.
- [x] CLI and code-mode preserve committed and unresolved outcomes.
- [x] Independent review, native Windows checks, and full CI pass.

## Plan Approval

Drew's explicit cleanup instruction authorizes fixing demonstrated defects, structural refactoring, child PRs, and merging reviewed green children into the parent. The coordinator accepts this concrete design under that authority. The local current-user identity is not configured, so `plan-approved-by` is omitted. The aggregate PR remains open for Drew; publication and merging it into `main` remain outside this work.

## Plan

### 1. Reuse one publication owner

Land the independently reviewed repair rename-order prerequisite first. Retire original sources before installing final files; restore in the reverse effect order, including valid older prepared journals. Preserve per-mutation namespace guards and existing validation recovery behavior.

Add one narrow `validate.ApplyNamespaceMutation` entrypoint. It accepts the existing run context, a planner invoked under an opaque engine-owned lease, the existing post-apply refresher, and an optional invocation-local NamespacePostCommit callback. Its plan contains repair operations and small bounded application summary JSON. The engine assigns a unique shared identity to every operation, groups them through the existing planner, and asserts one transaction. A caller-supplied action ID alone does not establish atomicity.

Accept only canonical rename operations and fully witnessed required writes. Reject arbitrary deletes, internal destinations, lifecycle overrides, duplicate or overlapping endpoints, malformed summaries, and plans without a rename. Direct native authorization bypasses validation action selection and historical-note policy inside this fixed entrypoint; generic validation callers keep their current rules. Keep required publication fixed to witnessed rename/write effects. The optional callback runs synchronously after required convergence and cleanup, before the existing lease release, only for a successful committed current request. It contributes no journal effects or engine errors and never runs during recovery.

Recover pending engine work before planning. If older work was recovered, return its separate outcomes, mark this request not started, and require replanning. Refuse pending ontology content-edit journals under the held lease using a read-only query owned by ontology. Never acquire a second vault lease while already holding the first.

### 2. Witness overwrite and complete content

Extend a rename's destination precondition explicitly: absent, the admitted case-only source, or a witnessed occupied regular destination. Include destination bytes/hash/mode in plan admission and fingerprints. An overwrite flag is permission to capture this witness, not permission to discard an unknown file.

Compose source-addressed content writes into the final destination and retain both original endpoint states. Rollback restores the original source and the original overwritten destination. Two existing endpoints are permitted only where the journal proves the original overwrite pair; retain refusal for recreated sources, unknown modes or bytes, symlinks, and ambiguous namespace identity. Test equal-content original/final states deliberately.

Refactor the CLI's backlink logic into one read-only planner using the existing endpoint plan. Plan against the virtual final namespace, including moved-note self-links and cross-links. Exclude overwritten destination bodies from the final backlink inventory while retaining rollback evidence. Preserve request-order counts and skipped paths; use stable execution ordering. Delete superseded direct required-backlink writes after both native entrypoints use the new owner.

### 3. Prepare one specific Git effect

Bind the supported directory-backed Git repository and index before preparation. Preserve existing unsupported-repository and untracked-source fallback policy. Environment-selected repository/index overrides must not redirect the transaction. Disable executable fsmonitor during scratch preparation. Linked-worktree support is outside this unit.

Use Git's own move operation against an owned copied index and affected-file shadow worktree. Pass `core.splitIndex=false` and `index.sparse=false` for both candidate and rollback preparation. Normalize the original copy through Git into a standalone rollback snapshot. Keep the original raw index hash separately for admission. No hand-written index parser, live `git mv`, or later `git add -A` is allowed.

Rollback preserves equivalent staging state, including entries, stages, persistent flags, unrelated staging, and staged versus dirty source content. Split/sparse storage encoding and cache bookkeeping may normalize. Existing immutable Git object/shared-index mtimes may be refreshed by Git; preparation must preserve authored files, live index, refs, configuration, existing file bytes/modes, and the set of live metadata paths. No new unowned object or dependency file is admitted. Prepared snapshots must remain usable after old split-index dependencies disappear.

Prepare mixed tracked/untracked batches without discarding successful tracked index changes. A prepublication Git preparation or lock-admission failure may take the existing filesystem fallback only when no live Git effect occurred. Record per-move history results truthfully. Cancellation remains cancellation.

The typed Git journal record binds canonical index/lock paths, raw original admission witness, normalized restoration witness, final witness, owned snapshots, and random lock ownership evidence. Acquire `index.lock` exclusively and revalidate the raw original index before authored publication. Retain the lock while publishing a separate staged index file. Never adopt or unlink a foreign lock.

### 4. Record the decision before releasing Git exclusion

Version the extended manifest in the existing discovery directory so older readers reject it. Preserve v1 recovery. A bounded native-purpose record identifies fixed recovery/postcheck semantics and receipt summary; it cannot be mixed with generic validation check selectors. The engine chooses an internal completion receipt under `.rhizome/edit-receipts/`.

Verify all final authored/index states immediately before synchronizing COMMITTED. Before that decision, failures attempt witnessed rollback. After proving full restoration, synchronize a native RESTORED marker. Conflicting markers or an uncertain synchronization/rollback leave an unresolved outcome and retained evidence.

After either durable decision, release only the proved-owned Git lock and durably record settled release before projection work. A missing lock can represent an interrupted successful release. A foreign replacement is preserved and cannot trigger rollback after a decision. A crash before durable ownership was established retains uncertainty rather than guessing ownership. Cancellation cannot skip safety work once publication starts.

COMMITTED and RESTORED replay preserve later authored and Git edits. Cleanup tombstones resume cleanup only. Generic validation journals retain their strict behavior. Startup stabilization can restore prepared files and release proved-owned Git exclusion without projection dependencies, leaving convergence work pending.

### 5. Keep writers and projections consistent

Add a small validate-owned `namespaceadmission` leaf with a read-only held-lease check. Share only the minimal purpose/version/decision envelope with the engine. Use standard library and path dependencies, avoiding an ontology/validate import cycle. Reject malformed, unsupported, prepared, unresolved, or unsettled native evidence. Permit terminal decisions only after Git release is settled. Non-native repair admission retains its existing behavior.

Call the guard after CLI writer acquisition and after ontology recognizes or acquires its writer lease, before per-note locks or recovery/publication. This covers heading pre-recovery, direct edit sessions, and node-link writes, including callers already holding the lease. Native application uses the recovery owner instead of this reject-only guard. Serve and web startup stabilize repair/native evidence before ontology recovery. Ontology exposes its own pending-content-journal query for native admission.

Use `ValidationProjectionPostApplyRefresher` under the engine's borrowed lease. Verify returned path coverage and runtime ownership. Initial apply refreshes the exact committed delta. After a terminal decision and restart, inspect the current union of original/final/backlink paths: present regular files are changed, absent files are deleted. Never replay historical renames, which would delete a legitimately recreated original path from projections. Refresh anchors for every present footprint path and revalidate current fingerprints and exact directory spelling around convergence. Canonical caseless candidate keys include Unicode case and normalization aliases, with exact-entry precedence and physical identity proof; retired logical keys remain deleted coverage. Unknown types or symlinks retain a refresh conflict without restoring old bytes. Do not add a second SQLite writer or a later full freshness pass.

### 6. Report the actual result

Return current and recovered outcomes separately, with `not_started`, `restored`, `committed`, or `unresolved` decision; transaction ID; normalized moves; Git moves; recovery-pending flag; receipt path; and bounded summary. Populate legacy successful fields only for a committed current operation.

Cobra renders outcome information before returning a follow-up error. Code-mode keeps the result payload with `OK=false`, nonzero exit status, and a structured diagnostic. Its existing workflow/JSON transport must retain decision, transaction ID, and receipt even for canceled or oversized replies; use a bounded mutation-status projection only where the existing size limit would otherwise lose them. Optional code-reference and URI effects run after required publication. Code-reference failures retain the existing nonfatal behavior and may add warning information; URI failures return an error alongside the committed outcome.

### 7. Verify in bounded phases

Use Sol 6.1 xhigh for implementation and Astra for foundation and final review. Review the engine API, purpose format, overwrite composition, and lease ownership before wiring the application. Implement the Git helper and read-only CLI planner independently where file ownership permits, then integrate through the reviewed contract. Each child uses the parent branch and merges only after fresh review, CI, and Greptile feedback are resolved.

Public tests cover real backlink failures, both path orders, multiple moves, overwrite bytes/modes, case-only endpoints, self/cross-links, every publication and reverse-rollback interruption, repeated startup recovery, external source/index drift, foreign locks, and later edits after both terminal decisions. Verify generic v1 recovery, writer admission, pending ontology evidence, exact current-footprint refresh, and no nested lock wait.

Git tests compare actual staging for ordinary, dirty/staged, mixed untracked, overwrite, executable, split/v4, persistent flags, merge stages, resolve-undo, sparse and absent/unborn cases. Install only prepared index bytes in a disposable repository and validate subsequent Git reads/tree production. Exercise native Windows replacement and locking in CI. Real CLI and code-mode checks cover committed refresh failures and cancellation. Run focused race checks locally; CI owns the full suite as requested.

## Original Intended Delivery

Native move and rename commands use one existing transaction owner for required publication, recover interrupted requests, and expose truthful outcomes through the CLI and agent surfaces.

## Actual Delivered

The required native rename and move paths now use the existing repair transaction owner. [PR #50](https://github.com/atomicobject/rhizome/pull/50) supplies repair ordering, [PR #53](https://github.com/atomicobject/rhizome/pull/53) supplies isolated Git preparation, [PR #63](https://github.com/atomicobject/rhizome/pull/63) supplies durable native publication and recovery, and [PR #66](https://github.com/atomicobject/rhizome/pull/66) wires the public CLI and code-mode results. All are independently reviewed, green in native CI, and merged into [PR #14](https://github.com/atomicobject/rhizome/pull/14).

The CLI plans the final namespace and required backlink content before publication, removes superseded direct writers, and preserves full current diagnostics while persisting bounded recovery summaries. Optional code-reference rewrites run under the existing lease only for a successfully committed current request. Native recovery proves physical identity for Unicode spelling candidates, preserves later authored and staging edits after settled decisions, and refreshes the current footprint. Generic v1 recovery retains its own contract.

## Execution Notes

- Executable engine phases are `7f17579`, `0d596a8`, and `b0761b9`. Focused native and generic repair tests pass, including six required-file publication boundaries, six reverse rollback boundaries, real Git staged-versus-dirty preservation, foreign locks, Git and non-Git decision-sync uncertainty, missing-refresher refusal, nested targets and input-order reporting.
- Independent native cleanup/replay probes passed under race across detached, marker-removed and manifest-removed cleanup boundaries, preserving later content and omitting unavailable reporting fields. Public startup and direct-writer test evidence is recorded in the engine delivery report. Subsequent final child review, CI and Greptile passed before integration.

- Independent public probes reproduced partial native publication and the repair engine's reverse-order defect. The prerequisite has its own child PR.
- The read-only Git prototype passed 15 staging variants plus absent/unborn and removed split-dependency controls. The smaller bound live-repository approach needs only copied indexes and an affected-file shadow tree. It does not prove integrated recovery or native Windows behavior.
- The design review required engine-assigned transaction identity, terminal restoration evidence, independent Git release, current-footprint replay, and admission coverage for direct ontology writers. These requirements are incorporated above.

- Final engine head `2565b531618600a65515651af3a086411a760de6` and CLI head `c6031eae72d7308a973e05d63a7f3cb9cca73090` passed independent Astra review, all 20 CI/review checks and final-head Greptile 5/5. Their merge commits are `38cf8b74a86f8d8d21499c384c766967893b68c6` and `b577df611c0f5979043d7b0702761518efcb3e7a`.
- Focused race evidence includes actual interruption and recovery boundaries, later edits after terminal decisions, projection convergence, direct-writer admission, real Git staging comparisons, filesystem capability checks, output transport, oversized diagnostics, ignored-file ambiguity and a real FIFO-controlled dependent code rewrite. The final path-policy repair uses lexical relative inventory entries through VaultPaths and retains symlink admission controls.

## Deviations

Native destination-parent preparation retains empty folders after rollback, matching the existing CLI and generic repair behavior. Required authored files and admitted staging still restore together. One settled-decision enum certifies terminal marker durability and Git release; native purpose-tagged cleanup names retain that decision after reporting metadata is removed.

Targeted local tests and full CI follow Drew's explicit instruction. Standalone Git rollback preserves staging semantics while allowing storage encoding and cache bookkeeping to normalize.

## Closure Checklist

- [x] Required direct mutation paths removed after integration.
- [x] Owning subsystem guidance updated with implementation.
- [x] Public failure, recovery, and product-result evidence recorded.
- [x] Every child independently reviewed and green in CI.

## Compounding Follow-ups

Linked-worktree support, replay of optional follow-up effects, and request idempotency keys are outside this bounded repair.

## Status

Complete. The bounded repair is integrated in [PR #14](https://github.com/atomicobject/rhizome/pull/14) through the reviewed children above. The aggregate PR remains open for Drew; completion does not publish a release or merge it into `main`.
