# Web structural audit

Read-only review of `web/` and `pkg/app/web` at `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. Production files were not edited. No builds, tests, browser sessions, or network-dependent commands ran. Reviewed owning web guidance, TypeScript guidance, AGENTS, and compounding lookup (no matching shards). Findings below distinguish confirmed coupling/surplus from behavior changes that need a contract decision.

## W1. Derive edit transport types from the existing generated contract

**Confirmed hidden link; worthwhile small refactor.**

- `web/src/api/types.ts:140` manually repeats every property of generated `OntologyEditOp` (`web/src/api/generated.ts:2072`), and `types.ts:214` repeats the edit snapshot at generated `:2130`. `pkg/app/web/openapi.yaml:3299` is already the authoritative shape and `web/package.json` already has `generate:api`.
- The handwritten operation differs intentionally in only the stronger `OntologyEditFieldValue` discriminated union and the nested expected field. Retain that union; do not widen it to the generated optional-field bag. Derive the rest with `Omit<components["schemas"]["OntologyEditOp"], "fieldValue" | "expected">` plus the narrowed fields. Likewise derive snapshot with its refined `ops`.
- Callers throughout editing, staging, table/board reordering, `api/client`, and `useOntologyEditSession` must retain their current type constraints. Current code requires a new edit property to be added in Go/OpenAPI/generated TS and once again in manual TS. Eliminating the final copy makes normal contract evolution visible to the compiler.
- Do not blindly replace `OntologyEditSessionResponse`: generated schema is incomplete and deliberately has additionalProperties, while the handwritten response describes plan/workspace/lineage fields. Completing that separate schema is larger contract work and is not necessary for this cut.
- Verification: frontend typechecks plus `api/client.test.ts`, `useOntologyEditSession.test.tsx`, staged query/state tests, editor tests. No new runtime test needed for a type-only derivation.

## W2. Delete frontend code whose only callers are tests or none

**Confirmed surplus; safest first implementation slice.** Whole-repository symbol search (excluding vendor and documentation) found:

- `web/src/components/workspaceGraph.ts:130` `selectWorkspaceFields`, and `:153` `selectWorkspaceCollections`: definitions only. Remove both and now-unused type imports. The live identity/property rendering consumes graph nodes directly; other selectors in this file are live and must stay.
- `web/src/components/useNotesQueries.ts:56` `useNoteSearchQuery`: definition only. Remove the hook and its `searchNotes` import. Do not remove `searchNotes` itself, which remains used elsewhere.
- `web/src/api/client.ts:682` `getRenderedFile`, `:807` `getOntologyInspect`, `:813` `getOntologyQuerySchema`, `:819` `queryOntology`: each has no live caller; only `api/client.test.ts:626` calls these in the omnibus compatibility test. Remove these four wrappers and just their assertions/imports, retaining transport assertions for live routes.
- `getPublicNodeDetail`, `queryPublicGraphQLOperation`, `nodeHref`, and `workspaceViewHref` are **not dead**: `web/kit` consumes them, even when a search confined to `web/src` suggests otherwise.
- Scope: client types/imports can shrink only where whole-repo references disappear; HTTP server routes and OpenAPI declarations remain supported and are not part of this deletion.
- Verification: typecheck, client and workspaceGraph tests, useNoteTabs/NotesShell tests. Existing live HTTP coverage remains; no test should be deleted merely because it checks request serialization.

## W3. Remove dead runtime mutators and production helpers maintained only for tests

**Confirmed surplus.**

- `pkg/app/web/server_runtime.go:62` `SetNoteEmbeddings` and `:85` `SetCodeEmbeddings` have no callers in the repository. Real read methods select `r.Live.Snapshot()` or immutable fallback fields. Delete the unused mutators; do not delete the corresponding read methods, fallback fields, resource ownership, or live wiring.
- `pkg/app/web/ontology.go:784` `executeOntologyQuery` only forwards to `executeOntologyQueryOperation(ctx, rawQuery, nil, "", nil)` and is called exclusively by tests. Move it to an existing `_test.go` helper file or make tests call the production operation directly. Preserve all current fixture assertions.
- `pkg/app/web/graph.go:386` `buildGlobalGraph` only forwards defaults to `buildGlobalGraphMode` and is test-only. Same treatment. Live global-graph HTTP behavior, cache lifetime, errors, and CLI graph work remain intact.
- Verification: focused `pkg/app/web` suite, including runtime-view, publication, public-recipe and graph/cache integration tests. Exported mutators are application-internal APIs, but compile all dependent packages to prove repo usage.

## W4. One note-target parser, used by routing, panes, and links

**Confirmed hidden link; small semantic review required before combining.**

- `web/src/components/notesRoute.ts:286` splits path, optional document query, and fragment; `web/src/lib/content.ts:88` repeats the same splitting inline in `buildNoteWebHref`.
- `web/src/components/usePaneStack.ts:70-90` declares a second `NoteTarget` and another `splitNoteTarget`, but splits only `#`, with a distinct nullable result and `anchor` vocabulary. `NoteTab.tsx:111` and `:318-320`, plus `useNoteTabs.ts:837`, reconstruct the same target in separate places.
- Extract the existing complete path/query/fragment parser and target serializer to a dependency-light `lib/noteTarget.ts` (or existing `lib/noteDeepLink.ts`). Routing, link creation, and tabs should consume it; adapt the pane's null case at its boundary. Do not make low-level `lib/content` import the React-adjacent route module to share it.
- Preserve `?` before `#`, queries authored by HTML documents, fragments containing `?`, empty fragment/query behavior, percent encoding, and NOTE/EMBEDDED identity. `publicWorkspaceRef` is a different conversion that handles `struct:`/`node:` and must not be collapsed into a naive note URL helper.
- Pane callers currently mostly pass canonical refs or plain paths. Thus the differing parser is proven maintenance coupling, not a reproduced user bug. Do not silently change handling of a query-bearing input without a focused expectation.
- Verification: literal input/output cases in `notesRoute.test.ts`, `lib/content.test.ts`, `NoteTab.test.tsx`, and mounted `usePaneStack` tests. Use authored HTML query + fragment cases. No round-trip-only assertions.

