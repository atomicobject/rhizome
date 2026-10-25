---
summary: "Browser and public HTTP adapters: provider-aware readiness, shared node reads and edit sessions, and bounded runtime ownership."
reference-kind: guide
last-verified: 2026-10-25
code-paths:
  - pkg/app/web
tags: [subsystem/web]
code-anchors:
  go:
    - label: subsystem.web.guidance
      ref: glob:pkg/app/web/**/*.go
---

# Web and HTTP guidance

## Scope

`pkg/app/web.NewServer` composes explorer, public query, configured view, edit-session, and event routes around an explicit vault, metadata provider runtime, and available live capabilities. It owns HTTP admission and response behavior. Domain reads, query execution, source mutation, and persistence retain their existing owners.

## Design constraints

- Typed node and relation reads use request-scoped `noderead` services. GraphQL uses the shared application query engine and recipe registry. Counts and public type summaries use their bounded read contracts; handlers do not rebuild ontology projections or scan the catalog per result.
- Readiness follows the selected provider and complete publication proof. Raw-only exact reads may use their admitted immutable provider-current fact bundle. Broad, fragmented, workspace, relation, and semantic reads wait for their required publication. Old ready flags alone do not certify current data.
- Server construction validates and retains the explicit metadata runtime. Every read scope receives that runtime so root capabilities follow the provider, including non-Markdown formats. Missing dependencies produce structured errors or retryable readiness responses.
- Live operations consume one runtime snapshot containing its matching vault definition and configuration. Runtime-free paths assemble an explicit snapshot. Read fallbacks use existing read-only stores and never create, migrate, repair, or rebuild an index.
- File and rendered-content routes admit only resolved paths within the resolved vault root. Ignore policy applies to both an authored symlink path and its target. Validate the application host before dispatch; isolated HTML viewing retains its separate origin and nonce contract.
- Source edits go through the shared edit-session and source-transaction owners under a real vault lease. Preview, rebase, commit, recovery, and idempotency receipts preserve conflict outcomes and later external edits. Browser backups are recovery input, not write authority.
- Event streams are invalidation signals. Preserve subscribed refs while tracking canonical relocated identity, advance versions after publication, and refresh affected panes and collection data after commits. Provider selection governs eligible events; filename assumptions do not exclude supported formats.
- Browser diagnostics batches (`POST /api/v1/diagnostics/client`) pass the global host and origin checks, are size-bounded and strictly validated, and become one `web.client` event each. Without a recorder or with persistence disabled they are accepted and discarded.
- Server-owned handles and subscriptions have one cleanup owner. Cancellation reaches reads and background work. A failed construction releases only resources that construction acquired.

## Must-dos when changing this subsystem

Exercise public HTTP routes with real fixture stores and configured providers. For editing or event changes, cover source bytes, readiness, canonical refs, receipts, and refresh behavior. Run the browser checks for the affected user flow and the repository's web end-to-end gate before delivery.

## Review checklist — problems to catch

- Direct catalog or file reads duplicating `noderead` hydration and overlay semantics.
- Writable fallback constructors on query routes, mixed configuration snapshots, or hidden provider initialization.
- Partial publication accepted as complete readiness, or stale source rows admitted by schema flags.
- Lost cancellation, leaked handles/subscriptions, or cleanup of borrowed resources.
- Web-only mutation logic, unbounded response bodies, or failure to refresh canonical node identity after source changes.

## Key files

- `pkg/app/web/server.go` and `handlers.go` for composition and HTTP adapters.
- `pkg/app/web/public_api.go`, `ontology.go`, and `node_workspace.go` for shared query and node reads.
- `pkg/app/web/ontology_sessions.go`, `ontology_commit_receipt.go`, and `node_events.go` for edit and refresh boundaries.
- `pkg/app/web/CONTEXT.md` for detailed route, origin, readiness, and browser contracts.

## Related docs

[[noderead]], [[graphql-query]], [[views]], [[ontology]], [[vault-runtime]], and [[stores]].
