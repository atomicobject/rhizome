---
type: ReferenceDoc
summary: "PHP indexer coverage catalog: classifies the top unresolved callees from `rzm code stats` on the wp-tst WordPress corpus into language-builtin / framework-noise / un-resolvable-pattern / real-gap sections. Consumed by `rzm code stats --exclude-from` (noise filter) and EFF-0035 US3 (gap-close targets)."
reference-kind: guide
status: active
language: php
corpus: wp-tst (WordPress + bundled themes)
generated: 2026-06-12
generated-via: rzm code stats + manual audit triage
baseline-source: docs/reference/analysis/php-coverage-2026-06-12.md
---

# PHP Coverage Catalog — 2026-06-12 (wp-tst baseline)

Classifies the top unresolved callees from `rzm code stats` on a freshly-indexed WordPress corpus. Consumed by `rzm code stats --exclude-from <this-file>` (filters noise from diagnostic output) and by EFF-0035 US3 (drives gap-close work — only `real-gap` entries become indexer fix targets).

**Baseline numbers** (from `rzm code stats --text` on wp-tst, 2026-06-12):

- PHP: 15,585 symbols across 1,119 files; 58,840 calls extracted, 25,303 resolved → **43% resolution**
- PHP imports: 779 (low — see `real-gap` section on dynamic includes)

**This catalog is a first pass.** It enumerates the top-5 visible unresolved callees plus reasonable extrapolations based on the WordPress corpus shape. Re-run `rzm code stats --unresolved-top 50` for a fuller picture; promote additional entries from raw output into the appropriate section below as they're classified.

---

## language-builtin

PHP language constructs and standard-library functions. These will never resolve to user-code symbols — they're not in the indexable corpus and shouldn't be. Filter from diagnostic output.

- `empty` — language construct (not technically a function)
- `isset` — language construct
- `sprintf` — standard library; string formatting
- `is_array` — standard library; type check
- `in_array` — standard library; array membership
- `count` — standard library; collection size
- `strlen` — standard library; string length
- `substr` — standard library; string slice
- `implode` — standard library; array join
- `is_string` — standard library; type check
- `is_null` — standard library; type check
- `is_numeric` — standard library; type check
- `is_object` — standard library; type check
- `is_bool` — standard library; type check
- `is_callable` — standard library; type check
- `is_int` — standard library; type check
- `array_keys` — standard library; array op
- `array_values` — standard library; array op
- `array_merge` — standard library; array op
- `array_map` — standard library; array op
- `array_filter` — standard library; array op
- `explode` — standard library; string split
- `trim` — standard library; string trim
- `strpos` — standard library; string search
- `str_replace` — standard library; string replace
- `preg_match` — standard library; regex
- `preg_replace` — standard library; regex
- `json_encode` — standard library; JSON
- `json_decode` — standard library; JSON
- `defined` — standard library; constant check
- `function_exists` — standard library; introspection
- `class_exists` — standard library; introspection
- `method_exists` — standard library; introspection
- `gettype` — standard library; introspection
- `var_dump` — standard library; debug
- `print_r` — standard library; debug
- `error_log` — standard library; logging
- `date` — standard library; date format
- `time` — standard library; timestamp
- `microtime` — standard library; high-res time
- `strtolower` — standard library; string case (surfaced 2026-06-12 after first filter pass)
- `array_key_exists` — standard library; array key check (surfaced 2026-06-12 after first filter pass)
- `printf` — standard library; string output (surfaced 2026-06-12 after first filter pass)
- `file_exists` — standard library; file system check (surfaced 2026-06-12 after first filter pass)

## framework-noise

WordPress framework functions and registration APIs. Calls TO these functions are noise (the function name itself is a WP-internal symbol, not user code). The *string arguments* passed to some of these (e.g. `add_action('init', 'my_handler')`) are the real gap — handled separately under `real-gap` as the string-argument callback resolution pattern.

