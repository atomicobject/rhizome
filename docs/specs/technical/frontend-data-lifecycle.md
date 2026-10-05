---
type: TechnicalSpec
summary: "Defines identity-keyed server state, cancellation, invalidation, mutation, and visible loading/error/empty contracts for the Rhizome web UI."
id: SPEC-0074
spec-status: active
last-updated: 2026-09-23
aliases:
  - SPEC-0074
---

# Frontend data lifecycle

## Summary

The web UI must never render server data under an identity it was not fetched for. Notes types/interfaces, Explorer paths and graph scopes, Agent sessions, ontology views, validation, and edit-session overlays share one server-state lifecycle: complete query identities, abortable reads, explicit invalidation, and distinct initial-loading, success, empty, background-refresh, and retryable-error states.

TanStack Query owns reusable server state. URL/route, selection, drafts, expansion, pane layout, and display preferences remain local client state. Existing REST, GraphQL, and SSE wire contracts remain authoritative.

## Goals

- prevent late responses from replacing data for a newer selection
- make missing, empty, loading, refreshing, and failed reads distinguishable
- centralize cache identity, cancellation, retry, and SSE invalidation rules
- simplify large workspaces by separating server-state ownership from local interaction state
- preserve canonical NodeRef and pane-generation invariants

## Non-Goals

- adding Redux, Zustand, or a routing library
- changing backend endpoints or public wire formats
- treating SSE payloads as content patches
- moving explicit GraphiQL execution into reusable query state
- redesigning workspace visuals while migrating data ownership

## User Stories

### US1 - Trust Notes type and configured-view results during navigation

- id:: ^SPEC-0074-US1
- summary:: Trust that the Notes list, search, validation, and configured view belong to the selected type or interface.
- status:: satisfied

#### Acceptance Criteria

- A late response for a previous type/interface cannot replace or blank the selected type/interface list. ^SPEC-0074-US1-AC1
- `getOntologyType(typeName)` is the canonical typed-list read; no redundant request is joined solely to recover fields already present in that response. ^SPEC-0074-US1-AC2
- Search results are rendered only for their exact query and selected type identity. ^SPEC-0074-US1-AC3
- Explicit category/type/view navigation resets transient filters or keys them to the destination so prior state cannot create a false empty result. ^SPEC-0074-US1-AC4
- Initial loading, empty success, retryable error, and same-key background refresh are visually distinct. ^SPEC-0074-US1-AC5

### US2 - Trust Explorer selection, expansion, and graph scope

- id:: ^SPEC-0074-US2
- summary:: Trust that Explorer folders, files, suggestions, and graphs belong to the current path and scope.
- status:: satisfied

#### Acceptance Criteria

- Folder, file, graph, suggestion, and path-search reads are keyed by every behavior-changing identity input. ^SPEC-0074-US2-AC1
- A late response cannot replace a newer folder/file/graph/suggestion selection. ^SPEC-0074-US2-AC2
- Collapsing a directory remains a local selection decision and cannot be undone by a late child-list response. ^SPEC-0074-US2-AC3
- File/module selection changes atomically before derived content and graph reads run. ^SPEC-0074-US2-AC4

### US3 - Keep secondary workspaces and mutations coherent

- id:: ^SPEC-0074-US3
- summary:: Keep Agent, status, atlas, validation, configured-view, settings, and edit-session state coherent under concurrency and invalidation.
- status:: satisfied

#### Acceptance Criteria

- Agent session reads cannot overwrite a newer selected session, and its EventSource remains stable while events arrive for that session. ^SPEC-0074-US3-AC1
- Status, atlas/type detail, validation, and configured-view reads use the shared server-state lifecycle and expose retryable failures. ^SPEC-0074-US3-AC2
- Mutations either roll back optimistic state or refetch authoritative state; writes targeting the same resource are sequenced. ^SPEC-0074-US3-AC3
- Edit-session busy state remains true until all queued operations finish; browser-storage failures do not invalidate successful server operations. ^SPEC-0074-US3-AC4

### US4 - Refresh live data through one invalidation contract

- id:: ^SPEC-0074-US4
- summary:: Refresh affected data consistently after vault, node, reconnect, and mutation events.
- status:: satisfied

#### Acceptance Criteria

- One app-level vault event bridge maps public SSE invalidation events and reconnects to typed query-prefix invalidation. ^SPEC-0074-US4-AC1
- Node subscriptions remain keyed by active canonical refs and refresh only affected pane identities. ^SPEC-0074-US4-AC2
- SSE remains an invalidation/refetch trigger, never an alternate content source. ^SPEC-0074-US4-AC3
- Mutations invalidate touched node, type, summary, validation, search, and configured-view identities without refresh nonces. ^SPEC-0074-US4-AC4

## Requirements

### Must

- One application-root `QueryClient` MUST own reusable server state.
- Query keys MUST include every input that changes response meaning: type/interface, path, session, search, view id/state, graph scope, and edit-session fingerprint.
- API read wrappers MUST accept an optional `AbortSignal` and pass it to `fetch` and retry waits.
- The existing typed `ApiError` MUST remain the HTTP failure contract; 4xx responses are not retried.
- Default cache policy MUST use 30-second staleness, five-minute garbage collection, one retry for network/5xx failures, no focus refetch, reconnect refetch, and no retries in tests.
- Different query identities MUST NOT retain or display each other's data. Same-identity data MAY remain visible during background refresh. A read MAY keep showing the previous result for the same subject (view id, node ref, type or interface, graph scope) while a refinement of it (view state, workspace version, edit-session fingerprint) refetches, provided the surface marks the refresh; a different subject MUST start empty.
- Route location MUST have one external-store source of truth rather than mirrored pathname state plus repair effects.
- Server-state mutations MUST invalidate or update typed query keys after success and recover coherently after failure.
- `usePaneStack` request-generation checks MUST remain effective during the migration.
- Tests MUST cover deferred/reordered responses, failure transitions, invalidation, stable streams, and queued mutation state.

### Should

- Query/controller hooks should separate orchestration from large workspace render components.
- Shared loading/error/empty affordances should encode the lifecycle contract consistently without forcing identical visual layouts.
- Query invalidation should be broad enough for correctness and narrow enough that inactive unrelated resources do not refetch.

### May

- Workspaces may migrate in independently reviewable phases as long as mixed old/new ownership does not duplicate authoritative server state.

## Open Questions

None for this implementation slice.
