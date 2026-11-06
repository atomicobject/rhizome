---
summary: "Delivery, candidate dispositions, regression evidence and verification for the approved whole-codebase untangle pass."
reference-kind: analysis
---

# Untangle delivery evidence

All 46 approved candidate groups are implemented, with both selected comment and test cleanup passes. The work also fixes the fallback heading identity mismatch found by the required packaged-app check. Final pre-commit verification and the carrying commit are recorded below and in [[2026-11-05-18-48-untangle-codebase|the owning effort]].

The result keeps the existing subsystem architecture. Shared rules now have one owner; obsolete APIs and state are removed; cache, bootstrap and configured-view code are split by responsibility. Intentional differences in graph output, missing preview values, Markdown syntax, ranking, scheduling and storage transactions remain explicit.

## Candidate dispositions

| ID | Disposition | Implemented result |
| --- | --- | --- |
| C1 | Implemented | Cache alias lookup uses `obsidian.AliasListFromFrontmatter`; removed its narrower duplicate. Timestamp/Stringer aliases, ignored values and collisions have public cache coverage. |
| C2 | Implemented | Deleted four unused Store APIs and inlined the sole ambient-target SQL path; retained transactional invalidation, batching, ordering and final limits. |
| C3 | Implemented | `InstallDefaultIgnore` calls `ignore.DefaultIgnoreFile` directly; deleted obsolete Obsidian forwarders. |
| C4 | Implemented | One ordered-word matcher serves both nonwildcard paths; distinct wildcard policies remain. |
| C5 | Implemented | Split cache methods into entries, refresh, crawl and code-reference files, retaining one Service and its existing locks. Removed narration and updated the package map. |
| C6 | Implemented | Cache refresh test performs one public dirty/refresh cycle; removed private-state preconditions, retry loop and unnecessary mtime sleeps. Asynchronous watcher coverage remains. |
| C7 | Implemented | Corruption classification retains nil/lowercase handling and the broad `emb_` check; removed subsumed table-name terms. |
| IDX-1 | Implemented | Queue state retains explicit code-kind identity independent of metric spelling and flush-list position; durable flush order is unchanged. |
| IDX-2 | Implemented | Ontology orchestration alone owns slicing/concurrency; its helper performs one provider call with the existing gate, metrics, vector validation and cancellation behavior. |
| IDX-3 | Implemented | Deleted unreachable single-item/queued-intel branches and narrowed `SyncWriteQueue`; retained public writer submission and direct-store batching/draining. |
| Q1 | Implemented | `ontology.MatchSectionNodesForField` serves assessment, projection, query and web. Literal cases cover order, H1 unwrap, levels, nesting, nil and whitespace-only programmatic headings. |
| Q2 | Implemented | Guide rendering uses existing guidance, enum and policy projection helpers; distinct guide/reference products and tests remain. |
| Q3 | Implemented | Removed no-op view pushdown/link adapters, shared one request-scoped `CatalogLinkResolver`, and split source resolution by responsibility. |
| Q4 | Implemented as narrowed | Indexed type execution checks the actual view Store. Constructor signature, narrow read port and optional broader QueryDeps remain; public filtering/sorting covers rows beyond a source cap. |
| Q5 | Implemented | Command/serve composition injects the existing bundled-overlay checker through preparation, indexed, live and fallback validation paths. Missing-checker errors remain explicit. |
| Q6 | Implemented with distinct policies | Ontology owns identifier selection and preview parsing. Query erases absent values; web retains their tokens. Raw-section extraction and projection hydration stay separate. |
| Q7 | Implemented | One private value/presence lookup replaces four scans, preserving exact-key precedence, normalized fallback, authored empty/null values and alias order. |
| SC-1 | Implemented | Removed unconsumed runtime source versions/sequence; retained source snapshots, call-file tracking, locks and cache invalidation. |
| SC-2 | Implemented | Anchors owns structural language mapping in `indexer.go`; CLI, codeintel, indexcore, bootstrap and the integration fixture use `LangForPath`. Supported extensions are unchanged. |
| SC-3 | Implemented | Retired legacy embedding Indexer/scanner. Its ignore scenario now traverses live ownership discovery, projection, persisted sections and `NoteSyncer`; current embedding docs and code anchor were updated. |
| SC-4 | Implemented | Removed unused language formatter/root walker and discovery-based DocLinker constructors; live count merge is private. Explicit-candidate tests retain mixed-case/alias and SQLite persistence assertions. |
| SC-5 | Implemented | Removed unused execution/scope progress hooks, unreachable branches and write-only counters; retained call-edge planning observation and real indexing diagnostics. |
| SC-6 | Implemented | One eligibility-tier calculation serves ranking before owner caps and after rollup; scoring, stage-specific tie-breaks and provider rerank order remain. |
| S1 | Implemented with distinct policies | `graphpresentation` owns wire DTOs, endpoint/edge projection and collapse. Direct CLI/web consumers retain their module/language/order differences; web retains readiness, caching, enrichment and notes-only behavior. |
| S2 | Implemented | Generic trust resolution/denial moved to `repositorytrust`; actions calls it directly and tasks passes its resolved checkout. Main/subdirectory/worktree/bare/non-Git/error/cancel behavior is retained. |
| S3 | Implemented | Thirty-seven per-operation factories colocate descriptor, schemas, metadata, access and extra conditions. Removed name-map joins; corrected semantic-query continuation wording while preserving request rules. |
| S4 | Implemented | CLI callers use existing codeintel owners; deleted command-era anchor/root/validation/language forwarders. Operations callers use `CodeModeSuccess` directly; meaningful adapters remain. |
| W1 | Implemented | Edit operation/snapshot transport fields derive from generated OpenAPI types while keeping stronger field-value/expected unions. |
| W2 | Implemented | Removed unused workspace selectors, note-search hook and four client wrappers; retained live transport tests, server routes/OpenAPI and browser-kit exports. |
| W3 | Implemented | Deleted unused runtime embedding mutators and production query wrapper. Graph wrapper moved to `graph_test.go`; existing integration assertions remain. |
| W4 | Implemented | Dependency-free `noteTarget.ts` owns parsing/serialization for routes, links, tabs and panes; query-bearing pane identity and literal URL semantics are retained. |
| W5 | Implemented | Theme names derive from descriptors; shared appearance settings feed Vite's synchronous bootstrap. Desktop precedence and invalid/denied-storage fallback remain. |
| RUNTIME-1 | Implemented | One private Claude session constructor owns initialized state/lifetime/reader startup; status remains noninteractive with its existing handshake. |
| RUNTIME-2 | Implemented | Desktop key mutation derives canonical keys/aliases from `NeedForProvider` while retaining its explicit Voyage/TypeSafe allowlist and runtime precedence. |
| RUNTIME-3 | Implemented | Bootstrap phases, capabilities, leader/session work, embeddings and diagnostics moved to cohesive sibling files; state/construction, function bodies and ordering remain. |
| RUNTIME-4 | Implemented | Replaced private slice membership with `slices.Contains` and inlined the one-use credential lookup wrapper. |
| T1 | Implemented after characterization | `markdownsyntax.FenceState` owns the common template fence transition. Init and both overlay marker passes reuse it while retaining their characterized accepted languages. |
| T2 | Implemented | Canonical compounding entrypoints share `resolveRzm`: explicit RZM, repository launcher, then PATH. Calibration now follows the same rule. |
| T3 | Implemented | Finding loader returns status/action/assignee from one note read; workspace projection no longer rereads/reparses those fields. Full-note display reads remain. |
| T4 | Implemented, review fixes included | Start/rebuild/crash share readiness/origin/Vite reconciliation. Failed background restarts are handled; shutdown checks prevent new backend/Vite starts. Native Node fixtures replace the TS-only adapter. |
| T5 | Implemented | Project setup-go steps read the intended root/service module files; scanner toolchain overrides and local mise pin remain separate. |
| T6 | Implemented | Release plan/dry-run assertions invoke Make with fake executables; retained publication authority, exact arguments and private-infrastructure exclusions. |
| SVC-1 | Implemented | Identity receives the already-open ledger through `UsageReader`; removed delayed interface assignment and traffic forwarding methods. |
| SVC-2 | Implemented | Shared pure receipt replay/settlement decisions live beside ledger types; SQLite/DynamoDB retain transactions, lease checks, error order, retries and counter updates. |
| SVC-3 | Implemented | Pricing bounds use `wire.MaxVoyageInputs`; literal 1000/1001 tests remain independent. |
| SVC-4 | Implemented | Deleted unused admin `negative` and traffic version forwarder; retained auth's distinct eligibility parser and core's sanitized diagnostics. |

