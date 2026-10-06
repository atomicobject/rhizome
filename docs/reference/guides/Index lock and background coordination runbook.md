---
type: ReferenceDoc
summary: "Runbook for Rhizome index coordination: the vault runtime and its single indexing lane, the index lock for in-process and one-shot writers, priority requests, heartbeats, stale recovery, and tests."
reference-kind: guide
derived-from:
  - docs/specs/technical/vault-runtime-coordination.md
  - docs/specs/product/indexing-workflow.md
  - docs/reference/analysis/Indexing pipeline - Concurrency + batching requirements.md
  - pkg/vault/indexlock/lock.go
  - pkg/app/bootstrap/lane/lane.go
last-verified: 2026-10-04
status: active
---

# Index lock and background coordination runbook

## Summary

The runtime and its writers coordinate through these files:

- `.rhizome/runtime.lock` decides which process is the **vault runtime** (`rzm serve`, attached or headless). It is process-lifetime: a dead owner in the same runtime or a proven earlier local boot can be reclaimed. Unproven foreign identities fail closed; heartbeat age never permits takeover.
- `.rhizome/index.lock` fences **index writes**. Runtime indexing uses the lane, once per job. Interactive repair transactions own their lease through commit, projection refresh, and postcheck, including browser saves in the runtime process. Other holders include in-process `rzm index` (fallback or `--in-process`), `rzm new-worktree`, validation repairs, and note edits.
- `.rhizome/db.sqlite.init.lock` serializes **database creation and schema migration**. Every write-capable store open holds it from before its first connection until the schema is validated, then releases it; it is never held while indexing runs. It exists because a one-shot command and the runtime it auto-starts open a fresh vault at the same time.

`rzm index` with a live runtime never touches `index.lock`: it submits an explicit job to the runtime's lane and streams that job's progress. Contention therefore happens between the lane and interactive repair or one-shot writers, and the protocol is: interactive callers request priority with `.rhizome/index.priority`; the lane cancels its background job within a second and holds background scheduling until the request clears; nobody deletes a live lock.

