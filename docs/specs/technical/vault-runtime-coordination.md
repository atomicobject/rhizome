---
type: TechnicalSpec
id: SPEC-0104
aliases: [SPEC-0104, vault-runtime-coordination]
summary: "One auto-started vault runtime per vault root owns watching, scheduling, and indexing; every client finds or starts it safely, `rzm index` and code mode execute inside it, and the cross-process lock protocol becomes diagnosable and prompt."
spec-status: active
last-updated: 2026-10-04
---

# Vault runtime coordination

## Summary

Today indexing freshness depends on a human running `rzm serve` in each worktree, and when serve is running, `rzm index` competes with its schedulers for `.rhizome/index.lock`. Two runtime holders never actually stop on a priority request, holders poll for priority every 30 seconds, the CLI gives up after two minutes, and the failure message names a PID but not what it is doing. The result is the dead end this spec removes: "another Rhizome instance is using the database" with no next step.

This spec makes the `rzm serve` process the single **vault runtime** for a vault root. It is auto-started headless by any client that needs freshness, it owns all in-process indexing work on one serialized lane, and it exposes a loopback control API through which `rzm index` and code mode execute. Clients that only read (`rzm agent …`, MCP) make sure a runtime exists and keep reading the shared SQLite index in-process. Follower mode and the cache-hints log are deleted; a second runtime for the same vault exits immediately. The `.rhizome/index.lock` protocol stays as the fence for in-process fallback and for one-shot writers, and it is hardened so waiting is bounded by the holder's real yield time rather than by a timeout.

Prior art: Watchman's per-root daemon started on demand by clients, and `git maintenance` style single-writer background work.

## Goals

- `rzm index` succeeds whenever the vault is usable, without lock contention, and shows live progress from wherever the work runs.
- A worktree with any Rhizome activity has a running runtime keeping its index fresh, without the user starting one.
- Exactly one runtime per vault root under any interleaving of concurrent clients, crashes, reboots, stale files, PID reuse, and hostname changes.
- Code mode scripts call into the running runtime instead of bootstrapping a private runtime per script.
- Every contention or lifecycle state is observable through `rzm index --status`, the runtime health endpoint, and messages that name the holder's role and the next action.
- Linux, macOS, and Windows are first-class for spawn, attach, liveness, and lock reclaim, with tests on the existing CI matrix.

## Non-Goals

- Routing one-shot `rzm agent` and MCP reads through the runtime. They stay in-process readers of the shared index.
- Multi-user or remote access. The control API is loopback only.
- A separate daemon binary or service manager installation (launchd, systemd, Windows services).
- Sharing one runtime or one database across worktrees. Each vault root keeps its own `.rhizome/db.sqlite` and its own runtime.
- Changing the indexing pipeline stages, writer lane, or barriers defined by [[indexing-pipeline-architecture|SPEC-0012]].
- Changing the code-mode stdio protocol or generated client artifacts defined by [[persistent-agent-code-mode|SPEC-0092]] beyond where operations execute.

## User Stories

### US1 - Find or start exactly one runtime for a vault root

- id:: ^SPEC-0104-US1
- summary:: Any Rhizome client that needs a live runtime for a vault root attaches to the existing one or starts a headless one, and concurrent clients never leave two runtimes alive.
- status:: ready

#### Acceptance Criteria

- A client reads the runtime manifest, verifies liveness with the health probe (matching instance id and PID), and attaches without spawning when the probe succeeds.
  verification:: unit test with a fake manifest and probe server; integration test attaching to a running headless runtime.
- When no live runtime exists, the client takes the short-lived spawn lease, starts a detached `rzm serve --headless` with the same executable and vault root, and returns once the manifest and probe succeed within the spawn budget.
  verification:: integration test on a temporary vault proves one child PID, manifest present, probe ready.
- N clients calling ensure concurrently on a vault without a runtime produce exactly one surviving runtime; every client attaches to the same instance id; extra runtimes exit with the documented "already running" exit code and never remove the winner's manifest or lock.
  verification:: race test with at least 8 concurrent processes on the CI matrix, repeated under `-race`.
- A manifest whose PID is dead, whose probe fails to answer with the same instance id, or whose runtime lock is stale is treated as absent and replaced. A manifest whose PID is alive but whose probe fails is not replaced; the client waits up to the spawn budget and then reports the unresponsive PID and the `rzm stop` remedy.
  verification:: unit tests for each manifest state.
