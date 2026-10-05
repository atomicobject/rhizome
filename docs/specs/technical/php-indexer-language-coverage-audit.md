---
type: TechnicalSpec
id: SPEC-0070
summary: "Iterative PHP indexer language-coverage audit: diagnose missing edges in the explorer graph (call resolution, dynamic includes via string concatenation, string-argument callback resolution, magic methods), close highest-value gaps surfaced by real codebase testing (WordPress dogfood + a client legacy PHP codebase), and re-verify via the explorer + the four assessment reports. Targets generic PHP patterns, not WP-specific calls — WordPress is the test corpus that surfaces patterns, not a Rhizome dependency."
spec-status: active
last-updated: 2026-09-05
aliases:
  - SPEC-0070
  - php-indexer-language-coverage-audit
---

# PHP Indexer Language-Coverage Audit

## Summary

[SPEC-0068](php-language-support.md) shipped a v1 PHP indexer covering classes, interfaces, traits, methods, properties, functions, `use` imports, `include`/`require` (string-literal only), and direct function/method/static call sites. EFF-0033's dogfood test on WordPress (`wp-tst`) confirmed the indexer extracts symbols correctly — 15,794 symbols across 2,594 PHP files — but Daniel observed in the explorer tab that **most of the graph is unconnected**: nodes float in a ring with very few inter-node edges, compared to the dense interconnected Rhizome repo graph.

The explorer tab is the primary place this gap shows up, and the intended response is a sub-agent review across the codebase for language structures the parser does not yet support. The fix is iterative — diagnose what's missing, add coverage for the highest-value gaps, re-verify in the explorer.

This spec defines the audit loop, not a fixed set of fixes. WordPress is the test corpus, but the *fixes* target generic PHP patterns — not WordPress-specific calls. WP's `add_action('init', 'my_fn')` is one surface of a broader PHP pattern (string-argument callee resolution) that also covers Laravel routes (`Route::get('/foo', 'FooController@show')`), Symfony event subscribers, `call_user_func`, `array_walk`, and any framework that registers callbacks by string name. The specific gaps are enumerated after the diagnostic baseline; candidates fall in roughly these generic-PHP categories:

- **String-argument callee resolution** — call expressions where one or more string-literal arguments name another indexed symbol. The indexer currently captures the call site but doesn't try to resolve the string args as callees. The WordPress hook system is the most visible instance but the pattern is generic
- **Dynamic includes with path concatenation** — `include`/`require` where the argument is a runtime expression (`require ABSPATH . 'foo.php'`, `require __DIR__ . '/inc/' . $name`). Currently only string-literal arguments are captured
- **Magic methods** — `__construct`, `__call`, `__get`, `__set`, `__invoke`, `__toString`, etc. Standard PHP class members that may not be extracted as `SymMethod` currently
- **Variable-variable / variable-function calls** — `$fn($args)`, `$obj->{$method}()`. Static analysis can't always resolve these but extracting the call site is still useful
- **Call-site resolution improvements** — even for symbols that exist in the index, the resolver may fail to match (case sensitivity, namespace inference, etc.)

