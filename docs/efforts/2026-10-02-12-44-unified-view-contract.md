---
type: EffortNote
id: EFF-2026-10-02-12-44
aliases: [EFF-2026-10-02-12-44]
name: Unified view contract
created-at: 2026-10-02T16:44:05Z
status: complete
summary: "Deliver a shared programming contract and host for built-in and custom type, display-group, and node views, including contextual navigation and agent guidance."
---

# Unified view contract

## Scope

Implement [[unified-view-contract|SPEC-0110]] on `feat/unified-view-contract`, starting from `3e2a5d3302b30237ccd68b42378ad7b29c9f771e`. Deliver one registration/context/configuration/lifecycle/query/navigation/staged-edit boundary for Overview, Table, Cards, Kanban, and custom HTML/TSX. Include exact and generic group mounts, specific type/interface mounts, concrete node mounts, multiple selectable definitions, default resolution, shared hosting, canonical embedded-node invocation, standalone parity, native execution reuse, and canonical skill guidance.

Exclude the bundled generic display-group page, which Drew will author with another agent after this work lands. Exclude public config migration, a separate plugin loader, ontology membership changes, native execution replacement, and isolated HTML-note runtime changes. Local commits are authorized; pushes, pull requests, merges, and publication are not.

## Spec Set (Frozen)

Freeze the specifications as revised by this effort's initial documentation commit on 2026-10-02; the scope baseline is the commit that first adds this record.

- [[unified-view-contract|SPEC-0110]] in full, including every Requirements subsection.
- [[configured-view-engine-and-repo-config|SPEC-0058]] for preserved v1 configuration, native execution, canonical items, and safe editing; mount/selection changes are governed by SPEC-0110.
- [[custom-view-apps|SPEC-0105]] for trusted serving, kit, frame bridge, reload, and validation; expanded applicability and runtime invocation are governed by SPEC-0110.

## Stories In Scope (Frozen)

Requirements-only delivery: all Requirements of [[unified-view-contract|SPEC-0110]] are selected. No new or historical story slice is invented.

## Spec Coverage Checklist

- [x] Registration/applicability and v1 compatibility verified, including exact/generic group defaults and retained built-ins.
- [x] Shared runtime contract and host used by Overview, native variants, and custom entries; native execution reusable by custom code.
- [x] Type/group/node selection, canonical invocation, standalone links, and contextual navigation verified.
- [x] Staged reads/edits survive presentation changes; frame trust and cleanup checks retained.
- [x] Agent guidance and example cover mounted authoring and dashboard-to-node navigation.
- [x] All required gates, independent review, and delivery alignment recorded.

## Plan

### Architecture and foundation

Normalize existing definitions and generated built-ins into one registration/choice model. Keep definition, applicability, and invocation separate. The shared host supplies a typed runtime object and dispatches existing React renderers or custom frames. Preserve native execution and the public custom serving boundary. Defaults resolve exact before generic; explicit links and valid user choices precede configuration.

The first batch fixes the additive config and runtime contracts. The orchestrator coordinates those types across workers and obtains a focused independent foundation review while implementation proceeds; the approved plan requests no additional approval pause for settled product choices.

### Batches and ownership

1. **Contract/backend worker:** owns view config, catalog applicability/defaults, validation, target discovery, API schema and generated types. Add group/node mounts and compatible custom type/interface mounts; retain generated options. Exit: focused Go tests prove compatibility, specificity, conflicting defaults, and invalid sibling isolation.
2. **Workspace worker:** owns browser registration/selection/host, type and group navigation, preference migration, native presentation adapters. Replace split Home/View selection and keep native state. Exit: web tests and mounted browser checks show default selection, alternatives, group restrictions, and fallback.
3. **Runtime/kit worker:** owns custom invocation context/configuration, contextual links, reusable native execution API, frame/runtime adapters, and standalone launch parity. Coordinate exact types with batches 1 and 2. Exit: tests prove embedded refs, two subjects, selected-view navigation, staging continuity, origin/source checks, and cleanup.
4. **Guidance worker:** owns canonical custom-view and Rhizome templates, kit API reference, example, and affected subsystem/CONTEXT notes. Teach discover/prove/author/validate/mounted verification using the actual exports. Exit: generated guidance works and the second init reports no updates.
5. **Documentation worker:** owns this effort, SPEC-0110, narrow SPEC-0058/0105 reconciliations, and CHANGELOG. Preserve historical closed efforts. Exit: changed docs and frozen scope validate.
6. **Orchestrator:** integrates workers' commits, reviews the full flow and spec alignment, fixes integration gaps, obtains independent review, and runs final gates. Stage only effort changes; preserve unrelated files.