- Clients that only read (`rzm agent start`, `rzm mcp serve`) trigger ensure without blocking on readiness; `rzm ci`, `--in-process`, config `runtime.autostart: false`, and `RZM_RUNTIME_AUTOSTART=0` disable auto-start.
  verification:: command tests assert spawn is skipped in each opt-out and does not delay agent start.

### US2 - `rzm index` executes in the runtime with live progress

- id:: ^SPEC-0104-US2
- summary:: `rzm index` sends the request to the runtime, renders its progress and timings locally, and falls back to in-process indexing only when no runtime can be used.
- status:: ready

#### Acceptance Criteria

- With a live runtime, `rzm index` submits an index job, streams progress segments, log lines, and the final summary over the control API, and exits with the job's outcome; the CLI acquires no index lock.
  verification:: integration test runs `rzm index` against a headless runtime on a fixture vault and asserts progress output, exit code, and that no `.rhizome/index.lock` was created by the CLI PID.
- A second `rzm index` while a job runs joins the running job and streams the same events; it does not start a competing job.
  verification:: integration test with two concurrent CLI invocations observes one job id.
- Interrupting the CLI cancels the job in the runtime; background lane work resumes afterwards.
  verification:: test sends interrupt mid-job, asserts job cancelled and the lane accepts the next job.
- Without a usable runtime (auto-start disabled, spawn failed, or `--in-process`), `rzm index` runs in-process under `.rhizome/index.lock` exactly as before, and says which path it took.
  verification:: existing `cmd` index tests plus a test for each fallback trigger.
- `rzm index --rebuild` stops a headless runtime, clobbers and rebuilds in-process under the index lock, then restarts the runtime; against an attached runtime it refuses with the PID and the instruction to stop it first. The database is never unlinked while any runtime holds it open.
  verification:: integration test with headless runtime asserts stop, rebuild, restart; test with attached runtime asserts refusal.

### US3 - One indexing lane inside the runtime yields promptly

- id:: ^SPEC-0104-US3
- summary:: All in-process indexing work (boot catch-up, watcher ownership batches, embedding cycles, graph cycles, explicit index jobs) runs on one serialized lane that preempts background work for explicit jobs and yields to external writers within seconds.
- status:: ready

#### Acceptance Criteria

- Only the lane acquires `.rhizome/index.lock` inside the runtime, once per job; schedulers and ownership batches submit jobs instead of acquiring the lock themselves.
  verification:: package test asserts no other runtime code path calls `TryAcquire`; grep-based guard test over `pkg/app/bootstrap`.
- An explicit index job cancels the running background job through its context and starts within one second; cancelled background work is requeued, not lost.
  verification:: lane test with a slow fake background job measures preemption latency and requeue.
- An external priority request (`.rhizome/index.priority`) cancels the running background job within one second and holds background scheduling until the request clears or goes stale.
  verification:: lane test creates a priority file and asserts cancellation latency and hold.
- Embedding and ownership work stop on cancellation between provider batches and between files, never only at the end of a cycle.
  verification:: tests inject cancellation mid-batch for `NoteSyncer.SyncPaths`, `Syncer.Sync`, and the ownership batch.

### US4 - Code mode calls execute in the runtime

- id:: ^SPEC-0104-US4
- summary:: `rzm agent code serve` keeps its stdio contract with generated clients but executes catalog operations in the vault runtime, so scripts see the live index and pay no per-script bootstrap.
- status:: ready

#### Acceptance Criteria

- The stdio host ensures a runtime at start and forwards each catalog operation call to the runtime's agent-operation endpoint with the connection's read-write authority and session id; results preserve `CallOutcome` semantics, diagnostics, and exit-code distinctions.
  verification:: real Node-to-compiled-Rhizome fixture proves one runtime PID serves several calls from one script and that no per-call runtime bootstrap occurs.
- Local-only operations (validation, identifiers, current user, note move, ontology authoring) keep executing in the stdio host process.
  verification:: coverage matrix test lists each operation's execution host.
- Configuration changes are detected by the runtime and reported to the client as the existing `code_mode_configuration_changed` outcome.
  verification:: test edits `.rhizome/config.yml` between calls and asserts the outcome.
- When no runtime can be used, the host executes operations in-process as before and reports the fallback once in diagnostics.
  verification:: test with auto-start disabled.

### US5 - Runtime lifecycle is observable and controllable

