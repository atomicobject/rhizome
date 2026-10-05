---
type: ReferenceDoc
summary: >
  Audit narrative paired with the PHP coverage catalog. Records the wp-tst baseline
  numbers, the audit procedure, classification decisions for each top-N unresolved
  callee, and (in later phases) before/after verification of gap-close work.
reference-kind: analysis
status: active
audit-date: 2026-06-12
audit-corpus: wp-tst (WordPress + bundled themes)
audit-binary: rhizome built from commit ac2deb73 (EFF-0035 Phase 1)
catalog: docs/reference/coverage-catalogs/php-coverage-2026-06-12.md
effort: EFF-0035 (docs/efforts/2026-06-11-18-45-php-indexer-language-coverage-audit.md)
spec: SPEC-0070 (docs/specs/technical/php-indexer-language-coverage-audit.md)
---

# PHP Indexer Coverage Audit — 2026-06-12 (wp-tst baseline)

## Baseline diagnostic

Captured via `rzm code stats --text` on a local WordPress checkout (`wp-tst`) after `rzm index --code` against the WordPress + bundled themes corpus.

```
[php] symbols=15585 files=1119 modules=1832
       calls extracted=58840 resolved=25303 unresolved=33537 (rate=0.43)
       member_refs=0 imports=779
       top unresolved callees:
         empty                          2319 calls
         isset                          2114 calls
         sprintf                        1157 calls
         is_array                       789 calls
         in_array                       654 calls

[ts] symbols=250 files=22 modules=455
       calls extracted=55608 resolved=199 unresolved=55409 (rate=0.00)
       member_refs=79734 imports=0
       top unresolved callees:
         $                              991 calls
         push                           628 calls
         extend                         627 calls
         on                             596 calls
         each                           530 calls
```

## Findings

### PHP at 43% resolution — better than the explorer view suggested

The explorer graph showed a sparse outer ring of mostly disconnected nodes, which I initially read as "very few edges getting drawn." The actual numbers tell a more nuanced story: 25,303 PHP call edges resolve. That's not zero — but it's roughly 60% of what *could* resolve. The visual sparseness is real, but it reflects both (a) the 57% of calls that don't resolve AND (b) some calls resolving to symbols that aren't visible in the current explorer layout for other reasons.

### TS at 0.4% — the bigger story

TS is dramatically worse. 199 of 55,608 calls resolve. The corpus is mostly WordPress block-editor JS which is jQuery-heavy (`$`, `extend`, `on`, `each` dominate the top-N), but even accounting for jQuery noise, the resolution rate is shockingly low.

Two interpretations, both probably partially true:

1. **jQuery + DOM API noise is huge.** The top-5 are all jQuery / Array.prototype methods. These will never resolve to user-code symbols. Filtering them out via the catalog (same pattern as PHP) should improve the apparent rate.
2. **The TS indexer's resolution layer may be doing less than the PHP indexer's.** 199 resolved calls across 250 symbols is about 0.8 resolutions per symbol on average — very low. Worth a separate audit; out of scope for SPEC-0070 (PHP language coverage).

### Member refs by language reveals indexer-categorization quirk

PHP shows `member_refs=0` while TS shows `member_refs=79,734`. The PHP indexer emits member call expressions (`$obj->method()`) under `ref_kind=calls`, not `member_ref`. The TS indexer uses `member_ref` for the same construct. This is a per-indexer categorization difference, not a bug — but it means the `rzm code stats` "member_refs" field isn't an apples-to-apples comparison across languages. Worth documenting in the `rzm code stats` help text or normalizing later. Captured as a Compounding Follow-up.

### 779 imports — confirms dynamic-include gap

WordPress has thousands of `include`/`require` sites; we captured 779. The gap is almost entirely dynamic includes with path concatenation (`require ABSPATH . 'wp-cron.php'` style), which the v1 indexer skips. This is a known SPEC-0068 follow-up; SPEC-0070 US3 picks it up explicitly under the `real-gap` section.

