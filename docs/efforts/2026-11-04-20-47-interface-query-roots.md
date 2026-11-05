---
type: EffortNote
id: EFF-2026-11-04-20-47
aliases: [EFF-2026-11-04-20-47]
name: Note-interface query roots
created-at: 2026-11-05T01:58:13Z
status: active
summary: "Generate bounded GraphQL roots for authored note-only interfaces."
governing-specs: ["[[ontology-graphql-query-contract]]"]
---

# Note-interface query roots

## Scope

Implement the investigation recommendation approved in chat: authored domain interfaces with a nonempty transitive implementor set consisting entirely of notes get typed roots. Preserve concrete identity, shared-field filtering, global sorting, selectors, offsets and coverage. Runtime, empty, section, embedded and mixed interfaces remain rootless. No vault edits, block-ID repairs, store schema changes or releases.

## Spec Set (Frozen)

- [[ontology-graphql-query-contract|SPEC-0042]], approved 2026-11-04 increment: generated note-interface roots, shared-field planning and existing typed-root bounds and coverage.

## Stories In Scope (Frozen)

Requirements-only increment; no additional stories selected.

## Spec Coverage Checklist

- [x] Eligible interface roots expose the existing typed-note API and concrete identity.
- [x] Shared fields, global ordering and pagination preserve bounded-read coverage.
- [x] Discovery, planning diagnostics and shipped guidance agree with execution.
- [ ] Required final verification and review evidence recorded.

## Plan

1. Generate eligible roots and dispatch through the executable root registry. Preserve transitive membership and concrete records.
2. Resolve shared constraints per concrete binding. Collapse equivalent indexed plans; otherwise use one bounded combined candidate set before global filtering, sorting and pagination. Keep truthful coverage and overlay/selector parity.
3. Cover schema discovery, introspection, planning diagnostics and behavior with regressions. Update the owning contract, subsystem note, package context, shipped guidance and changelog.
4. Run required gates and direct CLI fixture proof, obtain Sol-loop approval and current-head Greptile 5/5, then open a PR and wait for CI. Leave it open and unmerged under full-stop authority.

## Plan Approval

Drew approved the preceding investigation recommendation with "Do it. Sol loop review full stop" on 2026-11-04. This authorizes the four steps and PR preparation, excluding merge. Current-user discovery reports `missing_current_user`; no identity metadata is inferred.

## Original Intended Delivery

A shared `Meeting` root returns both general meetings and coaching sessions in one filtered, sorted page, retaining concrete resolved types and specialized coaching authoring guidance.

## Actual Delivered

Implementation and eight focused regressions are present on `investigate-interface-query-roots`. Delivery revision and final review evidence will be recorded after verification.

## Deviations

None in behavior or scope. Initial sandbox runs could not access the Go cache or runtime boot identity; the required checks run outside the sandbox. The local binary was rebuilt to restore Rhizome retrieval. Initial identifier allocation overlapped source edits and failed publication; allocation succeeded after the source froze.

## Closure Checklist

- [x] Approved behavior and owning documentation implemented.
- [x] `make check` passed, including Go unit tests, static checks, shipped-content tests and 1777 web tests.
- [ ] Direct CLI proof, integration and documentation validation complete.
- [ ] Final diff independently reviewed with Sol and Greptile.
- [ ] Delivery revision and PR recorded; CI green and PR left unmerged.

## Compounding Follow-ups

No additional reusable finding is proposed. The existing multi-type indexed read API suffices; mixed node-kind hydration remains outside this effort.

## Status

Active: implementation complete; final proof, review and PR gates remain.

## Execution Notes

- The baseline built CLI reproduced `Cannot query field "meeting" on type "Query"` against a disposable three-note meeting fixture.
- Eight `TestInterfaceRoot*` regressions pass: global pages and source aliases, inherited interfaces, selectors and overlays, path misses and private-field rejection, root eligibility/collisions, discovery/introspection, scoped semantic retrieval, planning diagnostics and candidate-bound coverage.
- The indexed read model already supports multi-type global ordering and pagination; this change adds no database API or persisted migration.
- Final rebuilt CLI proof passed: descending dates span CoachingSession and GeneralMeeting, the second page proves exhaustion, clientRef filtering returns all three fixture notes, path selection retains concrete type, and saved recipes retain typed-root extensions.
- A real `init --agents shared` followed by `init --check` exited 0 in a disposable external-binary fixture. `make build` completed. Default documentation validation passed with 0 issues. Frozen-scope validation reports only the unrelated pre-existing SPEC-0111 drift in EFF-2026-10-03-18-33; this effort has no drift.
- Full `GOCACHE=$PWD/.gocache go test -tags 'fts5 integration' ./...` passed all packages except the unchanged compounding redaction timing assertion under concurrent load (2529 ms against a 2000 ms bound). Its isolated `TestCompoundingScripts` rerun passed in 21.860 s. No unrelated test or threshold was changed.
- Sol-loop round 1 (GPT-6.1 Sol, xhigh) returned CHANGES with S1 (should, confirmed): shared link fields narrowed by concrete implementors rejected targets valid for another branch. Greptile independently reported the same defect (G1, 4/5). The real CLI reproduced the failure with a passing concrete-root control supplied by Sol.
- S1/G1 fix: a root-local resolver uses the shared link target contract for validation, indexed planning and residual filtering. Narrower concrete branches naturally contribute no matching edges. Removed the per-concrete residual grouping. A regression covers eq/in, indexed/residual/find/path routes and overlays, plus unresolved and ambiguous shared targets. Query, pushdown and recipe suites pass. No findings were declined. Usage was not exposed.
- After S1/G1: `make check` passed again (1777 web tests); rebuilt CLI fixture proof passed both the original meeting/recipe case and narrowed shared-link indexed/residual/path queries. Default documentation validation remains clean. Sol round 2 reviews the changed behavior and open S1 only; Greptile reruns on the committed correction.
