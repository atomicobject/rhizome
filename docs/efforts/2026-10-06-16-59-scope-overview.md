---
type: EffortNote
id: EFF-2026-10-06-16-59
aliases: [EFF-2026-10-06-16-59]
name: All notes and display group Overview
created-at: 2026-10-06T20:59:54Z
status: active
summary: "Deliver SPEC-0117: one scope Overview for All notes and display groups, Briefing for activity at every level, a workspace view mount, a public shape aggregate endpoint, and per-block loading."
plan-approved-by: Drew Colthorp
governing-specs:
  - "[[scope-overview]]"
---

# All notes and display group Overview

## Scope

Implement [[scope-overview|SPEC-0117]] end to end on branch `feat/scope-overview`, stacked on the spec branch (PR #4), then open a pull request. Includes the platform pieces the spec requires: the `workspace` mount kind, the shape aggregate endpoint, a capped recent-notes read, the `openSearch` kit host action, per-block loading for the scope views, and removal of the All notes graph page.

Excluded: changes to the type and interface Briefing, Trace, and Sections beyond what the spec names; any speed work on the Explorer graph.

## Spec Set (Frozen)

- [[scope-overview|SPEC-0117]], revision `2b9e99f5` (2026-10-06), all stories and requirements.

## Stories In Scope (Frozen)

All of SPEC-0117 US1 through US7. [[group-views-and-view-platform|SPEC-0111]], [[unified-view-contract|SPEC-0110]], [[view-preferences|SPEC-0114]], and [[type-collection-views|SPEC-0112]] are governing constraints; this effort updates SPEC-0111, SPEC-0110, and [[notes-workspace-shell|SPEC-0090]] US7 as the spec's documentation plan requires.

## Contracts fixed before delivery

These are settled so the batches can proceed in parallel.

**Workspace mount.** `viewconfig.MountKindWorkspace = "workspace"`, a mount with no subject fields, matching only the workspace target. Concrete context `{ "kind": "workspace" }` everywhere a view context is validated, canonicalized, or routed (Go `ViewContext`, preference scope, TS `ViewContext`, kit, frame bridge). The catalog exposes one target `{ kind: "workspace", name: "" }` whose standard choices are the bundled `workspace.briefing` then `workspace.overview`, default Briefing. A workspace target offers no built-in Overview.

**Bundled definitions.** In `web/bundled-views/group/`: `overview.yaml` (`group.overview`, mount `group: "*"`, order 0, entry `overview.tsx`), `workspace-overview.yaml` (`workspace.overview`, mount `workspace`, entry `overview.tsx`), and `workspace-briefing.yaml` (`workspace.briefing`, mount `workspace`, entry `workspace-briefing.tsx`). `group.overview` takes the built-in Overview's place among group choices; the built-in navigation list remains only as the fallback when the catalog cannot load, labeled "Types".

**Shape endpoint.** `GET /api/v1/ontology/shape?parts=members,links,folders` (any subset; default all). Counts exclude embedded types. Response:

```json
{
  "rebuilding": false,
  "totalNotes": 2515, "typedNotes": 1037, "untypedNotes": 1478, "ambiguousNotes": 38,
  "members": {
    "types": [{
      "name": "Idea", "kind": "type", "count": 22, "issueCount": 0, "lastChanged": 1789693632,
      "lifecycle": { "field": "stage", "values": [{ "name": "open", "count": 20 }] },
      "gaps": [{ "field": "nextStep", "empty": 5 }],
      "targetSets": [{ "types": ["Opportunity", "__untyped__"], "records": 10 }]
    }],
    "interfaces": [{ "name": "...", "count": 0, "issueCount": 0, "lastChanged": 0, "lifecycle": null, "gaps": [], "implementors": ["..."] }],
    "untyped": { "count": 1478, "links": 5096 }
  },
  "links": { "pairs": [{ "a": "Idea", "b": "Opportunity", "links": 33, "relationLinks": 27, "plainLinks": 6,
                         "fields": [{ "type": "Idea", "field": "opportunities", "count": 27 }] }] },
  "folders": { "rows": [{ "folder": "Notes", "total": 1083, "untyped": 825,
                          "typed": { "Person": 197 }, "untypedLinksTo": { "Meeting": 211 } }] }
}
```

`a`/`b` are concrete type names or `__untyped__`, with `a <= b`; `a == b` reports links among one type's own records. A pair counts once however many fields connect it; a relation link is a field whose declared target is the other end's type or an interface it implements, and broad fields such as `related: Note` count as plain. `targetSets` groups a type's records by the set of other types (and `__untyped__`) each links to. Interface lifecycle and gaps come from the interface's own profile over its implementors' records.

**Recent notes.** `GET /api/v1/ontology/types/__all__?limit=N` returns the N most recently changed notes, untyped included, newest first, with the existing item shape.

**Kit.** `openSearch({ folder })` posts `rhizome:open-search`; the host opens a project search tab with that folder filter. `useViewContext()` can return `{ kind: "workspace" }`.

## Spec Coverage Checklist

- [ ] US1 group map, Matrix, outside ring, unused relations, Trace on relation edges
- [ ] US2 member table
- [ ] US3 All notes map with collapsed groups and member table
- [ ] US4 untyped-by-folder block, coverage line, folder action through search
- [ ] US5 All notes Briefing as default; graph and Most linked removed
- [ ] US6 group Briefing trimmed; Declared unused, Outside, Guide and views on Overview
- [ ] US7 frame first, per-block loading and failure, every scope, rebuilding state
- [ ] Requirements: shape endpoint and budget, recent-notes limit, workspace mount, built-in fallback relabel, preferences, accessibility, density, performance check

## Plan

1. **Backend platform** (owner: backend worker, worktree `.claude/worktrees/so-backend`, branch `so/backend`). `MountKindWorkspace` through viewconfig validation and matching, catalog targets (the workspace target and its default), userstate scope canonicalization, custom view context validation and serving, the shape endpoint with tests against the integration fixture vault (relation, plain, mixed, broad-field, interface, embedded, self pairs, folder rollup), the `__all__` `limit`, OpenAPI updates. Exit: `make check-fast`, focused `go test` for touched packages, an endpoint latency check on a 2,500-note fixture under 200 ms.
2. **Web platform** (owner: web worker, `.claude/worktrees/so-web`, `so/web`). TS `ViewContext` workspace, kit types and `openSearch`, frame bridge and `NotesShell` routing for workspace views and `open-search`, preference scopes, HomeTab selecting the workspace target for All notes, the built-in group Overview no longer a choice and relabeled "Types" as fallback, deletion of the All notes graph path (`HomeGraphLayout` all mode, `NotesAllHome`, and their tests) while keeping what the type fallback still uses, regenerated API types. Exit: `make check-fast`, web unit tests.
3. **Scope views** (owner: views worker, `.claude/worktrees/so-views`, `so/views`, starts after batch 1's contract lands, against fixtures until then). `overview.tsx` with modules for the shape client, plain-TS derivations (map nodes and edges, collapse, outside ring, unused relations, member rows, linked share, folders), a deterministic SVG map with keyboard focus, the Matrix, the member table, scope-only blocks (folder block and coverage; Declared unused, Outside the group reusing the existing list, Guide and views), per-block loading and errors, preferences; `workspace-briefing.tsx` and the trimmed group Briefing with per-block loading; YAML definitions. Exit: unit tests for derivations and components, `make check-fast`.
4. **Integration, docs, and evidence** (owner: coordinator). Merge batches into `feat/scope-overview`, run `make check`, browser checks at 1280 and 1440 on the fixture vault, the performance check, dogfood on a real vault, `make web-e2e` on a unique port, documentation (SPEC-0090 US7 superseded, SPEC-0110 workspace mount, SPEC-0111 Briefing and Overview, bundled views README, subsystem and CONTEXT notes, views reference templates, CHANGELOG), then an independent review and a pull request.

## Plan Approval

Drew Colthorp, 2026-10-06T20:55Z in conversation: "Yes. Then implement the spec." That instruction authorizes implementing SPEC-0117 at revision `2b9e99f5`; this plan is its decomposition and adds no scope.

## Original Intended Delivery

SPEC-0117 implemented on `feat/scope-overview` with a pull request, all gates green.

## Actual Delivered

## Execution Notes

- 2026-10-06: Effort opened after the GPT-6.1 Sol review fixes landed in the spec (`2b9e99f5`).
- 2026-10-06: Backend batch 1 implemented in `so/backend`: workspace mounts, scope view slots, shape aggregates, bounded recent notes, OpenAPI, and generated API types. The views worker must replace the no-op `overview.tsx` and `workspace-briefing.tsx` entries; they exist only because the bundled loader checks entry existence. Embedded typed endpoints and fields are excluded; independent host document links remain plain, matching the existing index. An independent review found no remaining contract bugs after correcting published validation issue counts and metadata/ontology publication witnesses.
- Backend verification passed: `cd web && npm ci && npm run generate:api`; `GOCACHE=/Users/colthorp/Library/Caches/go-build make check-fast`; `GOCACHE=/Users/colthorp/Library/Caches/go-build go test -tags fts5 -mod=vendor ./pkg/ontology/viewconfig ./pkg/app/views ./pkg/app/userstate ./pkg/ontology/noderead ./pkg/ontology/readmodel ./pkg/anchors/sqlite ./pkg/app/web`; `GOCACHE=/Users/colthorp/Library/Caches/go-build NO_WEB=1 make build`; `GOCACHE=/Users/colthorp/Library/Caches/go-build make check` (all Go packages and 1,433 web tests passed). No web end-to-end gate was run in this backend batch.
- Shape latency: `GOCACHE=/Users/colthorp/Library/Caches/go-build go test -tags fts5 -mod=vendor ./pkg/app/web -run TestOntologyShapeWarmLatency2500Notes -count=1 -v` is the repeatable measurement command. The focused shape regression run measured 39.232466 ms mean and 50.194250 ms maximum across ten warm HTTP-handler requests, including JSON encoding, on a synthetic persisted fixture with 2,500 notes, 15 types, and 17,500 distinct note pairs. No note hydration or new write path is used.
- `RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm validate` completed all configured checks: identifiers and broken links passed; ontology reported three existing empty sections in this active effort (`actualDelivered`, `deviations`, `compoundingFollowUps`). These are pre-existing coordinator closure work, outside backend batch 1. The sandboxed validation prerequisite attempt failed; the approved unsandboxed retry prepared the projection successfully.


## Deviations

## Closure Checklist

- [ ] All stories and requirements delivered or deviations recorded
- [ ] Gates recorded with commands and results
- [ ] Documentation plan done
- [ ] PR opened and linked

## Compounding Follow-ups

## Status

Active.
