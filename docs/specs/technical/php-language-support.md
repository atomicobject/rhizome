---
type: TechnicalSpec
id: SPEC-0068
summary: "Adds PHP as a supported language in Rhizome's code indexing pipeline: tree-sitter-based symbol extraction, call-site indexing, PSR-4 namespace resolution, doc-comment extraction, and integration with the existing LanguageIndexer dispatch architecture."
spec-status: active
last-updated: 2026-09-05
aliases:
  - SPEC-0068
  - php-language-support
---

# PHP Language Support

## Summary

Rhizome's code indexing pipeline supports Go, TypeScript/JavaScript, Python, and C# through a `LanguageIndexer` dispatch architecture. PHP is not currently supported. This spec adds PHP as a first-class indexed language — enabling `doc_coverage`, `hotspots`, `complexity`, code anchor lookup, and `file-context` retrieval on PHP codebases.

The immediate driver was a client legacy-codebase engagement (June–July 2026), where the target codebase is primarily PHP and largely undocumented. Without PHP indexing, the `client-harness-builder` and `assumption-tracker` skills (SPEC-0067) can still run, but the automated assessment reports (doc_coverage, hotspots, complexity) return no code-side signal.

## Goals

- An AO engineer can run `rzm index` on a PHP codebase and build a full symbol, call-site, and import index
- `rzm agent report --op doc_coverage` and `--op hotspots` return PHP-backed results
- `rzm agent file-context` surfaces code anchor context from PHP files
- PHP coderefs (wikilinks of the form `\[\[TargetNote\]\]` placed in PHPDoc comments) are indexed and queryable
- The implementation follows the established `LanguageIndexer` pattern so it integrates without changes to the dispatch, persistence, or retrieval layers

## Non-Goals

- Language server protocol (LSP) integration — Rhizome is not an IDE
- Full type inference or generic resolution (best-effort type annotations only)
- Support for PHP 4 / PHP 5 syntax (PHP 7.4+ is the target minimum)
- Composer dependency resolution across vendor packages (indexed code only; no cross-vendor call edges)
- Modifying the semantic search ranking or retrieval contract for PHP — PHP symbols enter the same pipeline as all other languages

## User Stories

### US1 - Index a PHP codebase and run assessment reports

- id:: ^SPEC-0068-US1
- summary:: An AO engineer can point Rhizome at a PHP project, run `rzm index`, and get populated doc_coverage, hotspots, and complexity reports.
- status:: satisfied

#### Acceptance Criteria

- Given a `.rhizome/config.yml` with PHP roots configured, `rzm index` scans PHP files and symbols/calls/imports land in the unified index with no unsupported-language errors.
  verification:: Run `rzm index` on a PHP project; confirm indexed file count > 0 and no `ErrUnsupportedLanguage` errors.
- Given an indexed PHP codebase, `rzm agent report --op doc_coverage --path .` returns PHP files in `coveredFiles` and `uncoveredFiles` (total > 0).
  verification:: Report output contains at least one PHP file path.

---

### US2 - Resolve PHP symbols via file-context and code anchors

- id:: ^SPEC-0068-US2
- summary:: An agent running `rzm agent file-context` on a PHP file receives symbol-level code anchor context, not just raw note matches.
- status:: satisfied

#### Acceptance Criteria

- Given an indexed PHP file with classes or functions, `rzm agent file-context --file <php-file>` returns at least one code anchor entry with a PHP FQN and source span.
  verification:: File-context response includes a non-empty anchor list for a PHP file.
- Given a vault note with a `code-anchors:` block pointing at a PHP file, the index resolves those anchors to valid PHP symbol FQNs.
  verification:: `rzm agent validate code-anchors` passes for a PHP anchor note.

---

### US3 - PHP coderefs in docblocks are indexed

- id:: ^SPEC-0068-US3
- summary:: A `\[\[VaultNote\]\]` wikilink placed in a PHPDoc comment is discovered by the coderef scanner and appears as a code-to-note edge in the graph.
- status:: satisfied

#### Acceptance Criteria

- Given a PHP file contains a PHPDoc comment with `\[\[SomeVaultNote\]\]`, `rzm index` creates a coderef edge between the PHP file and the target note.
  verification:: `rzm agent files --inputs <target-note>` returns the PHP file as a backlink source.

---

### US4 - Integration test coverage follows the polyglot fixture pattern

