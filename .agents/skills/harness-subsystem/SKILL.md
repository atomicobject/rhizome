---
name: harness-subsystem
description: Use when implementing, modifying, or reviewing the coding-agent harness under pkg/harness (Codex app-server and Claude Code stream-json drivers, sessions, approvals, one-shot generation) or its consumers' harness wiring. Loads harness design constraints and review checklist.
---

# Coding-agent harness subsystem

## Goal

Change `pkg/harness` and its drivers without breaking the credential, process-ownership, approval, or protocol-isolation contracts.

Read `docs/reference/subsystems/harness.md` first; its constraints are normative for this skill.

## Load-bearing rules

1. The vendor CLI is the credential: spawn it, inherit the user's login, never read or forward tokens, never override `HOME`.
2. One child process per session, owned by the session; `Stop` denies pending approvals, kills with a bounded force-kill, closes `Events()`. `Status` and `Generate` spawn their own short-lived processes.
3. Protocol never leaks: consumers see `harness.Event` kinds and phase errors only. Unknown messages become diagnostics and never abort a turn.
4. Approvals block until `Respond`; nothing auto-allows because the repository is trusted.
5. `SendTurn` is synchronous and `Events()` must be drained concurrently; a second turn during a running one is an error.
6. Zero `SessionOptions` means repository-default behavior; new fields must be plumbed through both drivers with per-field transcript tests.
7. Tests use `harnesstest` fakes and golden transcripts recaptured from the CLI; live tests stay behind `RHIZOME_HARNESS_LIVE=1`.

## Pre-handoff checklist

- [ ] No consumer decodes protocol JSON or spawns a CLI directly
- [ ] `Stop`, context cancellation, and transport EOF leave no child process and no pending approval
- [ ] New options honored by both drivers or documented as an exception
- [ ] Driver `doc.go` and the subsystem note record the CLI version verified against
- [ ] `go test -race -tags fts5 ./pkg/harness/...` passes; live test run and recorded when the protocol or CLI version changed
- [ ] `make check` before commit
