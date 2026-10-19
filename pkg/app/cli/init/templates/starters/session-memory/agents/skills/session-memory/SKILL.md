---
name: session-memory
description: Use when mining this repository's Claude Code or Codex sessions for reusable project knowledge, estimating or resuming a mining run, or checking which models a run will use.
---

# Session memory

This skill turns the agent sessions recorded for this repository into proposed context shards: short, verified facts and rules that later agents should know. Proposals stay local until a person reviews them.

Run every script from the repository root with Node 20 or later. Scripts reach models only through `rzm agent generate` and Jev through `rzm agent evaluate-batch`; they never call a provider directly.

## Choose the provider

Pass `--harness claude` when you are running in Claude Code and `--harness codex` in Codex. Transcript text goes only to that provider and to Jev, which screens passages. Without the flag, the run uses the user's `agent.harness` setting, or else the only provider installed and signed in, and otherwise stops and asks.

## Status

- `node <skill>/scripts/sessions.mjs survey [--since YYYY-MM-DD]` counts the sessions and episodes it found, without reading their content into the reply.
- `node <skill>/scripts/mine.mjs --harness <h>` with no `--budget` prints the resolved model for each role, every provider that will receive transcript text, and the estimated cost. It spends nothing.

## Mine

1. Run `mine.mjs --harness <h>` without a budget and show the person the estimate and the bindings.
2. Ask for a spend limit, then run `mine.mjs --harness <h> --budget <USD>`.
3. Exit 3 means the run stopped at its budget. Rerunning with a new budget resumes it with the same models; finished calls are cached and not repeated. To discard an open run instead, pass `--abandon`.
4. Proposals land in `.rhizome/session-memory/proposals.json`. Summarize them for the person; do not write them into the repository.

Useful options:
- `--since` and `--until` bound the sessions by start date. Later runs continue from each session's cursor, so they read only new turns.
- `--verify-budget N` verifies up to N single-session clusters per run (default 40). `--verify-held N` verifies N clusters that earlier runs held back.
- `--model <role>=<provider>:<model>:<effort>` overrides one role for this run. A local `.rhizome/session-memory/models.json` overrides the shipped `models.json`.

## Not yet available

Review and promotion into `docs/context-shards/`, lookup, maintenance of stale shards, and calibration are planned (SPEC-0121) but not part of this version. Tell the person so rather than improvising them.
