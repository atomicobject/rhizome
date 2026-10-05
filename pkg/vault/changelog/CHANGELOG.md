# Changelog

## [Unreleased]

## [v0.50.5] - 2026-08-11

- Make `rzm index --rebuild` clobber and recreate the unified SQLite index, including stale WAL/SHM sidecars.

## [v0.50.4] - 2026-08-11

- Indexing now always discovers exact `CONTEXT.md` files without weakening repository containment or ignore rules.
- Semantic-query now uses indexed execution paths that reduce latency while preserving output parity and reporting actionable readiness problems.
- One-shot agent and CLI operations now avoid result-neutral runtime startup, watchers, leader work, and unnecessary stores.
- Effort identifiers now support a filename-derived DATETIME strategy with deterministic allocation and collision reconciliation.
- The release branch now uses migrated DATETIME effort identifiers across its authoritative notes and structured references.

## [v0.50.3] - 2026-07-31

- Improved file-context performance and consistency while preserving indexed retrieval parity and making freshness and truncation behavior explicit.

## [v0.50.2] - 2026-07-23

- Release publishing is now atomic, retry-safe, topology-validated, and supports both main-line releases and release-branch hotfixes.
- Pinned command delegation now uses one shared resolver and reports multi-target results accurately.
- Targeted agent startup now uses a fast minimal path and read-only indexed enrichment for richer context with lower latency.

## [v0.50.1] - 2026-07-18

- Repository configuration now preserves settings it does not yet understand, improving compatibility across versions.
- Validation now provides deterministic identifier repair and link rewriting, while warning when delegated repositories use unsupported configuration.
- Code intelligence now resolves modern TypeScript and JavaScript modules and relationships more accurately across NodeNext, monorepo, ESM, CommonJS, and barrel patterns.

## [v0.50.0] - 2026-07-17

- Added projection-backed validation, deterministic identifier reconciliation, transactional repair operations, CI suites, and executable remediation guidance.
- Consolidated public API and agent surfaces around typed registries and canonical versioned contracts while retiring legacy routes, filenames, compatibility paths, and configuration keys.
- Added first-launch index readiness signaling and retries, plus runtime-aware SQLite locking and safeguards for shared filesystems and WAL recovery.
- Replaced generated answer cards with richer source-owned semantic chunks for ontology and code retrieval.
- Hardened web data lifecycles, restored interface-aware note search and graph document discovery, and restored public workspace projection parity.
- Fixed embedded-node GraphQL resolution for Markdown and code-path collisions while retaining code reference fallbacks.
- Added evidence-grounded release orchestration with reviewable artifacts, curated changelog generation, validation, resumable publishing, and safe partial retries.

## [v0.49.0] - 2026-06-19

- PHP support added for indexing and code intelligence, with better coverage on legacy PHP codebases.
- Sparse-graph fallback keeps hotspot and doc coverage reports useful even when call graphs are incomplete.
- Validation now surfaces next actions and clearer issue states.
- Init/onboarding improved with smoother bootstrap, self-hosted dev binary delegation, and starter management workflows.
- Search and query outputs now provide better ranking, rationale evidence, and tighter validation.
- Spec-driven workflow guidance and agent docs were simplified and expanded for easier use.
- Removed the `RHIZOME_SQLITE_TXLOCK` tuning knob; Rhizome now selects transaction locking internally per operation.

## [v0.48.0] - 2026-05-21

- Better link hygiene checks, with validation catching and fixing more broken or non-Obsidian-friendly links.
- Execution notes now use UTC timestamps and event kinds for clearer effort history.
- Identifier-first Obsidian wikilinks and alias mirroring are now the preferred note-linking path.
- Live serve/index behavior is more reliable with shared SQLite handles and improved freshness events.
- Notes and type navigation now share a single scrollable rail for easier browsing.
- macOS installer PATH registration now fails fast when `/etc/paths.d` cannot be written.

## [v0.47.0] - 2026-05-14

- Add the `complex-domain` starter for source-backed requirements, feature areas, domain models, workflows, and traceability views.
- Add recipes and views for requirement coverage, source review, feature-area backlogs, and domain traceability.
- Add domain-focused skills for requirements ingest, curation, modeling, workflow mapping, spec drafting, traceability review, and domain backport.
- Support starter composition through skill-template overlays, including `complex-domain` extending `spec-driven` skills at render time.
- Add `skill-overlays` validation and `--skill-overlay-manifest` debug output for init runs.
- Switch effort closure to a `Closure Checklist` and remove the old `blocked` / `audit-status` / `backport-status` / `compound-status` workflow fields.

## [v0.46.0] - 2026-05-11

- Added a focused-note **Info** sidebar with related notes, code links, issues, nearby nodes, and local graph
- Added **List/Info** sidebar tabs and automatic Info focus when a pane is selected
- Kept focused note state in the URL `note=` query and clear it when the last pane closes
- Rendered wikilinks and note links now use real web URLs while preserving click-to-open behavior
- Improved stacked-pane focus/scroll behavior for more reliable navigation
- Improved note and section resolution, including structural refs and `name` frontmatter title fallback
- Moved the local graph out of the inline note pane and into the sidebar Info view

## [v0.45.1] - 2026-05-10

- More reliable cache readiness checks during concurrent refreshes.
- Retry certain unique-constraint index failures with a fresh rebuild.
- Release builds now include the frontend assets automatically.
- Improved startup robustness when a crawl is already in progress.
- Fewer false failures from partially stale index state during rebuilds.

## [v0.45.0] - 2026-05-10

- Better configured-table editing with improved overlays, row actions, and inline field editing
- More reliable edit sessions across the web UI and API, including cleaner dirty-state handling and replay behavior
- Fewer ontology/live-sync surprises: indexing, visibility, and locking issues are hardened
- Faster and more accurate configured views from ontology pushdown and full read-model replacement
- Action-items and other views now fail more gracefully, with richer error reporting instead of crashes
- Smoother setup and runtime commands for install, init, serve, and index
- Stricter view/config validation surfaces invalid recipes earlier

## [v0.44.0] - 2026-05-07