[[vault-runtime-coordination|SPEC-0104]] owns runtime, lane, and delegation behavior. [[indexing-workflow#^SPEC-0036-US3]] owns the in-process and one-shot writer behavior. [[Indexing pipeline - Phase ownership matrix]] owns where lock acquisition sits in the batch sequence.

Writable code-index startup retries typed SQLite busy/locked opening failures for 30 seconds from its first attempt, with cancellable waits growing from 100 ms to at most one second. Readiness stays pending until opening succeeds or recovery expires. Read-only one-shot commands attempt opening once and report contention promptly. Missing or incompatible indexes fail immediately. Native SQLite timeouts apply to each attempt, which may outlast the recovery window; no new attempt starts after that window expires. Shutdown cancels waits and prevents subsequent attempts, then drains an in-flight open.

## Actors

| Actor | Code | Behavior |
| --- | --- | --- |
| Vault runtime election | `pkg/app/bootstrap/live_election.go`, `pkg/app/runtime` | `TryAcquire` on `runtime.lock` before listening; the winner writes `runtime.json` after the listener is up; a loser exits 3 and touches nothing. |
| Indexing lane | `pkg/app/bootstrap/lane` | One job at a time. Kinds: explicit index, boot catch-up, watcher batch, validation refresh, embed cycle, graph cycle. Holds `index.lock` per job with the job kind as `role`, yielding heartbeat with a 1 s priority poll. Explicit jobs preempt and coalesce; cancelled background work requeues. |
| `rzm index` with a runtime | `cmd/index.go`, `pkg/app/runtime` | Ensures a runtime, `POST /api/v1/runtime/index`, streams `/events`, maps the job outcome to the exit code. Ctrl-C requests cancellation and waits a bounded interval for completion; an unresponsive worker produces an explicit unconfirmed-completion message. |
| `rzm index --in-process` or no runtime | `cmd/index.go`, `pkg/app/indexing/lock.go` | `TryAcquireIndexLock(..., requestPriority=true)`: creates the priority request, waits without a deadline while printing the holder's role, PID, age, and last heartbeat once per second; Ctrl-C exits cleanly. |
| `rzm index --rebuild` | `cmd/index.go`, `pkg/app/indexing/rebuild_fresh.go` | Stops a headless runtime, holds both spawn and runtime election leases while rebuilding in-process under `index.lock`, then releases ownership before restarting the runtime. Refuses against an attached runtime and whenever a live manifest exists. |
| Direct note, heading, tag, and property mutations | `pkg/app/cli/write_lock.go` | Wait cancellably for `index.lock` before reading content to edit; retain it across the whole batch. Preview paths remain read-only. |
| Validation repairs, note edits, `rzm new-worktree` | `pkg/validate/postapply.go`, `pkg/ontology/edit_write_lock.go`, `cmd/new_worktree.go` | Acquire `index.lock` as one-shot writers with their own role; the lane yields to them through the priority protocol. |

`rzm new-worktree` reads the source as one SQLite snapshot (`VACUUM INTO`) and takes no source-side lock.

## Lock file state machine (`index.lock` and `runtime.lock`)

Both files hold JSON `LockData`: `pid`, `started`, `host`, `runtime` (identity), `token`, `role`.

| State | `TryAcquire` behavior | Notes |
| --- | --- | --- |
| Missing | Prepare and close complete sibling metadata, then publish to the absent final path under the guard. | The only normal acquisition path. |
| Appears then disappears | Retry one atomic create. | Release race. |
| Unreadable | Held. | Never deleted. |
| Invalid JSON, age < 2 s | Held. | Writer race. |
| Invalid JSON, age < 10 min | Held. | Recently corrupt. |
| Invalid JSON, age ≥ 10 min | Recheck under the guard and atomically replace. | Corrupt-lock recovery. |
| Proven earlier local boot, PID dead | Recheck the owner under the guard and atomically replace. | Requires the exact hostname, valid earlier boot identity, and acquisition before this boot. Linux uses kernel `btime` and never treats another namespace on the same boot as a reboot. |
| Foreign or uncertain runtime identity | `ErrForeignRuntime`; operator must verify before removing. | A hostname change across reboot, missing boot evidence, or a reused live PID remains protected. |
| Valid, PID dead | Recheck under the guard and atomically replace. | Dead-PID recovery. |
| Valid, PID alive, any heartbeat age | Held. | A paused process or sleeping laptop can resume. Heartbeat age is diagnostic evidence, never permission to replace a live writer. |

Heartbeats and release verify the full ownership token. Metadata creation, stale recovery, heartbeat updates, and release serialize through an operating-system guard lock. Stale takeover prepares and closes the complete successor before atomically replacing an existing record through `fileio.Replace`, so concurrent cooperative readers see a complete old or new owner. Failed preparation or replacement preserves the old record and removes only the owned temporary file; an owner that disappeared during recheck uses absent-path creation. Ordinary Windows readers that deny deletion sharing cause a truthful error and permit retry after closing. The guard file remains on disk; the kernel releases its lock if the process exits or crashes. Do not delete guard files during normal operation. Stop older Rhizome binaries before running concurrent writers with this protocol: older releases use a transient `.guard` file and do not participate in the kernel guard. A live older owner is still never reclaimed by heartbeat age.

## Priority protocol

1. An interactive one-shot writer that fails its first `index.lock` attempt publishes `.rhizome/index.priority/<token>.json` (same `LockData` shape) and receives an owned `PriorityRequest` handle. Its heartbeat refreshes that record every 30 s until acquisition or exit. Waiting repair, CLI mutation, ontology recovery, and indexing loops check their own handle's `Active` health and close/replace it when missing, expired, replaced, or unreadable, even if another waiter remains live. Interactive indexing keeps publication errors nonfatal and retries on its normal poll. `Close` joins the heartbeat and removes only its own record. [[indexing]] documents the transition from the former single-file layout.
2. The lane polls `CheckPriority` every second while a background job runs; on a request it cancels the background job, requeues its work, and sets `Held` in its status until the request clears or goes stale (90 s).
3. Explicit index jobs are not interrupted by external priority; a one-shot writer waits for them, printing the holder role.
4. Priority records from dead PIDs in the matching runtime are removed. Fresh foreign-runtime records remain valid until their heartbeat expires. Requests from the current PID are honored because a browser save and the background indexing lane can share a process. Repair applies and retry receipts wait with cancellation, retain their own priority request, and recheck source preconditions after acquiring the lock. Serve startup journal recovery waits with cancellation for an active writer to finish before starting readers or background work. Direct nonblocking recovery APIs remain available to callers that manage their own startup boundary.
5. Publication and cleanup operate on independent token paths. Cleanup joins the waiter's heartbeat and checks its owner before removal; ready timer ticks stop before beginning another guarded refresh after cleanup starts. Heartbeats never republish missing/replaced ownership. The persistent `index.priority.guard.lock` serializes refresh with stale pruning. A busy guard causes aggregate checks to read published records without pruning; the guard itself never requests priority. Checks prune only token records and recognized publication temporary files, preserve unrelated entries, and reject a symlink or non-directory at `index.priority` before scanning. Parent vault aliases are supported.

## Runtime lifecycle files

- `.rhizome/runtime.json` (0600): instance id, run id, PID, mode, version, build id, executable, HTTP URL, control token, readiness. Trusted by clients only after `GET /api/v1/runtime` answers with the same run id and PID.
- `.rhizome/runtime-spawn.lock`: client lease while spawning `rzm serve --headless`; live owners retain it regardless of heartbeat age. A dead owner can be reclaimed.
- Per-vault `.intent.lock` in the user-local instance registry: serializes startup registration, cancellation, manifest publication, and pending-record reads. Polling and stop-all readers use this gate so they cannot block cancellation's file replacement on Windows. Waits honor the caller's context and startup or stop deadline. The registry records each pending start before a detached process is launched, so `stop --all` can find it before HTTP publication. Cancellation targets that start's token; a replacement start has its own token.
- `.rhizome/runtime.log`: headless stdout/stderr, 5 MiB cap, one rotation to `runtime.log.1`.

## User-visible messages

- `rzm index`: `joining index already running (job <id>)`, `no vault runtime available; indexing in-process (<reason>)`, waiting line `waiting for index lock: <role> pid <n>, held <age>, last heartbeat <ago>`.
- `rzm serve` when another runtime owns the vault: prints that runtime's PID and URL, exits 3.
- `rzm index --rebuild` against an attached runtime: refuses and names the PID; run `rzm stop` first.
- `rzm stop`: reports graceful exit, forced termination (headless only), or refusal (attached). Stop cancels pending startup and waits for ownership release, including recovery waiting behind another writer. A missing manifest during startup or draining is not proof of exit. Stop reports incomplete shutdown if the owner remains beyond the grace period.
- `rzm serve` and `rzm start`: report `Rhizome serve stopped` only after shutdown has drained runtime work, removed the owned manifest, and released runtime ownership. Startup recovery failures also release election ownership.

## Recovery runbook

1. `rzm index --status` first: it names the runtime, its current job, and the lock holder.
2. Runtime present and healthy but a job seems stuck: `rzm stop` (headless: forced after 10 s), then rerun `rzm index`.
3. Waiting on a one-shot holder (`cli/validate`, `cli/edit`, `cli/index`): let it finish or stop that process; the waiter continues automatically.
4. `ErrForeignRuntime` on a lock: confirm no Rhizome process is running for the vault on any host that shares the directory, then delete the named lock file. This is the only case that needs manual lock surgery.
5. Corrupt, same-runtime dead-PID, and proven earlier-boot locks recover on the next acquisition; do not pre-empt them. Earlier-boot recovery covers runtime election, spawn leases, index writers, and startup-intent locks on macOS, Windows, and Linux.
6. A manifest that points at a dead process or a wrong process is ignored and replaced by the next client; no manual cleanup.

## Operation coverage

| Interaction | Required result | Regression coverage |
| --- | --- | --- |
| Concurrent start, serve, and index clients | One elected runtime; competing clients attach or report the owner. | `tests/integration/runtime/runtime_test.go`, `pkg/app/runtime/ensure_test.go` |
| Ctrl-C, immediate restart, and startup behind a writer | Cancellation drains owned work; recovery waits without stealing the writer lock. | `tests/integration/runtime/startup_interrupt_test.go`, `startup_contention_test.go` |
| Stop or stop-all during startup or shutdown | Cancel the observed start and drain it; a missing HTTP manifest does not mean the owner has exited, and a successor retains its own token. | `pkg/app/runtimestop/stop_test.go`, `startup_contention_test.go` |
| Save while indexing, with a changed source or a retry | Background work yields; source preconditions are checked after waiting; saved edits retain retry receipts. | Ontology edit and validation repair tests; browser save journeys. |
| Filesystem change or manual validation refresh | Changed inputs invalidate validation; queued refreshes eventually publish and retain truthful progress. | Readiness/lane tests, validation API tests, Problems browser journeys. |
| Runtime crash, reboot, and paused writer | Dead owners recover, including proven earlier local boots; live owners remain exclusive regardless of heartbeat age. | `pkg/vault/indexlock/hardening_test.go`, `reboot_test.go`, runtime crash and reboot integration tests. |
| Rebuild or copy into a worktree with active writers | Database replacement excludes runtime openers and one-shot writers. | `cmd/index_runtime_test.go`, `cmd/new_worktree_test.go` |
| Direct note mutations beside an active writer | Wait before reading source; cancellation while waiting makes no changes. | `pkg/app/cli` mutation lock tests. |
| Code-mode connection while a runtime starts | Initialization and local operations stay responsive; forwarded work waits with cancellation. | `cmd/agent_code_runtime_test.go` |
| Interrupted delegated index with an unresponsive worker | Request cancellation and bound the wait for completion; report when completion is unconfirmed. | `cmd/index_runtime_cancel_test.go` |
| Unix pinned launcher under a process supervisor | The delegated command retains the supervised PID and handles termination directly. | `cmd/repo_delegate_exec_unix_test.go` |

The writer protocol coordinates cooperating Rhizome processes. External editors do not acquire these locks; save preconditions still detect intervening source changes. Holding a writer lease prevents overlap, but does not turn older multi-file CLI operations into rollback transactions.

## Tests and validation

- `pkg/vault/indexlock/lock_test.go`: acquire, block, concurrent acquire, dead-PID and aged-corrupt recovery, identity normalization, process-lifetime mode, role round-trip, heartbeat ownership check, priority lifecycle, 1 s yield.
- `pkg/app/bootstrap/lane/*_test.go`: preemption latency, coalescing, replay then stream, job-wide cancel, external priority hold, lock role, all under `-race`.
- `pkg/app/runtime/*_test.go`: manifest states, probe classes, concurrent ensure with helper processes, spawn lease, build mismatch policy.
- `cmd/index*_test.go`, `cmd/serve*_test.go`, `cmd/stop_test.go`: delegation, fallback, rebuild stop/restart and refusal, election loser exit, stop modes.

```bash
go test -race ./pkg/vault/indexlock/... ./pkg/app/bootstrap/... ./pkg/app/runtime/... ./cmd/
rzm agent validate all --max-issues 40
```

## Related

- [[vault-runtime-coordination|SPEC-0104]] · [[vault-runtime]]
- [[indexing-workflow#^SPEC-0036-US3]]
- [[Indexing pipeline (Hub)]] · [[Indexing pipeline - Concurrency + batching requirements]] · [[Indexing pipeline - Phase ownership matrix]]
- [[LiveRuntime (async server bootstrap)]]
