---
aliases:
    - SPEC-0021
id: SPEC-0021
last-updated: 2026-05-19T00:00:00Z
spec-status: archived
summary: Defines how delivery friction becomes durable future capacity in the spec-driven loop.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Compounding work

## Summary

Compounding work turns repeated delivery friction into future capacity. It is the final core phase of the spec-driven loop: after backport reconciles reality into specs and docs, `compound` captures what the cycle taught the team about validation, evidence, skills, tools, access paths, review rules, and process.

## Goals

- prevent repeated human mediation from staying private
- surface agent blockers such as missing design access, runtime evidence, setup paths, or safe approval boundaries
- convert repeated corrections into the smallest durable improvement
- keep future-capacity work visible in the owning effort

## Non-Goals

- replacing `backport` for spec/doc reconciliation
- creating platform wishlists without a concrete friction trigger
- forcing every one-off annoyance into process work
- granting agents external access without explicit safety boundaries

## Requirements

### Must

- Run `compound` before an effort is marked complete.
- Record compounding triage in the owning effort's `Compounding Follow-ups` section.
- Tick the compounding item in `Closure Checklist` only after the effort's friction has been reviewed for durable follow-up.
- Treat repeated, validation-blocking, trust-reducing, or definition-blocking friction as compounding input.
- Classify each accepted compounding item by surface: definition, design, delivery leadership, implementation, verification, workflow, architecture, observability, safety, or tooling.
- Name the missing support: what the agent, reviewer, designer, delivery lead, verifier, or developer could not see, run, verify, access, infer, or safely decide.
- Choose the smallest durable home: test, lint, script, template, skill, spec, reference doc, review rule, PR evidence rule, safety gate, or follow-up effort.
- For missing capabilities, record the smallest useful enablement and safety boundary. Examples include Figma/design access, logs/metrics/traces, preview environments, seed/reset scripts, workboard/PR status, evidence bundles, and approval gates.
- Keep one-off local friction out of the system unless it predicts future drag.
- Use `backport` instead when the work is known reconciliation of delivered behavior into specs, references, code docs, skills, or effort deviations.

### Should

- Prefer Simple improvements that fit the next cycle.
- Turn objective repeated rules into executable checks where practical.
- Turn judgment-heavy repeated feedback into examples, specs, skills, or review rubrics.
- Use compact failure summaries and agent-facing command contracts when tool output is too noisy.

### May

- Create a follow-on effort when the durable fix is larger than the current scope.
- Defer capability work when access, security, production data, or client risk requires human approval.

## Open Questions

- whether a future workboard ontology should replace prose follow-up entries once compounding tasks become first-class planning objects