- id:: ^SPEC-0068-US4
- summary:: PHP indexing has integration test coverage within the shared polyglot fixture vault, following the established pattern for Python and TypeScript.
- status:: satisfied

#### Acceptance Criteria

- A PHP source subtree exists at `testdata/integration/python-app/vault/php/` covering: class, interface, trait, standalone global function, method, call site, `include`/`require` edge, PHPDoc wikilink, namespaced file, and a file with no namespace declaration.
  verification:: `ls testdata/integration/python-app/vault/php/` shows representative PHP files matching those kinds.
- An integration test (build tag `integration`) asserts symbols, calls, and coderef edges are indexed with expected PHP FQNs and source spans.
  verification:: `go test -tags=integration ./...` passes including the PHP integration test.

## Requirements

### Indexer architecture

- MUST implement the `LanguageIndexer` interface (`pkg/anchors/indexer.go`)
- MUST use `github.com/tree-sitter/tree-sitter-php/bindings/go` for parsing
- MUST follow the cgo/no-cgo dual-build pattern: `indexer_php.go` (cgo) + `indexer_php_nocgo.go` (no-cgo stub)
- MUST use a `sync.Pool` for parser reuse, matching the Python/TypeScript pattern
- MUST return `ParseRecovered` (not `ParseError`) when tree-sitter recovers from syntax errors, so partial results are still indexed

### Symbol extraction

- MUST extract these symbol kinds: class (`SymClass`), interface (`SymInterface`), function (`SymFunc`), method (`SymMethod`), and class property (`SymField`)
- MUST represent PHP traits as `SymClass` with a note in `Signature` (no new SymKind for v1; avoids schema change)
- MUST produce FQNs in PSR-4 backslash-separated form: `Vendor\Package\ClassName` for namespaced symbols, bare `ClassName` for global-namespace symbols
- MUST extract PHPDoc `/** ... */` block comments as `DocComment` on the owning symbol
- SHOULD extract method visibility (`public`/`protected`/`private`) into `Exported` best-effort

### Call and import edges

- MUST extract function/method call sites with callee symbol name and owning FQN as `CallSite` rows
- MUST extract `use` statements as `ImportEdge` rows with the fully-qualified imported name as `Module`
- MUST extract `include`/`require`/`include_once`/`require_once` calls as `ImportEdge` rows with the path argument as `Module` — this is the primary dependency mechanism in procedural and pre-composer PHP codebases
- SHOULD extract `new ClassName()` constructor calls as `CallSite` rows

### Namespace resolution

- MUST parse the `namespace` declaration at the top of each file and use it as the `Pkg` for all symbols in that file
- Module identity for `BuildModuleDefRows` MUST use the file's declared namespace, or the vault-relative file path when no namespace is declared (consistent with how Python uses file-path-derived modules)
- MAY read `composer.json` `autoload.psr-4` entries if present to refine namespace→path mappings, but MUST NOT fail if `composer.json` is absent

### Language detection and config

- MUST add `LangPhp Lang = "php"` to `pkg/anchors/types.go`
- MUST add `.php` to `DetectCodeLang()` in `pkg/app/codeintel/ingest.go`
- MUST add `PHPRoots []string` to `pkg/anchors/config.go` Config struct with sensible defaults (`["src", "app", "lib"]`)
- MUST register the PHP indexer in `pkg/app/bootstrap/code_anchor.go` alongside existing language indexers
- MUST add `**/*.php` glob to default extension patterns

### Testing

- MUST add unit tests in `pkg/anchors/indexer_php_test.go` covering: namespace extraction, FQN generation, class/interface/trait/function/method symbols, call site extraction, import edges, PHPDoc comment extraction, and parse-recovery behavior
- MUST add PHP subtree to the polyglot integration fixture at `testdata/integration/python-app/vault/php/`
- MUST add an integration test (build tag `integration`) asserting symbol + call + coderef shapes
- All existing tests MUST continue to pass (`go test ./...`)

### Dependencies

- Add `github.com/tree-sitter/tree-sitter-php/bindings/go` to `go.mod` and vendor it
- Confirm CGO build tag applies consistently with the other tree-sitter language bindings

## Open Questions

1. **Composer namespace resolution**: PHP namespaces are declared explicitly in each file, so FQNs are accurate without `composer.json`. Reading it would allow verifying that `use` import targets resolve to real file paths (tighter cross-file call edges), but unverified import strings are sufficient for assessment use cases. Proposal: skip `composer.json` in v1; add path-verified import resolution as a follow-on if gap surfaces.