- id:: ^SPEC-0104-US5
- summary:: A user or agent can see which runtime serves a vault, what it is doing, stop it, and trust that headless runtimes go away on their own.
- status:: ready

#### Acceptance Criteria

- `rzm index --status` shows the runtime (mode, PID, build, ready, current job) and the index lock holder (role, PID, age) or states that none exist.
  verification:: command test with fixtures for each state.
- `rzm stop` shuts down the vault's runtime gracefully and waits for exit; `rzm stop --all` stops every runtime listed in the global registry; a headless runtime that ignores graceful shutdown is terminated after a bounded grace period, an attached one is never force-killed.
  verification:: integration tests for graceful, forced-headless, and attached refusal.
- A headless runtime exits after one hour with no client requests, no open event streams, and an idle lane; an attached runtime never idle-exits. Either exits when its vault root disappears.
  verification:: test with a shortened idle timeout; test removing the vault root.
- A client whose build identity differs from a headless runtime's replaces it (graceful stop, then spawn); against an attached mismatched runtime it reports the mismatch and falls back.
  verification:: tests with a fake manifest carrying a different build id in each mode.
- Headless runtimes persist structured diagnostics under `.rhizome/diagnostics`, with a separate rotated `runtime-output.log` for startup and crash output as defined by [[persistent-diagnostics|SPEC-0116]]. `rzm start` and `rzm serve` both take over from a live headless runtime (graceful stop, then election), because an attached runtime never idle-exits under an open browser; against a live attached runtime `rzm start` opens the browser at its URL and exits successfully, while `rzm serve` exits 3.
  verification:: integration tests.

### US6 - The cross-process lock is diagnosable and recovers safely

- id:: ^SPEC-0104-US6
- summary:: In-process writers that still use `.rhizome/index.lock` wait as long as the holder needs, show who holds it and why, and reclaim stale locks correctly across reboots and hostname changes.
- status:: ready

#### Acceptance Criteria

- Lock data carries a `role` (`cli/index`, `runtime/lane`, `cli/validate`, `cli/edit`, `cli/worktree`) and the waiting CLI prints the role, PID, age, and last heartbeat while it waits, refreshing every second; there is no two-minute give-up, and Ctrl-C exits cleanly.
  verification:: lock package tests for role round-trip; command test drives the wait loop with a fake holder.
- Runtime identity is the lowercase short hostname plus boot time on every platform, so a lock left by a process before a reboot is reclaimable and a hostname suffix change does not make a lock foreign.
  verification:: identity tests for suffix normalization and boot-time inclusion on each platform.
- Yielding holders poll for priority every second; the priority file is refreshed and expires as today.
  verification:: existing yield tests adjusted to the new interval.

## Requirements

### Runtime process

- MUST be the existing `rzm serve` code path. `rzm serve` and `rzm start` run it attached; `rzm serve --headless` runs it detached with no browser and persists diagnostics under `.rhizome/diagnostics`. The web UI and API stay available in both modes.
- MUST win election on `.rhizome/runtime.lock` (renamed from `watcher.lock`) before writing the manifest, listening, or starting any lane work. A process that loses election MUST exit immediately with the documented exit code, print the winner's manifest on stderr, and MUST NOT touch the winner's manifest, lock, or log.
- MUST write `.rhizome/runtime.json` (renamed from `serve-dev.json`) atomically with mode 0600. The manifest carries instance id, PID, vault path, mode, version, build id, executable path, HTTP host and port and URL, control token, started and updated timestamps, and readiness. The build id combines the version with the executable's size and modification time so development builds are distinguishable.
- MUST refresh the manifest and the global instance registry on a heartbeat and MUST remove both only when the manifest still names its own PID.
- MUST expose `GET /api/v1/runtime` without a token, returning instance id, PID, mode, build id, readiness, lane state, and idle time. Control endpoints (`POST /api/v1/runtime/index`, its job event stream and cancel, `POST /api/v1/runtime/shutdown`, `POST /api/v1/agent/ops/{name}`) MUST require `Authorization: Bearer <control token>` and MUST bind to loopback only.
- MUST treat a live pre-SPEC-0104 serve (one that elected on `watcher.lock`) as the vault's owner: election loses to it, `Ensure` refuses to spawn beside it with `ErrLegacyRuntime`, and `rzm index --status` names it. A new owner removes stale `watcher.lock`, `serve-dev.json`, and hints-log files.
- MUST detect that the index database on disk is no longer the file it opened (deleted, renamed, or replaced). The lane fails the job with `ErrDatabaseReplaced` and the runtime shuts down; a delegating `rzm index` then indexes in-process and restarts a runtime for the new file.
- MUST delete follower mode: the `cachehints` package, the hints tailer, periodic re-election, follower readiness paths, the `leaderFollower` config block, and the `--leader-follower*` flags. A leftover `leaderFollower` key MUST produce the existing unrecognized-key warning naming `rzm init`.

