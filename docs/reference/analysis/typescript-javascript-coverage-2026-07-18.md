---
type: ReferenceDoc
summary: "Baseline evidence, diagnosed gaps, and rebuild protocol for the 2026-07-18 TypeScript/JavaScript coverage audit."
reference-kind: analysis
status: active
audit-date: 2026-07-18
audit-corpus: private TypeScript monorepo
effort: EFF-0058
spec: SPEC-0079
aliases:
  - typescript-javascript-coverage-2026-07-18
---

# TypeScript and JavaScript coverage audit — 2026-07-18

## Baseline

The existing private monorepo index reported:

| Metric | Before hardening |
|---|---:|
| TypeScript symbols | 175 |
| TypeScript files | 35 |
| Modules | 51 |
| Calls extracted | 849 |
| Calls resolved | 90 |
| Calls unresolved | 759 |
| Resolution rate | 10.6% |
| Member references | 800 |
| Import edges | 3 |

These are index statistics, not a claim that all 759 unresolved calls should resolve. Browser, framework, runtime, and external-package calls are expected static-analysis noise.

## Diagnosed coverage gaps

The dominant actionable failures were structural and deterministic:

1. NodeNext source imports such as `./app.js` stopped at the runtime suffix instead of finding the repository's `app.ts` source.
2. Workspace imports such as `@acme/db` did not join package-export entry points to source-owned FQNs such as `@acme/db/src/index.createDb`.
3. Only `.ts`, `.tsx`, `.js`, and `.jsx` were consistently registered; `.mts`, `.cts`, `.mjs`, and `.cjs` could be invisible depending on the discovery surface.
4. Side-effect imports, literal dynamic imports, and literal CommonJS requires were not uniformly represented as import relationships.
5. Compound dependency-injection calls such as `(deps.buildApp ?? buildApp)()` did not enumerate the statically knowable fallback.
6. Unknown member receivers could fall back to an unrelated same-file symbol with the same member name, increasing false edges.

These findings define the bounded remediation in [[typescript-javascript-language-support|SPEC-0079]]. They do not justify compiler-equivalent inference or indexing arbitrary dependencies.

## Evidence required after implementation

Rebuild the corpus with the changed indexer rather than comparing against stale cached rows:

```sh
rzm index --code --rebuild
rzm code stats --text
```

Record the exact built commit and append the after values for indexed files, extracted calls, resolved calls, unresolved calls, resolution rate, and import edges. Then inspect representative NodeNext and workspace-package files with `rzm code symbols` and `rzm code anchors explain` to prove that the added edges join real destination definitions.

## After hardening

The private monorepo corpus was rebuilt after reconciliation with `main` using implementation commit `38642012` and `IndexerVersion` `v1.9.0`:

| Metric | Before | After |
|---|---:|---:|
| TypeScript symbols | 175 | 187 |
| TypeScript files | 35 | 35 |
| Modules | 51 | 51 |
| Calls extracted | 849 | 531 |
| Calls resolved | 90 | 160 |
| Calls unresolved | 759 | 371 |
| Resolution rate | 10.6% | 30% |
| Member references | 800 | 109 |
| Import edges | 3 | 89 |

The lower extracted-call and member-reference totals are expected precision gains: unknown receiver members and other non-call structural nodes are no longer promoted into speculative call relationships. At the same time, the number of calls joined to repository definitions increased by 78%, and import relationships increased from three to 89.

The raw 30% rate is not a parser-completeness percentage because its denominator includes calls whose definitions are intentionally outside the repository index. The 371 unresolved calls decompose into:

| Unresolved class | Calls | Interpretation |
|---|---:|---|
| Third-party and Node module imports | 256 | Expected closed-world misses; dependencies are not indexed. |
| ECMAScript, Web, and runtime globals | 88 | Expected built-ins such as `Error`, `URL`, `Promise`, and timers. |
| Runtime-valued local names | 27 | Callback parameters, Promise executors, destructured results, dependency-injected values, or framework-provided callbacks. |
| Deterministic repository-local gaps | 0 | All statically identifiable misses found in the audit corpus resolve after hardening. |

Before the final lexical pass, the audit found five nested callable bindings and two static class-method calls as deterministic misses. Scope-aware local symbols, known-only callable aliases, and exact static method FQNs now resolve those cases. The observed deterministic-local rate is therefore 100%; broader receiver/type inference remains intentionally outside this parser-only slice.

Persisted relationship inspection confirmed the original failure paths:

- `apps/api/src/main.ts` imports resolve to `apps/api/src/app.ts`, `apps/api/src/auth/auth.ts`, `apps/api/src/config.ts`, `apps/api/src/lifecycle.ts`, and the workspace export `packages/db/src/index.ts`.
- `@acme/api/src/main.start` calls `@acme/api/src/app.buildApp`, proving the NodeNext `./app.js` source substitution and compound fallback call.
- `@acme/api/src/main.database` calls `@acme/db/src/client.createDb`, and its type relationship joins `@acme/db/src/client.CreateDbResult`.
- The package barrel exposes source-owned FQNs such as `@acme/db/src/index.createDb`, `@acme/db/src/index.items`, and `@acme/db/src/index.schema`.
- Nested lexical calls join scope-qualified definitions such as `createShutdownController.clearDrainTimer`, `createShutdownController.closeOnce`, and `setup.stopContainer`.
- Static calls join `IntegrationSetupError.from`, and the `authFactory` alias records only the statically known imported `createAuth` fallback.

## Known limits retained

Computed module sources, external dependency symbols, runtime/framework dispatch, unknown receiver types, and ambiguous repository targets remain unresolved by design. See [[typescript-javascript-indexer-coverage]] for the maintained operational boundary.