### Top-5 PHP unresolved are all language built-ins

`empty`, `isset`, `sprintf`, `is_array`, `in_array` — all standard library or language constructs. They'll never resolve to user-code symbols. The catalog filters them so the next pass shows the real gaps.

## Catalog produced

See `docs/reference/coverage-catalogs/php-coverage-2026-06-12.md`.

The catalog classifies the observed top-5 plus reasonable extrapolations into four sections:

| Section | Entries | Purpose |
|---|---|---|
| `language-builtin` | ~40 (PHP language constructs + std library highlights) | Filter from diagnostic — never resolvable |
| `framework-noise` | ~37 (WP hook system, options API, escaping, template tags, i18n) | The functions themselves are noise; their string-argument callbacks are the real gap (see real-gap section) |
| `un-resolvable-pattern` | 7 (`call_user_func` family, runtime callable dispatch) | Structurally un-static-analyzable |
| `real-gap` | 4 patterns | EFF-0035 US3 targets |

The first three sections are noise to filter; the fourth is actionable.

## Real-gap targets (input to US3)

Drawn from the catalog. Ordered by estimated impact:

1. **String-argument callback resolution** (high impact). Patterns like `add_action('init', 'my_handler')` register a callback by string name. The indexer extracts the outer call but not the string-argument callback target. Closing this resolves a large class of orphan-symbol situations — every WP theme has hundreds of these registrations.
2. **Dynamic includes with path concatenation** (medium impact). `require ABSPATH . 'foo.php'` style. Doesn't add user-code-to-user-code edges but materially improves include-graph completeness. Confirmed gap from the 779-import count.
3. **Magic methods extraction** (low-medium impact). PHP `__construct`/`__call`/`__invoke` etc. Need to audit `indexer_php.go method_declaration` walker to confirm whether these are extracted.
4. **TS resolution gap** (out of scope here; flagged for separate effort). 0.4% rate is too low to address inside SPEC-0070's PHP audit. Either spin a separate TS audit or accept low TS resolution for v1.

## Filter verification (T018, 2026-06-12)

`rzm code stats --exclude-from docs/reference/coverage-catalogs/php-coverage-2026-06-12.md --text` ran against the same wp-tst index. Result: filter works as designed.

Top-5 PHP unresolved **before** catalog filter:

| Name | Calls |
|---|---|
| `empty` | 2319 |
| `isset` | 2114 |
| `sprintf` | 1157 |
| `is_array` | 789 |
| `in_array` | 654 |

Top-5 PHP unresolved **after** catalog filter:

| Name | Calls |
|---|---|
| `strtolower` | 237 |
| `array_key_exists` | 207 |
| `printf` | 186 |
| `prepare` | 163 |
| `file_exists` | 141 |

Four of the five new entries are next-tier PHP std-lib functions (catalog updated to include them in `language-builtin`). One is interesting:

- **`prepare` (163 calls)** — almost certainly `$wpdb->prepare()` method call expressions. Two possibilities: (a) framework-noise (WP `$wpdb` API; we'd add to `framework-noise` and move on), or (b) method-resolution gap (the `wpdb::prepare` symbol IS indexed but method call resolution isn't matching). Tentatively classified as framework-noise pending Phase 4 audit confirmation. Catalog entry includes the hypothesis.

TS top-N **unchanged** by the filter — confirms the filter only excludes what the catalog enumerates. The catalog has zero TS entries; a TS catalog would need a separate file (or sections in this one) covering jQuery + Array.prototype noise. Out of scope for SPEC-0070's PHP audit.

**Filter behavior confirmed end-to-end.** T018 satisfied.

## Catalog refresh policy

The catalog is a living document. Each refinement cycle:

1. Run `rzm code stats --exclude-from <catalog> --text` to see what's still in the top-N
2. Classify the newly-visible entries and add them to the appropriate catalog section
3. After enough iteration, the top-N becomes signal-dense — entries that are genuine `real-gap` candidates dominate

This audit document is a snapshot; the catalog is the live artifact.

