---
summary: "Live docket for the engine performance and quality effort: evidence, ownership, decisions, PRs and verification."
---

# Engine performance docket

Effort: `2026-09-05-14-10-engine-performance-and-quality`. Contract: [[docs/specs/technical/engine-performance-and-quality]].

Integration branch: `codex/engine-performance-integration`. Initial/main base: `af100a9059fe1e34a88a59d70af26cf75e5e1368` (PR #145). Coordinator task: `01a072bf-e520-7603-bb3d-468148d9a63b`.

## Current state

Comprehensive lane audits and baseline gates are complete. All initially selected batches B01–B10, including both B03 parts, are integrated after clean reviews and all CI checks. B11 is integrated after independent review and all hosted checks. B12 is integrated after independent review and all twelve hosted checks. B01–B12 and B14–B18 are integrated in PR #206. Drew ended queue replenishment and then directed that PR #206 be merged to main; B13 was not delivered, and B19 and other queued candidates are deferred. No release is authorized. Measurements below distinguish component timings from end-to-end behavior.

## Ownership and coverage

| Lane | Scope | Worktree / branch | State | Evidence / PR |
| --- | --- | --- | --- | --- |
| Reads | Ontology, catalog, noderead, graph reads, GraphQL, pushdown, recipes | `8562/rhizome`, `codex/engine-read-path-performance` | B01/B04/B05/B08 integrated; B14/B17 integrated; B19 deferred | [Read paths](engine-read-path-research.md), [catalog](catalog-resolver-reuse.md), [sort](typed-sort-scope.md) |
| Writes | Indexing, notemeta, stores, SQLite, watchhub, ingestion, write freshness/config | `79ee/rhizome`, `codex/engine-write-path-research` | B02/B06/B07/B09/B10 integrated; B15/B16/B18 integrated | [Write paths](engine-write-path-followup.md) |
| Browser/runtime | Browser requests, web server, graph HTTP cache, runtime, MCP/agentapi, adjacent search | `1ba5/rhizome`, `codex/engine-browser-runtime` | B03a/B03b/B11 integrated; B13 not delivered (deferred to handoff) | [Graph revision](engine-graph-revision.md), [node opening](node-open-latency.md) |
| Coordinator | Shared contracts, prioritization, reviews, integration, final verification | `102c/rhizome` | Active | This docket |

Worker task IDs: read `01a072c1-8c35-7553-bf0f-fc2534fdf917`; write `01a072c1-af78-79e1-8a9f-cc4e52d4f94c`; browser/runtime `01a072c1-bfee-78d3-b929-94fe583dabdb`. Additional isolated branches: `/tmp/rhizome-engine-graph` (B03b), `/tmp/rhizome-engine-alias` (B06), `/tmp/rhizome-engine-retry` (B07), `/tmp/rhizome-engine-code-freshness` (B09).

## Opportunities

| ID | Finding / evidence | Classification | Expected impact | Owner / dependencies | Decision / PR / verification |
| --- | --- | --- | --- | --- | --- |
| C01 | Warm graph HTTP cache checks call full-table `GraphWebFingerprint` signatures (`pkg/anchors/sqlite/graph_inputs.go`) | Verified stale-cache collision; measured linear cost | Warm cache check median 0.085ms empty → 30.808ms at 100k edges | Browser/runtime owns investigation and graph_inputs.go; shared store changes require coordination | B03b migrated revision approved; write tradeoff accepted under D11; corrected trigger validation passed full gate |

| C02 | Catalog repeatedly creates projection resolvers per child | Measured quadratic cost | 100→500 descendants: 3.779→97.603ms; 6.16→172.37MB | Read lane | B05 PR202; controlled 26.4x catalog gain, full gate passed |
| C03 | Cold exact GraphQL note lookup scans unrelated notes | Measured scaling | 1k unrelated notes: existing 13.691ms; missing 131.148ms and 2000 reads | Read lane | B04 PR199; controlled bounded reads and corrected error regression pass |
| C04 | Graph precedence/hydration performs work before small output limits | Measured hotspot | 10k typed + 10k document edges: 1.92s with 10/10 output limits | Read lane | B01 integrated with parity and controlled CPU measurements |
| C05 | Code persistence metadata commits before later content chunk failure | Verified failure-injection bug | Freshness retry can skip missing anchors | Write lane | B02 PR195 integrated after all checks |
| C06 | Alias loading scans all existing alias entries for each path | Profiled quadratic work | 1k-note one-path delta 16ms / 4.56MB; removal 51.5% CPU | Write lane | B06 PR197 integrated; one-note delta 16.058→4.345ms |
| C07 | Same-ref pane refresh opens two extra SSE connections; obsolete reads lack aborts | Reproduced lifecycle defect | Redundant subscriptions/projection and abandoned requests | Browser/runtime | B03a PR194 integrated; lifecycle and real-browser checks passed |
| C08 | No-op metadata checks reproject/read full vault | Measured source work | Dirty scan 14.75ms / 40.6MB at 1k notes; freshness two full scans | Write lane | Deferred: freshness contract needed; see write-path follow-up |

Initial opportunity timings are exploratory. Accepted batch measurements below were repeated with controlled host contention; lane reports own exact fixtures and commands.

## Selected batches

### B03a — pane lifecycle

Owner: browser/runtime worker. Approved 2026-09-05 under D01. Preserve one SSE connection per stable canonical ref set; abort superseded, closed and unmounted reads; preserve generation guards, current pane identity, edit restore and preview. Verify with deferred-request tests and actual browser flow. Excludes graph-cache/follower abstractions. Integrated through PR #194 after focused regressions, full gates, and real browser checks.

### B01, B04, B05 — read path

Read worker approved for three distinct PRs: B01 graph precedence bookkeeping and bounded assembly; B04 requested-host hydration, exact-path query resolution, inbound type batching and summary-to-content upgrade correctness; B05 snapshot/schema-local catalog resolver reuse. Preserve edge selection/ranking/diagnostics, typed/embedded identity, overlay semantics, identifier ambiguity, and exact catalog rows/fields/order. No persistent cross-request cache or schema migration. Evidence: lane research scale curves; require behavioral parity and bounded source/store-call counts.

### B02, B06, B07 — write path

Write worker approved for distinct PRs: B02 one atomic transaction per complete queued CodePersistenceBatch; B06 existing bulk alias constructor with complete candidate/alias-owner union; B07 classify retryable transaction-open errors and honor cancellation during backoff. Atomicity regression covers previous state, first/later chunk and rationale failure, >128 paths, and successful service retry. Measure healthy persistence/hold time at 1/128/512 paths. Alias delta must equal fresh metadata after ambiguity, changes, deletions and relative links. Retry tests assert observable errors/cancellation without sleep-count coupling.

### B03b — graph revision

Browser/runtime worker approved to replace aggregate signatures with a migrated singleton incarnation token and revision incremented by graph-domain INSERT/UPDATE/DELETE triggers. Audit graph label/type/score inputs, OLD/NEW filtered membership, external-target isolation, transaction rollback, cross-connection updates, migration upgrade/reopen/read-only access and unchanged-state cache reuse. Benchmark write overhead as well as read gains. Separate PR from B03a; follower cancellation is a follow-up.

### B08 — SQL field-sort scope

Approved and integrated after B01/B04/B05. Fixed ten candidates slow from 0.067ms to 2.83ms when unrelated rows rise from 100 to 10k. Candidate-type-scoped aggregate approved: restrict field rows to selected ontology node IDs, preserve field-name-only MIN/MAX/null/tie semantics. Actual generated SQL EXPLAIN and fixed-candidate parity/scale evidence required. No migration.

### B09 — authoritative code freshness

Write lane approved to remove the explicit index mtime pre-read skip and retain content-hash/indexer-version/trusted-status parse/persist guards. Existing watchers already read/hash bytes. Remove the redundant code-mtime aggregate load and unchanged-code touch writes from this path. Verify actual full/code commands detect same-second, backdated and preserved-mtime edits; unchanged content performs no parser/persistence work. Measure selected bytes and 1k/5k-file no-op costs; no new inode/ctime contract or blanket force-reparse.

### B11 — real note/node opening latency

User-prioritized 2026-09-05 after reporting roughly ten seconds to open notes or graph nodes in the Rhizome repository. Browser/runtime worker owns real click-to-content and full PublicNodeDetail tracing on a disposable copy of that repository/index. Compare original base with integrated read/graph fixes before selecting another implementation. Preserve the live vault and index. Diagnose startup gates, freshness, projection/graph work, payload and rendering rather than assuming stale index.

### B12 — one automatic index command

User-selected 2026-09-05: remove `rzm index --code` and `rzm index --semantic` mode switches. Plain `rzm index` determines required note, code and enabled embedding work from configuration and freshness. The write worker owns a separate reviewed batch; the read worker audits invocation surfaces and retained contracts. Preserve explicit full `--rebuild` and supported runtime indexing boundaries; top-level mode-scoped rebuilds retire with the mode flags. B12 implementation and matched plain-index measurements are verified at `35394d21`; later diagnostic and guidance corrections preserve measured runtime paths. PR #205 final head `50029c2d` passed all twelve checks in complete workflow `33993794450` and was squash merged as `6cdc5a53`. Both all-author review threads were resolved; Greptile reviewed that head at 5/5. Historical B11 and code-only indexing measurements retain their original source and workload attribution.

### B13 — usable published reads during indexing

Browser/runtime owner: design, prototype and deliver a consistent published read generation so existing note/node browsing can continue while indexing or embedding work is blocked. The current runtime snapshot contains mutable services and refresh-on-read source access; retaining it alone is insufficient. Preserve indexed data, source bytes/inventory, schema/configuration identity and request/graph generation consistency together. No premature Ready publication or replacement of the live writer database beneath readers. Initial absence, restart, followers, failure and reclamation need explicit behavior. A retryable 503 guard can support correctness but does not complete the usable-read outcome. Independent formative review precedes implementation; the worker owns the review and subsequent delivery within D22.

### B14 — bounded find-query work

Read/query owner: measure and reduce surviving whole-inventory scans, sorting and type hydration for bounded find/interface roots, including multiple roots in one operation. Preserve ordering, filters, type membership, identity, freshness/error ownership and fallback providers. Only keep an optimization with concrete evidence and behavior parity. Cold projection coalescing and durable ensure-apply edge repair remain subsequent candidates, coordinated with the runtime publication owner.

### B15 — avoid identical configuration replacement

Write/configuration owner: skip byte-identical config/workflow replacement after the existing merge and serialization, preserving real changes, permissions, unknown fields, cleanup and errors at the owning persistence boundary. Fresh disposable evidence found identical configuration replacement on three unchanged index runs. One settled no-op observation produced seven invalidation signals and two additional runtime epochs. This demonstrates avoidable live work, not seven HTTP requests or a measured rendering/latency saving. Deterministic config/watch tests must preserve notifications for real changes. Graph write batching and rationale schema ownership are subsequent write candidates.

### B16 — graph-score write batching

Write owner: replace per-row document/anchor score inserts with the existing bounded multirow helper inside the existing transaction. Preserve triggers, graph identity/revision, filtering, input order, duplicate replacement and rollback. Five corrected quiet pairs of actual populated replacement methods at 10,000 rows measured document writes 183.055→53.353ms and anchor writes 124.127→24.567ms. Allocated bytes increase from 4.72→7.90MB and 2.40→3.19MB respectively; this is accepted for the shorter measured persistence operation, without an end-to-end latency claim. The original `8f4d3c45` timing is superseded: review moved conversion before the writer lock and added cancellation checks. PR #209 corrected head `17a6553f` passed full local gates and complete hosted CI and is integrated as `75d6b939`. Measured writer hold at 10k rows fell from 183.052→52.241ms for documents and 124.124→24.140ms for anchors.

### B17 — ensure-apply durable edge repair

Read/query owner: repair D13 through the owning source synchronization and ontology path. Applying a new embedded block identity must update persisted edges and remain correct through a fresh scope. Preserve current mutation authority and lock ordering; a sealed B13 generation remains immutable, and partial synchronization cannot announce a complete new generation. Retain original/B04 reproduction, regression, failure and cancellation evidence. Independently review any new callback or ownership seam before implementation.

### B18 — rationale schema ownership

Write owner: measure the actual post-B02 empty and populated batch at 1/128/512 paths before removing repeated runtime schema work. Source shows three ensure calls with three DDL statements each per path. Prove supported store opens establish the required schema first, including read-only and migration behavior; preserve FTS/map ordering, atomicity and errors. Initial historical timings are not the acceptance baseline. Keep only a measured simplification with schema and rollback parity.

### B19 — embedded validation projection reuse (deferred)

Deferred when Drew froze the queue before implementation started. Future read/query assessment: assess reusing one document/schema-local projection resolver in the embedded validation traversal. A fresh profile of two existing views tests at `884d68cd` found repeated resolver construction under `validateEmbeddedProjectionFields`; source creates a new resolver for each field, collection and global child before the traversal's seen check. Race-enabled CPU samples attribute 4.77s cumulatively to validation, 4.45s to resolver creation and 3.92s to global-source indexing. This shared-host diagnostic is not a production timing or accepted speedup. Preserve recursive/cycle behavior, issue ordering, global sources and error ownership; require non-race scale and parity evidence before implementation. Artifacts: `/tmp/engine-views-audit.jsonl`, `/tmp/engine-views-audit.cpu`, `/tmp/engine-views-audit.test`.

## Decisions

- D01: Keep public behavior stable by default. Scoped exceptions require a concrete coordinator-approved migration decision before edits. User delegated this authority at kickoff.
- D02: Three independent research lanes precede batch selection. Workers own their local checkout and lane report; coordinator owns this docket, shared effort/spec records, and final changelog reconciliation.
- D03: Squash merge approved child PRs into integration only; never merge/release main. One coherent Conventional Commit per worker PR. Update shared bases without rewriting published worker history unnecessarily.
- D04: Timing results under concurrent builds are exploratory. Repeat accepted before/after comparisons with controlled host contention for final claims.

- D05 (foundation review, approved): A graph-domain persisted revision is simpler and correct across supported database writers; PRAGMA data_version would require connection-lifetime handling and invalidate on explicitly isolated external evidence. Migration preserves existing derived data, uses the Intel domain runner and new database incarnation, and adds no public CLI/API/config shape. Automatic live DB file replacement remains unsupported. Worker must audit trigger coverage and write overhead before review acceptance.
- D06 (foundation review, approved): Complete code persistence is the atomic publication boundary. Existing queue bounds govern normal batches; internal statement chunking may remain. Removing artifact-family transaction splits prevents false freshness and mixed committed generations. If writer hold materially regresses, reconsider complete per-path bundles rather than partial artifact publication.
- D07: Exact same-mtime code changes were reproduced as stale after a successful index. This needs a concrete freshness/I/O tradeoff decision after first write batches; it is not categorized as low-value.

- D08: One small SQLite transaction retry seam may replace three demonstrated duplicated loops (Intel and two embedding stores), with typed busy/locked classification and prompt cancellation. Document internal backoff convergence; preserve writer locking and avoid a generic retry framework.
- D09 (approved): Explicit code indexing must use content to prove freshness. Timestamp equality/order alone cannot establish unchanged content; selected-byte hashing is accepted while unchanged parse/persist work remains skipped. This corrects the reproduced stale-index behavior without a public option or stored-schema change.

- D10: Identity-only query resolution may explicitly omit locator formatting through the existing Scope.Resolve seam. Preserve canonical refs/ambiguity checks; defaults and ensure-plan/apply retain locator behavior. No CLI/HTTP change or separate query SQL bypass.
- B10 approved: ownership transitions dominate the measured write fixture (686ms cold,46ms no-op). Batch source/coexisting-owner reads and reverse-index retirement/pruning for the complete effective path set inside the same transaction. Reuse existing retirement seams; preserve unconditional orphan cleanup, exact equality predicates, affected sets and reconciliation generation. No wholesale retirement rewrite or schema change.

- D11 (user steering, 2026-09-05): Favor faster reads when indexing is marginally slower; Drew notes embeddings dominate indexing in his usage. B03b accepted after five paired fixture runs: cold median 677→708ms and incremental 382→406ms, with warm 100k-edge fingerprint about 26ms→3–5µs. Benchmark fixtures exclude real embedding-service latency; this preference does not turn component measurements into end-to-end production claims.

- D12: B09 authoritative content check cost accepted. Warm no-op root walk at 5,000 files / 20.48MB rises from 129.313ms to 186.200ms; candidate-only work rises from 66.303ms to 146.673ms. Parser/persistence work stays zero; allocation increases are retained in the batch report. This is a correctness correction, not a speedup.
- D13: EnsureLinkTargetApply persisted edge convergence is a confirmed pre-existing defect. Original and B04 both retain old edge identities in fresh scopes after a block-ID fix. B04 clears request caches but does not claim to repair durable edges. A separate source-publication and ontology.SyncPaths design is required at runtime/indexer authority; expanding CatalogStore mutation authority inside this read batch is deferred. Retained reproduction and proposed owner are in B04 evidence.
- D14: User requested checking the installed slop/code-assessment skill. No Ponytail skill was found; Poteto Mode and Pstack deslop/strict code-quality skills are installed. Fresh cross-lane reviews found no structural issue in B03a/B03b/B05/B08 or the selected write changes; B04 obsolete partial-result bookkeeping was removed with its reviewed error-handling correction. This assessment does not replace correctness or current-head PR checks.


- D15: Startup/live ownership availability remains a separate contract follow-up. Both original and combined builds can report HTTP ready while an ownership transition clears global metadata readiness through code/semantic publication. Publishing metadata Ready early would expose partially converged ontology/graph state and violates existing publication invariants. A durable generation pre/post request guard could return a truthful retryable response; uninterrupted browsing requires a prior published read generation or a distinct raw-note contract. These changes are not hidden inside B11 link lookup optimization. Evidence and acceptance: [startup read availability](startup-read-availability-followup.md).

- D16: B11 source-link resolution uses current Markdown metadata intersected with live inventory and persisted aliases. Aliases cannot re-admit excluded paths. Resolved means an eligible listed path, not a promise that an unrelated body read/parse will succeed; actual opening retains content-error ownership. This intentionally removes incidental eager validation of every unrelated target.
- D17: B11 ontology defaults reuse NewServer's existing noteReader/NoteAdapter. It already owns cold/stale crawl, dirty-path refresh, deletion, watcher/config selection and provider snapshot behavior. Explicit readers and no-cache fallback remain. This replaces bypassing that authority with raw vault discovery; no new cache or lifetime is introduced.

- D18: Actual serve starts before the live cache may be published, so a constructor-only reader copy is insufficient. B11 exposes the currently available NoteReader through runtimeview.Snapshot and resolves the default reader from the live snapshot, then configured/raw fallback. Snapshot lookup remains nonblocking and does not initialize or refresh the cache; explicit readers retain priority.
- D19: Browser instrumentation measured four unchanged global graph rebuilds and 12 WebGL contexts for one note click. B11 separates click-handler updates from renderer lifetime using committed callback state. Preserve current handler behavior, graph-data replacement and unmount cleanup; verify context counts and in-page pointer-to-content/paint timing. Test-driver wall time is not the interaction metric.

- D20 (explicit user direction): `rzm index` is the single automatic indexing command; retire the `--code` and `--semantic` CLI modes and their obsolete plumbing. Configuration retains authority over enabled providers and source scope; indexing must not silently enable a disabled embedding provider. Update active callers, diagnostics, templates and documentation at their sources. This intentionally changes CLI flag compatibility: callers migrate to plain `index`; there is no persisted-data migration. Rollback reverts the coherent CLI/orchestration/caller change, without downgrading Intel v62. The former code-only/semantic-only rebuild selections retire with these flags; plain `index --rebuild` remains an explicit lock-protected full rebuild. Reconcile the living indexing-workflow specification, while preserving historical frozen effort records. Verify automatic configured coverage, unchanged-content skips, disabled-provider behavior, full rebuild controls, generated-surface consistency and applicable gates. B12 is selected work and must finish before closure.

- D21 (explicit renewed user direction): Continue all three workers until Drew says stop. Once a worker opens a PR, it starts its next bounded batch in a separate worktree while independent review/CI monitoring proceeds. Workers own arranging independent review, fixing feedback, satisfying applicable gates and merging accepted PRs into `codex/engine-performance-integration`; routine coordinator approval is not a merge gate. Fetch integration and assess intervening changes before merging, coordinate brief merge actions with peers, preserve unrelated work, and escalate material contract exceptions. Never merge main or release. Coordinator owns prioritization, shared delivery records and cross-batch coordination.
- D22 (B13 formative scope approved): Retain a complete prior published read generation during indexing, with honest generation/freshness semantics. Preserve Markdown/SDL and mutation authority, complete publication barriers and consistent content/schema/index dependencies. No mixed-generation success, silent live-disk fallback into a sealed generation, early Ready state or unsafe live database replacement. The worker may choose a supported snapshot/storage mechanism after independent formative review and must measure preparation/disk/retention costs. A bounded prototype is an intermediate proof, not completion of the browsing outcome; restart/follower/no-prior-generation behavior must be explicit and verified.
- D23 (B14 scope approved): Optimize surviving bounded find-root work only after a representative baseline identifies the cost. Preserve public query result semantics and owning provider boundaries; independent review and before/after parity are required. Do not manufacture a finding if accepted batches already removed the cost.
- D24 (B15 selected from new evidence): Byte-identical configuration replacement is now a reproduced source of live reconciliation/invalidation, superseding its earlier unmeasured-amplification disposition. Fix at configuration persistence, without broadening generic atomic-file semantics or suppressing genuine config events. Build source for the observed binary is `e1a31fe3`, SHA256 `166b23cc2232b6e6e8d0a5fb56bc8fbe3a265e553eabcbef000398b2403b65b2`; inspected `61d9730b` differs only in delivery records. Retained observations: `/tmp/engine-next-write-probe/evidence.json` and `/tmp/engine-next-write-probe-isolated/evidence.json`.

- D25 (B16 accepted tradeoff): Actual score replacement profiles and five controlled pairs support bounded multirow writes. Preserve revision triggers and transaction boundaries; accept the documented allocation increase. Isolated old edge-trigger costs do not establish this batch's gain.
- D26 (B17 selected): Resolve the known durable-edge defect at source publication authority, with independent formative review of any new seam and explicit compatibility with B13 sealed reads.
- D27 (B18 conditional selection): Remove repeated rationale DDL only after current-method measurement and proof that supported store construction owns schema readiness. No generic schema or transaction redesign.
- D28 (B19 conditional selection): Reduce repeated resolver setup within one immutable snapshot/schema validation traversal only after representative non-race measurement and full issue/identity parity. No cross-request cache or validation-policy change.
- D29 (latest explicit user direction): Freeze new work. Finish only current inflight B13, B14, B16, B17 and B18, including their reviews, fixes, gates and integration merges. B15 is already integrated. B19 and all other queued candidates are deferred. After final integrated verification, finalize the existing PR #206 to main and stop, without merging main or releasing. This supersedes the ongoing replenishment in D21.
- D30 (B17 compatibility decision): Current writer authority is required for whole link-target apply. CLI composition supplies the indexing-owned capability; writable MCP embedders must provide the explicit live apply callback. Missing authority rejects before any source write; read-only and preview behavior remain. This scoped API exception avoids the known partial catalog-only apply and dependency cycles. The B17 report must name capability fields, migration example, error and retry behavior, rollback limitations, and supplied/absent-capability verification.
- D31 (B13 completeness correction within approved scope): A valid assessment can intentionally emit no catalog root, but missing rows cannot prove that the expected catalog is empty. Full and scoped writers therefore record the exact projected node count and a stable digest of node IDs, kinds, types, parents, locators and structural fingerprints in each assessment. Read-only completeness requires those witnesses to match and retains required typed/fallback-root checks; missing legacy evidence is not equivalent to zero. Ontology materialization version 4 causes the normal writable path to regenerate older derived evidence. Sealed consumers never repair their bundle, and future materialization versions still fail closed. Independent ontology review accepted this direction after deleted-child and same-count-substitution regressions rejected weaker alternatives. Final B13 gates, migration evidence and integration acceptance remain pending.
- D32 (B17 CI correction within frozen closeout): A deterministic real-SQLite/queued-writer regression reproduces an initial body worker publishing state after the final catalog refresh deletes its reused chunk. Full indexing will finish initial streaming producers and flush queued writes after the post-code submitter closes and before the final ontology catalog refresh starts. This intentionally removes that phase overlap; it does not add an indexing pass or change stored schema. The final body pass, failure/cancellation propagation and complete publication barriers remain required. Returning to the old ordering would reintroduce the race. Verify final chunk/embedding/state completeness and provider calls, measure the enabled-provider ordering cost, and keep prior B12 timings qualified as historical. Final independent review, gates and hosted acceptance remain pending.
- D33 (closeout and main merge): B17 and B18 are integrated at `b1a75a82` and `04be6542`, completing the inflight queue. B13 published reads is not delivered and returns to the handoff as an unstarted-from-main candidate; nothing in this delivery claims its behavior. Drew directed that PR #206 merge to main once hosted CI is green, which supersedes the no-main-merge constraint in D29. No release is cut.
- Execution coordination: Local heavy work uses `/tmp/rhizome-engine-heavy-slot.py` with shared locks for builds/tests/indexing/browser runs and exclusive locks for complete timed comparisons. Every worker propagates this to subagents; no detached heavy children may outlive the wrapper. Source work, reviews and hosted checks remain parallel.

## Verification record

- Initial clean worktree and current-main/PR #145 ancestry verified on 2026-09-05.
- `make build`: passed at initial base.
- Default documentation validation and frozen-scope-drift: 0 issues; typed effort/frozen-spec links resolve at creation commit `76d41fd3`.
- Rhizome agent session and note-backed current user: working.
- Baseline `GOMAXPROCS=4 make check CHECK_JOBS=2 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4`: passed (all gates, log `/tmp/rhizome-engine-baseline-check.log`). Production code remains at af100a9 for this baseline; current integration docs commit 2070ffa6. Selected-batch verification pending.

- Clean final-comparison baseline built from detached `af100a9059fe1e34a88a59d70af26cf75e5e1368` with `GOMAXPROCS=2 GOFLAGS=-p=2 NO_WEB=1 make build`; clean before/after, exit 0. Binary SHA256 `a0f950bc5a5874317de92ec3704ed816c250feb124c09827d9d282a8ae2d5316`.

- B11 complete hosted workflow `33991478028` passed at `7d9550de7b07e855b7521699866eebf186f22ff1`, including both platform builds; all twelve checks passed and all-author review threads were empty. Integration squash `865d8440e6a9e0d5a01ee35cb174c23ecda44ca1` differs from the tested B11 commit only in changelog, effort and guidance/evidence Markdown. Gate attestation: `/tmp/rhizome-b11-fixtures/evidence/b11-gate-attestation.json`; full `make check` and ten-test `make web-e2e` exits were zero. B12 changes require their own subsequent gate.
- B11 evidence audit corrected an initial provider-isolation assumption: explicit semantic indexing enabled configured intent embedding work. No outbound trace was retained. Startup-overlapped baseline samples are labeled; accepted quiet HTTP claims use the linked specification and pizza notes. Full payload equality across four notes remains separate evidence. The retained reproduction now uses plain indexing, disabled providers and separate per-binary fixture copies.

- B12 local verification: `GOCACHE=<worktree>/.gocache GOMAXPROCS=4 make check GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4 CHECK_JOBS=2` exited zero (`/tmp/b12-make-check.txt`). Full `make build` passed; `make web-e2e` passed all ten tests using plain-index fixture setup. Default and frozen-scope validation reported zero issues; repeated `init --yes` reported no template updates. Four subsequent diagnostic strings passed focused `cmd` and CLI tests; eight subsequent guidance files passed independent review, default/frozen validation and 143 code-anchor checks on a freshly indexed disposable copy. Their indexing/runtime/frontend/build-input delta from the measured source is empty.
- B12 accepted full-UI comparison: original `af100a90` versus `35394d21`, plain index on both sides. Main fresh/no-op/edit medians: 1073.842→992.135 / 223.223→188.997 / 330.196→296.512 ms. Separate graph fresh/edit: 3623.524→1430.670 / 3200.359→921.549 ms. All 50 phase invocations passed; compared counts and selected final normalized topology match. Sources, binary/asset/runner hashes, samples, provider settings and limitations are recorded in [write evidence](engine-write-path-followup.md) and [graph evidence](engine-graph-revision.md). Preliminary asset-mismatched and historical code-only runs are explicitly separated.
- B12 final browser smoke: ordinary index plus both retained probes passed against the full-UI binary, four valid detail responses and visible fetched pane confirmed, providers stayed disabled with zero embedding rows, fixture stopped. Fresh four-copy compatibility checks passed old-binary refusal without mutation and explicit old/current/future-version rebuilds; accepted fixtures and binaries remained unchanged. These are bounded correctness checks, not added timing samples.

- Final B12 integration: `6cdc5a53730357fa3650a3e271dea79697e52e74` differs from tested/reviewed child `50029c2d4e87079b671cda349bcf860e7a43824c` only in four performance Markdown files. No production, test, template or build inputs differ. The completed local/hosted gates and measured runtime paths therefore apply to the integrated production tree; final documentation and artifact checks remain separate.

- Final integration artifact: clean source `e1a31fe346da3ac7bdc0e82fc8c59006bd40de50`, `GOMAXPROCS=4 GOFLAGS=-p=4 make build` exited zero (`/tmp/engine-final-integration-build.txt`). Full-UI binary SHA256 `166b23cc2232b6e6e8d0a5fb56bc8fbe3a265e553eabcbef000398b2403b65b2`; direct `index --help` confirms retired flags absent and full rebuild retained. `RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm validate` and `validate frozen-scope-drift` both reported zero issues; the anchored `closure-drift-pack` resolved this active effort, frozen SPEC-0089 and delivery/deviation fields. Current main remains `af100a90` and is an ancestor. The final build reuses no historical benchmark timing attribution.

## PR ledger

| Batch | PR / head | Local evidence | Independent review | Integration |
| --- | --- | --- | --- | --- |
| B01 graph precedence | #193, `e0821f14` | Full make check passed; 1919.702→22.102ms mixed20k-edge assembly, 43.594→45.162MB | Coordinator reviewed code+parity; Greptile5/5, no findings at same head | Squashed as `f5a71124`; all current-head CI passed |
| B03a pane lifecycle | #194, `fd4014ba` | Full make check; 17 focused tests; 10 browser E2E; same-ref connections 1/2/3→1/1/1 | Original P2 fixed with stale session, SSE, and same-file embedded-to-root regressions; coordinator source review clean | Squashed as `1ec502b3`; all current-head CI and reviewed navigation regressions passed |
| B02 atomic persistence | #195, `7a8e707c` | Before→after ms/op at1/128/512 paths:1.094→1.073 /17.819→17.742 /73.278→72.360; regressions green | Coordinator code/test review clean; continuous512-path hold~72ms accepted (no fairness claim) | Squashed as `c76535e1`; all current-head CI passed, Greptile 5/5 and no review threads |

| B03b graph revision | #196, `5bb1c6d4` | Corrected full make check and docs pass; migration/trigger/cross-handle regression coverage; retained read/write/open benchmarks | Coordinator source review clean; write tradeoff accepted under D11 | Squashed as `df56c3b9`; literal-case regression, corrected full gate, fresh review and all CI passed |
| B06 aliases | #197, `dc379f0b` | Controlled one-note delta: 16.058→4.345ms, 4.490→4.094MB; component allocation tradeoff documented | Coordinator source/parity review clean | Squashed as `e5fcfb31`; all current-head CI passed; stale helper pointer fixed |

| B07 SQLite retries | #198, `eee51327` | Full isolated gate plus merged atomicity/retry race tests and docs validation | Greptile 5/5; stale retry-policy documentation corrected | Squashed as `b30e8657`; all current-head CI passed |
| B04 bounded reads | #199, `f8ce6416` | Existing 1k-note path-qualified query 11.297→0.114ms; missing 116.468→0.062ms, source reads 2000→0; full gate passed | Partial cached-eligibility error suppression fixed with red/green regression and simpler fallback. Separate persisted-edge defect disposition D13 | Squashed as `2242706a`; corrected full gate and all current-head CI passed |
| B09 content freshness | #200, `4a70442e` | Full gate/docs pass; preserved/backdated timestamp regression; accepted no-op I/O cost D12 | Independent source review and Greptile 5/5 | Squashed as `5cd03e3c`; all current-head CI passed |
| B10 ownership batching | #201, `e046dca8` | Full gate/docs pass; no-op 20.560→2.684ms; populated retirement 254.491→148.736ms; 901-path cleanup regression | Independent source review and Greptile 5/5 | Squashed as `c169f358`; all current-head CI passed |
| B05 catalog reuse | #202, `fcf5c431` | 500 descendants 91.305→3.460ms; allocations 172.129→4.951MB; full gate passed | Baseline/current parity and independent structural review clean; Greptile 5/5 | Squashed as `4712d0c5`; all current-head CI passed |
| B08 typed sort | #203, `956d317b` | Fixed ten candidates among 10k nodes: 2722.385→55.178µs; actual SQL plan and baseline/current ordering parity pass | Independent source and structural review clean; Greptile 5/5 | Squashed as `dd0da048`; full gate/docs and all CI passed |

| B11 full node opening | #204, `7d9550de` | Full make check and ten browser E2E tests pass; quiet spec/pizza HTTP medians 1802.46→32.86ms and 1491.77→8.03ms; renderer-only summary DOM 198.6→93.0ms | Independent runtime/renderer/cross-batch reviews clean; source-equivalent full gate; startup-overlap and provider audit corrections retained | Squashed as `865d8440`; all twelve checks and complete workflow passed |

| B12 plain indexing | #205, `50029c2d` | Full local gate, build, ten E2E tests, idempotent init, docs/frozen and 143 code anchors passed; matched full-UI fixtures and four-copy rebuild checks passed | Independent command/configuration, guidance, evidence and compatibility audits passed; both hosted findings resolved; Greptile 5/5 | Squashed as `6cdc5a53`; all twelve checks and complete workflow passed |

| B15 identical config saves | #207, `705573ea` | Full local gate/build/docs passed; unchanged index preserved config identity and emitted zero SSE signals; real edit positive control passed | Independent Standards/Spec reviews clean; Greptile 5/5; zero all-author review threads | Worker squashed as `48880f93`; all twelve hosted checks passed in complete workflow `33996383440` |

| B14 query inventory | #208, `8e31fee5` | Full gate/build/ten E2E/docs passed; five quiet pairs over three broad roots at 10k notes: 54.295→29.461ms, inventory calls 3→1; single-root control unchanged | Independent review clean; exact-head Greptile 5/5 and no actionable or inline findings | Worker squashed as `6c3302e5`; all twelve hosted checks passed in complete workflow `33996924417` |

| B16 graph scores | #209, `17a6553f` | Corrected full gate/build/docs and five paired measurements passed; complete row/revision and canceled/invalid-before-lock regressions passed | Independent correction review clean; hosted 8f4d3c45 finding addressed and its thread resolved. No new-head bot-review claim | Squashed as `75d6b939`; complete workflow `33997841778` and all eleven CI jobs passed at the accepted head |

| B18 rationale schema | #210, `067f6e5c` | Store initialization owns rationale FTS schema creation and validates it before trusting an open proof; all eleven hosted checks passed | Worker-owned independent review clean | Squashed as `04be6542` |

| B17 durable link edges | #211, `a582cb3d` | Ensure-link apply converges durable edges, cache publication is generation-fenced, and full indexing drains initial streaming writes before the final catalog refresh; all twelve hosted checks passed at the accepted head | Independent review clean; Greptile 5/5 at `30d93d4e`, with the later CI-ordering correction reviewed by the worker | Squashed as `b1a75a82`; this correction also resolves the `FOREIGN KEY constraint failed` failure seen in PR #206 run `34000908821` |

## Remaining / deferred

Resume guide requested by Drew: [remaining work handoff](engine-follow-up-handoff.md). It separates inflight delivery from unstarted candidates and records the evidence, constraints and first useful proof for another AI.

Lane reports retain the remaining opportunities and their evidence. B01–B12 and B14–B18 are integrated in PR #206. B17 (`b1a75a82`) and B18 (`04be6542`) completed the inflight queue. B13 published reads did not reach a child PR and is not delivered; its formative decisions (D22, D31) and its unmerged `codex/engine-published-reads` work stay in the [handoff](engine-follow-up-handoff.md) as a resume candidate, not as shipped behavior. B19 and subsequent opportunities remain deferred. Drew subsequently directed that PR #206 be merged to main; that instruction supersedes the earlier no-main-merge constraint in D29. No release is authorized.

## Consolidated follow-ups

| Area | Evidence and disposition | Next useful proof / owner |
| --- | --- | --- |
| Full note opening | B11 integrated; quiet spec/pizza HTTP comparisons and hardware renderer measurements support the improvement. Early README/indexing samples overlapped startup and are qualified separately. | Retained evidence and reproducible probes: [node opening](node-open-latency.md). |
| Ensure-apply durable edge identities | Repaired by B17 (`b1a75a82`); regressions cover edge convergence, cache-generation fencing and publication ordering. | Closed. |
| Full-vault metadata freshness | Source verification and broad new-path link derivation remain; untrusted mtime shortcuts are excluded. | Metadata owner: measure reuse of already-computed snapshot with freshness parity. |
| Rationale schema work | Delivered by B18 (`04be6542`): store initialization owns schema creation and validates FTS schema before trusting an open proof. | Closed. |
| Watch debounce and maintenance | Sustained events delay delivery until quiet; ANALYZE cost is small in the original fixture. Config replacement amplification is now reproduced and selected as B15. | Runtime/config owners: B15 removes identical saves; maximum-age policy and maintenance due-gating remain separate. |
| Graph followers and projection cache lifetime | Cancellation coupling and duplicate cold loads are source findings; impact unmeasured. | Web/runtime owner: actual concurrent/cancelled request evidence before new cache policy. |
| Invalidation bursts and graph layout | Callback-driven global renderer rebuilds were reproduced and selected in B11. Broader layout and invalidation-burst impact remains unmeasured. | Browser owner: request counts and rendering profile after B11. |
| Broad typed roots, schema/recipe caching, derived IDs | Smaller measured setup costs or unresolved freshness/identity contracts; deferred. | Read/query owner: selector-specific scales and cache authority before implementation. |
| Assessment error cache | Source-level suspicion only; cross-request lifetime/impact unproven. | Noderead owner: reproduce observable stale result before selecting a fix. |

B04 review-summary caveat was checked at `f8ce6416` with a temporary three-case mixed-known-miss matrix. Unknown failed metadata remains unknown, propagates its error, and succeeds on retry; established misses plus eligible property failure recover only the eligible snapshot. All cases passed; no additional production defect was established.


## CI setup candidate

Completed workflow `33993794450`, Linux unit job `101380555371`, shows setup-go running `go env`, downloading Go 1.24.2, then attempting to restore a cache containing the same toolchain paths. Tar reports `Cannot open: File exists` and restoration fails. The local workflow requests Go 1.23 in six setup steps while `go.mod` declares Go 1.24.0 and toolchain 1.24.2. Aligning setup with the repository toolchain is a bounded future cleanup candidate; confirm a successful hosted cache restore before claiming improvement. This does not explain the full Windows unit duration or justify reducing test coverage. Retained log: `/tmp/engine-ci-linux-unit-baseline.log`.