- Unified the configured type home and table view experience
- Added direct cell editing for enum, boolean, and relation fields
- Added GraphQL-backed configured views with ordered grouped tables
- Improved ontology table loading with indexed field-value read support
- Added core identity and action item starter templates
- Added item-backed embedded action items and unified type-home entry points

## [v0.43.0] - 2026-05-04

- New public API for GraphQL and REST access to ontology data
- Added an in-app GraphQL explorer for public queries
- Aligned CLI and web GraphQL behavior for parity
- Stricter validation now catches invalid lifecycle states and broken links earlier
- Improved ontology/schema validation for schema-first remediation
- Hardened query recipes for skill metadata and saved query use
- Breaking/behavior change: previously tolerated invalid notes, links, or lifecycle states may now fail validation

## [v0.42.0] - 2026-05-02

- Support nested spec and effort directories without false typed-note classification.
- Keep freeform sibling notes untyped unless they provide the required identifier.
- Add first-class `Plan` notes and updated spec-driven authoring guidance.
- Add `note-by-path` for loading a note and its immediate neighborhood.
- Add batch `next-id` allocation for creating multiple same-type notes at once.
- Add `companion-docs` validation to catch broken companion-doc paths earlier.
- Update docs for durable wikilinks, multi-template installs, and the new id workflow.

## [v0.41.0] - 2026-05-02

- Add safe heading rename commands (`note rename-heading`, `agent note-rename-heading`) that update links and guard fragile external references.
- Switch embedded nodes to identifier-backed block IDs and add validation for orphaned block IDs to reduce broken references.
- Extend `validate` with new checks and auto-fix guidance for fragile external links and block ID issues.
- Promote query recipes to first-class, humanized artifacts with improved CLI (`query-recipe`, `query-recipe render`) and refreshed examples.
- Enhance `init` interactive flow with a settings menu, including toggling the default workflow template.
- Update agent skills, ontology docs, and starters to remove context packs, clarify quality gates, and improve spec-driven workflows.

## Unreleased

- Improve spec-driven story authoring: section-backed embedded nodes now parse metadata bullets, and starter specs use descriptive `USn - Outcome` headings with list-style `#criterion` acceptance criteria.
- Repo launchers now auto-repair missing or stale pinned platform binaries before running normal commands.
- Add `rhizome.devBinaryDir` for self-hosting development repos that must delegate to `bin/<goos>/rzm` without a release pin.
- Promote query-recipe out of `ontology`: `rzm query-recipe {list,show,validate,run}` and `rzm agent query-recipe {list,validate,run}` are the new entry points; the old `rzm ontology query-recipe` and `rzm agent ontology-query-recipe` paths are removed.
- Rename MCP tool `ontology_query_recipe` to `query_recipe`.
- Bump recipe `apiVersion` from `rhizome.ontology-query-recipe.v1` to `rhizome.query-recipe.v1`. Recipes carrying the old envelope now fail validation as `unsupported_api_version`.
- Default `rzm query-recipe list` to a human-friendly table and `rzm query-recipe validate` to grouped per-recipe issue rendering. Add `--json` to either for the legacy JSON shape.
- Add `rzm query-recipe show <id>` printing problem, inputs, GraphQL preview, output contract, adaptation guidance, and a synthesized example invocation.
- Add `rzm query-recipe run --summary` to emit a stderr summary footer alongside the JSON envelope.
- Fix the bundled `story-acceptance-pack` recipe so it selects `UserStory.efforts` (plural) to match the live schema.

## [v0.40.0] - 2026-04-30

- Add integrated agent chat surface with richer retrieval tools and provider/model filtering.
- Introduce saved ontology query recipes (YAML + GraphQL variables) and `query_recipe` CLI support.
- Align spec-driven and new project-kb starters around AO-KB query recipes and phase-oriented skills.
- Document rationale and subsystem flows for search, ontology, indexing, and agent behavior.
- Upgrade ontology read/query runtime for more robust graph reads, answer cards, and runtime queries.
- Tighten validation and lifecycle rules (including frozen-scope drift detection) to surface vault/ontology issues earlier.
- Refine LLM provider handling and model metadata; model selection behavior may differ when defaults are used.

## [v0.39.0] - 2026-04-28

- Introduced NodeRead-based ontology graph reads and embedded node IDs, improving graph navigation, local graphs, and type detail UX.
- Enhanced search and answer engine with ontology-aware answer packets, a stronger card index, and refined semantic ranking.
- Improved unified indexing for ontology, notes, and code, fixing ontology-aware note chunking/indexing and reducing regressions.
- Added manifest-backed install and `rzm update` flow for safer, version-pinned installation and upgrades.
- Expanded skills and guidance with new refactor-planning and development-loop skills plus richer spec-driven/ontology docs.
- Improved `rzm init` with better credential rejection/skip handling and new environment configuration helpers.
- Breaking: retired reorganize commands in favor of refactor-planning; ontology schema and search behavior are more ontology-driven and may alter existing queries and navigation.

## [v0.38.0] - 2026-04-20

- Revamped ontology UI (type detail, node views, styling) for clearer graph exploration and navigation.
- Introduced ontology type-instance handling to better represent and inspect concrete entities.
- Improved search locality and repair logic to return more relevant ontology results and recover from partial data.
- Refined Notes and Explorer Workspace components and added tests to increase stability of the web experience.
- Standardized userstory endpoint on canonical browser identity for more predictable cross-session behavior.
- Renamed installer entrypoint to `install-rzm.sh`; update scripts or docs that referenced the previous name.

## [v0.37.0] - 2026-04-18

- Add Ontology Atlas workspace for interactive exploration of note types and their relationships
- Improve ontology graph layout and edge routing for clearer, more readable diagrams
- Introduce ontology note and type detail panes for in-place inspection of schema and related notes
- Enhance Notes workspace with improved type-aware navigation and hierarchical note-type rail
- Unify graph styling and type accent colors across ontology and notes views
- Increase test coverage for ontology and notes UI components to improve stability and confidence

## Unreleased

