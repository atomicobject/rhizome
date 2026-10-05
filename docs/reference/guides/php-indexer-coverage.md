---
type: ReferenceDoc
summary: "Durable engagement reference for the PHP indexer — enumerates what the tree-sitter PHP indexer extracts (symbols, calls, imports, magic methods, dynamic includes) and the documented gaps (method-resolution without type inference, TS-side noise). Paired with the legacy-codebase-assessment playbook; cross-links to the live coverage catalog."
reference-kind: guide
status: active
last-verified: 2026-06-12
language: php
catalog: docs/reference/coverage-catalogs/php-coverage-2026-06-12.md
spec: SPEC-0070
effort: EFF-0035
---

# PHP Indexer Coverage

Operational reference for Rhizome's PHP indexer (`pkg/anchors/indexer_php.go`). Lists what's covered, what's not, and which gaps are tracked. Use this when scoping a PHP engagement so the team knows what to expect from `rzm code stats`, the explorer view, and code-context flows.

The live coverage signal is the timestamped catalog at `docs/reference/coverage-catalogs/php-coverage-YYYY-MM-DD.md` (current: `php-coverage-2026-06-12.md`). This guide is a stable reference; the catalog moves as audits land.

---

## Covered

**Symbols** (lands in `symbols` table; reachable via `rzm code symbols <file>` and code anchors)

- Classes (`class Foo { ... }`)
- Interfaces (`interface Bar { ... }`)
- Traits (`trait Baz { ... }` — indexed as `SymClass`; no separate trait kind in v1)
- Enums (`enum Status { case Pending; }`) and enum cases (`Status::Pending`) as field-like symbols
- Free functions (`function foo() { ... }`) — including procedural files with no namespace
- Methods (`public function ...`) — instance, static, abstract, magic (`__construct`, `__call`, `__invoke`, `__toString`, `__get`, `__set`, `__isset`, `__destruct`); no name filter applied
- Properties (`public string $name;`) — indexed as `SymField` with FQN `Class::$name`
- Namespaced FQNs (`Acme\Billing\Invoice::total`) via `namespace ...;` declarations

**Calls** (lands in `intel_symbol_refs` + `intel_symbol_ref_targets`; surfaces in `rzm code stats` resolution rate)