### Discovery, attach, and spawn

- Clients MUST trust a manifest only after a health probe answers with the same instance id and PID within two seconds.
- Spawn MUST be guarded by `.rhizome/runtime-spawn.lock` (same primitive as the index lock, stale after 60 seconds or a dead PID). The spawner MUST release the lease when the manifest becomes probe-ready or the child exits. Losing the lease means wait and re-probe, never spawn.
- The spawned process MUST be the client's own executable, invoked with `serve --headless` and the explicit vault root, with `RZM_SKIP_REPO_DELEGATE=1`, detached from the client's session and terminal: `Setsid` with stdio redirected on Unix; `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW` on Windows. The client MUST NOT wait on the child process handle beyond the spawn budget.
- Default spawn budget is 60 seconds to a probe-ready manifest; full index readiness is not required for attach.
- Ensure MUST be idempotent and safe to call from every entry point listed in US1 and US4; `rzm new-worktree` MUST use it instead of indexing in-process after copying the database.

### Indexing lane and locks

- The lane MUST be the only runtime component that acquires `.rhizome/index.lock`, held per job with a yielding heartbeat whose priority poll interval is one second.
- Job kinds: explicit index (from the control API), boot catch-up, watcher ownership batch, embedding cycle, graph cycle. Explicit jobs MUST preempt background jobs by cancelling their context; a preempted job MUST learn which explicit job displaced it. Background jobs MUST requeue their pending work on cancellation, with one exception: a boot catch-up displaced by an explicit index that then completes successfully is satisfied by that index and MUST NOT run again (it would only make indexed reads unavailable a second time); if the explicit index fails or is cancelled, the catch-up MUST be resubmitted. Two explicit requests MUST coalesce into one job.
- The lane MUST publish progress segments, log lines, and completion to job subscribers, and MUST record the current job kind in the lock's `role` field.
- The runtime MUST NOT unlink the database. `PrepareFreshRebuild` MUST refuse when a live manifest exists for the vault. Any command that replaces the database in-process (`rzm index --rebuild`, `rzm new-worktree`) MUST hold the spawn lease from stopping the runtime until the new database exists, so no client starts a runtime that opens the file about to be unlinked; the database guard MUST record the file identity when it is installed, not at the first job.
- Creating or migrating the unified database MUST be serialized across processes: every write-capable store open holds `.rhizome/db.sqlite.init.lock` (`sqliteutil.LockSchemaInit`) from before its first connection until the schema is validated, so a one-shot command and the runtime it auto-starts can both open a fresh or upgraded vault without one losing the WAL switch or replaying a migration.
- `rzm new-worktree` MUST copy the source database as one consistent snapshot even while any owner (runtime, one-shot indexer, note edit) writes the source. It MAY clone the database and its WAL with a copy-on-write filesystem clone (`clonefile` on macOS, `FICLONE` on Linux) only while it holds the source's SQLite write lock, which freezes the WAL so every page the clone could see half-checkpointed is superseded by a WAL frame; the SHM index is never copied. When cloning is unsupported, crosses volumes, or the write lock is not acquired within a short bound, it MUST fall back to `VACUUM INTO`. It MUST NOT copy database files without either guarantee.
- The automatic boot catch-up job MUST NOT rewrite `.rhizome/config.yml`; only an explicit index persists resolved code and embeddings settings. Background work that rewrites configuration invalidates code-mode connections opened moments earlier.
- In-process fallback and one-shot writers keep the lock protocol: interactive callers request priority, wait without a deadline while reporting the holder, and reclaim stale locks under the identity rules in US6.

### Index command

- `rzm index` MUST ensure a runtime unless auto-start is disabled or `--in-process` is given, then submit the job and render its events with the existing progress bar and `--timings` output. Exit codes MUST match the in-process outcome classes.
- `rzm index --rebuild` MUST stop a headless runtime, rebuild in-process, and restart the runtime; it MUST refuse against an attached runtime.
- `rzm index --status` MUST report runtime and lock state as in US5.

### Code mode

