---
type: TechnicalSpec
id: SPEC-0079
summary: "Defines comprehensive, deterministic TypeScript and JavaScript code-intelligence coverage for modern module formats, monorepo resolution, and statically knowable relationships."
spec-status: active
last-updated: 2026-09-05
aliases:
  - SPEC-0079
  - typescript-javascript-language-support
---

# TypeScript and JavaScript Language Support

## Summary

Rhizome indexes TypeScript and JavaScript with tree-sitter, but its current coverage is internally inconsistent: only part of the Node extension family is discovered, NodeNext source imports do not map emitted runtime extensions back to TypeScript sources, and workspace/configured module identities often remain unresolved. This contract makes literal module resolution and statically enumerable code relationships reliable across modern ESM, CommonJS, and monorepo layouts without embedding the TypeScript compiler.

The resolver remains deterministic, filesystem-local, and best-effort. It must prefer precision over speculative edges, preserve existing FQN shapes, and expose documented limits where static syntax is insufficient.

## Goals

- Index the full `.ts`, `.tsx`, `.mts`, `.cts`, `.js`, `.jsx`, `.mjs`, and `.cjs` source/runtime extension family consistently across discovery, parsing, watching, coderefs, and relationship indexing.
- Resolve NodeNext runtime specifiers, workspace package exports/imports, and configured TypeScript path aliases to indexed source files with deterministic precedence.
- Extract module and call relationships from common static ESM, literal dynamic-import, and CommonJS forms without inventing edges for computed or ambiguous targets.
- Preserve a reusable coverage matrix, regression corpus, and real-repository before/after evidence so future syntax gaps are visible instead of implicit.

## Non-Goals

- Embedding the TypeScript compiler, language server, or full Node module loader.
- Indexing arbitrary external `node_modules` contents or resolving calls into dependencies that are not part of configured code roots.
- Whole-program type inference, runtime value flow, framework-specific dispatch inference, or authoritative instance-method resolution.
- Resolving computed module specifiers or executing JavaScript, package configuration, or build tooling.
- Complete modeling of every legacy CommonJS export mutation; literal `require()` dependency and binding patterns are the supported boundary.

## User Stories

### US1 - Index every modern TypeScript and JavaScript module format

- id:: ^SPEC-0079-US1
- summary:: An engineer can index ESM, CommonJS, NodeNext, JSX, and TSX sources without extension-specific blind spots.
- status:: ready

#### Acceptance Criteria

- Files ending in `.ts`, `.tsx`, `.mts`, `.cts`, `.js`, `.jsx`, `.mjs`, and `.cjs` are discovered, parsed with the correct tree-sitter grammar, watched, classified, and available to code-intel and coderef surfaces. ^SPEC-0079-US1-AC1
- NodeNext runtime specifiers map to source counterparts with deterministic precedence: `.js` to `.ts`/`.tsx`/`.js`, `.jsx` to `.tsx`/`.jsx`, `.mjs` to `.mts`/`.mjs`, and `.cjs` to `.cts`/`.cjs`. ^SPEC-0079-US1-AC2
- Extensionless and directory-index imports probe the supported extension family in documented TypeScript/JavaScript precedence order, remain inside the repository root, and never depend on filesystem or map iteration order. ^SPEC-0079-US1-AC3

### US2 - Resolve monorepo and configured module identities

- id:: ^SPEC-0079-US2
- summary:: An engineer indexing a modern monorepo gets cross-package relationships for workspace exports and configured aliases.
- status:: ready

#### Acceptance Criteria

- Local package names and subpaths resolve through `package.json` `exports`, including deterministic source-oriented conditional, array, exact-key, and wildcard targets; unresolved external packages remain external. ^SPEC-0079-US2-AC1
- Local package `imports` aliases and nearest `tsconfig.json` or `jsconfig.json` `baseUrl`/`paths` mappings resolve exact and wildcard literal specifiers, including relative `extends` with cycle protection. ^SPEC-0079-US2-AC2
- Resolution is package-manager neutral, cached outside the persistence hot path, constrained to configured repository roots, and deterministic when multiple candidate files or mappings exist. ^SPEC-0079-US2-AC3
- Package and barrel resolution supports root/subpath entry points plus explicit, aliased, type-only, star, namespace, and chained re-exports while keeping cycles or ambiguous exports unresolved. ^SPEC-0079-US2-AC4