Each worker uses an isolated worktree where practical, remains aware of parallel ownership, and reports exact checks and remaining integration obligations. Commits stay local.

### Verification and close-out

Run focused tests as each batch lands; run `make check-fast` in the code edit loop and `make check` before code commits. Verify the actual UI with `make web-e2e` and focused browser journeys covering defaults/reset, Overview/native/custom switching, exact/generic groups, canonical node contexts, two nodes, direct URLs, and pending edits. Native execution reuse needs a custom consumer check.

Run `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift`; classify unrelated/historical findings without rewriting closed efforts. Run `make build` and `./scripts/rzm init --yes` twice, retaining generated copies and requiring no second-run updates. Run `make check-full` before effort closure without a PR. Record commands, results, limitations, deviations, review resolution, and actual delivery; reconcile durable docs before marking complete.

## Plan Approval

Drew approved the design and execution in chat on 2026-10-02: "Are we ready to update specs, create an effort, and implement? If so, go straight for the jugular" and requested GPT-6.1 Sol subagents to divide the implementation. This authorizes the bounded plan above and routine integration fixes without another approval stop. The local Rhizome current user is unconfigured, so `plan-approved-by` is omitted rather than inferred from chat identity.

## Original Intended Delivery

All SPEC-0110 requirements implemented and verified in a local branch, with compatible v1 configs and agent guidance. A subsequent agent can register the bundled generic group view through the shared contract without changing the host or routing.

## Actual Delivered

Implemented SPEC-0110 through the existing v1 loader and native execution service. Overview, native layouts, and custom HTML/TSX share target choices, invocation context, configuration, selection, navigation, lifecycle, and staged-edit services. Type/interface, exact/generic group, and concrete node mounts support configured defaults and vault/subject preferences. Canonical node navigation supports independent same-file subject tabs and direct contextual launches. Canonical skills, examples, generated harness copies, API types, subsystem guidance, and browser regressions are included. Verified through the full local gate, 59 browser journeys, documentation validation, and idempotent template regeneration. The bundled generic group page remains explicitly subsequent work.

## Execution Notes

- 2026-10-02T16:44:05Z [decision] Opened this bounded effort from `3e2a5d3302b30237ccd68b42378ad7b29c9f771e` after explicit chat approval. Live TechnicalSpec/EffortNote guides and runtime authoring recipe were read; IDs were allocated through Rhizome. Current user is unconfigured.
- 2026-10-02T16:44:05Z [learning] Search enrichment has no local index, while live ontology guides, allocation, and recipes work. Normal indexing would enable configured Voyage embeddings, so no external embedding requests were made during documentation setup.
- 2026-10-02T16:47:57Z [validation] Initial specification and effort pass `./scripts/rzm validate` (441 identifier nodes; 0 issues; exit 0) and `./scripts/rzm validate frozen-scope-drift --max-issues 1000` (0 issues; exit 0). SPEC-0058 and SPEC-0105 were narrowly reconciled before freezing this new scope; no historical effort was changed.