- Register the macOS user install path in `/etc/paths.d/rhizome` so desktop apps can discover `rzm`.
- Add manifest-backed `rzm update` with repo version pinning via `.rhizome/config.yml`.
- Replace the installer zip flow with a hosted `install-rzm.sh` bootstrap that supports user installs and repo launchers at caller-chosen paths.
- Delegate normal global `rzm` commands to the repo-pinned platform binary when `rhizome.version` is configured.
- Prefer `make build` output at `bin/<goos>/rzm` when running inside the Rhizome source checkout.
- Refresh managed repo launchers during `update --pinned` and guard against overwriting unmanaged launcher files.
- Publish latest and versioned S3 artifacts with SHA256 checksums while keeping legacy secret-suffixed archive aliases.
- Add Ontology Atlas page (`/ontology`) to the web app: hub view with stats, schema ER diagram (Mermaid), and per-type detail pages showing matchers, fields, typed relations, implemented interfaces, companion docs, and example notes.
- New `GET /api/ontology/atlas` endpoint and path-style `GET /api/ontology/types/{name}` (replaces the older `/api/ontology/type?name=` query form). Extended `TypeDoc` JSON with `locator`, `propertyCase`, `semantics`, `implements`, and `companionDocs`.

## [v0.36.1] - 2026-04-17

- Add macOS installer zip (`install-rzm.command` + `rzm` launcher) for simplified one-time installation.
- Publish the macOS installer zip as an extra file in GitHub Releases.
- Document a stable macOS installer S3 URL in the README alongside existing platform binaries.
- Update `make cut-release` flow to publish GitHub artifacts and stable S3 downloads, including the installer, in one step.

## [v0.36.0] - 2026-04-17

- Introduced ontology-driven Notes workspace with dedicated home views (all, modified, issues, by type).
- Enhanced ontology editing UI with inline body/narrative editing, property panels, and improved identity display.
- Expanded validation system with cached results, richer checks, and staged autofix workflows.
- Added UI issue widgets for broken links, duplicate preferred identifiers, and generic validation issues.
- Documented and scaffolded ontology-driven transcript ingestion, including starter templates.
- Improved semantic index migration and embed owner handling for more robust indexing and search.
- Updated OpenAPI schema and generated web client to cover new notes, ontology, and validation endpoints.

## [v0.35.0] - 2026-04-15

- Expand `rzm agent surface`:
  - Return a curated, ordered command list with `source` and `category` metadata.
  - Advertise discovery tools (`semantic-query`, `files`, `file-context`, `report`, ontology commands) and note-safe operations like `note-move`.
  - Document a canonical agent retrieval loop and session usage in the surface notes.
- Harden indexing:
  - Add automatic detection of corrupt or schema-incompatible SQLite indexes and trigger a full rebuild when needed.
  - Factor index rebuild into a reusable helper and improve error messages instead of failing with opaque SQL errors.
- Improve note search and `list` performance:
  - Index normalized note search terms (path, title, content segments) in a new `note_search_terms` table.
  - Use the metadata store to pre-filter candidates for `find:` and boolean expressions, falling back to fuzzy matching only when necessary.
- Make `rzm serve` more robust:
  - Defer runtime shutdown until after the HTTP server stops and background goroutines complete.
  - Attach the watcher hub to the web runtime so browser surfaces can observe live note changes.
- Evolve the ontology browser workspace:
  - Introduce a canonical node workspace graph (heterogeneous nodes, typed edges, derived views) as the primary browser contract.
  - Add SSE-first node event plumbing (`/api/ontology/events`) and node-scoped workspace APIs (`/api/ontology/node-workspace`, `/api/ontology/nodes/resolve`) behind the UI.
  - Update the web app to consume node-centric workspaces, unify structural/section handling, and avoid duplicate section rendering.
- Clarify inline property and schema authoring:
  - Update authoring guides to prefer one `key:: value` per line for inline properties and avoid list bullets for metadata.
  - Align embedded/section schema guidance and examples with the new inline property stance.
- Add `foundation-review` agent skill:
  - Ship a new skill for reviewing foundational architecture phases, wired into `.agents`, `.claude`, and starter templates.
  - Document it in the agent workflow as the canonical “Review” phase between implement and audit.

## [v0.34.0] - 2026-04-13

- Add ontology workspace to the web app with URL-based ontology note navigation and improved graph views.
- Default typed notes to a structural view for clearer, spec-driven editing.
- Introduce validation CLI with deterministic fixes and fix-plan reporting, plus a rationale CLI with confidence-aware graph analysis.
- Consolidate Rhizome docs into a spec-driven knowledge base, including ontology section types, starter patterns, and requirements-only specs.
- Implement ontology edit session read/write APIs and web UI support for interactive ontology editing.
- Enhance Obsidian alias handling and coderef parsing (including HTML/CSS comment families) for more reliable anchors and references.
- Optimize indexing and semantic sync (call-edge streaming, writeback batching, web graph caching) for better performance on large repositories.

## [v0.33.1] - 2026-04-07

- Streamlined `serve` stderr output in non-debug mode, reducing background indexer and watcher noise.
- Added compact, color-aware C/M/D indicators for watcher file activity on long-running sessions.
- Improved progress bar log filtering so important lines are kept even when prefixed with timestamps.
- Fixed background indexing cleanup to restore logging correctly and emit a clear “Watching file system…” ready message.
- Refined semantic indexing progress reporting, including better unchanged/skip messages and a final “Index complete” for notes-only runs.
- Updated internal gitignore to exclude SQLite database artifacts from Rhizome’s internal repository.

## [v0.33.0] - 2026-04-07

- Added ontology support and documentation for GraphQL interfaces and section contracts, including section-local neighbor traversal.
- Expanded ontology reference docs with guidance on property naming, interfaces, sections, query usage, and revision workflows.
- Updated bundled example ontologies (project KB, codebase docs) to showcase typed sections and interface-backed contracts.
- Introduced `rhizome-skill-creator` skill plus reference notes for building Rhizome-aware Agent Skills from live repo surfaces.
- Renamed skills to clarify scope: `rhizome-docs` → `rhizome-code-docs`, `rhizome-markdown-authoring` → `rhizome-note-authoring`, and updated all callers.
- Corrected ontology compiler check so `@section` fields must target `Section` or a concrete type that implements `Section`.
- Removed deprecated `.mcp.json` configuration and adjusted `.rhizome/.gitignore` to reflect current Rhizome database handling.

