---
type: TechnicalSpec
id: SPEC-0115
aliases: [SPEC-0115, code-mode-skill-scripts]
summary: "Code-mode typed reads run in the vault runtime behind a disk-checked freshness barrier, view runs can omit the capability catalog, saved scripts take a JSON input, and skill authors learn to ship scripts for the work their skills govern."
spec-status: active
last-updated: 2026-10-04
---

# Code-mode skill scripts

## Summary

Skills brief agents faster when they ship code-mode scripts that page, deduplicate, and shape typed reads into one compact result. Three costs got in the way. Every `ontologyQuery`, `queryRecipe` run, and `view` call in code mode built a one-shot runtime and refreshed the whole projection: about one second each, serialized under an exclusive lock, even when the vault runtime could answer the same query in milliseconds. `view` runs returned the per-field capability catalog, often most of the response. Saved scripts had no supported way to take arguments.

This spec refines [[persistent-agent-code-mode|SPEC-0092]] and the code-mode routing in [[vault-runtime-coordination|SPEC-0104]]. Typed reads move to the runtime only when the runtime can show its read models match the notes on disk; otherwise the host keeps its in-process refresh.

## Goals

- Typed reads in code mode cost tens of milliseconds and overlap under `Promise.all`.
- A read issued after a completed write observes it, whichever host serves the read.
- Agents can read view rows without the capability catalog.
- Saved scripts take explicit JSON input.
- Skill authors know to ship scripts for the activities their skills govern.

## Non-Goals

- Speeding up `rzm ontology query` or other one-shot CLI reads.
- A Briefing-style agent operation, semantic score cutoffs, or reducing the watcher's per-change processing cost.

## Requirements

### Typed reads in the runtime

- The tool catalog MUST declare which calls of a local-override operation the runtime may serve: every `ontology_query`; `query_recipe` with `op: run`; `view` with action `list`, `show`, `validate`, or `run`. A call that supplies `path` MUST stay in the host, because the path resolves against the host's working directory.
- The host MUST forward admitted reads under shared connection access so independent reads overlap. The runtime MUST serve them through the same workflows the host runs, over its open index and a schema cached by source hash, and MUST keep refusing other local-override calls with `code_mode_local_only`.
- Before an `ontology_query` or recipe run, the runtime MUST show its projection is current: compare every selected note on disk with the persisted metadata rows by size and modification second, when size and second match, compare the content hash once per exact stat (nanosecond time and size) so a rewrite that keeps both is caught however old it is, and treat created and removed notes as changes. A pending resync, a pending configuration, ignore, or schema change, and a stored note that discovery no longer returns while its file remains, MUST also count as waiting. Waiting work MUST go to one watcher batch that starts after the call, with throttling lifted for changed notes; afterward nothing may still be waiting. The persisted ontology state MUST also be ready, match the current schema hash and materialization version, and match the metadata notes hash.
- When the runtime cannot show this within its budgets (two seconds for index readiness, ten for the watcher batch), while a boot catch-up or explicit index runs, when it cannot confirm semantic readiness for a query that needs it, or when no runtime is usable, the host MUST serve that call in-process with its existing refresh. The runtime signals this with an internal outcome the host consumes; generated clients never see it.
- After a connection makes a call that can write, its typed reads MUST stay in-process for the rest of the connection.
- View reads keep their persisted-projection semantics on either host.
- Results, `extensions.typedRoots`, errors, and exit codes MUST match the in-process path.

### View output

- `view` runs MUST accept `omitCapabilities: true` in code mode and `--omit-capabilities` on `view run`, which drop only the `capabilities` field. The default response is unchanged.

### Script input

- `rzm agent code execute` MUST accept `--input '<json>'` or `--input-file <path>`, not both. The value MUST be one JSON value of at most 1 MiB, rejected before Node starts when invalid. Scripts read it as `input`, which is `null` when neither flag is given.

### Guidance

- The `rhizome` skill's authoring guidance MUST teach shipping scripts for the activities a skill governs, with principles and one generic example rather than a mandatory checklist, in a reference the authoring reference routes to. The code-mode reference MUST document `input`, and the views reference MUST document `omitCapabilities`.

### Readable information design

- Skill-authoring guidance MUST start from the user's questions, representative readable notes, relationships, context scripts, and views together. New structured fields or workflows need a concrete authorized consumer; possible future usefulness is insufficient.
- Guidance MUST preserve each record type's responsibility and distinguish captured possibilities, proposed actions, accepted work, and recorded decisions. Missing optional information MUST NOT imply an obligation merely because a script can detect it.
- A singular section-backed field MAY declare `@display(role: SUMMARY)`. Its body supplies the compact summary used by native views, previews, and bundled views, while typed GraphQL retains the section object and exposes its `content`. Existing scalar summaries remain supported. Indexed preview reads MUST retain their existing no-source-read boundary.
- Bundled views and the generic skill example MUST distinguish declared validation or workflow rules from contextual observations such as optional missing information, age, and relationship counts. Context alone MUST NOT be presented as required action.
- Typed hover cards MUST emphasize identity, summary, and schema-selected context, omitting duplicate tag chips and the file path/date footer. The title retains the path as secondary hover text.
- A disposable migration of the DAI IP workspace MUST demonstrate readable summaries and attribution, preserve identities and exact relationships, and remove duplicated opportunity/idea action bookkeeping. Initiatives retain accepted action ownership. Unique source text and historical proposals MUST survive in readable content without promotion to commitments.

## Verification

Measure the same workloads on a disposable copy of a real vault before and after: one small query, four parallel queries, a nested subject query, a typed briefing, a view run, and a read issued one millisecond after an external write. Cover catalog admission, host fallback and write tracking, the disk check for same-size edits and created and removed notes, script input, and regenerated skill guidance.
