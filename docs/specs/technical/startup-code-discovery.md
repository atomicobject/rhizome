---
type: TechnicalSpec
id: SPEC-0095
aliases: [SPEC-0095]
summary: "Teach code-mode selection at startup and fetch selected operation contracts in one call."
spec-status: active
last-updated: 2026-11-05
---

# Startup code discovery

## Summary

Session startup provides the compact live code-operation catalog and an obvious path to batched contract discovery and managed execution. The base Rhizome router explains when code mode helps; its reference teaches execution; live operation discovery owns exact schemas.

## Goals

- Let an agent recognize useful operations without a separate catalog lookup.
- Make startup plus one selected discovery call sufficient to write a basic composed script.
- Preserve minimal startup and avoid duplicated schema documentation.

## Requirements

- Every successful agent start adds the compact catalog's operation names, JavaScript methods, summaries, effects, version hash, and describe/execute examples. Preserve existing session and CLI-discovery fields. Derive metadata from the existing code catalog without full schemas or additional vault/index work.
- Startup also reports an additive `runtime` connection check, bounded to 500 ms and run alongside context assembly. Report `connected` with the runtime's readiness, `starting` when an owner or spawn is in progress, `access_denied` with actionable permission guidance, `disabled`, or `unavailable`. Preserve the session and context when connection verification fails. The check may initiate the existing auto-start path but never waits through its full spawn budget, opens the index, or runs a verification query.
- The base router explains code mode's value for evidence across files, dependent queries, and filtering large responses; it points to the detailed reference before first use. AGENTS.md routes through that skill. A single simple call may use the CLI.
- The detailed reference teaches selecting operations from startup, fetching unfamiliar contracts together, and executing with the provided `rzm` namespace. It explains operation outcomes versus payload schemas, session reuse, evidence qualifications, and the separate need for live ontology discovery when query fields depend on the vault.
- Selected ontology discovery accepts one type or a set of types, and optional bounded related-type expansion. Return whole definitions, root arguments, included and unexpanded type names, and explicit limit metadata. Depth, schema bytes, and output-type count bound expansion; cycles and repeated dependencies do not repeat definitions. Full unselected schema discovery remains available.
- Exact operation schemas and examples remain live discovery's responsibility. Skills do not duplicate them as an authoritative registry. Existing CLI discovery and managed execution contracts remain available.

## Verification

Check catalog agreement and additive startup output, preserve minimal-runtime tests, and execute startup followed by one batched describe and a composed script against a disposable vault. Measure startup catalog size and local startup latency. Regenerate installed guidance, verify repeated init stability, run required repository checks, inspect the complete diff, and obtain fresh independent review.

## Non-Goals

New operations, automatic indexing, full schemas in startup, task-specific operation prediction, new caches, a new model evaluation campaign, main merge, or release.