## W5. Theme definitions and synchronous bootstrap must change together

**Confirmed hidden link; lower priority than the deletion/type cuts.**

- `web/src/lib/themes.ts:4-18` maintains the theme-name union, descriptors, default and validator. `web/index.html:25` repeats those names in an independent regex.
- The inline boot script also repeats appearance/storage rules owned by `lib/appearance.ts` and desktop theme precedence owned by `components/desktopHost.ts:151`. Adding a theme currently needs changes in the shared TS model and handwritten HTML script, or it paints the old/default theme until React loads.
- Derive `ThemeName` from the descriptor literals. Make the existing Vite build inject the synchronous bootstrap from dependency-free shared theme/bootstrap data, avoiding a second list in the HTML. Preserve synchronous execution before first paint, desktop baked timestamp precedence, and storage-denied fallback. A normal delayed module import would regress the reason for this code.
- Coordinate with runtime lane before touching desktop shared theme consumers. Do not consolidate CSS color values into TS or add theme configuration/features.
- Verification: `lib/themes.test.ts`, `lib/bootScript.test.ts` (executes actual inline script, valuable), `AppearanceMenu.test.tsx`, build then visual first-paint check if implemented. Existing boot test covers only desktop; add browser and invalid/denied-storage branches when moving logic.

## Cross-lane findings delegated

- **Section matching**: `pkg/app/web/ontology.go:2798-2823` explicitly says it mirrors the query resolver. Identical core selection in `pkg/ontology/query/sections.go:303`, `pkg/ontology/build_assessment.go:302`; query lane owns the recommendation to place `MatchSectionNodesForField` in ontology. Preserve H1 unwrap, direct-child list semantics, explicit-heading recursive semantics, order, nil handling, and clarify whitespace-heading behavior before normalizing.
- **Preview/identifier helpers**: web `ontology.go:2783` mirrors query `workspace_bodies.go:198`; shared identifier selection is a clean cut. Preview interpolation at web `:2878` vs query `workspace_bodies.go:214` differs on missing values (literal token versus empty text); query lane owns it and must preserve/decide the discrepancy rather than casually merging.
- **Graph transformation**: CLI/web duplicate projection/collapse belongs to surfaces lane, including provenance preservation.
- **Development supervisor**: tooling lane owns `scripts/dev_supervisor.mjs` and its TS adapter.

## Useful exceptions to retain

- Retry of only `INDEX_INITIALIZING` read responses, with bounded Retry-After and AbortSignal, protects first-launch navigation. Mutating POSTs must not gain automatic retry.
- EventSource manual reconnect handles permanently CLOSED sources after proxy HTTP errors; browser automatic retries are not equivalent. Vault/node stream consumers have different event/reconnect semantics and should remain separate over the existing shared transport.
- Delayed global-graph invalidation avoids repeated multi-megabyte layouts; do not remove timer without a server revision replacement and measured equivalent behavior.
- Browser storage catches are intentional because denied/quota storage must not break navigation/edit recovery. Do not promote optional storage to a fatal dependency.
- Edit-session persistence/recovery distinguishes source witnesses, revisions, dirty state and acknowledged server state. No generic state utility should combine them merely because similar effects exist.
- Structural API boundary tests, real inline bootstrap execution, CSS contrast tests, and GraphQL operation validation are meaningful constraints, not implementation-only tests to delete.

## Comments cleanup

Remove redundant comments in the exact code deleted/refactored above. One clearly stale comment is `web/src/components/useNoteTabs.ts:82` describing modifier-click directly above `SEARCH_SCROLL_COMMIT_MS`; its subject is actually `openModeFor` below the intervening helper. Prefer remove or move beside the function if still useful. Retain comments about asynchronous ownership, storage fallbacks, transport retry and visual coalescing because the code does not show those external constraints.

## Coverage and limits

Inventoried all web/API production exports for suspicious no-caller functions, then verified candidates against the whole repo including `web/kit` and bundled views; reviewed source and owning tests around the findings. This was a structural source audit, not an exhaustive dynamic correctness or accessibility test. No failure/flakiness claims were made without running a reproducer. Large files (`useNoteTabs.ts`, `OntologyNotePane.tsx`, `ontology.go`) alone are not findings; move cohesive policy only when the relationships above justify it.