## [v0.32.0] - 2026-04-06

- Strengthen ontology validation: `rzm ontology validate` now enforces schema/typed-note contracts, flags unresolved internal note links with line/link details, and prints per-type note inventories.
- Add ontology sections: introduce `@section(level:, heading:, required:)`, `SectionLevel`, and the `Section` interface for heading-derived body structure with subtree-scoped `@neighbors`.
- Add ontology interfaces: support GraphQL `interface` / `implements` for shared contracts, including interface-aware validation, reference docs, authoring guides, and query fragments.
- Enhance ontology tooling surfaces: reference/authoring docs and schema views now show type roles (note/section/interface), implemented interfaces, section bindings, and neighbor scopes.
- Extend ontology query engine: allow querying sections (built-in `Section` fields, nested section types, subtree neighbors) and using interfaces in roots, fragments, and ambient relations.
- Simplify agent integration: consolidate MCP tooling behind `agentapi`, removing legacy MCP server/adapters while keeping startup responsive via the existing async runtime.

## [v0.31.0] - 2026-04-06

- Introduced a full ontology subsystem for notes and skills (GraphQL SDL schemas, CLI commands, and guides) to support ontology-driven authoring and querying.
- Switched from MCP to an agent CLI + serve runtime as the default integration surface, with `.agents` skills and refreshed Rhizome skill templates.
- Enhanced `rzm init` to manage `.rhizome` (including gitignore), add a Rhizome skill-creator template, and streamline onboarding docs and workflows.
- Changed default note property naming to kebab-case and added `rzm list` / `rzm properties` tooling for inspecting and working with note metadata. **Breaking:** update any configs or queries that relied on old property names.
- Re-architected the indexing and embeddings pipeline (single-writer, batched writes, sqlite-vec) for more reliable runs, fewer lock issues, and clearer unified index progress.
- Improved semantic and ontology-aware search with locality-aware retrieval, better seed-local behavior, and correct enforcement of traversal `maxDepth`.
- Added `rzm serve` and related agent/ontology/query commands, plus web runtime updates, to provide more stable, ontology-backed agent sessions.

## [v0.30.0] - 2026-02-09

- Speed up indexing with parallel note/code ingest, content-hash + mtime guards, and adaptive batched writers; unchanged files are skipped more reliably.
- Move unified search and code-intel/indexing orchestration into shared app packages, simplifying `rzm code search` and `rzm index` behavior and making future tuning safer.
- Tighten SQLite usage for unified indexes: per-open tx-lock options, 1 GiB mmap default, and explicit WAL checkpoints after batch index / graph maintenance.
- Make MCP watching more resilient: better handling of Windows directory deletes, suppression of noisy cache paths, and coalesced stale/resync events to avoid hot loops.
- Extend MCP + agent integration: Cursor skills are now generated by `rzm init`, MCP tool list is complete, and skill docs include Cursor tool-name conventions.
- Clarify docs around unified code index design, concurrency, and rebuild behavior, including where to look for reverse-index and checkpoint logic.
- Behavior change: tx-lock is no longer controlled by mutating `RHIZOME_SQLITE_TXLOCK` at runtime; callers should rely on DSN/open options instead.

## [v0.29.0] - 2026-02-04

- Optimize call-edge indexing with a reverse index and def-delta based rebuilds, so only changed and impacted files have call edges recomputed (with deterministic logging and safe fallbacks).
- Extend `semantic_query` with explicit `mode` support (including per-query modes), richer intent catalog (`search`, `docs_for_code`, `related_to_seed`, `overview`, `code_for_docs`, `find_usages`, `go_to_def`, `explain_symbol`, `tests_for_code`, etc.), and response metadata (`modeApplied`, `modeDetected`, `modeScore`, `warnings`).
- Add dedicated retrievers for definitions, call edges, and tests-for-code, improving IDE-style intents (go-to-definition, find usages/callers/callees, tests for a file) while avoiding vector noise.
- Harden code indexing: introduce tree-sitter parse timeouts and limits, preserve previous intel on parse failures/timeouts, and improve Python/TS/C# call/type-ref extraction and logging.
- Switch ignore handling to go-git’s gitignore implementation with root + nested `.gitignore` support and clearer directory negation semantics, aligning indexing/search with Git behavior.
- Enhance compression and embeddings: add token-aware chunked compression with parallelism and Cerebras-tuned defaults; integrate Voyage AI as an embeddings provider with token-aware batching and team API key support.
- Improve robustness of env and MCP tooling by tolerating invalid `.env` keys and malformed YAML frontmatter, and by enforcing `mode` (not `intent`) for MCP `semantic_query` calls.

## [v0.28.0] - 2026-01-16

- Add intent-driven LLM compression for tools and contextpacks, with new principle-based prompts and a compression cache (now the default; can be tuned/disabled in config).
- Introduce Contextpack Hub and expanded documentation for defining, sharing, and reusing contextpacks.
- Harden vault watcher behavior with retries and fsnotify-based fallbacks to reduce missed updates and improve reliability across platforms.
- Improve `init` command idempotency, diff UX, and add `--reject-all` to quickly decline all proposed template/config updates.
- Enhance code anchors with suffix matching and a `code anchors validate` command for safer, more flexible anchor references.
- Migrate MCP server to the new async LiveRuntime for more robust long-running sessions and capability-based tools.
- Add Cerebras LLM provider and refine embeddings configuration (preserve explicit overrides; remove redundant fields).

## [v0.27.11] - 2026-01-14

- Improve SQLite integrity checks with retries and a longer timeout to better handle slow or locked databases.
- Avoid retrying when actual corruption is detected, keeping corruption reporting accurate.
- Extend integrity-check timeout to 15s to better support slow filesystems (e.g., remote dev environments).
- Add detailed diagnostic logging to the filesystem watch hub for incoming events and watcher lifecycle.
- Log when internal, ignored, or out-of-vault paths are dropped to make watch behavior easier to understand.
- Add logging for recursive directory walking and watch additions to assist in debugging missing file events.

## [v0.27.10] - 2026-01-14