- Direct function calls (`foo($x)`)
- Method calls (`$obj->method()`, `$obj?->method()`, `static::method()`, `Class::method()`)
- Typed receiver method calls when syntax provides a cheap local type signal: `$client = new SyncClient(); $client->pushUpdates()`, promoted constructor properties such as `public function __construct(private SyncClient $client) {}` followed by `$this->client->pushUpdates()`, and static calls such as `TaskEvent::dispatch()`
- String-argument callee resolution — `function_call_expression` whose string-literal argument matches the PHP identifier regex emits an extra `CallSite` to the named handler. Covers WP `add_action('init', 'my_handler')`, Laravel routes (`Route::get('/x', 'Ctrl@m')`), Symfony event subscribers, `call_user_func('handler')`, and any custom registration API. AST-shape detection only; no framework function-name list. Fires at function/method scope AND file scope.
- File-scope call extraction — file-scope statements (e.g. WordPress's `default-filters.php` shape — 600 lines of top-level `add_action(...)` / `add_filter(...)`) emit `CallSite` rows with the namespace as owner FQN (or empty string when no namespace). Two-pass walker: structured walker collects function/method/class-body calls; second pass picks up everything else at file scope while skipping into already-walked boundaries. Delivered by EFF-0037; cascades make string-argument callback resolution complete.
- Static enum/class member reads such as `TaskStatus::Pending` emit `member_ref` rows.

**Type, inheritance, and attribute edges**

- `extends` / `implements` clauses emit `SuperEdge` rows used by `baseClass:` anchors.
- PHP 8 attributes (`#[Route('/x', methods: ['POST'])]`) emit `AnnotationUse` rows with simple scalar/array argument filters for annotation anchors.
- Parameter, return, property, and promoted-property type hints emit `type_ref` rows.

**Imports** (lands in `intel_import_refs`)

- `use Vendor\Pkg\Class` and `use Vendor\Pkg\{A, B}` grouped imports
- `include`/`require`/`include_once`/`require_once` with bare string literals
- `include`/`require` with constant + literal concatenation chains — emitted with placeholders for unresolved fragments. Examples: `require ABSPATH . 'foo.php'` → `{ABSPATH}foo.php`; `require __DIR__ . '/inc/' . $name` → `{__DIR__}/inc/{var}`. Placeholders: `{ABSPATH}`, `{__DIR__}`, `{__FILE__}`, `{any-constant-name}`, `{var}` (variable), `{expr}` (function call / subscript / other expression).

**Doc comments**

- PHPDoc blocks (`/** ... */`) immediately preceding a class/interface/trait/function/method declaration attach as `DocComment`. Other comment styles (`//`, `#`, `/* */` without leading `**`) are skipped.

---

## Not covered (intentional)

- **TS↔PHP cross-language edges via static analysis.** Runtime-only: REST URLs, JS-embedded-in-PHP, hook callback strings registered for JS dispatch. Use authored coderefs (`\[\[note\]\]` wikilinks in PHPDoc) or code anchors instead.
- **PHP language built-ins** (`empty`, `isset`, `sprintf`, `is_array`, `count`, `strlen`, ...). These never resolve to user-code symbols. Filter from diagnostics via `rzm code stats --exclude-from <catalog>`.
- **Variable-function and variable-method calls** (`$fn($args)`, `$obj->{$method}()`). Not extracted as CallSites at all — un-resolvable by static analysis; emitting partial info would pollute the unresolved-callees list with un-actionable entries.
- **Conditional include extraction**. `include`/`require` statements inside function/method bodies are visited and (for top-level shapes) extracted, but the walker's call-collection path doesn't separately re-emit ImportEdges nested inside calls. Top-level program-scope and conditional-block includes work; deeply-nested-inside-conditional-runtime-construction does not.

---

## Known gaps (tracked)

These are documented in the live catalog under `real-gap` and surfaced to skills like `legacy-codebase-assessor` so engagement starts know what to expect.

| Gap | Pattern | Status | Tracking |
|---|---|---|---|
| Method-resolution without owner-class | `$obj->method()` calls resolve when the receiver is locally typed via `new`, a promoted property, or static syntax. Unannotated service-locator/container returns, dynamic properties, framework magic, and wider data-flow still store `CalleeSymbol{Name: "method"}` with no Pkg/FQN. | Partially improved in v1.8.0; broader data-flow still deferred | EFF-0035 compounding follow-up |
| `hotspots` + `doc_coverage` reports return JS-only on sparse-PHP corpora | Both reports score by resolved call-graph connectivity; PHP's ~38% resolution on wp-tst leaves the call graph too sparse, so PHP authority scores collapse to zero. Falls back to JS/TS files with high in-module resolution. | Tracked (post-EFF-0035 closure finding) | Candidate fresh effort (report-scoring fallback) |
| `rzm index --rebuild` silent no-op when binary already matches indexed `IndexerVersion` | Operational confusion — no confirmation message when rebuild is a no-op. Cascades from the existing pipeline-cache invalidation issue. | Tracked (post-EFF-0035 closure finding) | Candidate fresh effort (operational signal + pipeline-cache fix) |
| TS resolution rate (0.4% on wp-tst block-editor JS) | jQuery + DOM API noise dominates; TS indexer would benefit from the same catalog-driven audit applied to PHP | Out of EFF-0035 scope | Captured as candidate effort |

---

## Operational notes

- **`rzm code stats`** is the diagnostic surface. JSON by default; `--text` for human summary; `--exclude-from <catalog>` filters known noise from the unresolved-callees list. See `cmd/code_stats.go` for the JSON contract.
- **Coverage catalog** lives at `docs/reference/coverage-catalogs/php-coverage-YYYY-MM-DD.md`. Refresh by running `rzm code stats --exclude-from <catalog>` against a representative corpus, classifying newly-visible top-N entries into one of the four sections (`language-builtin`, `framework-noise`, `un-resolvable-pattern`, `real-gap`), and re-running the filter.
- **Engagement use**: the `legacy-codebase-assessor` skill consumes both this guide and the catalog when sizing a PHP codebase; the playbook (`playbook-legacy-codebase-assessment.md`) orients the engineer to the skill chain.
- **Refresh after indexer changes:** `rzm index --rebuild` explicitly replaces the unified database and reconstructs indexes from current source. Ordinary `rzm index` uses content freshness to refresh changed files.
- **Audit lineage**: [[php-indexer-language-coverage-audit|SPEC-0070]] defines the iterative audit loop. EFF-2026-06-11-18-45 delivered v1 closure 2026-06-12. EFF-0037 closed file-scope call extraction 2026-06-15 (IndexerVersion v1.6.0 → v1.7.0). A later v1.8.0 parser pass added enums, supers, attributes, type refs, static member refs, typed receiver calls, and stronger PHP integration fixture proof. EFF-2026-06-15-15-46 / [[sparse-graph-fallback-for-assessment-reports|SPEC-0073]] closed the `hotspots`/`doc_coverage` PHP-blind-spot 2026-06-15 via a sparse-graph fallback CTE that supplements scoring from `intel_symbol_refs` when a language's resolution rate is below `SparseCallGraphThreshold` (default 0.60); `--lang` + `--fallback-threshold` CLI flags added.

---

## Related

- [[php-language-support|SPEC-0068]] — v1 PHP indexer foundation
- [[php-indexer-language-coverage-audit|SPEC-0070]] — iterative audit loop spec
- `pkg/anchors/indexer_php.go` — extraction logic
- `pkg/anchors/indexer_php_test.go` — unit tests pinning extraction behavior
- `tests/integration/php/file_context_test.go` — integration tests over polyglot fixture
- `docs/reference/coverage-catalogs/php-coverage-2026-06-12.md` — live coverage catalog
- `docs/reference/analysis/php-coverage-2026-06-12.md` — audit narrative + verification