### US3 - Extract high-confidence module and call relationships

- id:: ^SPEC-0079-US3
- summary:: An agent sees relationships for common modern module and dependency-injection syntax without false same-file edges from unknown receivers.
- status:: ready

#### Acceptance Criteria

- Static imports, side-effect imports, re-exports, literal dynamic `import()`, and literal `require()` calls create local import edges when their targets resolve; computed specifiers do not. ^SPEC-0079-US3-AC1
- Named, default, namespace, type-only, destructured literal-`require`, and statically resolved barrel bindings produce call/type targets that join the indexed destination FQN. ^SPEC-0079-US3-AC2
- Parenthesized nullish/logical/conditional callable expressions emit each statically resolvable candidate and do not emit an edge for an unknown branch. ^SPEC-0079-US3-AC3
- Unknown instance receivers do not fall back to unrelated same-file symbols merely because their member names match; unresolved framework and runtime calls remain explicit best-effort noise. ^SPEC-0079-US3-AC4

### US4 - Verify and document the coverage contract

- id:: ^SPEC-0079-US4
- summary:: Maintainers can prove coverage changes locally and against a representative NodeNext workspace repository.
- status:: ready

#### Acceptance Criteria

- Unit tests cover extension discovery, module-resolution precedence, ESM/CommonJS syntax, workspace exports/imports, tsconfig paths, barrels, ambiguity, and negative false-positive cases. ^SPEC-0079-US4-AC1
- Integration tests assert definition-site and cross-file call/import/type relationships inside the shared polyglot fixture. ^SPEC-0079-US4-AC2
- The indexer version is bumped and a real `rzm index --rebuild` on a private TypeScript monorepo records before/after indexed files, extracted calls, resolved calls, resolution rate, and import edges. ^SPEC-0079-US4-AC3
- A maintained coverage guide states supported syntax, deterministic precedence, and known limitations so “comprehensive” means an auditable static-analysis boundary rather than undocumented compiler equivalence. ^SPEC-0079-US4-AC4

## Requirements

- MUST keep all persisted paths vault-relative and preserve existing TypeScript/JavaScript FQN shapes.
- MUST keep resolution filesystem-local and cached; the batch ingest path must not add per-file store queries or external network/package-manager calls.
- MUST reject resolution targets outside the repository root and retain ambiguity rather than choosing nondeterministically.
- MUST parse module syntax structurally where tree-sitter exposes source and clause nodes; raw-text fallback may be used only for syntax not represented consistently by the grammar and must have regression coverage.
- MUST bump `IndexerVersion` whenever changed extraction or resolution output requires existing files to be reindexed.
- MUST keep no-cgo builds returning the established unsupported-indexer behavior.
- SHOULD centralize the supported extension family so discovery, parser dispatch, watcher routing, coderef scanning, and CLI/report filters cannot drift independently.
- SHOULD keep resolver modules below the repository size guideline and separate filesystem/config resolution from AST extraction.
- MAY leave package-based `tsconfig extends`, non-literal module sources, external dependency symbols, and whole-program receiver inference unresolved when the limitation is documented and tested negatively.

## Open Questions

None. The supported boundary is deterministic static resolution for repository-local literal module references and enumerable callable targets; compiler-level inference remains explicitly outside this contract.

## Documentation Plan

- Add a TypeScript/JavaScript indexer coverage guide and a private-monorepo verification report.
- Update `pkg/anchors/CONTEXT.md`, `docs/reference/subsystems/code-intel.md`, and the Code anchors hub with extension, resolver, rebuild, and limitation invariants.
- Record any remaining real gaps as explicit follow-ups rather than widening this effort with speculative inference.