- `add_action` — WP hook registration
- `add_filter` — WP filter registration
- `do_action` — WP hook dispatch
- `apply_filters` — WP filter dispatch
- `wp_enqueue_script` — WP asset registration
- `wp_enqueue_style` — WP asset registration
- `register_post_type` — WP post type registration
- `register_taxonomy` — WP taxonomy registration
- `register_block_type` — WP block registration
- `register_block_style` — WP block style registration
- `get_option` — WP options API
- `update_option` — WP options API
- `get_post` — WP post API
- `get_posts` — WP post API
- `wp_insert_post` — WP post API
- `wp_update_post` — WP post API
- `__` — WP i18n
- `_e` — WP i18n
- `esc_html` — WP escaping
- `esc_attr` — WP escaping
- `esc_url` — WP escaping
- `wp_kses` — WP sanitization
- `current_user_can` — WP auth
- `is_user_logged_in` — WP auth
- `wp_get_current_user` — WP auth
- `get_template_directory` — WP path
- `get_stylesheet_directory` — WP path
- `plugins_url` — WP path
- `wp_die` — WP error
- `is_admin` — WP context check
- `is_singular` — WP context check
- `is_page` — WP context check
- `is_archive` — WP context check
- `have_posts` — WP loop
- `the_post` — WP loop
- `get_the_title` — WP template tag
- `the_title` — WP template tag
- `get_permalink` — WP template tag
- ~~`prepare`~~ — **reclassified 2026-06-12 → moved to `real-gap` (method-resolution).** `wpdb::prepare` is indexed at `wp-includes/class-wpdb.php`, so the 163 unresolved `prepare` calls are a real method-resolution gap (`$wpdb->prepare(...)` collects `CalleeSymbol{Name: "prepare"}` without owner FQN; resolver can't match against `wpdb::prepare`). See real-gap section.

## un-resolvable-pattern

Patterns that fundamentally can't be statically resolved. The indexer extracts the call site (a `function_call_expression` is still a valid AST node) but no static analysis can resolve the callee to an indexed symbol.

- `call_user_func` — dispatches to a callable resolved at runtime (string, array, closure)
- `call_user_func_array` — same as above with array argument unpacking
- `array_walk` — callback applied at runtime; callable is the second arg
- `array_map` — same; callable is first arg
- `array_filter` — same; optional callable is second arg
- `usort` — same; comparison callable is second arg
- `array_reduce` — same; callable is second arg

(Note: when these appear in the top-N, the *function being called* — `call_user_func` etc. — is itself indexed as a language-builtin. The unresolved-ness is structural.)

Variable-function calls (`$fn($args)`, `$obj->{$method}()`) — also un-resolvable; not in the top-N because they're not extracted as named call sites (the indexer currently doesn't emit a CallSite for these at all, which is its own gap but a separate one).

## real-gap

Patterns where extraction or resolution SHOULD work. These are the audit's actionable output and become EFF-0035 US3 targets.

### ~~String-argument callback resolution~~ — **delivered 2026-06-12 (commit `77e6b82f`); scope completed 2026-06-15 by EFF-0037**
- **Pattern**: `function_call_expression` where one or more string-literal arguments name an indexed user symbol. The classic instance is WP's hook system (`add_action('init', 'my_handler')`), but the same pattern appears in Laravel routes, Symfony event subscribers, Slim, and any custom registration API.
- **Indexer behavior today**: emits an additional `CallSite` for each string-literal argument matching the PHP identifier regex. Detection is AST-shape only — no framework-coupled function-name list. See `pkg/anchors/indexer_php.go::phpCollectStringArgCallees`. Fires at both function/method scope AND file scope (the file-scope path delivered by EFF-0037; see entry below).
- **Tests**: `TestPHPIndexer_StringArgCallback_BasicHook`, `_QualifiedName`, `_NotIdentifier`, `_NonStringArg`; integration test `string_arg_callback_emits_call_edge`. EFF-0037 adds `TestPHPIndexer_FileScopeCalls` covering the file-scope path of the same pattern, plus integration test `file_scope_hook_registration_persists_symbol_refs`.

### ~~Dynamic includes with path concatenation~~ — **delivered 2026-06-12 (EFF-0035 Phase 5)**
- **Pattern**: `include`/`require`/`include_once`/`require_once` where the argument is a `binary_expression` concat chain (`require ABSPATH . 'wp-cron.php'`, `require __DIR__ . '/inc/' . $name`).
- **Indexer behavior today**: `pkg/anchors/indexer_php.go::phpExtractIncludePath` recursively joins literal fragments and substitutes placeholders for unresolved fragments: `{ABSPATH}`, `{__DIR__}`, `{any-constant-name}`, `{var}`, `{expr}`. Surfaces partial-info `ImportEdge` rows; downstream tooling can still grep the literal portion or trace the constant.
- **Tests**: `TestPHPIndexer_IncludeRequire` covers four shapes (bare literal, constant-prefix, magic-constant-prefix + two literals joined, variable-suffix). Integration fixture `testdata/integration/python-app/vault/php/bootstrap.php`.
- **Impact (measured)**: +244 dynamic include ImportEdges across 58 wp-includes files via direct extraction (PHP imports 779 → 1,023). See audit narrative.

### ~~Magic methods extraction~~ — **verified already-supported 2026-06-12**
- **Pattern**: PHP magic methods (`__construct`, `__call`, `__get`, `__set`, `__invoke`, `__toString`, `__destruct`, `__sleep`, `__wakeup`, `__isset`, `__unset`, `__clone`, `__debugInfo`) inside classes.
- **Indexer behavior today**: `method_declaration` walker applies no name filter, so magic methods emit as `SymMethod` with FQN `Class::__name`. No code change needed; behavior pinned with `TestPHPIndexer_MagicMethods` (8 magic methods) and integration subtest `magic_methods_indexed_as_methods`.