- Filter out internal `.rhizome/` events at the watcher backend level to avoid `db.sqlite-wal` flooding the event buffer
- Improve watcher stability and responsiveness on large or busy vaults by reducing noise from internal database writes
- Prevent unnecessary rescans and downstream reactions to changes in `.rhizome/` internals (no breaking changes)

## [v0.27.9] - 2026-01-14

- Avoid unnecessary `RebuildAllCallEdges` on boot when call edges are already present, improving startup performance
- Add `HasAnyCallEdges` check to the SQLite store to detect existing call edges efficiently
- Stop attaching file-system watches to the `.rhizome/` directory to prevent event buffer flooding from database files
- Continue tracking `.rhizome/ignore` changes while filtering other `.rhizome/` files from watcher events
- Improve overall watcher robustness and reduce risk of buffer overflows in large or busy vaults

## [v0.27.8] - 2026-01-14

- Reduce verbosity of filesystem watcher (fsnotify/FSEvents) logging
- Continue logging user-relevant file events for visibility into changes
- Simplify internal watcher logging while preserving error diagnostics
- Keep watcher behavior and defaults unchanged (no breaking changes)

## [v0.27.7] - 2026-01-14

- Improve watchhub diagnostic logging for filesystem watcher behavior
- Log backend creation and FS notification enablement to aid troubleshooting
- Add detailed logs for root additions, recursive walks, and already-walked paths
- Log handling of internal ignore roots and final watcher setup state (including watch counts)

## [v0.27.6] - 2026-01-14

- Fix fsnotify watcher not receiving events in leader/follower mode when the backend is enabled after startup  
- Ensure watch roots are only marked as walked when a backend is present, allowing proper re-walk on later fsnotify enablement  
- Improve reliability of file/secret change detection in clustered and delayed-initialization setups

## [v0.27.5] - 2026-01-14

- Toned down repeated debug logging during graph score lock contention for clearer diagnostics
- Improved lock contention message to explicitly note when another process is indexing
- Added a summary log of total watched paths after watcher setup to assist troubleshooting
- Preserved existing graph scoring and watcher behavior; no breaking changes or default flips

## [v0.27.4] - 2026-01-14
- Treat MCP server `context.Canceled` as a normal shutdown instead of a fatal error
- Add debug logs for successful and failed code root registration in MCP/codeanchor
- Log when the code watcher is ready, including counts of note and code roots
- Improve embedding watcher logs with duration and error counts for note embedding runs
- Increase debug output to make MCP and embedding behavior easier to diagnose without altering user-facing defaults

## [v0.27.3] - 2026-01-14

- Improve MCP startup time by opening the session store asynchronously so the initial handshake is not blocked
- Make MCP connections more responsive for clients while background session initialization completes
- Ensure session cleanup is started only after the session store is successfully opened
- Update MCP documentation to explicitly require async handling for session store opening alongside other slow operations

## [v0.27.2] - 2026-01-14

- Fix MCP handshake timeouts by ensuring initialization is async and non-blocking
- Run directory watch root enumeration in a background goroutine to avoid blocking MCP startup
- Move session cleanup to an asynchronous task so clients connect reliably even on large stores
- Document MCP startup responsiveness requirements in AGENTS and indexing pipeline reference

## [v0.27.1] - 2026-01-14

- Fix edge re-indexing so existing code edges are correctly refreshed, improving code graph accuracy.
- Treat unchanged-but-touched files as “touched” in the index and refresh their mtimes, reducing stale code intel.
- Clarify incremental indexing stats by accurately counting `Indexed` vs `Unchanged` files.
- Add store support for bulk mtime refresh of code paths to make incremental indexing more reliable.
- Correct `init` config saving so updated embedding provider settings are written to the proper project config path.
- Refine Rhizome docs/onboarding guidance for better agent use of CONTEXT and note tools (docs-only).

## [v0.27.0] - 2026-01-14

- Optimize incremental indexing: rebuild call edges and wikilink graph only for changed files/notes to speed up `rzm index` on small edits.
- Harden SQLite usage: distinguish real corruption from transient busy/locked errors and gate destructive recovery on index-lock ownership.
- Improve CLI/MCP coordination: background semantic embedding and graph scoring defer when CLI requests priority via the shared index lock.
- Require explicit embeddings provider configuration (breaking): semantic search and embeddings now fail fast unless a provider is set; `rzm init` upgrades old configs by prompting for a provider or disabling embeddings.
- Preserve embeddings `provider` in config to avoid silent default changes between releases.
- Track anchor changes more precisely so scope recomputation and graph scoring only run when anchors or notes actually change.

## [v0.26.0] - 2026-01-13

- Change default embeddings provider to Ollama, add provider-choice prompts in `rzm init`, and wire Ollama endpoints into `.rhizome/config.yml`.
- Optimize MCP context tools: reuse `sessionId`, prewarm graph analysis on server boot, and refine `vault_context`/`file_context` guidance for once-per-session usage.
- Extend `file_context` with `submoduleDepth` to include submodule `CONTEXT.md` docs, and tighten vault_context output to focus on high-signal module docs.
- Add interactive, diff-based template updates to `rzm init` with per-hunk approval, persistent rejection tracking, and new `--skip-rejected` / `--clear-rejections` flags.
- Harden SQLite index lifecycle: safe WAL sidecar cleanup, embeddings-domain repair, and more conservative purge behavior to avoid corruption under concurrent MCP/index use.
- Enable FTS5 in CI/tests and builds, add a vendor patch for go-sqlite3 FTS5, and switch to native per-OS builds instead of cross-compiling.
- Reduce default context packing budget from 150k to 50k chars and update Rhizome docs/skills (specify/plan/implement, hub-notes, docs) to favor token-dense, two-tier note models.

## [v0.25.1] - 2026-01-10

- Enabled SQLite FTS5 in all release binaries for improved full-text search
- Updated `go install` instructions to include `-tags fts5` for consistent behavior with releases
- Clarified that local builds without `-tags fts5` will not include the enhanced search capabilities

## [v0.25.0] - 2026-01-10