## Explicitly retained limits

- **Q4:** no common dependency bundle, stricter constructor requirement or blanket mismatch rejection. Plain type/catalog callers need no GraphQL execution store.
- **S1/Q6:** no universal CLI/web display or missing-preview-value policy. Collapsed graph edges still lose relationship metadata as before; web owns scores/community enrichment and notes-only no-collapse. This effort does not correct that pre-existing aggregation behavior.
- **T1:** no blanket `ProtectedSpans`/`CodeMask` substitution. Unmatched/escaped backticks, multiline inline code, indented markers, false closing fences and CRLF retain the characterized template behavior; no new Markdown acceptance rule is claimed.
- **SVC optional file move:** backend-independent runtime encode/decode/apply functions remain where they were because none needed changing. Moving them would add unrelated churn.
- **SVC optional coalescing test:** retained its existing synchronization because no failure and no equivalent observation proving pending-request sharing were established. Also retained the separate whole-process ontology goroutine-bound test and Ollama concurrency observation; provider concurrency alone would weaken their claims.
- **Supported boundaries:** retained `RankRelated` and the live watcher, vendor-specific driver/version/cache behavior, raw-note fallback, broader query capabilities, meaningful command/runtime adapters, separate agent/operations outcome types, OS-specific checks/skips, and generated-source ownership. No generic registry/transaction framework, new dependency, schema/data migration, provider change, deployment or publication is included.