### ~~File-scope call extraction~~ — **delivered 2026-06-15 by EFF-0037**

- **Pattern**: PHP code that lives at file scope rather than inside a `function_definition` or `method_declaration`. Canonical instance: `wp-includes/default-filters.php` — WordPress's central hook registry, 600+ lines of file-scope `add_action(...)` / `add_filter(...)` calls. Every theme/plugin's top-level bootstrap code (custom-post-type registrations, hook registrations executed at require-time) hits this same shape.
- **Indexer behavior today**: `pkg/anchors/indexer_php.go::phpCollectCalls` gained a `stopAtBoundaries bool` parameter. After `walkNode(root, "")` completes (namespace fully resolved), `IndexFile` runs a second pass: `phpCollectCalls(root, src, namespace, filePath, &calls, true)`. The boundary-skip returns early on `function_definition` / `method_declaration` / `class_declaration` / `interface_declaration` / `trait_declaration` nodes — those are already collected by the structured walker's per-function `phpCollectCalls` invocations. File-scope owner FQN is the namespace (or empty string when no namespace), symmetric with how free functions are already attributed.
- **Tests**: `TestPHPIndexer_FileScopeCalls` (basic file-scope add_action/add_filter); `_Namespaced` (namespaced file-scope); `_NoRegression` (function-body + file-scope mixed, asserts no double-emit). Integration test `file_scope_hook_registration_persists_symbol_refs` queries `intel_symbol_refs` directly.
- **Impact (measured)**: wp-tst v1.7.0 reindex shows `default-filters.php` rises from 0 → 537 `intel_symbol_refs` rows; PHP `callsExtracted` 70,771 → 85,257 (+14,486); `callsResolved` 26,875 → 35,306 (+8,431); resolution rate 37.97% → 41.41% (+3.44pp). See audit narrative `EFF-0037 verification — 2026-06-15`.
- **Lifecycle**: SPEC-0070 was re-opened `complete → active` on 2026-06-15 to add US5; EFF-0037 froze US5 + this catalog entry.

### Method-resolution gap (member calls lose owner-class context)

- **Pattern**: `$obj->method()` and `$obj?->method()` calls. Today `phpCollectCalls` stores `CalleeSymbol{Name: "method"}` with no `Pkg`/owner-FQN. The resolver joins call targets to symbols on `(lang, fqn)`, so even when the owning class's method exists in the symbol table (e.g., `wpdb::prepare` is indexed at `wp-includes/class-wpdb.php`) the call doesn't resolve. Surfaced 2026-06-12 by `prepare` (163 unresolved calls — `$wpdb->prepare(...)`) after reclassifying it out of framework-noise.
- **Indexer behavior today**: emits a `CallSite` with bare method name; resolution silently fails because there's no class context to disambiguate. Many distinct `prepare`/`get`/`set` methods across the corpus all collapse to one unresolved name.
- **Impact estimate**: high — every member call on a typed variable suffers from this; the unresolved-callees list is dominated by short method names that exist as indexed methods on some class but can't be resolved without type inference.
- **Suggested implementation**: deferred. Closing this needs lightweight type inference (at minimum: track `new ClassName()` assignments to local variables; resolve `$var->m()` against the inferred class). Out of EFF-0035 scope; tracked as a compounding follow-up on EFF-0035 (`needs type inference; large surface; defer to follow-on effort`).

### TS resolution gap (separate concern, possibly own effort)
- **Observation**: TS at 0.4% resolution rate (199 of 55,608 calls) is dramatically worse than PHP at 43%. The corpus is WordPress block-editor JS which is jQuery-heavy, so much of the unresolved volume is framework noise (`$`, `extend`, `on`, `each` — jQuery API).
- **Suggested implementation**: out of scope for SPEC-0070 PHP audit. Flag as a follow-on: either add a `ts` coverage catalog for jQuery + DOM API noise and audit the TS indexer the same way, or accept TS at low resolution and scope EFF-0035 to PHP-only.

---

## How this catalog is consumed

**By `rzm code stats --exclude-from`** (US1): names listed under `language-builtin`, `framework-noise`, and `un-resolvable-pattern` are excluded from the `unresolvedCallees` output. After applying this catalog, the top-N unresolved should surface actual user-code symbols not yet resolving — the real-gap signal.

**By EFF-0035 US3** (gap close): each entry in `real-gap` becomes an implementation target. Targets describe PHP AST patterns, not framework-specific function names. The catalog's `real-gap` count drops as resolution work lands.

## Refresh policy

Re-run `rzm code stats` after each US3 implementation pass; update this catalog by moving entries between sections (e.g., when string-argument callback resolution lands, the WP hook registrations move from `framework-noise` → still framework-noise for the call itself, but a NEW section "resolved-via-string-arg" may emerge for the registered handlers that now have incoming edges).

If new high-volume unresolved names appear that aren't in this catalog, classify them and append.
