---
type: ReferenceDoc
summary: "Operational coverage contract for TypeScript and JavaScript indexing, module resolution, and relationship extraction."
reference-kind: guide
last-verified: 2026-07-18
status: active
aliases:
  - typescript-javascript-indexer-coverage
  - TypeScript and JavaScript indexer coverage
---

# TypeScript and JavaScript indexer coverage

## Supported source formats

Rhizome discovers, watches, parses, classifies, and scans coderefs in these repository-local source formats:

| Grammar | Extensions |
|---|---|
| TypeScript / TSX | `.ts`, `.tsx`, `.mts`, `.cts` |
| JavaScript / JSX | `.js`, `.jsx`, `.mjs`, `.cjs` |

An indexer behavior change requires an `IndexerVersion` bump and `rzm index --rebuild`. A normal incremental run can otherwise retain facts produced by the old resolver.

## Guaranteed static module cases

“Supported” means a literal repository-local target resolves uniquely and stays inside the configured root. Rhizome does not execute Node, package-manager commands, or build configuration.

Resolution uses deterministic source-oriented precedence:

1. Relative or absolute-in-root specifiers, including extensionless files and directory `index` files.
2. NodeNext source substitution: `.js` probes `.ts`, `.tsx`, then `.js`; `.jsx` probes `.tsx`, then `.jsx`; `.mjs` probes `.mts`, then `.mjs`; `.cjs` probes `.cts`, then `.cjs`.
3. Nearest `tsconfig.json` or `jsconfig.json` exact/wildcard `paths` mappings and `baseUrl`, including bounded relative `extends`.
4. Repository-local package names and subpaths through `package.json` `exports`; package-local `#imports` through `imports`.
5. Explicit barrel exports: named, aliased, type-only, star, namespace, and chained re-exports when the exported binding is unique.

The listed source-extension probe order is authoritative. Coexisting candidates at different positions in that order are resolved by TypeScript/JavaScript precedence; ambiguity means equally valid mappings or exports after precedence has been applied.

Package export conditions, arrays, exact keys, and wildcard keys are traversed deterministically toward repository source. Cycles, root escapes, and multiple equally valid targets remain unresolved.

## Guaranteed relationship syntax

When the module target and binding are statically resolvable, Rhizome records relationships for:

- static named, default, namespace, and type-only imports;
- side-effect imports and explicit re-exports;
- literal dynamic `import("./module.js")` dependencies;
- literal `require("./module.cjs")`, including direct, assigned, and destructured bindings;
- calls and type references through resolved import/require/barrel bindings;
- parenthesized nullish, logical, and conditional callable expressions, emitting only statically resolvable branches.

FQNs retain the existing dotted module identity. This expansion changes resolution density, not persisted identity shape.

## Best-effort and unsupported cases

Rhizome intentionally leaves these cases unresolved:

- computed specifiers such as `import(prefix + name)` or `require(variable)`;
- arbitrary external `node_modules` symbols outside configured repository code roots;
- package-based `tsconfig extends`, executable package configuration, and custom loader/plugin behavior;
- runtime dependency injection, reflection, framework dispatch, and whole-program value/type flow;
- authoritative instance-method resolution when the receiver type is unknown;
- ambiguous export maps, barrels, aliases, or filesystem candidates;
- arbitrary legacy CommonJS export mutation beyond literal dependency and binding patterns.

An unresolved relationship is preferable to a false local edge. In particular, an unknown receiver such as `array.map()` must not resolve to an unrelated same-file function named `map`.

## Verification checklist

For any JS/TS coverage change:

1. Add a red unit matrix case covering positive precedence and negative ambiguity/root-escape behavior.
2. Add representative files under `testdata/integration/python-app/vault/` and assertions under `tests/integration/ts/` for definition-site plus cross-file call/import/type relationships.
3. Run focused anchor/coderef/code-pattern/codeintel tests and `go test -tags=integration ./...`.
4. Bump `IndexerVersion`, rebuild a representative workspace with `rzm index --rebuild`, and compare `rzm code stats --text`.
5. Debug unexpected gaps with `rzm code anchors explain <file>` and `rzm code symbols <file>` before changing matcher behavior.

## Related

- [[typescript-javascript-language-support|SPEC-0079]]
- [[Code anchors (Hub)]]
- [[code-anchors-language-support]]
- [[typescript-javascript-coverage-2026-07-18]]