2. **Anonymous classes**: PHP 7+ allows `new class { ... }`. Should these be indexed as symbols? Proposal: skip anonymous classes in v1 (no stable name → no stable FQN); record a skipped-count metric.

## Compounding Follow-ups

Tracks PHP-indexer coverage gaps not in v1's MUST scope but that surface in real-world use. Updated as audits land. See `docs/reference/guides/php-indexer-coverage.md` for the live operational reference and the per-corpus catalog at `docs/reference/coverage-catalogs/php-coverage-YYYY-MM-DD.md` for the audit-driven gap list.

### Delivered

- **String-argument callback resolution** — **delivered 2026-06-12 via EFF-2026-06-11-18-45 (commit `77e6b82f`); scope completed 2026-06-15 by EFF-2026-06-15-13-51.** `function_call_expression` arguments whose string-literal value matches the PHP identifier regex emit an additional `CallSite` to the named handler. AST-shape detection (no framework-coupled function-name list); covers WP `add_action`, Laravel routes, Symfony events, `call_user_func`, custom registration APIs. EFF-0035 delivered the helper for function-body scope; EFF-0037 added the file-scope walker pass so the same resolution fires at the program root (e.g. WordPress's `default-filters.php`). Tests: `TestPHPIndexer_StringArgCallback_*` + integration subtest `string_arg_callback_emits_call_edge` (function-body scope, EFF-0035); `TestPHPIndexer_FileScopeCalls` / `_Namespaced` / `_NoRegression` + integration subtest `file_scope_hook_registration_persists_symbol_refs` (file scope, EFF-0037).
- **File-scope call extraction** — **delivered 2026-06-15 via EFF-2026-06-15-13-51.** Surfaced post-EFF-0035 by the 2026-06-15 wp-tst assessment: file-scope statements (canonical instance `default-filters.php` — 600 lines of top-level `add_action`/`add_filter`) produced zero `intel_symbol_refs` rows in v1.6.0. `phpCollectCalls` gained a `stopAtBoundaries bool` parameter; `IndexFile` runs a second pass at the program root after `walkNode(root, "")` completes, with boundary-skip on `function_definition` / `method_declaration` / `class_declaration` / `interface_declaration` / `trait_declaration` nodes. File-scope owner FQN = namespace (or empty string). `IndexerVersion` bumped v1.6.0 → v1.7.0. Measured wp-tst impact: `default-filters.php` 0 → 537 refs; PHP resolution rate 37.97% → 41.41%. Tests: `TestPHPIndexer_FileScopeCalls`, `_Namespaced`, `_NoRegression`; integration subtest `file_scope_hook_registration_persists_symbol_refs`.
- **Dynamic includes with path concatenation** — **delivered 2026-06-12 via EFF-2026-06-11-18-45 Phase 5.** `phpExtractIncludePath` recursively walks include/require argument expressions, joining literal fragments and substituting placeholders (`{ABSPATH}`, `{__DIR__}`, `{__FILE__}`, `{any-constant-name}`, `{var}`, `{expr}`) for unresolved fragments. Replaces the v1 string-literal-only path. Measured impact on wp-includes corpus: PHP imports 779 → 1,023 (+244 / +31%) across 58 files. Bumped `IndexerVersion` v1.5.0 → v1.6.0 to invalidate per-file extraction cache. Tests: `TestPHPIndexer_IncludeRequire` + integration fixture `bootstrap.php`.
- **Magic methods extraction** — **verified already-supported 2026-06-12 via EFF-2026-06-11-18-45.** v1's `method_declaration` walker applies no name filter, so PHP magic methods (`__construct`, `__call`, `__get`, `__set`, `__invoke`, `__toString`, `__destruct`, `__sleep`, `__wakeup`, `__isset`, `__unset`, `__clone`, `__debugInfo`) emit as `SymMethod`. Behavior pinned with `TestPHPIndexer_MagicMethods` and integration subtest `magic_methods_indexed_as_methods` over fixture `Container.php`.

### Deferred

- **Method-resolution gap (member calls lose owner-class context)** — surfaced by EFF-2026-06-11-18-45; deferred. `$obj->method()` and `$obj?->method()` call sites store `CalleeSymbol{Name: "method"}` with no `Pkg`/owner-FQN. The resolver joins on `(lang, fqn)`, so even when the owning class's method exists (e.g., `wpdb::prepare` is indexed) the call doesn't resolve. Closing this needs lightweight type inference (track `new ClassName()` assignments to local variables; resolve `$var->m()` against the inferred class). Out of EFF-0035 scope — surface is large (every member call on a typed variable). Tracked in the live catalog under `real-gap`.
- **TS-coverage audit (analogous to EFF-0035 for TypeScript)** — surfaced by EFF-2026-06-11-18-45; out of PHP-spec scope. TS resolution rate on wp-tst block-editor JS measures 0.4% (vs PHP at 43%) — jQuery + DOM API noise dominates. Would benefit from the same catalog-driven audit applied to PHP. Candidate for a follow-on `ts-indexer-language-coverage-audit` spec.
- **`rzm index --code --rebuild` does not re-extract cached files** — surfaced by EFF-2026-06-11-18-45 Phase 5; orthogonal to PHP scope. The `--code`-only path runs `PrepareRebuild` (soft) rather than `PrepareFreshRebuild`, so `IndexerVersion` bumps don't invalidate the per-file extraction cache via the soft-reset path. Direct extraction confirmed the indexer changes work; pipeline-cache invalidation is the orthogonal issue. First touchpoints: `pkg/app/indexing/rebuild.go`, `pkg/anchors/batch.go`.
- **`legacy-codebase-assessor` SKILL.md scope diagnostic should adopt `rzm code stats`** — surfaced by EFF-2026-06-11-18-45 Documentation Plan (T024). The skill's Step 2 currently runs inline `sqlite3` queries against `.rhizome/db.sqlite`; `rzm code stats --text` now provides the same diagnostic surface portably with a locked JSON contract. Deferred to a small follow-on so the skill template change doesn't drift the closed EFF-2026-06-11-18-32. Edit target: `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/SKILL.md` (template is source of truth; `.codex/`/`.claude/`/`.agents/` copies are generated by `rzm init`).

## Documentation Plan

- Update `cmd/agent_surface.go` `configuredCodeLangs` so `rzm agent surface` advertises PHP (replaces the original reference to `pkg/app/mcp/resources.go`, which no longer exists in this branch; backported during EFF-0033 closure)
- Update `README.md` supported languages list
- Update `docs/reference/guides/playbook-legacy-codebase-assessment.md` supported languages list (currently lists Go, TS, JS, Python, C#, Java, C/C++, Rust, Ruby)
- Update `docs/hubs/Code anchors (Hub).md` supported-languages cheatsheet line
- Add a code-anchor note under `testdata/integration/python-app/vault/notes/code-anchors/` covering PHP symbols for the fixture
- Update `AGENTS.md` Active Technologies section when implementation lands

## Implementation Notes — language-wiring checklist

When adding a new language indexer, parity threads through **two** separate root-aggregation paths that must both be updated. Phase 5 of EFF-0033 missed the batch path on initial implementation and was patched post-closure (see EFF-0033 Deviations 2026-06-11). To avoid the same gap on the next language:

- **Live indexer** (`rzm` daemons, file watcher):
  - `pkg/app/bootstrap/code_anchor.go` — register the indexer with its source roots
  - `pkg/app/bootstrap/live.go` — append `<Lang>Roots` and `<Lang>Ignore` into `codeRoots` / `ignoreGlobs`
  - `pkg/app/bootstrap/schedulers.go` — `detectCodeLang` extension case
  - `pkg/anchors/watcher.go` — `defaultLangExts` entry
- **Batch indexer** (`rzm index`):
  - `pkg/app/indexing/unified.go` — empty-roots check + `codeRoots` slice build
  - `pkg/app/indexing/commands.go` — empty-roots check + every `for _, root := range append(...)` aggregation
  - `pkg/app/indexing/status.go` — display row
- **Config + persistence** (covered well in Phase 5):
  - `pkg/anchors/config.go`, `pkg/vault/obsidian/local_config.go`, `pkg/vault/obsidian/code_config.go`, `pkg/app/cli/init/{detect,configure_code,write_config,rhizome_md}.go`
- **Surface + CLI**:
  - `cmd/code.go` status display, `cmd/agent_surface.go` `configuredCodeLangs`, `pkg/app/cli/code_relatedness.go` `langByExt`

Smoke test: run `rzm index --rebuild` on a real repo where the new language is the *only* indexed language; symbol count and file count should match the actual disk file count for that language. If the count is far below disk reality, the batch aggregation is the most likely missing site.