## Next steps

- T018: verify `--exclude-from` filter end-to-end against this catalog
- Phase 4 (US3): implement string-argument callback resolution in `pkg/anchors/indexer_php.go`; add unit + integration tests; re-run baseline
- Phase 5 (US4): document before/after numbers in a follow-up section of this file (or a sibling audit doc)

---

## Audit verification (US4 — 2026-06-12)

After landing Phase 4 work (string-argument callback resolution in commit `77e6b82f`; dynamic includes with path concatenation; magic methods verified already-supported), this section captures before/after numbers.

### Indexer changes shipped under EFF-0035 (catalog `real-gap` closures)

1. **String-argument callback resolution** (commit `77e6b82f`, [[php-indexer-language-coverage-audit#^SPEC-0070-US3|SPEC-0070.US3]]). Pattern: `function_call_expression` argument that is a string literal matching the PHP identifier regex emits an additional `CallSite` whose callee is the literal value. Resolves WP `add_action('init', 'my_handler')` → `my_handler` edge; same shape covers Laravel routes, Symfony events, `call_user_func`, custom registration APIs. AST-shape detection; no framework function-name list.
2. **Dynamic includes with path concatenation** (this commit). Pattern: `include`/`require` argument that is a `binary_expression` concat chain (`ABSPATH . 'foo.php'`, `__DIR__ . '/inc/' . $name`). New `phpExtractIncludePath` recursively joins literal fragments and emits placeholders for unresolved fragments (constants `{ABSPATH}`, magic constants `{__DIR__}`, variables `{var}`, expressions `{expr}`). Surfaces partial-information `ImportEdge` rows that the v1 indexer silently dropped.
3. **Magic methods extraction** (verified already-supported; pinned with `TestPHPIndexer_MagicMethods`). The `method_declaration` walker applies no name filter; `__construct`, `__call`, `__invoke`, `__toString`, etc. emit as `SymMethod` with FQN `Class::__name`. No code change needed; catalog entry annotated and integration test added.

### Quantitative impact (direct indexer extraction over wp-includes/**/*.php)

The Rhizome wp-tst index pipeline preserves cached `files` records when `rzm index --code --rebuild` runs without `--semantic` (`PrepareRebuild` path resets domain tables but the per-file hash invalidation does not re-fire from the file-walk for unchanged content). To verify the new extraction quantitatively without disturbing the user's wp-tst DB state, the new indexer was driven directly across the wp-includes subtree (1,048 files):

| Metric | Before EFF-0035 indexer changes | After (direct extraction) | Delta |
|---|---:|---:|---:|
| Total PHP imports extracted (wp-includes) | 779 (use-only) | 1,023 | **+244 (+31.3%)** |
| Of those: literal include/require + use | 779 | 779 | 0 |
| Of those: dynamic include w/ placeholder | 0 | 244 | **+244** |
| wp-includes files emitting dynamic includes | 0 | 58 | **+58** |

Representative new ImportEdges (paths the v1 indexer dropped):

```
{ABSPATH}{WPINC}/class-requests.php
{BLOCKS_PATH}legacy-widget.php
{BLOCKS_PATH}widget-group.php
{BLOCKS_PATH}require-dynamic-blocks.php
{__DIR__}/shared/item-should-render.php
{__DIR__}/shared/render-submenu-icon.php
{expr}/Renderer.php
{var}
```

### Call-resolution rate (string-argument callbacks)

The string-argument callback resolution (Phase 4.1) makes registered handler functions reachable from their `add_action`/`add_filter` call sites. For the `bootstrap_theme → theme_handler` integration fixture, the edge resolves via the indexer-emitted CallSite; the integration test (`tests/integration/php/file_context_test.go::string_arg_callback_emits_call_edge`) pins this end-to-end. Full wp-tst delta on call resolution requires the indexer-version-bump rebuild path to be exercised (see Compounding Follow-ups on EFF-0035).

### Catalog moves

The `real-gap` section's three Phase 4 entries are now delivered:

- **String-argument callback resolution** — closed (commit `77e6b82f`)
- **Dynamic includes with path concatenation** — closed (this commit)
- **Magic methods extraction** — verified already-supported in v1; pinned with regression test

Remaining `real-gap` entry:

- **Method-resolution gap** (newly reclassified from framework-noise after `wpdb::prepare` investigation) — deferred. Requires lightweight type inference; tracked as compounding follow-up on EFF-0035.

### Pipeline-caching follow-up

Re-running `rzm index --code --rebuild` against an existing wp-tst index does NOT re-extract files whose content hash matches; the IndexerVersion bump from v1.5.0 → v1.6.0 should invalidate the per-file cache but the `files` table in the unified DB retains v1.5.0 stamps post-rebuild on the `--code`-only path. Surfaces as a compounding follow-up on EFF-0035: `rzm index --code --rebuild` either (a) propagates the indexer-version bump through the file table during ResetDomain → re-extraction, or (b) `--rebuild` should require `--semantic --code` to be fully effective, with the help text updated to reflect the smaller scope. Direct-extraction numbers above proved the indexer change works; the pipeline-cache invalidation issue is orthogonal.

### Explorer view

Qualitative explorer impact is not captured in this run — the wp-tst DB still holds v1.5.0-extracted edges due to the pipeline-cache issue above. Once the next `rzm index --code --semantic --rebuild` (or equivalent fresh build) lands, the explorer should densify in two ways: (1) wp-includes files gain outgoing edges to their dynamic-include targets (currently a ring of orphan files); (2) registered hook handlers (`add_action` strings) gain inbound edges from their registration sites.

---

## Post-closure findings — wp-tst assessment 2026-06-15

After EFF-0035 closed (2026-06-12), the `legacy-codebase-assessor` skill ran a full assessment against wp-tst with the v1.6.0 binary and produced `docs/assessment/findings-summary.md` in the wp-tst repo. The assessment confirmed three EFF-0035 deliverables landed correctly in production and surfaced three new gaps now tracked.

### Confirmed in production

| Feature | Result | Evidence |
|---|---|---|
| Dynamic includes | **PASS** | 709 entries with `{ABSPATH}`, `{__DIR__}`, `{var}`, `{expr}`, `{BLOCKS_PATH}` placeholders in `intel_import_refs` (above the +244 projected from wp-includes/**/*.php direct extraction — wider scope picked up more) |
| Magic methods | **PASS** | 385 `__construct` entries with FQNs like `IXR_Client::__construct`, all `kind=method` |
| Coverage catalog `--exclude-from` filter | **PASS** | Removes built-ins; remaining top unresolved surfaces real gaps: `prepare`, `__construct`, `get_instance` |

### New gaps (post-closure)

These are tracked in the catalog's `real-gap` section and in the durable reference `docs/reference/guides/php-indexer-coverage.md` under "Known gaps (tracked)". They were not visible during the EFF-0035 audit (Phase 4 fixture-based testing exercised the inside-function-body path) and only surfaced when the wp-tst assessment ran against file-scope-heavy WordPress core code.

1. **File-scope call extraction (highest impact).** Top-level `add_action` / `add_filter` / etc. produce zero `intel_symbol_refs` rows. `wp-includes/default-filters.php` — WordPress's central hook registry, 600 lines of file-scope registrations — has zero CallSite rows in the v1.6.0 index. Root cause: walker invokes `phpCollectCalls` only from `function_definition` / `method_declaration` cases. Cascades into the EFF-0035 string-argument callback resolution being scope-limited (works inside function bodies; doesn't fire at file scope). Tracked under SPEC-0070 US5 + EFF-0037.

2. **`hotspots` + `doc_coverage` reports return JS-only on sparse-PHP corpora.** wp-tst's 38% PHP call resolution leaves the call graph too sparse for hotspot scoring; PHP authority scores collapse to zero, so both reports fall back to JS/TS files with high in-module resolution. WP's documentation-priority signal is lost. Fix candidates: import-fan-in fallback metric, or a `--lang` filter when call resolution < ~60%. Out of SPEC-0070 scope (report-scoring, not indexer extraction); candidate fresh effort.

3. **`rzm index --code --rebuild` silent no-op.** When the binary's `IndexerVersion` matches what's already stamped in the `files` table, the rebuild is effectively a no-op but emits no confirmation message. Compounds the existing pipeline-cache invalidation gap below. Candidate fresh effort.

### Validation snapshot

String-argument callback resolution works as designed **inside function bodies** — verified by three production edges on wp-tst: `wp_cron → _wp_cron`, `WP_Customize_Manager::__construct → wp_cron`, `WP_Scripts::init → wp_default_scripts`. The shape works; the missing surface is file-scope code, addressed by the file-scope call extraction gap above.

---

## EFF-0037 verification — 2026-06-15

EFF-2026-06-15-13-51 closed the highest-impact post-EFF-0035 finding (file-scope call extraction) by adding a `stopAtBoundaries bool` parameter to `phpCollectCalls` and invoking a second pass at the program root in `IndexFile`. The boundary-skip prevents double-emission for top-level functions/methods/classes. `IndexerVersion` bumped v1.6.0 → v1.7.0. Two-pass walker design chosen for consistency with the C# and TS indexers (architectural symmetry — see Execution Notes on EFF-0037).

### Verification corpus

wp-tst (WordPress + bundled themes), fresh-rebuilt against the v1.7.0 binary on 2026-06-15. Per-file `files` table fully stamped at v1.7.0 (1,834 PHP files; 456 TS files).

### Quantitative deltas

| Metric | v1.6.0 baseline | v1.7.0 after EFF-0037 | Delta |
|---|---:|---:|---:|
| `default-filters.php` `intel_symbol_refs` rows | 0 | **537** | **+537** |
| PHP `callsExtracted` | 70,771 | **85,257** | **+14,486** |
| PHP `callsResolved` | 26,875 | **35,306** | **+8,431** |
| PHP `resolutionRate` | 37.97% | **41.41%** | **+3.44 pp** |
| PHP `imports` | 1,488 | 1,488 | 0 (unchanged — dynamic includes were already covered by EFF-0035) |

`default-filters.php` going from 0 → 537 refs is the direct US5 verification target: WordPress's 600-line file-scope hook registry now contributes to the call graph for the first time. The +14,486 net `callsExtracted` reflect every file-scope call site across the wp-tst corpus that v1.6.0 missed — themes' `functions.php` bootstrap, plugin top-level registrations, procedural admin scripts.

### Resolution-rate context

The 3.44 percentage-point improvement (37.97% → 41.41%) is meaningful but understated by the raw ratio. The absolute number of resolved calls grew from 26,875 → 35,306 (+31%), and the unresolved-callees top-N now surfaces sharper signal because the file-scope outer registration calls (`add_action`, `add_filter` — framework-noise) get filtered by the existing `--exclude-from` catalog flag. The string-arg callees from file-scope registrations are now reachable as actual call edges.

### Catalog move

The catalog's **File-scope call extraction** entry moves from `real-gap` to delivered. The string-argument callback resolution entry's scope-limitation annotation is removed (now complete at both function-body and file-scope).

### Out-of-scope cascades observed

- **Hotspot signal** for PHP improves but the report itself still returns JS-only on the wp-tst corpus. Reaching the threshold where PHP authority scores stop collapsing to zero needs either the report-scoring fallback fix (hotspot/doc_coverage PHP-blind-spot follow-up) or a further-improved resolution rate. Not in EFF-0037 scope.
- **`rzm index --semantic --code --rebuild` silent no-op** observed again during EFF-0037 verification: the rebuild form exits in ~156ms without doing real work even when the DB has been freshly deleted. The reliable verification path is `rzm index --code` (without `--semantic`, which sidesteps the progress-bar log suppression in non-TTY context). This is a separate operational gap; the verification numbers above were captured via the workaround.