- 2026-10-02T17:14:11Z [implementation] Integrated the catalog, shared host and selector, Overview/native adapters, type/group navigation, node presentations, HTML/TSX invocation, kit native execution and navigation, and canonical guidance. Workers used isolated branches; integration remains local.
- 2026-10-02T17:14:11Z [review] Independent foundation and integration review found and resolved native-definition navigation, explicit-selection migration precedence, full-ref row navigation, independent embedded-node tab identity, retained generated layouts beside filtered authored sources, forged generated provenance, and missing-ID default isolation. The final browser run is checking these fixes together. A GraphQL canonical-ref finding remains under repair.
- 2026-10-02T17:14:11Z [validation] `make check` passes after correcting canonical skill routes and their existing init contract expectations: Go units, lint, typechecks, vet, credential checks, and 112 web test files / 926 tests. The earlier failed run was limited to those four init-template contract checks. Focused route tests also pass for malformed group URLs.
- 2026-10-02T17:14:11Z [validation] The first integrated `make web-e2e` run exposed legacy source/structure button selectors and new fixture identity assumptions; these were corrected. The run was interrupted after identifying the repeated selector failures; a complete final run remains required. Separate focused real-browser checks pass collection/group defaults, native switching, node preferences, staged switching, and direct HTML/TSX launches.
- 2026-10-02T17:14:11Z [review] A scoped comment review found no concealed workaround; removed three narration-only comments. Kept non-obvious authority, frame lifetime, HTML injection, and optional storage rationale.


- 2026-10-02T17:23:00Z [validation] `make build` passed. `RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm init --yes` updated the managed custom-view/Rhizome skills in both harnesses; the second run reported no updates. Current documentation and frozen-scope checks both pass with zero issues. Excluded disposable browser fixture notes from repository note discovery, matching the existing testdata boundary.
- 2026-10-02T17:23:00Z [validation] Seven focused browser journeys passed on port 4183. Visual artifacts exposed a disabled group note query shown as a perpetual spinner and file titles reused for distinct embedded tabs; the rail now omits a note list for group context and explicit node tabs use the focused title. Root rail/route checks pass 59 tests; focused node-title checks pass 25 tests. The first complete integrated browser run passed 50 tests with two URL-parameter-order expectation failures and seven dependent HTML tests skipped. Those expectations now inspect parsed URL fields, and the complete suite is rerunning.
- 2026-10-02T17:23:00Z [review] Direct GraphQL embedded-node refs previously omitted the structural fingerprint; a fail-before/pass-after regression now compares direct and locator results to the authoritative projection, preserving the indexed content-free path. Independent review measured repeated projection work for 1/10/40 child refs: exact metadata reads grew 2/11/41. Reusing the existing request read scope reduces them to 1/1/1; source reads increase from one to two per host note, remaining constant as the child count grows. No separate cache or executor was introduced.
- 2026-10-02T17:23:00Z [environment] Isolated the integration worktree's dependencies after another active worktree changed the shared installation, and moved its browser gate to port 4179. Collaborative preview navigation failed with a client-automation error after status/open/navigation retries; real repository Playwright journeys and their rendered screenshots supply browser evidence.


- 2026-10-02T17:35:00Z [validation] Final `make check-full` passes: race-enabled Go unit and integration suites, benchmark contracts, formatting/vet/typechecks/credential checks, and 112 web files / 929 tests. Its first run caught an old persistence test harness without a vault identity; supplying the fixture identity aligned it with the new guarded storage contract, and the complete gate passed on rerun.
- 2026-10-02T17:35:00Z [validation] Final `RHIZOME_E2E_PORT=4179 make web-e2e` passes all 59 tests in 1.2 minutes, including the seven new unified-view journeys and all seven existing isolated HTML-note journeys. Earlier browser failures were resolved through selector/URL/locator corrections and the recorded product fixes; none remain skipped. Reviewed rendered group and embedded-node screenshots.
- 2026-10-02T17:35:00Z [validation] Final `make build` passes with the latest code. Both subsequent `RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm init --yes` runs report no updates. Generated skill copies match their canonical sources. The startup preference regression now proves migration waits for the real vault identity.
- 2026-10-02T17:35:00Z [delivery] Compared actual delivery against every frozen SPEC-0110 requirement and the subsequent user corrections. All selected requirements are implemented; built-in and custom views share the host/runtime contract, and a future bundled generic group page requires only its registration and entry. Independent findings are resolved. Delivery remains local as authorized.