## Defects and review follow-through

- Alias conversion, indexed-view store gating and explicit writer code-kind scheduling failed focused regressions before their fixes.
- The original immediate-decode shutdown test failed at baseline. Separate regressions reproduced blocked exit logging and unnecessary grace consuming the kill/reap budget. The exit signal now follows process reaping before logging, and grace reserves cleanup time. A timing-sensitive injected-process fixture was replaced by literal budget cases; real child, blocked-write and descendant tests remain. The original failure's historical timing split is unknown; no passing rerun was treated as the fix.
- The final pre-commit pass exposed an existing redaction test that counted host scheduling pauses as redactor work. A controlled process pause reproduced the false wall-clock failure. The guard now measures process CPU against the same two-second budget; production redaction is unchanged.
- Development-server crash recovery now reconciles readiness, backend origin and Vite. Independent review found unhandled replacement failures and a Vite start after shutdown. Both were reproduced and fixed, and all ten supervisor tests pass.
- Packaged verification exposed an existing producer/resolver mismatch for untyped headings. Four root/nested round trips failed before the fix. Fallback construction now owns single-H1 parent normalization, and lookup accepts canonical and previously emitted raw-parent identities only for untyped, unassessed sections. Typed identities and block IDs retain their rules. The final tests cover direct reads, enumeration, nesting, blocks, a literal historical selector and typed controls.
- The core review's missed integration caller now uses `LangForPath`. The surface review's graph wrapper is test-only, and the actions/tasks context names the actual trust owner. The supplemental heading review found no further actionable issue.
- Removed the stale scanner exemption from the note-format architecture test and six obsolete oversized-file allowances. Updated owning notes, code anchors, source maps and the generated Greptile ownership configuration. The approved plan and frozen spec remain unchanged.

Five integrated review passes covered core/search/indexing/service, surfaces/web, runtime/tooling/validation, the final recovery delta, and the supplemental heading delta. Review findings are resolved; these source reviews were separate from the execution evidence below.

## Verification

| Check | Result |
| --- | --- |
| Focused behavioral regressions and characterization | Passed for aliases, views, writer kinds, parser boundaries, raw-note discovery, graph/catalog contracts, validation composition, ranking and recovery. |
| Harness repetition | `go test -race -tags fts5 ./pkg/harness/internal/jsonstdio -count=10` passed, including real process cleanup. |
| Integrated gate after final production changes | `TEAMAPI_DYNAMODB_ENDPOINT=http://127.0.0.1:59695 make verify` passed: build, fast checks, race-enabled unit tests, benchmark contracts, web tests, language/runtime/mixed integration and browser E2E. |
| Web behavior | 1,844 unit tests and 112 browser E2E tests passed; all ten native supervisor tests passed. |
| Billing adapters | Explicit `go test -race -count=1 -v ./internal/ledger` in `services/team-api`, with the disposable DynamoDB endpoint, passed both SQLite and DynamoDB contracts and lease-expiry cases. No DynamoDB skip. |
| Desktop | `make desktop-deps`, `make desktop-check`, and `make desktop-build` passed. Desktop checks included 113 UI tests, 111 Rust tests, formatting, clippy and app-instance lifecycle fixtures. The final isolated bundle was rebuilt after the heading fix with a unique test identifier. |
| Packaged interaction | On macOS arm64, the final app's bundled CLI matched the verified branch binary by SHA-256. A disposable repo loaded through that CLI. Newly emitted heading URLs reloaded Details correctly; an authored saved-selector link also opened and reloaded, while the literal historical selector is covered by the round-trip test. Light/Indigo appearance persisted. The synthetic fixture had unrelated validation findings; repository validation is recorded separately. |
| Generated init output | Disposable initialization followed by `init --check` exited 0. Root init preview identified exactly nine generated compounding files; init refreshed them, and the subsequent root check exited 0. Generated copies were not edited by hand. |
| Documentation | `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` passed with zero issues; final reconciliation is checked again at closure. |
| Public operation catalog | Actual before/after CLI catalogs retained all 37 operation shapes and ordering. The only contract-text change says continuation requests repeat their original queries. |
| Pre-commit | Final `TEAMAPI_DYNAMODB_ENDPOINT=http://127.0.0.1:59695 make check` passed after the redaction-test correction. |

The first broad run found integration/test-fixture mistakes, which were corrected before the final passing gate. Sandboxed process/socket failures were rerun with the local access needed by those fixtures; an unavailable check is never recorded as passed. Logs from this execution remain under `/tmp/rhizome-untangle-*` on the verification host.

## Limits

No push, pull request, merge, deployment, real provider request, Windows execution or external Go-consumer migration was performed. Retired exported helpers can require updates in consumers outside this repository. Fallback heading identities keep their existing source-offset sensitivity after content edits. The audit and reviews establish the bounded findings above, not the absence of every possible future improvement.