- `rzm agent code serve` MUST keep SPEC-0092's stdio protocol, framing, queueing, cancellation, and shutdown. Catalog operations MUST execute through `POST /api/v1/agent/ops/{name}` with input, read-write flag, and session id; the runtime MUST dispatch through `agentapi.CallJSON` with its live configuration and MUST include a configuration generation so the host can report `code_mode_configuration_changed`.
- Local-only operations MUST keep executing in the host process. Write authority MUST remain explicit per connection and per operation.
- Runtime-executed operations MUST keep the one-shot host's note-state policy: only a cache-snapshot plan receives the runtime's note cache, and only after a forced reconcile with disk completes; live, selected-file, and stateless plans read source from disk. A watcher cache that lags a completed external edit is never handed to a source read.
- Request cancellation from the stdio host MUST cancel the forwarded HTTP request.

### Lifecycle

- Headless idle timeout defaults to one hour and is configurable through `runtime.idleTimeout`; activity means any control-API or agent-operation request or open event stream. Attached runtimes never idle-exit.
- `rzm stop` MUST use the shutdown endpoint, wait for the runtime to exit, and for headless runtimes MUST terminate after a 10 second grace period. Exit means the PID is gone or the runtime released a `runtime.lock` that named it: an exited runtime whose parent has not reaped it still has a PID. `rzm stop --all` uses the global registry.
- Build-identity mismatch: a client MUST replace a headless runtime and MUST NOT replace an attached one.
- A runtime MUST shut down when its vault root is removed.

### Platform and tests

- Spawn, liveness, identity, and lock reclaim MUST have Unix and Windows implementations with tests that run on the existing `ubuntu-latest` and `windows-latest` CI matrix. Detached-spawn tests use the helper-process pattern so they do not require a separately built binary.
- Race coverage MUST include concurrent ensure, concurrent `rzm index`, election with a stale lock, PID reuse (manifest PID alive but instance id mismatch), and priority yield latency.
- The lifecycle MUST be exercised through the real binary on both CI platforms (`tests/integration/runtime`): concurrent `rzm index` on a fresh vault sharing one runtime, stop and restart, recovery after the runtime is killed, auto-start disabled, rebuild stop and restart, an attached serve replacing a headless runtime and a second attached serve exiting 3, `rzm start` taking over a headless runtime and joining an attached one, idle exit, `rzm index` racing `rzm stop`, index freshness after an edit with no explicit index, `rzm new-worktree` from a source with a live runtime, and `rzm stop --all`.
- A runtime MUST report ready on a vault whose configuration turns embeddings off; only a semantic capability that was requested and failed blocks readiness.

### Configuration

- New `runtime` block in `.rhizome/config.yml`: `autostart` (default true), `idleTimeout` (default `1h`). `RZM_RUNTIME_AUTOSTART=0` overrides `autostart` for one invocation.

### Cleanup and documentation

- Delete `pkg/vault/cachehints`, follower code in `pkg/app/bootstrap`, and follower paths in `pkg/app/cli/serve`. Move serve orchestration below Cobra into `pkg/app/cli/serve`. Split scheduler code into lane-owned files under 500 lines each.
- Add a `vault-runtime` subsystem note and skill owning `pkg/app/runtime`, `pkg/app/bootstrap`, and `pkg/app/cli/serve`; update the indexing subsystem note, the index-lock runbook, the LiveRuntime and watcher analysis notes, and package `CONTEXT.md` files in the same change set.
- Update the installed `rhizome` skill's indexing-and-freshness reference (template source under `pkg/app/cli/init/templates/skills/markdown/rhizome`) to describe the runtime and `rzm stop`.

## Open questions

None. Settled 2026-09-17: `rzm stop` may stop an attached runtime because it is an explicit user command; the election loser exits with code `3`, distinct from the `0/1/2` outcome classes and `130` for interrupt.

## Documentation plan

- New subsystem note `docs/reference/subsystems/vault-runtime.md` with matching skill in `.agents/skills` and `.claude/skills`; regenerate the Greptile map.
- Rewrite `docs/reference/guides/Index lock and background coordination runbook.md` around the lane and the runtime.
- Update `docs/reference/subsystems/indexing.md`, `docs/reference/analysis/LiveRuntime (async server bootstrap).md`, `docs/reference/analysis/Watcher design + degraded mode.md`, and the affected `CONTEXT.md` files.
- Update SPEC-0092 for the execution host and SPEC-0036 US3 for the runtime-hosted case.