- Optimize unified indexing to rebuild call edges only for files that actually changed, speeding up incremental runs.
- Auto-detect scope config changes (vault includes/excludes, code roots) and invalidate mtime caches so new in-scope files are picked up without `--rebuild`.
- Switch all SQLite usage to `mattn/go-sqlite3` with FTS5 enabled by default for intel and embeddings stores.
- Introduce explicit write-access mode for anchor services, making read-only commands (e.g., `rzm code explain`) avoid index writes.
- Add scope-config hash metadata and mtime-cache invalidation APIs to the intel store for more predictable reindex behavior.
- Breaking: remove Windows arm64 release target and associated documentation; Windows support is now amd64-only.

## [v0.24.0] - 2026-01-10

- Add `rzm changelog` command backed by an embedded `CHANGELOG.md` for easy release history inspection
- Recompute code anchor scopes when notes defining `code-anchors` are ingested, even if no code files changed
- Improve codeanchor watcher with periodic deletion reconciliation on Linux/Windows to clean up stale indexed paths
- Tighten RHIZOME.md and MCP usage templates with explicit `vault_context` session rules and code-anchor authoring guidance
- Refine ignore system documentation, splitting behavior vs. implementation notes to make configuration and debugging clearer
- Enhance release script to manage version files, embedded changelog, tags, and cleanup automatically during `cut_release` runs

## [v0.23.0] - 2026-01-10

- Switched macOS vault watching to an FSEvents backend to avoid file descriptor exhaustion on large trees
- Added shared write mutexes and more aggressive WAL checkpointing across MCP stores to improve SQLite index stability
- Fixed semantic chunk hash computation and unified hashing between note and code indexers for correct embedding cache reuse
- Clarified and implemented indexing pipeline concurrency + batching requirements (per-path transactions, parallel-parse/serial-write, provider batching)
- Updated `rzm init` to remove stale rhizome.* prompts/skills, standardize on `rhizome-` names, and keep agent artifacts in sync
- Expanded RHIZOME.md and related docs with a unified documentation philosophy (agents as primary audience, tests as executable docs)

## [v0.22.5] - 2026-01-10

- No user-facing changes; behavior matches v0.22.4.
- Internal maintenance only; no new features or bug fixes.
- No breaking changes or default flips in this release.
- No configuration changes or migrations required.

## [v0.22.4] - 2026-01-10
- Avoid duplicate `RebuildAllCallEdges` execution to reduce redundant analysis work
- Improve performance and stability for workflows that trigger repeated call-graph rebuilds
- Ensure `RecomputeAnchorScopes` respects completed call-edge rebuilds via internal state tracking

## [v0.22.3] - 2026-01-10

- Parallelized `RebuildAllCallEdges` to speed up code-intel indexing on larger projects.
- Added periodic progress logging during call-edge rebuilds for better visibility into long runs.
- Improved robustness of call-edge rebuilding by continuing past individual file errors.
- Updated `cut_release.sh` to present explicit major/minor/patch version choices with a suggested option.
- Changed release script fallback to default to a minor version bump when LLM suggestions are unavailable and surfaced LLM reasoning text.

## [0.22.2] - 2026-01-10

- Speed up `RebuildAllCallEdges` by batching call/import/type-ref edge writes to the intel store
- Add `UpsertIntelCallEdgesForPathsBatch` to SQLite store for transactional, batched edge upserts
- Preserve compatibility for non-batch intel stores by falling back to per-path edge updates

## [v0.22.1] - 2026-01-10

- Fix duplicate `doc_links` constraint violations in SQLite-backed stores by using `INSERT OR REPLACE`
- Improve reliability of `ReplaceDocLinksForPath` and batch variants when re-indexing documentation links
- Ensure doc link updates consistently overwrite existing records instead of failing on duplicates

## [v0.22.0] - 2026-01-10

- Speed up note and code indexing by increasing batch sizes to 500 items per write lane.
- Batch note upserts into single transactions to reduce database round-trips during indexing.
- Batch doc link writes for both notes and code, improving performance on large projects.
- Clarify that multi-process leader/follower watcher mode is enabled by default, with optional config/CLI overrides.
- Improve robustness and throughput for large vaults by reducing per-file transaction overhead.

## [v0.21.0] - 2026-01-10

- Speed up code indexing by batching file summary updates into a single transaction
- Improve consistency of symbol, inheritance, and annotation data when files are reindexed
- Reduce chances of partial index updates on failures through atomic batch operations
- Optimize SQLite index writes by reusing prepared statements for batched summaries
- Fix S3 release publishing script to correctly match goreleaser output directories across OS/arch targets

## [v0.20.0] - 2026-01-10

- Enable CGO in release builds and add cross-compilation toolchains so tree-sitter indexing works on macOS, Linux, and Windows amd64.
- Add index lock priority mechanism so CLI index commands can preempt background MCP indexing instead of waiting indefinitely.
- Update MCP background index, semantic embed scheduler, and graph score scheduler to use yielding heartbeats that respect CLI priority and re-queue remaining work.
- Refine graph edge weights and doc scoring so documentation links drive importance, with tuned weights for calls, type refs, imports, and tests.
- Remove redundant embedding cache writes during semantic sync to reduce indexing overhead on large repositories.
- Refresh banner copy and expand documentation on Rhizome’s purpose, release build requirements, and recommended documentation structure (CONTEXT.md, in-code docs).

## [v0.18.0] - 2026-01-09

- Add `rzm graph web` command to mirror web UI graph endpoints from the CLI, with JSON and timing output.
- Extend Python and TypeScript indexers to emit import and type-reference edges, enriching the code graph used by search and navigation.
- Introduce a centralized edge-kind registry to control weights and priorities for calls/imports/tests/type refs, fixing under-weighted import/test edges in graph-based ranking.
- Optimize graph construction (global, local, and module views) with edge aggregation, module collapsing, and cached ignore checks for better performance on large vaults.
- Normalize embedding storage so the cache is the primary source of truth, add pruning of stale cache entries, and route chunk/item queries through the cache.
- Improve embedding sync robustness with expanded OpenAI/network retry logic and progress that separates cache reuse from new embedding work.
- Ensure release tooling updates the in-repo version so `rzm --version` correctly reports v0.18.0.

