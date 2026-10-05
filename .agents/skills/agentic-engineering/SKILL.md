---
name: agentic-engineering
description: "Use to classify, specify, plan, implement, verify, align, reconcile, or close engineering work in this repository. The phase is the first argument."
argument-hint: "[phase] [effort-or-spec path]"
user-invocable: true
---

# Agentic Engineering

Choose the phase, then start with its one reference; load another only when that reference names it. The phases are entry points into one task, not mandatory stops or a checklist to run in full. Team policy lives in `docs/engineering/`; the precedence is the user's request, then those documents, then this skill. Compose with the installed `rhizome` skill for sessions, retrieval, validation, and safe mutations.

| Phase argument | When | Load |
| --- | --- | --- |
| (none) or `classify` | Start, resume, or unclear work | `references/workflow-state.md` |
| `specify` | Define or revise intended behavior | `references/specification.md` |
| `effort` | Freeze a bounded execution slice | `references/effort-setup.md` |
| `plan` | Design approved work | `references/planning.md` |
| `implement` | Execute an approved plan, a clear local task, or a bug fix | `references/implementation.md` |
| `gates` | Gather repository verification evidence | `references/quality-gates.md` |
| `align` | Check delivered work against its contract | `references/alignment.md` |
| `reconcile` | Backport delivery truth into durable docs | `references/reconciliation.md` |
| `compound` | Turn repeated friction into leverage | `references/compounding.md` |
| `finish` | Close an effort | `references/closure.md`, then `references/closure-report-contract.md` for delegated passes |

Separate skills exist only for distinct workflows: `foundation-review` for a plan-requested pause on formative decisions, and `ingest-transcript` for provenance-bearing source material.

## Authority

- The human owns intent, scope, plan approval, and completion truth; the effort and spec records carry them.
- An approved plan is authorization across phase transitions, review fixes, and fresh sessions: resolve routine implementation choices yourself and complete every step it covers. Stop only for a material decision outside that authority, a destructive action, or a genuine scope change, and when a stop comes from this skill, say which reference caused it.
- Reuse current scope and evidence across phases. Refresh the parts whose source, revision, or recorded execution changed; do not repeat retrieval or checks only because the phase or agent changed.
- Closed (`complete` or `archived`) efforts are historical record. New effort-driven work against delivered scope gets a fresh effort; a clear local fix remains local.