WP-specific patterns (e.g. WP's hook system in particular, or `WP_Query` chains) are out of scope as "named-target" features; they show up only as instances of the generic patterns above.

## Goals

- An AO engineer (or an agent) can run a diagnostic command/query and see which PHP edge categories are populated vs. empty, with raw counts
- The audit loop is reproducible: same diagnostic on the same indexed corpus yields the same gap report
- Highest-value gaps surfaced by the WordPress corpus are closed in v1 of this effort; lower-value gaps are documented as compounding follow-ups. **Targets generic PHP patterns**, not WP-specific functions
- After audit fixes land, the explorer graph for a re-indexed WordPress vault visibly densifies (the ring layout collapses into clusters); the same fixes should also benefit any future PHP engagement using similar patterns (Laravel, Symfony, custom procedural codebases)
- The audit produces a documented checklist of PHP language constructs covered + not covered, so future engagement starts know what to expect

## Non-Goals

- Does not promise 100% edge coverage — Drew explicitly said "partial-working is fine; if the graph isn't 100% complete, it won't be broken — just less rich"
- Does not address TS↔PHP cross-language edges via static analysis (those are fundamentally runtime: REST URLs, hook callback strings, JS-embedded-in-PHP — not reachable from AST). Cross-language linkage stays the domain of authored coderefs and code anchors.
- Does not change the v1 indexer architecture (`pkg/anchors/indexer_php.go`) wholesale — only adds extraction cases and resolution improvements

## User Stories

### US1 - Diagnostic baseline (`rzm code stats`)

- id:: ^SPEC-0070-US1
- summary:: A new `rzm code stats` subcommand returns a structured per-language and per-edge-category gap report for any indexed codebase. Becomes the single source of truth for "what's in the index" that other skills (`legacy-codebase-assessor`, this audit) can consume.
- status:: satisfied

The audit can't start until we measure. Today the only way to see the gap is the explorer tab visualization (diagnostic-by-vibes, not counts) and ad-hoc sqlite queries. This story ships a first-class CLI that produces a reproducible numeric baseline, available to both interactive use and skill orchestration. The `legacy-codebase-assessor` skill (SPEC-0069) currently embeds its own sqlite queries for scope diagnostic — it can adopt this command in a follow-on, consolidating two implementations into one.

#### Acceptance Criteria

- **`rzm code stats` exists as a `code` subcommand**: The command is registered alongside `code status` / `code symbols` / `code anchors` in `cmd/code.go` and shows up in `rzm code --help`.
  verification:: `rzm code stats --help` returns a help screen describing the subcommand.
- **Default output is structured JSON**: The command outputs JSON by default (parseable by agents and skills). A `--text` flag (or similar) MAY exist for human-readable summary, but JSON is the contract.
  verification:: `rzm code stats` returns valid JSON with at minimum the four top-level groups described below.
- **Output covers per-language counts**: For every language present in the index, the JSON includes: symbol count, distinct file count, module-def count. Mirrors what `code status` shows for *configured* roots but ranges over what's *actually* in the index.
  verification:: Running on `wp-tst` returns a `languages` object with non-zero rows for `php` and `ts`.
- **Output covers per-language edge categories**: For every language: call sites extracted, call edges resolved (those whose callee exists in the symbol table), import edges. This is the critical "what's missing" signal — Drew's gap diagnostic — and the difference between *call sites* and *call edges* names whether extraction or resolution is the gap.
  verification:: Running on `wp-tst` shows >0 extracted PHP call sites and a measurable resolved-vs-extracted ratio for PHP.
- **Output includes top-N unresolved callees with names**: A `unresolvedCallees` array per language, default top 20, sortable by call frequency, includes the callee name (so a human or agent can see "we have 1200 unresolved calls to `<callee>`" at a glance). `--unresolved-top <n>` flag tunes the cap.
  verification:: Running on any indexed codebase returns a non-empty list of callees that the indexer extracted as call sites but couldn't resolve against the symbol table. The audit phase uses this list to separate "known-unindexable" (language built-ins, framework hook strings, etc.) from "actual indexer gaps."
- **Optional `--exclude-from <catalog-path>` flag filters known-noise callees**: The command accepts an `--exclude-from <markdown-catalog-path>` flag. When supplied, names enumerated in the catalog under `language-builtin`, `framework-noise`, or `un-resolvable-pattern` sections are excluded from the `unresolvedCallees` output. Reads the markdown catalog produced by US2; format is the catalog deliverable defined there.
  verification:: Given a catalog listing `empty` and `isset` as language built-ins, running `rzm code stats --exclude-from <catalog>` on wp-tst returns a `unresolvedCallees.php` array that does NOT contain `empty` or `isset` but DOES contain entries not classified in the catalog. Same command without the flag returns those names as before.

  Note — this filter is purely diagnostic. It changes what the top-N unresolved list shows; it does not modify the underlying `intel_symbol_refs` / `intel_symbol_ref_targets` data. The explorer graph and other consumers are unaffected — graph density is a US3 concern, not a US1 catalog concern.
- **Output includes per-directory distribution**: A `byDirectory` section showing top-N top-level directories by indexed-file count, per language. Lets a user/agent spot when the indexer walked a subset of the expected roots (the wp-tst trap from EFF-0033).
  verification:: Running on `wp-tst` shows `wp-includes`, `wp-admin`, `wp-content` with non-zero PHP file counts (after the batch-indexer fix from `b072a80a`).
- **Subcommand has a small unit test**: A test in `cmd/code_stats_test.go` (or appropriate test location) exercises the query path against a synthetic index DB and asserts the output shape. Doesn't need to cover every edge case — just enough to lock the JSON contract against accidental field drift.
  verification:: `go test ./cmd/...` passes including the new test.

---

### US2 - Sub-agent audit pass + coverage catalog

- id:: ^SPEC-0070-US2
- summary:: Drew's recommended sub-agent audit ("brainstorm all of the PHP features... do an audit of PHP language syntax that we're using") is captured as a reproducible workflow that classifies the top-N unresolved callees and surfaces real indexer gaps. Produces a durable **coverage catalog** consumed by both US1 (filter flag) and US3 (gap targets).
- status:: satisfied

This is the sub-agent loop Drew described. An LLM already knows what PHP built-ins look like and can recognize framework hook patterns — the skill leverages that knowledge to classify the unresolved-callees list, write the classification to a durable markdown catalog, and surface the "real gap" subset to the next phase. The catalog becomes a first-class engagement artifact: future audits start with the existing catalog and refine it rather than starting from scratch.

#### Acceptance Criteria

- **Audit procedure is documented and reproducible**: A documented procedure exists (skill SKILL.md or skill-equivalent inline procedure) specifying: which `rzm code stats` output to ingest, which codebase files to inspect, which indexer source files to cross-reference, and what output to produce.
  verification:: Running the procedure on `wp-tst` produces both a coverage catalog (per below) and an audit report (`docs/reference/analysis/php-coverage-YYYY-MM-DD.md`) enumerating language constructs found, indexed, and missed.
- **Coverage catalog is the primary durable deliverable**: The audit produces a markdown catalog at `docs/reference/coverage-catalogs/<lang>-coverage-YYYY-MM-DD.md`. The catalog classifies each entry from `rzm code stats`' `unresolvedCallees` top-N (and additional entries the auditor surfaces) into exactly one of four categories:
    - `language-builtin` — part of the language itself (e.g., PHP `isset`, `empty`, `sprintf`; Python `len`, `print`; TS `Array.prototype.map`)
    - `framework-noise` — framework-provided registration / hook functions that produce string-callback patterns rather than indexable user code (e.g., WP `add_action`, `do_action`, `apply_filters`; Laravel `Route::get`; Symfony event dispatcher names). Calls TO these are noise; the *string arguments* of these calls are the real gap (handled by US3 string-argument callback resolution)
    - `un-resolvable-pattern` — pattern that fundamentally can't be statically resolved (variable-function calls, dynamic dispatch on runtime values, reflection)
    - `real-gap` — extraction or resolution should have worked. This is the audit's actionable output for US3.

    Each entry includes: name, call count from the diagnostic, classification, one-line rationale, and (for `real-gap` entries) suggested resolution category (e.g., "string-argument callback resolution," "magic method extraction," "namespace inference fix").
  verification:: A valid catalog exists at the documented path with all four classification sections populated. `real-gap` entries have non-empty `suggested-resolution-category` fields.
- **Catalog format is consumable by US1's `--exclude-from` flag**: The catalog format is structured enough that `rzm code stats --exclude-from <catalog>` can parse it and extract the names in `language-builtin`, `framework-noise`, and `un-resolvable-pattern` sections.
  verification:: After the catalog is generated, running `rzm code stats --exclude-from <catalog>` excludes those names from `unresolvedCallees` output. Implementation-wise, the catalog uses a stable bullet/heading format (specified in the audit procedure doc) that the CLI's parser handles.
- **Gap list is generic, not framework-specific**: `real-gap` entries describe extraction/resolution work in PHP-AST terms (e.g., "function_call_expression with string-literal argument where the literal matches an indexed symbol"), not framework function names. The catalog explicitly notes framework instances as examples of patterns, not as gap targets in their own right.
  verification:: No `real-gap` entry names a framework-specific function (no entries like "WordPress add_action support"). Framework instances appear in the rationale text under their pattern entry.

---

### US3 - Close highest-value gaps

- id:: ^SPEC-0070-US3
- summary:: The indexer adds extraction + resolution support for the top-priority generic-PHP patterns identified by US2's catalog — drawn exclusively from the catalog's `real-gap` section. Likely candidates include string-argument callee resolution (the underlying pattern for WP hooks, Laravel routes, Symfony events, `call_user_func`, etc.), magic methods, dynamic includes with path concatenation, and improved call-site resolution. Specific scope is bounded by what the US2 catalog surfaces.
- status:: satisfied

Acceptance criteria authored 2026-06-12 against the US2 catalog's `real-gap` entries.

#### Acceptance Criteria

- **String-argument callback resolution lands as a CallSite emitted per string-literal argument that matches the PHP-identifier regex**: `function_call_expression` arguments whose contents match `^\\?[a-zA-Z_][a-zA-Z0-9_]*(?:\\[a-zA-Z_][a-zA-Z0-9_]*)*$` emit an additional `CallSite` whose callee FQN is the literal value (with backslash-namespaced names split into Pkg/Name). Detection is AST-shape only; no framework-coupled function-name list. Closes catalog `real-gap` → String-argument callback resolution.
  verification:: Commit `77e6b82f`; `pkg/anchors/indexer_php.go::phpCollectStringArgCallees`; unit tests `TestPHPIndexer_StringArgCallback_BasicHook`, `_QualifiedName`, `_NotIdentifier`, `_NonStringArg`; integration test `string_arg_callback_emits_call_edge` in `tests/integration/php/file_context_test.go`.
- **Dynamic includes with path concatenation extract as partial-info ImportEdges with placeholders for unresolved fragments**: `include`/`require` arguments that are `binary_expression` concat chains join literal string fragments and substitute placeholders for constants (`{ABSPATH}`), magic constants (`{__DIR__}`), variables (`{var}`), and unresolvable expressions (`{expr}`). Closes catalog `real-gap` → Dynamic includes with path concatenation.
  verification:: `pkg/anchors/indexer_php.go::phpExtractIncludePath`; unit test `TestPHPIndexer_IncludeRequire` covers four shapes (`ABSPATH . 'wp-cron.php'`, `__DIR__ . '/inc/' . 'helpers.php'`, `__DIR__ . '/inc/' . $name`, plus the v1 bare-literal case); integration fixture `testdata/integration/python-app/vault/php/bootstrap.php`. Direct extraction over wp-includes/**/*.php shows +244 dynamic ImportEdges across 58 files (see audit narrative).
- **Magic methods extraction confirmed already-supported in v1**: PHP magic methods (`__construct`, `__call`, `__invoke`, `__toString`, `__get`, `__set`, `__isset`, `__destruct`) emit as `SymMethod` with FQN `Class::__name` because the `method_declaration` walker applies no name filter. Catalog entry annotated as verified; no code change required. Closes catalog `real-gap` → Magic methods extraction.
  verification:: Unit test `TestPHPIndexer_MagicMethods`; integration subtest `magic_methods_indexed_as_methods` over fixture `testdata/integration/python-app/vault/php/Container.php`.
- **Coverage catalog is refreshed to reflect closed real-gap entries**: After US3 implementation, the catalog's `real-gap` section marks each closed entry with a delivered cross-reference back to the closing commit; deferred entries (method-resolution gap surfaced via `wpdb::prepare` reclassification) carry an explicit `deferred` status with the reason.
  verification:: `docs/reference/coverage-catalogs/php-coverage-2026-06-12.md` updated 2026-06-12 with three delivered closures plus the new method-resolution `real-gap` entry tracked as deferred.
- **All extraction additions land with unit tests in `pkg/anchors/indexer_php_test.go` and integration tests in `tests/integration/php/`**: Mirrors SPEC-0068.US4 polyglot fixture pattern (per `AGENTS.md` Polyglot integration fixture guidance).
  verification:: `go test ./pkg/anchors/... -run TestPHPIndexer` and `go test -race -tags=integration -run TestPHP_FileContext ./tests/integration/php/...` both pass.

---

### US4 - Re-verify in explorer

- id:: ^SPEC-0070-US4
- summary:: After US3 fixes land, the same wp-tst index re-rebuilt against the new binary shows visibly denser connectivity in the explorer tab — the diagnostic numbers from US1 improve in the categories targeted by US3.
- status:: satisfied

Closure verification. Mirrors Drew's "look at that explore view to kinda get an idea of, at least at the file level, does it look like it has a realistic set of edges".

#### Acceptance Criteria

- **The US1 diagnostic shows the targeted categories with improved resolved-edge counts on the same corpus**: Direct extraction over wp-includes/**/*.php (1,048 files) shows PHP imports rise from 779 (use-only) to 1,023, including 244 new dynamic-include ImportEdges across 58 files that the v1 indexer dropped silently. The 779 literal-import baseline is preserved (no regression).
  verification:: Audit narrative `docs/reference/analysis/php-coverage-2026-06-12.md::Audit verification (US4 — 2026-06-12)` records the before/after numbers via direct extraction (the wp-tst `rzm index --code --rebuild` pipeline retains cached files records on the `--code`-only path; this is captured as a compounding follow-up on EFF-0035 — the indexer changes themselves are proven by unit + integration + direct-extraction tests).
- **A short before/after note documents what changed (qualitative + quantitative)**: Captured inline in the audit narrative rather than a separate `docs/audits/php-coverage-followup-*.md` file, since the audit narrative is the durable artifact for this engagement (`audits/` → `reference/analysis/` per EFF-0035 reclassification).
  verification:: `docs/reference/analysis/php-coverage-2026-06-12.md::Audit verification (US4 — 2026-06-12)` section exists with quantitative table, catalog moves, pipeline-cache follow-up note, and explorer-view qualitative note.

---

### US5 - File-scope call extraction (post-EFF-0035 finding)

- id:: ^SPEC-0070-US5
- summary:: The indexer extracts call sites from PHP code at file scope (not wrapped in a function or method). Closes the highest-impact post-closure `real-gap` surfaced by the 2026-06-15 wp-tst assessment: `default-filters.php` and similar file-scope registry files currently produce zero `intel_symbol_refs` rows because `phpCollectCalls` only fires from inside `function_definition` / `method_declaration` switch cases. Cascades into the US3-delivered string-argument callback resolution being scope-limited (works inside function bodies; doesn't fire for file-scope `add_action(...)` registrations).
- status:: satisfied

**Lifecycle note (2026-06-15)**: SPEC-0070's `spec-status` was flipped `complete → active` to add this story. The spec text explicitly frames the audit as iterative ("This spec defines the audit loop, not a fixed set of fixes."), and the EFF-0035 closure was about delivering v1 of the loop — not closing the loop itself. US5 captures the next iteration's first gap, surfaced by post-closure dogfooding. Per SPEC-0051, completed efforts (EFF-0035) stay closed; this new story is picked up by a fresh effort.

Acceptance criteria authored against the catalog `real-gap` entry for File-scope call extraction (`docs/reference/coverage-catalogs/php-coverage-2026-06-12.md`):

#### Acceptance Criteria

- **File-scope `function_call_expression` nodes emit `CallSite` rows with a file-scope owner FQN**: For PHP code at the program root (not inside any `function_definition` or `method_declaration` body), every direct, member, scoped, and nullsafe-member call site emits a `CallSite` row. The owner FQN is the file's namespace (or empty string when no namespace is declared) — agents and downstream tooling can distinguish "owned by `<file>` at file scope" from "owned by `Class::method`".
  verification:: New unit test in `pkg/anchors/indexer_php_test.go` — `TestPHPIndexer_FileScopeCalls` — exercises a fixture containing top-level `add_action(...)` and `add_filter(...)` calls (mimics `default-filters.php` shape) and asserts that `summary.Calls` contains the outer registration calls plus the string-arg callees, all with the expected file-scope owner FQN.
- **String-argument callback resolution applies to file-scope call sites**: The `phpCollectStringArgCallees` helper (delivered under US3 / commit `77e6b82f`) is reachable from file-scope call expressions, not just from inside function/method bodies. `add_action('init', 'theme_init')` at file scope must emit a `CallSite` whose callee resolves to `theme_init` if `theme_init` is indexed in the symbol table.
  verification:: Extended integration subtest in `tests/integration/php/file_context_test.go` — adds a fixture file (e.g. `default-filters-style.php`) with file-scope hook registrations and asserts the string-arg callee edge surfaces via `CallsFromFile`. Mirrors the existing `string_arg_callback_emits_call_edge` subtest pattern.
- **wp-tst re-extraction shows `default-filters.php` populated**: After the file-scope-call indexer change lands and the wp-tst index re-extracts (via `rzm index --rebuild`), `intel_symbol_refs` rows for `src_path='wp-includes/default-filters.php'` is greater than zero — a strong indicator that file-scope WordPress registration code is now in the call graph.
  verification:: Sqlite query result captured in the effort note's verification section: `SELECT COUNT(*) FROM intel_symbol_refs WHERE src_path='wp-includes/default-filters.php'` returns a non-zero count (target: high three-digits or more, consistent with 600 lines of dense `add_action`/`add_filter` registrations).
- **Hotspot signal for PHP improves materially**: As a side effect of populating the call graph at file scope, the `rzm code stats` PHP resolution rate improves (currently 38% on wp-tst). Magnitude TBD; minimum: measurably above the current baseline. This is also the precondition for closing the separate `hotspots` / `doc_coverage` PHP-blind-spot gap tracked as a candidate fresh effort.
  verification:: Before/after `rzm code stats --text` numbers captured in the effort's audit note; PHP `callsResolved` count + `resolutionRate` ratio both rise.
- **No regression in existing call extraction**: Calls inside function/method bodies continue to extract with their existing owner FQN (function/method FQN). The file-scope pass MUST NOT double-emit calls that the structured walker already collects.
  verification:: Existing PHP unit + integration tests pass unchanged; new dedup-check unit test asserts file-scope walking skips into function/method bodies (or otherwise dedupes by byte range).

## Requirements

- The diagnostic recipe in US1 MUST be reproducible against any indexed PHP codebase, not just wp-tst
- All extraction additions MUST land with unit tests in `pkg/anchors/indexer_php_test.go` mirroring SPEC-0068's pattern
- Integration tests in `tests/integration/php/` MUST be extended to cover any new construct added (per SPEC-0068.US4 polyglot fixture pattern)
- The audit MUST NOT introduce new dependencies on dynamic-analysis tooling — this remains a static-analysis spec
- Documented coverage list MUST be authored as a durable note (not just an effort artifact) so future PHP engagements have a starting reference

## Open Questions

_None at present._

## Resolved

- ~~Should the diagnostic (US1) ship as a new `rzm code stats` subcommand or stay as a sqlite-query recipe?~~ **Resolved 2026-06-11 → CLI subcommand.** Rationale: single source of truth for `legacy-codebase-assessor` and this audit; structured JSON for agent consumption; cross-platform (no external sqlite CLI dependency); discoverable via `rzm code --help`. Cost was real but low (~200 lines of Go + a thin test). The `legacy-codebase-assessor` skill can adopt it as a follow-on, consolidating two query implementations.
- ~~Should string-argument callback edges (where a call like `someRegistrar('event', 'my_handler_fn')` carries a string-literal callee name that matches an indexed symbol) be modeled as ImportEdges, CallEdges, or a new "registration" edge kind?~~ **Resolved 2026-06-12 → CallEdge (CallSite).** Rationale: call edges count toward hotspot ranking and dependency-tracing in the explorer view; that's the diagnostic surface this audit targets. Modeling string-arg callees as imports would understate connectivity (imports don't contribute to hotspot scoring); a new "registration" kind would force every downstream consumer (file-context, hotspots, doc_coverage, explorer layout) to learn a third edge type for marginal classification value. EFF-0035 Phase 4.1 landed this in commit `77e6b82f` as an extra `CallSite` emitted per string-literal argument matching the PHP identifier regex; detection is AST-shape only (no framework-coupled function-name list) so the same code covers WP hooks, Laravel routes, Symfony events, and custom registration APIs uniformly.

## Documentation Plan

- **New** `cmd/code_stats.go` — `rzm code stats` subcommand (per resolved Open Question above)
- **New** `cmd/code_stats_test.go` — unit test locking the JSON output contract
- New test cases in `pkg/anchors/indexer_php_test.go` and `tests/integration/php/` per new construct added during US3
- New durable note: `docs/reference/guides/php-indexer-coverage.md` — the "what we cover, what we don't" reference; lives alongside `playbook-legacy-codebase-assessment.md` so engagement starts have a paired playbook+coverage view
- Audit-report archive: `docs/audits/php-coverage-YYYY-MM-DD.md` (one per audit pass)
- Update `docs/specs/technical/php-language-support.md` (SPEC-0068) Compounding Follow-ups as gaps close — promote items from "follow-up" to "delivered" with cross-reference to this spec
- Backport candidate (deferred to EFF-0035 closure): `legacy-codebase-assessor` SKILL.md `Procedure` step 2 (scope diagnostic) — replace the inline sqlite queries with a single `rzm code stats` call. Captured as a Compounding Follow-up on this effort rather than landing during initial v1 since EFF-0034 is closed and we want to avoid frozen-scope-drift on it.