## [0.17.0] - 2026-01-08
- Speed up unified indexing with better concurrency, planning, and mtime-based short-circuiting
- Add automatic recovery for corrupted SQLite index databases to reduce rebuilds and failures
- Improve Go and Python code indexing and fix missing web graph edges for test files
- Add markdown link rewriting on note rename/move and prefer markdown links for coderefs by default
- Switch search embeddings to oai provider and refresh skills/onboarding documentation
- Refine web UI graph styling, colors, and file tree for clearer navigation
- Expand and reorganize MCP tools for semantic queries, files/context, graph, and health operations

## [v0.16.0] - 2026-01-07

- Speed up incremental code and note indexing with mtime-based skips, batched SQLite writes, and conditional call-edge/scope recomputation.
- Improve unified index and semantic embedding concurrency with shared write locks, coordinated provider concurrency, and parallel planning/embedding.
- Make filesystem watching case-insensitive on Windows/macOS for watcher paths, watch roots, and pending events to reduce missed/duplicate updates.
- Harden MCP leader transitions by enabling FS notifications only after subscribers are registered, and add exponential backoff/retry for semantic embeds and graph score rebuilds.
- Add S3 release utilities: `make release-s3-check` for AWS preflight validation and `make release-s3-dry` for dry-run uploads; integrate preflight into `cut_release.sh`.
- Enhance SQLite-based intel and embedding stores with centralized write helpers, schema-rebuild safety, and periodic logging of write wait/hold times.

## [v0.15.0] - 2026-01-06

- Add bundled web UI (`rzm web`) with graph and search views for interactive vault exploration.
- Enhance semantic search: type filters, query normalization, better packing, and fixes for non-code vaults.
- Speed up and harden indexing with background indexing, unified WatchHub, index locks, and fewer unnecessary resyncs.
- Improve onboarding and init flows with updated MCP-aware templates, agent skills, and RHIZOME.md/CONTEXT.md docs.
- Add Ollama-first embeddings defaults, batching, and safer rebuild behavior; refine OpenAI retries and embedding reuse.
- Upgrade code intelligence with better language indexers, unified note/code embeddings, and tuned graph/community scoring.
- Centralize path handling and ignore rules for more reliable behavior across platforms, especially Windows.

## [v0.14.0] - 2025-12-17

- Add `--use-fts-body` option to unified search to render code snippets from the FTS code index (faster, includes doc comments; falls back to file reads when unavailable).
- Enhance `rzm code overview` with directory-level aggregation, graph-based ranking, CONTEXT.md summaries, submodule listings, better test detection, and a new `--depth` flag.
- Improve embeddings performance and resilience: raise OpenAI concurrency caps and introduce generation-based lazy pruning for note and code indexes to preserve useful items across branch switches.
- Index Go package-level doc comments into anchors and semantic chunks so package docs appear in search and context.
- Add `--no-config` flag to `rzm init` to update project templates without modifying existing config.
- Tidy indexing UX and config writes: drop verbose per-item embedding logs from `rzm index` and limit `SaveCodeConfig` to minimal local code settings.
- Add and wire new documentation hubs and reference notes for search, retrieval, relevance, graph algorithms, vault config/ignore, and knowledge handles.

## [v0.13.0] - 2025-12-17

- Generate a standalone `RHIZOME.md` during `rzm init` and switch AGENTS/CLAUDE/Cursor/Codex harnesses to reference it instead of embedding Rhizome guidance.
- Restructure `.rhizome/config.yml` to use top-level `noteEmbeddings`, `codeEmbeddings`, `graph`, and `agents` keys; `rzm init` migrates existing `agent:` configs in-place.
- Change default code query embeddings to `text-embedding-3-small`, inherit provider/endpoint from note embeddings, and auto-reset code indexes when metadata (model/provider/dimensions) changes.
- Move logs to `.rhizome/logs.d/` with a `.rhizome/logs` symlink to today’s file, and always print logging setup warnings even without `--debug`.
- Improve `rzm init` robustness: handle `.claude` as a file, tolerate problematic symlinked directories, add `--agentsmd` to disable `AGENTS.md` updates, and treat some path issues as non-fatal warnings.
- Drop legacy `appliesToAnchors` frontmatter and CLI output; code anchors now rely solely on `code-anchors` definitions.

## [v0.12.0] - 2025-12-17

