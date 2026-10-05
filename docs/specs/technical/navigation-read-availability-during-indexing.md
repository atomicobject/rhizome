---
type: TechnicalSpec
id: SPEC-0097
summary: "Defines note and node read availability while metadata ownership and ontology indexes refresh, including startup gating and cross-process behavior."
spec-status: active
last-updated: 2026-09-08
aliases:
  - SPEC-0097
  - navigation-read-availability-during-indexing
---

# Navigation read availability during indexing

## Summary

Existing notes must remain available to the browser, public GraphQL API, CLI, and MCP readers while background indexing refreshes unrelated metadata or ontology state. Freshness transitions may mark a global projection as incomplete, but they must not turn an eligible durable row for an unaffected requested path into a false absence.

Startup gating must reflect whether a request has any usable read model. A warm server with a previously published index serves existing-note navigation while later indexing stages continue. A cold server may report initialization until the minimum source and indexed state needed for the requested read exists.

## Goals

- Preserve the last usable eligible metadata and node projection for unaffected requested paths during ownership reconciliation and background publication.
- Converge changed, deleted, excluded, and provider-transitioned paths without returning stale or wrong-provider content.
- Keep warm note navigation independent of unrelated code, embedding, graph, or full-index work.
- Apply the same availability contract to startup indexing, watcher updates, leaders, and followers.
- Make cold and warm behavior measurable through public HTTP requests and real browser navigation.

## Non-Goals

- Waiting for every indexing lane before serving an existing note.
- Hiding consistency defects with additional frontend retries.
- Treating durable rows as authoritative when current ownership or provider eligibility rejects the requested path.
- Redesigning the complete indexing engine or changing the public GraphQL schema.
- Claiming that the ontology catalog's two-transaction publication gap caused the reported null result without a reproducing test.

## Requirements

- A requested note whose durable metadata row remains eligible MUST resolve while an unrelated ownership transition marks global metadata freshness pending.
- Requested-path reads MUST distinguish global freshness from path availability and MUST retain bounded batch behavior.
- A path removed from source, excluded by selection policy, transferred to an incompatible provider, or made otherwise ineligible MUST remain absent after its transition is durably published.
- Failed or canceled background refresh MUST retain the prior usable snapshot for unaffected paths and MUST leave freshness state truthful.
- Successful refresh MUST publish the new eligible set and retire removed or transferred paths.
- Startup and watcher publication MUST order ownership, note metadata, and ontology work so readers never observe a false absence for an unaffected existing note.
- A process with a usable prior index MUST serve existing-note GraphQL navigation without waiting for unrelated indexing stages.
- Early GraphQL execution MUST verify a provider-current durable row for every requested exact note path; a globally open note-read gate alone MUST NOT turn a missing or unpublished requested path into `null` while the complete model is pending.
- Early GraphQL execution MUST require complete readiness for fragmented `node` refs because section and embedded-node identity depends on the ontology catalog and projection.
- The ownership decision for an early exact-path read MUST use the same live selection policy as indexing and MUST change when note includes, excludes, or code ownership configuration reloads.
- A process without usable note state MUST return the established initialization response until the minimum requested-read state exists; the gate MUST remain cancellable and bounded.
- Followers MUST observe the same usable-read transition as the indexing leader through durable state, without process-local readiness assumptions.
- Public HTTP concurrency coverage MUST exercise reads across the real ownership/publication boundary. Package regressions MUST cover unrelated transition, deletion or exclusion, provider transition, successful refresh, failed refresh, and canceled refresh.
- Validation MUST include cold and warm startup timing, immediate graph-to-note navigation during controlled background work, and exact request timing bounds from the tested environment.

## Open Questions

None. The approved implementation plan authorizes routine choices within this contract; evidence that requires a public API, schema, or ownership-boundary change must be recorded as a deviation before proceeding.