- 2026-10-02T20:00:00Z [review] A post-closure review of the full branch showed the earlier closure was premature. Confirmed defects: duplicate `mount.default` was fatal and took both views offline; `hosted=1` skipped node-ref validation; Go and TypeScript display-group discovery diverged; node mounts accepted section types; non-string YAML configuration keys were fatal; the Notes home header lost its indexing, issue-count, mean-relation, and error states; legacy `"read"` and native-variant preferences migrated incorrectly; dead switcher, `onOpenTable`, context-parsing, and CSS code remained; one browser assertion could not fail; `web/CONTEXT.md` contradicted the delivered behavior. Drew authorized fixing all of them.
- 2026-10-02T20:00:00Z [decision] Drew chose to open type and interface collections in their generated Table instead of Overview when no authored default exists, because the Overview graph is rarely useful. Overview remains selectable; groups keep Overview's type list and nodes keep Structured.
- 2026-10-02T20:00:00Z [implementation] Fixed every confirmed finding. Duplicate defaults and stray `mount.group` fields are warnings; node refs are validated in hosted and direct launches; `/views/<id>` validates through the catalog and keeps standalone views available when the ontology cannot load; Go and the rail share `NavigationMembers` plus one fixture (`testdata/display-groups/rail-groups.json`) asserted by both test suites. SPEC-0014 and SPEC-0090 Home/Table criteria now point to SPEC-0110.
- 2026-10-02T20:00:00Z [review] An independent final review of the fix diff found two compatibility regressions (standalone views failing when the ontology failed to load; stray `mount.group` becoming fatal), a legacy Table migration gap, and stale Home/Table specs. All were fixed, and its re-review judged the branch merge-ready.
- 2026-10-02T20:00:00Z [validation] `make check-full` passes (Go race, integration, and benchmark contracts; 113 web files / 941 tests). `RHIZOME_E2E_PORT=4193 make web-e2e` passes 59/59. `make build` passes; the second `RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm init --yes` reports no updates. `./scripts/rzm validate` and `frozen-scope-drift` report 0 issues. `make check` and the browser suite were rerun after the last three review nits.
- 2026-10-02T20:30:00Z [decision] Merging `main` brought in `view-manual-ordering` as SPEC-0108, colliding with this spec. Because that spec had already merged, this spec was renumbered to SPEC-0110; `rzm agent next-id` offered SPEC-0109, but the open `t3code/speed-view-refresh-after-save` branch already claims it. Manual-ordering references keep SPEC-0108.

## Deviations

- 2026-10-02: SPEC-0110 gained the requirement that type and interface collections without an authored default open their generated Table, at Drew's request after the frozen scope. It also records superseding the Home/Table toggle in SPEC-0014 and SPEC-0090. Existing completed efforts remain historical and are not reopened.

## Compounding Follow-ups

- The bundled generic display-group page is explicitly subsequent work. Its implementation should exercise registration and context without a host change.
- `findCustomView` builds the full catalog per request (twice per HTML view load); cache it if view loads become slow.
- `rzm validate views` fails on warning-severity issues such as `duplicate_mount_default`; decide whether warnings should pass the check.
- `unexpected_field` now has two severities; a distinct code for the ignored `mount.group` warning would be cleaner for issue-code consumers.

## Closure Checklist

- [x] Frozen scope accounted for by actual delivery and executable evidence.
- [x] Required local gates and template idempotence passed or unresolved failures recorded.
- [x] Independent foundation/integrated review findings resolved.
- [x] Specs, subsystem notes, kit reference, skills, and changelog match delivery.
- [x] Follow-ups classified and authority boundaries preserved.

## Status

Complete. SPEC-0110 is delivered and verified on `feat/unified-view-contract`, including the post-closure review fixes. Drew authorized pushing the branch and opening a pull request; merging remains his decision.