- Unify note + code embeddings and intel graph into a single index, improving semantic linking between notes and code.
- Introduce unified semantic + code search and code-intel graph (anchors/edges), with new CLI commands for code search, overview, analytics, and relatedness (including C# and multi-language support).
- Optimize search and indexing performance via SQL-filtered vector search, batch indexing, adaptive retriever concurrency, and an embedding reuse cache; preserve embeddings across schema rebuilds.
- Make MCP startup asynchronous with readiness-gated tools, fixing init blocking and improving reliability of semantic tools.
- Expand and refine AGENTS/init pipeline: agent-agnostic harness templates, richer code-intel guidance, and persisted agent mode overrides.
- Add lexical prefiltering for notes and adaptive retriever timing to improve result relevance and reduce unnecessary work.
- Breaking: migrate to a unified SQLite schema and reorganized packages (`app`, `vault`, `anchors`, `search`); code-intel and embedding data will be migrated and some CLI code commands have changed.

## [v0.11.0] - 2025-12-12

- BREAKING: `graph file-context`, `graph vault-context`, and MCP `file_context`/`vault_context` now return budgeted, LLM-optimized text output by default (instead of JSON).
- Add code anchor indexing for Go and TypeScript/JavaScript (Tree-sitter), including improved matching (glob/dir anchors, TS import path-tail fallback) and updated CLI/MCP wiring.
- Add `index` command to manage semantic + code indexes together (`--status`, `--rebuild`, `--semantic`, `--code`).
- Improve `file_context` output: directory-aware context, explicit output budgeting (`budgetChars`), frontmatter stubs/blessed-frontmatter surfacing when content is omitted, and expanded linked notes for hubs/MOCs.
- Add optional inclusion of context docs in graph analysis (with cache key support and tests).
- Refine `vault_context` defaults/payload shape (trim components/topAuthority payload, hide components by default).
- Streamline `init` (AGENTS.md Rhizome section generator + templates) and add CLI banner/logo assets.
- Add comprehensive docs covering graph analysis, list/prompt DSL, coderefs, code anchors, and embeddings; add a polyglot integration fixture + integration tests.

## [v0.10.0] - 2025-12-09

- Add `graph file-context` CLI command and MCP `file_context` tool (alias: `note_context`) to return graph + community context for notes and documentation context for code files (linked notes, ancestor `CONTEXT.md`, inferred communities).
- Add optional code reference scanning (configured via `.rhizome.yaml`) to index `[[wikilinks]]` and `@NotePath` mentions in source code; expose `codeRefs` in `files`/`file_context` responses and apply a soft authority boost to referenced notes.
- Support collection-style vaults with `vault add` options (`--root`, `--includes`, `--excludes`, `--links`) and enhanced `vault list`; semantic indexing and MCP caching now respect glob-based includes/excludes.
- Add `graph broken-links`, `graph dead-ends`, and `graph stale` commands to surface missing wikilinks, inbound-only notes, and notes stale by modification time.
- Add `vault_health` MCP tool to return a JSON health report (brokenLinks, staleNotes, deadEnds, suggestedMerges) with configurable thresholds and filters.
- Update `rename` and `move` commands to optionally rewrite code references when code ref scanning is enabled, reporting code ref update counts.
- Wire CLI version to `pkg/version.Version` and set it via GoReleaser ldflags for accurate release reporting.

## [v0.9.0] - 2025-12-08
- Improve `semantic search` to interleave chunk matches across notes and rank notes by average chunk relevance; `--limit` now controls total chunks (default 25).
- Add `semantic find-connections` (alias: `similar`) to discover notes connected to a given note via stored chunk embeddings, with chunk-level context.
- Change MCP `semantic_query` to return per-note groups with aggregate scores and chunk arrays, matching the CLI’s chunk-based search model.
- Add MCP `find_connections` tool to expose embedding-based note connections (with query/match metadata and reasons) to agents.
- Simplify `graph note-context` output by replacing the `related` notes list with a single embedding-based `similarity` score to top community notes.
- BREAKING: Remove `semantic search --chunks` flag and change `--limit` semantics; scripts depending on the old note-level search output may need updates.

## [v0.8.0] - 2025-12-07

- Add opt-in semantic search CLI (`rhizome semantic index/search/similar/status/enable/disable/rebuild`) using OpenAI or local Ollama embeddings
- Wire semantic index into MCP: `note_context` now includes `related` semantic notes; new `semantic_query` tool returns chunk-level matches with text
- Simplify MCP surface: remove `community_detail`, drop daily-note tools, and streamline `vault_context` to a compact summary plus optional `note_context` payloads
- Enhance graph commands: `graph note-context` accepts file arguments instead of `--files` and can include semantically similar notes; `graph vault-context` defaults to concise output with `--all` for full detail
- Improve semantic index UX: progress bar during indexing, `semantic status` for provider/index metadata, deterministic test provider, and automatic gitignore entries for the SQLite index
- Strengthen cache and watcher reliability, including `.obsidianignore`-aware resyncs, better dirty-path tracking, and reduced SQLite lock contention during background updates

## [v0.7.0] - 2025-12-07
- Added `graph vault-context` and `graph note-context` commands to emit JSON vault and per-note graph context (communities, hubs/authorities, neighbors, backlinks, recency).
- Replaced PageRank with HITS hub/authority scores across graph stats, communities, and MCP responses, with CLI output updated to show both hub and authority metrics.
- Turned on multi-hop recency cascading by default for graph analysis (CLI and MCP); use `--recency-cascade=false` or `recencyCascade:false` to opt out. **Default behavior change.**
- Enriched MCP graph tools (`community_list`, `community_detail`) and added `note_context` / `vault_context` tools with authority distributions, recency summaries, bridge strength, weak components, and key-note/MOC detection.
- Parallelized graph build and recency computation, reused cached note metadata (including derived content times), and fixed a Windows watcher deadlock for more reliable, faster runs on large vaults.
- Increased default graph listing limits (e.g., `graph --limit` now 100) and added optional timing output for graph commands and MCP to inspect analysis cost.

## [0.6.2] - 2025-12-06
- Refresh analysis cache providers before using cached backlinks/graph data for more accurate results.
- Prevent the files MCP tool from mutating base `SuppressedTags` when per-call overrides are supplied.
- Reload `.obsidianignore` patterns on crawl/resync so ignore changes apply without restarting.
- Clarify daily note MCP tool description (no longer claims to create missing notes).
- Update `move_notes` MCP tool comment to reflect default backlink rewriting behavior.
- Simplify release script to pass the generated release notes file directly to GoReleaser.

## [0.6.1] - 2025-12-06

- Fix Windows-specific issues (path separators, JSON escaping, permission/error handling) for more reliable behavior on Windows
- Harden file-watcher and cache behavior, including race-condition fixes and real fsnotify-based integration tests
- Improve analysis cache correctness by avoiding caching results when the vault version changes during computation
- Add GitHub Actions CI workflow for linting, unit/integration tests across Linux/macOS/Windows, and multi-OS builds
- Enhance developer tooling with new Makefile targets (`lint`, `integration`, `test_all`) and a more capable release helper script
- Rewrite README into a clearer project landing page with badges, feature overview, command reference, and MCP usage docs

## [v0.6.0] - 2025-12-06

- Add `file` command group so move/rename operations work for both notes and attachments, updating backlinks/embeds by default.
- Improve backlink/graph performance and reliability via analysis memoization and a hardened vault cache with watcher fallback.
- Extend `properties` CLI with `set`, `delete`, and `rename` operations for bulk frontmatter editing (YAML-aware, with dry-run and worker controls).
- Replace MCP tag/property tools with unified `mutate_tags` and `mutate_properties` operations, supporting scoped inputs and dry-run summaries.
- Enhance `prompt` command to optionally emit absolute paths in `<file path="...">` blocks for better downstream tooling.
- Breaking: flip default for `note move --update-backlinks` to `true`; pass `--update-backlinks=false` to skip backlink/embedding rewrites.
