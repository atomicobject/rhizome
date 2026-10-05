---
name: vault-runtime-subsystem
description: Use when implementing, modifying, or reviewing the vault runtime under pkg/app/runtime, pkg/app/bootstrap, or pkg/app/cli/serve (runtime election, manifest and probe, ensure/spawn, the indexing lane, control routes, headless lifecycle, rzm stop) or a command that attaches to it. Loads vault-runtime design constraints and review checklist.
---

# Vault runtime subsystem

## Goal

Change how a vault's runtime process is found, started, elected, or driven without ever leaving two runtimes alive, trusting a stale manifest, contending on the index lock, or clobbering the database under an open handle.

Read `docs/reference/subsystems/vault-runtime.md` first; its constraints are normative for this skill.

## Load-bearing rules

1. Election decides, the manifest publishes: acquire `.rhizome/runtime.lock` before listening, write `runtime.json` after listening, remove it only as its owner and before releasing the lock; a loser exits 3 and touches nothing.
2. Trust a manifest only after `Probe` matches run id and PID; nobody listening is absent, another process answering is absent, listening-but-unhealthy with a live PID is unresponsive (wait, never spawn).
3. Spawn only under `runtime-spawn.lock`, detached on every platform, with the client's own executable; replace a headless runtime on build mismatch, never an attached one.
4. Inside the runtime only the lane acquires `.rhizome/index.lock`; jobs carry a role, poll priority every second, and background jobs cancel promptly and requeue.
5. Rebuild never runs inside a live runtime.
6. Control routes are loopback and token-gated except health; every authorized request and open stream is idle activity.
7. Readers (`rzm agent`, MCP) only ensure a runtime exists; `rzm index` and code-mode catalog operations execute inside it.

## Pre-handoff checklist

- [ ] Concurrent-ensure and election tests still prove exactly one runtime; race tests run with `-race`
- [ ] No new `indexlock.TryAcquire` outside the lane and election (guard test green)
- [ ] Platform code behind build tags, `GOOS=windows go build ./...` clean, helper-process spawn tests pass
- [ ] Health, manifest, and client agree on every new field
- [ ] Subsystem note, runbook, and package `CONTEXT.md` updated with `last-verified` bumped
- [ ] `go test -race ./pkg/app/runtime/... ./pkg/app/bootstrap/... ./pkg/app/cli/serve/... ./cmd/` and `make check` before commit
