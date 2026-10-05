---
type: TechnicalSpec
id: SPEC-0091
summary: "Selective discovery and an explicitly generated typed client for bounded file-context and file reads through the existing agent CLI."
spec-status: superseded
successor: "[[persistent-agent-code-mode|SPEC-0092]]"
last-updated: 2026-09-07
code-anchors:
  go:
    - label: agent.code_mode.describe
      symbol: github.com/atomicobject/rhizome/pkg/app/agentcode.Describe
    - label: agent.code_mode.generate
      symbol: github.com/atomicobject/rhizome/pkg/app/agentcode.Generate
aliases:
  - SPEC-0091
  - progressive-agent-code-mode
---

# Progressive agent code mode

## Summary

This pilot contract is superseded by [[persistent-agent-code-mode|SPEC-0092]], which supplies the persistent transport and full task surface. The pilot requirements below are retained as the prior delivery record.

Agents can discover only the supported operations they need and generate a small typed ESM client that invokes the existing CLI. The pilot supports `file_context` and `files`; the existing operation catalog owns eligibility and contracts. Current payloads, warning/ref fields, command behavior and runtime effects survive the adapter unchanged. Minimal startup does not load the code surface.

Drew authorized this narrowed delivery through the coordinator on 2026-09-07. The existing-catalog and thin-CLI boundaries were accepted; the earlier proposed indexed-query prerequisite was explicitly removed.

## Goals

- Keep initial discovery small and operation descriptions selective.
- Provide concrete input/output declarations and bounded process orchestration without another runtime.
- Preserve evidence and expose failures, effects and setup costs honestly.

## Non-Goals

Semantic writes, persistent service, full-catalog typing, automatic init/index generation, a new passive query API, arbitrary query-result type generation, agent evaluation runs, and an assertion of measured model benefit. Coverage uses existing CLI/GraphQL and is not presented as a typed recipe operation in this pilot.

## Requirements

`agent code surface`, `describe`, and `generate` use runtime-free composition. Describe/generate require explicit supported operation selection and never expand to a full catalog implicitly. Schema, declaration and client eligibility derive from the selected metadata attached to `ToolDescriptor`; it is not a second dispatch registry.

Generate writes only explicitly requested task artifacts. Artifacts contain no notes, session IDs, credentials or provider state. An immutable hash directory and manifest publication prevent partially generated output from being advertised as complete. A client verifies its selected contract against the live executable before task calls and reports stale artifacts without regenerating them.

The client accepts the explicit absolute executable and vault root, executes argument arrays without a shell, disables repo delegation for that selected executable, and uses only permitted flags. `file_context` fixes link-target behavior to `never`; `files` restricts inputs to explicit paths, bounded reads and depth zero. No generic command escape hatch is exposed. The host harness still owns authorization and sandboxing of model-written code.

Each result preserves the parsed CLI payload, exit status and stderr. Nested domain errors, freshness, omissions, refs and warnings remain visible. Nonzero exit with JSON is not discarded; malformed/oversized output is an explicit transport failure. Generated declarations must be concrete for known fields and use `unknown` only for genuinely open values. They must not promise validation or type precision that the runtime lacks.

Deadline, cancellation, bounded concurrent calls, output limits and idempotent close must reap owned children. No automatic retry or transaction semantics are implied. Existing source-read/index/cache/session effects are disclosed; this client does not promise a new passive query runtime. All tests use disposable fixtures and preserve note/code sources.

## User Stories

### US1 - Discover only selected contracts

- id:: ^SPEC-0091-US1
- summary:: An agent can select a bounded operation without loading all schemas or starting runtime services.
- status:: ready

#### Acceptance Criteria

- Surface lists only pilot operation identities, summaries and effects; describe emits complete selected schemas and no unselected contract. Unknown or empty explicit selections fail. ^SPEC-0091-US1-AC1
- Existing minimal start is unchanged and discovery/description do not create an index, session store, watcher or provider runtime. ^SPEC-0091-US1-AC2
- Descriptions and client eligibility derive from the existing catalog and deterministic selected contract hashes. ^SPEC-0091-US1-AC3

### US2 - Generate and use a typed CLI client

- id:: ^SPEC-0091-US2
- summary:: An agent can generate a selected client in a writable task directory and use existing CLI reads from host code.
- status:: ready

#### Acceptance Criteria

- Generation produces executable ESM, adjacent concrete declarations and a manifest deterministically; identical regeneration reuses complete artifacts and cannot replace unrelated files. ^SPEC-0091-US2-AC1
- Real Node invocation reaches the existing CLI with explicit executable/vault, safe argument arrays, restricted inputs and session reuse; task JSON, warnings, refs and stderr remain available. ^SPEC-0091-US2-AC2
- A stale selected contract fails before task calls without automatic generation; artifacts do not embed session, note contents or credentials. ^SPEC-0091-US2-AC3

### US3 - Bound failures and establish comparison readiness

- id:: ^SPEC-0091-US3
- summary:: A caller can cancel or close work and distinguish partial domain evidence from transport failure.
- status:: ready

#### Acceptance Criteria

- Deadline, cancellation, concurrency and output bounds are tested with real child processes; close is idempotent and no owned child survives completion or cancellation. ^SPEC-0091-US3-AC1
- Domain failure payloads survive nonzero exit, malformed output fails explicitly, and adapters retain structured warning/ref/truncation fields without inventing evidence. ^SPEC-0091-US3-AC2
- Deterministic base-context and trivial lookup examples support A's later shared comparison; requirement coverage uses the same existing recipe access across arms, labels any hybrid honestly, and counts generation/discovery and index/cache side effects. No independent model-run matrix is launched. ^SPEC-0091-US3-AC3
