---
summary: "Synthesis of the 2026-10-17 subsystem quality review: confirmed correctness bugs, the root causes behind most findings, a test-suite verdict, and a phased refactor roadmap across indexing, ontology, query, noderead, search, vault-core, validation, and package architecture."
---

# Subsystem quality review, 2026-10-17

Snapshot of commit `5251923f`. Nine reviewers each covered one area and wrote a full report in this folder. This page combines them into one plan for the refactoring pass.

## Bottom line

The designs on paper are mostly right. The code drifts from them in one consistent way: **the same concept is implemented two to twelve times, and the copies disagree.** Nearly every confirmed correctness bug below is a disagreement between two copies:

- The full ontology rebuild versus incremental sync.
- SQLite pushdown versus in-memory filtering.
- The frontmatter detector used by edits versus the one used by projection.
- Validation's link scanner versus the indexer's.

The second pattern is that **replaced code was never deleted**: disabled streaming machinery, a second journal engine, dead ranking channels, and runners that bypass the prepared runtime. The tests mostly assert real behavior, but the equivalence tests that would catch the drift are missing or use fixtures too clean to find it. In a few places the tests pin the buggy output as the expected result.

The refactoring pass should therefore be organized around **collapsing duplicates into one owner and deleting what was replaced**, with a differential test in front of each collapse. Rearranging packages comes last; it gets easy once the duplicates are gone.

| Lane | P0 | P1 | P2 | P3 | Test findings | Report |
| --- | --- | --- | --- | --- | --- | --- |
| Indexing | 0 | 4 | 6 | 4 | 9 | [indexing.md](indexing.md) |
| Ontology core | 1 | 5 | 5 | 2 | 11 | [ontology-core.md](ontology-core.md) |
| Ontology edits | 2 | 4 | 6 | 2 | 13 | [ontology-edit.md](ontology-edit.md) |
| Typed query | 1 | 6 | 5 | 3 | 10 | [query.md](query.md) |
| Noderead | 0 | 4 | 9 | 2 | 7 | [noderead.md](noderead.md) |
| Search | 1 | 1 | 8 | 5 | 11 | [search.md](search.md) |
| Foundation (notemeta, stores, vault-core) | 0 | 7 | 6 | 1 | 8 | [foundation.md](foundation.md) |
| Validation | 0 | 5 | 6 | 3 | 10 | [validate.md](validate.md) |
| Cross-cutting architecture and tests | 0 | 2 | 9 | 3 | 9 | [cross-cutting.md](cross-cutting.md) |
| **Total** | **5** | **38** | **60** | **25** | **88** | |

### Scope and method

- **Reviewers:** Nine Opus 5.5 reviewers worked read-only. Each read the owning subsystem note first and judged the code against it.
- **Probes:** Correctness claims were checked with throwaway tests run through `go test -overlay`, so the repository was never modified. The probe sources are in the `probes/` folder, with a `.txt` suffix so `go test ./...` ignores them. Several are close to ready-made regression tests.
- **Independent verification:** I re-ran or re-read the headline claims myself. The "Verified" column below says which.
- **Not covered:** code-intel (`pkg/anchors` language indexers, about 50k lines), the agent and MCP surface, the harness, the vault runtime, and the web UI. The cross-cutting lane touched `pkg/anchors/sqlite` and `pkg/app/web` only as architecture.

## 1. Confirmed correctness bugs: fix these first

Each is small or medium on its own and should land test-first, ahead of any structural refactor. Several are the first slice of a later consolidation, noted in the last column.

| ID | What goes wrong | Trigger | Verified | Size | Seeds |
| --- | --- | --- | --- | --- | --- |
| SRCH-1 | `rzm search` can **drop the note-embedding tables and the paid embedding cache**. | A search run while the configured provider, model, or dimensions differ from the stored metadata. `unifiedsearch/run.go:181` → `OpenWithMetadata` → `ValidateOrInitMetadata` → `resetAndInit` → `ResetDomain`. Every search also writes to the database and runs migrations. | Code read by me | S | Use the existing read-only `ValidateExistingMetadata`. Ship alone. |
| EDIT-1 | A narrative edit **changes an inline field's value**: `status:: TODO` becomes `status:: TODOEDITED SECOND`. | Editing a paragraph that follows an inline field when the submitted text lacks the leading blank line. The block's range owns the previous line's `\n`. | Probe re-run by me | S | |
| EDIT-2 | A frontmatter edit on a note that starts with a UTF-8 BOM **prepends a second frontmatter block**, and unsetting a field does nothing. | Any BOM-prefixed note. Three frontmatter detectors disagree about BOMs and leading blank lines. | Reviewer probe | S–M | The first piece of the shared Markdown syntax layer (§2-A) |
| EDIT-3 | Web `setFrontmatter`/`appendAlias` **rewrite the whole frontmatter**: comments are dropped, keys are alphabetized, and `2024-01-01` becomes `2024-01-01T00:00:00Z`. | The validation type picker fix | Reviewer | S | |
| EDIT-4 | Narrative replay **edits the wrong paragraph** when two paragraphs are identical, or when a paragraph changed but kept its length. | | Reviewer | S | |
| ONT-1 | `rzm validate` output **depends on which writer last built the rows**. Production sync omits embedded-field, `contains_min/max`, and block-ID issues that a full rebuild reports. | Any vault with embedded types or cardinality rules. The probe gets 3 issues from a full rebuild and 1 from sync. | Probe re-run by me | M | One per-note pipeline (§2-B) |
| ONT-2 | Incremental sync **is not equal to a fresh rebuild**: the `frozenStories` edge is missing after an incremental sync. Starter vocabulary (`EffortNote`, `UserStory`, "Stories In Scope (Frozen)") is hardcoded in `sync.go:580-643`. | Effort notes, edited incrementally | Probe re-run by me | M | Needs a decision (§5) |
| QRY-1 | Typed queries **return wrong results when pushed to SQLite**. `eq "Ship \| Docs"` returns `[]`, `eq "ship"` matches `"Ship \| Docs"` and `"Ship \| Tests"`, and `contains "docs"` misses. | Any String or enum value containing `\|` or `[[...]]`. The indexer strips links and aliases from every value; pushed operands are only lowercased. | Probe re-run by me | M | One value normalizer (§2-B) |
| QRY-2 | An unquoted YAML date **passes a date filter, then reads back as `null`**. | `due: 2026-01-01` without quotes | Probe re-run by me | M | |
| QRY-3 | DateTime filters and sorts **compare text, not instants**. `10:00+02:00` (08:00Z) is treated as later than `08:30Z`. An existing parity test asserts this wrong answer. | Any offset other than `Z` | Probe re-run by me | M | |
| VAL-1 / FND-5 | The default, "safe" block-ID migration fix **rewrites `[[...]]` inside code blocks, inline code, and frontmatter**. | A note containing a link-shaped example in code. `validate/identifier_block_ids.go:364` runs one regex over the whole file. | Code read by me | S | |
| FND-1 | `[[my note]]` **does not resolve** to `My Note.md`; aliases and NFC/NFD names are also case- and form-sensitive. Link health lowercases, so health says "not broken" while the graph edge is missing. | Any case or Unicode-form difference | Probe re-run by me | M | Needs a decision (§5) |
| FND-2 | `anchors.ParseNote` **drops every code anchor** in a note whose frontmatter contains `---`, such as the title `Build --- Release`. | | Probe re-run by me | S | |
| FND-3 | `ExtractHashtags` **persists false tags** from `~~~` fences, indented code, YAML comments, and `#123`. | | Reviewer probe | S | Bump the Markdown projection version. |
| NR-3 | Graph truncation can return **edges whose endpoints are missing**. | `localGraph`, MCP, or graph-ppr with a limit | Reviewer | S | |
| NR-4 | Previews of interface types **come back with short pages** when they use filters or sorting, and a non-zero `Offset` would be applied twice. | Views with an overlay | Reviewer | S | |
| IDX-3 | `rzm code index` **matches vault ignore rules against code-root-relative paths**, so `src/generated/` is missed when code roots are subdirectories. | | Reviewer (medium confidence) | S | Or retire the command (§5) |
| IDX-2 | A slow consumer **aborts `rzm index`** with "post-write dispatcher queue full" instead of slowing it down. | Load | Code read by me | M | |
| EDIT-6 | One unrecoverable ontology journal **makes `rzm serve` fail at startup** and blocks every later edit, with no documented way to clear it. | A corrupt or partial journal | Reviewer | S | |
| FND-10, FND-12 | `LoadAliasMap` swallows store errors, so alias-blind link resolution is published durably. A legacy embedding rebuild uses connection-scoped temp tables across a pool and ignores the errors. | | Reviewer | S | |

## 2. Root causes behind most findings

Each theme lists the findings it explains and the single owner it should collapse into.

### A. No single Markdown syntax layer

There are about 12 frontmatter-boundary detectors, 6 code-region detectors, and at least 3 wikilink scanners, and they disagree (FND-4). That disagreement directly causes EDIT-2, FND-2, FND-3, FND-5, and VAL-1. It also causes VAL-2, where validation judges links differently from the indexer, and ONT-10, where Markdown lexing is duplicated inside ontology.

**Target:** one `pkg/markdownsyntax` package (or a cleaned `pkg/vault/frontmatter` plus `obsidian`). It would provide a BOM-aware frontmatter locator that matches Obsidian's line-1 rule, one protected-span mask (fences, inline code, frontmatter, comments), and one link scanner. Every reader and writer would use it. Fuzz targets go here; the repository has none today.

### B. Two or more implementations of the same semantics

| Concept | Copies | Findings | Collapse into |
| --- | --- | --- | --- |
| Building the ontology read model | Full build (`buildIndexFromDocs`) and incremental (`SyncPublishedPaths`) | ONT-1, ONT-3, ONT-5 | One per-note "project once, assess once" function used by both paths |
| Field value normalization | About 6 (indexer, pushed operands, residual, overlay, views) | QRY-1, QRY-2 | `ontology.FieldValueRow` / `FieldTextValues` |
| Scalar type semantics | 5 classifiers | ONT-6, QRY-3 | `schema.ScalarKind` / `ParseScalar` with instant semantics for DateTime |
| Filter and sort evaluation | 4 (SQL, `query/root_filters.go`, noderead overlay, `app/views`) | QRY-7, QRY-8, NR-T-5 | A predicate IR in `pushdown` with one in-memory evaluator |
| Journaled multi-file writes | 2 engines (validate's for browser saves, ontology's for CLI rename and link apply) | EDIT-5, VAL-3 | One source-transaction package outside `pkg/validate` |
| Indexing pipelines | 2 (unified pipeline and `rzm code index`), with 3 copies of call-edge logic | IDX-3, IDX-1 | The unified pipeline |
| Running the default validation suite | 3 (CLI, serve live, indexing snapshot) | VAL-5 | One `Prepare` + `RunValidation` |
| Search scoring | 2 scorers, 2 intent→weights tables | SRCH-5 | One scorer with adjusters |
| Notes-hash aggregate | 8 copies, 2 already behaving differently | FND-8 | One digest type |
| Opening the database | About 22 variants; 13 in `obsidian` | DUP-1, DEAD-1 | One opener in the store package |
| Loading config | 9 loaders, about 70 call sites re-reading from disk | DUP-2 | Load once into a resolved config |
| Link resolution | 3 resolvers in validation, plus notemeta and `VaultFileIndex` | VAL-2, FND-1 | One resolver with a folded `LinkKey` |
| Small helpers | `titleFromPath` ×8 (6 bodies), `slugifyHeading` ×3 | DUP-6 | Shared helpers; these change behavior, so add regression tests |
| Embedding stores | Note and code stores sharing about 50 function names | DUP-4 | One store parameterized by domain |

### C. Replaced code still runs or ships

| Finding | What is left behind |
| --- | --- |
| IDX-1 | About 700 lines of streaming call-edge and scope machinery that production disables (`unified.go:539-542`). It still runs planning queries and can fail the index run. |
| EDIT-5 | The ontology receipt and completion plumbing, which has no production caller and leaks temp files on failure. |
| VAL-6 | Non-prepared validation runners that keep the forbidden `EnsureFreshRuntime` fallback. |
| SRCH-4, SRCH-6, SRCH-7 | The fusion and recency ranking channels, `retriever_rank` evidence on every candidate, a dead answer builder, and filter fields that drag `pkg/app/cli` into `pkg/search`. |
| NR-8 | `SharedNodeCache`, `ScopeOptions.Budget` (documented as respected, never read), and `HydrateWorkspace`. |
| DEAD-1 to DEAD-5 | 138 exported declarations with no production caller, 58 of them used only by tests, and test doubles compiled into the shipped binary. |

**This is the cheapest work with the best payoff.** It shrinks every later diff and deletes tests along with it.

### D. Whole-vault work on per-request paths

| Finding | Whole-vault work |
| --- | --- |
| NR-1 | Each locator batch walks the vault directory and re-parses notes. Web calls this once per value in a loop. |
| NR-2 | An embedded type with zero catalog rows projects every note in the vault. |
| QRY-4 | `task(first:40)` makes 82 store reads. |
| QRY-5 | One missing ref causes 73 file reads. |
| QRY-6 | Section `@neighbors` rebuilds an inbound-link index on every request. |
| ONT-3 | Each note is projected 3–5 times per sync, with quadratic resolver construction. |
| ONT-5 | The freshness check scans the whole vault. |
| FND-6 | Seven MCP handlers re-read and re-project every note; adding or deleting one note re-projects the vault. |
| SRCH-9, SRCH-10 | Repeated catalog scans per facet, and Markdown re-parsed at query time. |
| FND-13 | An integrity check scans the whole database on each write-capable open, taking 1.1 s on the 1.2 GB index. |

Most of these get fixed once reads go through `noderead.Scope` with the store doing the batching, and once theme B removes the re-projection.

### E. Invariants enforced by convention, not by types

| Invariant | How it is broken today | Findings |
| --- | --- | --- |
| Single writer | A nil writer reports success, direct-write fallbacks exist, and `EnsureIndexed` writes outside the writer lane. | IDX-6, ONT-5 |
| Index lock held | It is only a precondition by convention. | IDX-10 |
| Accepted writes survive cancellation | The writer drops them. | IDX-8 |
| Searches never repair metadata | Searches can reset embedding tables (SRCH-1). | SRCH-1 |
| Conflicts are typed | Conflict kinds are decided by matching error strings. | EDIT-8 |
| One store interface | Store capabilities are discovered through 14 silent type assertions. | NR-9 |
| Domain-specific prerequisites | One freshness probe certifies three domains. | VAL-8 |

### F. Layering and package shape

| Finding | Problem |
| --- | --- |
| ARCH-1 | `pkg/anchors/sqlite` is the whole unified database (27.7k lines, a 9,790-line `store.go`, 345 exported methods), but it is named and owned as code-intel. |
| ARCH-2 / FND-7 | `pkg/vault/obsidian` (11.2k lines) should hold primitives but opens the database and imports search, embeddings, and llm. |
| ARCH-3 | Six domain packages import `pkg/app/*`. `pkg/search` → `pkg/app/cli` alone grows its closure from 37 to 62 packages. |
| ONT-9 | `pkg/ontology` is about 24k production lines in one package. |
| QRY-10 | `execute.go` is 5,268 lines. |
| ARCH-5 | `cmd/` is 20k lines with 190 package-level globals and raw SQL. |
| ARCH-7 | 40% of production lines are in files over the 500-line limit. `.golangci.yml` is never run; it reports 519 issues, including 31 uses of deprecated `pkg/paths` shims. |

## 3. Test suite verdict

The suite is 263k lines and 6,713 tests, a 0.74 test-to-production ratio. Assertion quality is better than expected: pure tautologies, constant pinning, unconditional skips, and "only asserts no error" tests are rare. Most packages run in seconds. The real problems are different.

**1. The most valuable tests are missing or too weak.** Every stated equivalence invariant either has no differential test, or has one whose fixture avoids the cases that break. That is why the bugs in §1 shipped green.

| Invariant | Status | Add |
| --- | --- | --- |
| Full rebuild == incremental sync (ontology) | About 40 `TestBuildIndex_*` tests use only the non-production in-memory build. | ONT-T-1 differential harness: full, `SyncPaths`, and incremental touch on a rich fixture. ONT-T-2 build-twice byte equality. |
| Pushdown == in-memory evaluation | `root_scalar_parity_test.go` exists, but its fixture quotes every value and avoids pipes, wikilinks, and mixed case. | QRY-T-1: widen the fixture, add views as a fifth engine, and make it the gate for the predicate IR. |
| Delta == rebuild (notemeta) | Holds for the hand-picked cases only. | FND-T-1 randomized property test. |
| Validation agrees with the indexer | No test | VAL-T-4 agreement tests, plus HTML notes and code blocks. |
| Crash consistency of edits | Recovery fixtures are hand-built, and one builds a state a real run cannot produce. | EDIT-T-1: crash the real writer at every step and assert all-old or all-new. |
| Search is read-only | No test | SRCH-T-8. |
| Search CI runs production wiring | Integration tests build their own `search.Service` without the graph rankers and rollup. | SRCH-T-6, SRCH-T-10. |
| Batching (no N+1, no vault walk) | Unguarded | QRY-T-3, NR-T-7: assert store-call counts that do not grow with N. |
| Fresh schema == upgraded schema | No test | FND-T-5. |
| Parsers and paths | Zero fuzz targets in the repository | FND-T-2. |

**2. Some tests pin the bugs.** Rewrite these to state the intended contract:

- `QRY-T-2` asserts the textual DateTime offset ordering as correct.
- `FND-T-3`: `TestNormalize`, `TestNormalizeNote`, and `TestNormalizePath` assert the non-idempotent `paths.Normalize` output.
- `IDX-T-6`: the drain-cancellation test only checks the error type, so it cannot detect the guarantee it is named for.
- `IDX-T-7`: `TestWatchHub_EmitsEventForNestedFile` skips itself on darwin, the main development platform.
- `EDIT-T-4`: `TestEditSession_CommitChecksDiskBeforeReplace` triggers its race on the second file read, so removing a redundant read breaks it.

**3. Tests of dead or duplicate code.** Delete each group together with its code:

- **Indexing:** the 15 tests of the disabled streaming path (IDX-T-1), tests of dead reverse-index functions (IDX-T-5), and the writer passthrough tests (IDX-T-3).
- **Ontology:**
  - `TestSyncPaths_QueueIsSoleFieldRowsPlannedRecorder` checks a counter ontology never emits.
  - `TestBuildIndex_ManagedDocTemplateLinksUseFutureRepositoryRoot` checks an issue code ontology never emits.
  - `TestIndexedFieldCapabilityForField_PlansIndexedOperators` and `TestFallbackNoteTypeIsInternalByDefault` duplicate coverage in pushdown and query.
  - Five `EnsureIndexed`/`SyncPaths` twin pairs: merge them once ONT-1 lands.
- **Edits:**
  - Remove `TestEditSession_PreviewPlansFilesConcurrently` and `TestEditSession_SetInlineFieldPreservesPackedSiblings`, which another test already covers.
  - Merge `TestRecoverEditWriteJournalPreservesUnexpectedExternalChange` into the remaining journal tests.
  - Remove the receipt-matrix tests along with the receipt plumbing.
- **Noderead:** the two pre-canceled cancellation tests, which cannot detect cache poisoning (NR-T-1), and the tautological helper tests (NR-T-4).
- **Search:** tests of the fusion ranker, `BuildAnswer`, `retriever_rank`, and constants (SRCH-T-1, SRCH-T-2).
- **Foundation:** `TestDeduplicateResults`, `TestNoteStateDigestIgnoresMtime`, and `TestTitleFor_PriorityChain`.
- **Validation:** registry tests that assert the static registry against itself (VAL-T-1); merge the per-check alias tests into one selector table (VAL-T-2).

**4. Prose pinning.** About 350 assertions match exact sentences in templates, schema guidance, and live repository docs (TEST-4). `pkg/app/cli/init` alone has 629 `Contains` assertions. Fifteen test files resolve the live repo root, so editing `docs/` can fail `go test ./pkg/ontology`.

- **Keep:** reachability tests, retired-name absence lists, line and token budgets, and the managed-block markers.
- **Rewrite:** sentence pins, as structural assertions or as `rzm validate` / `rzm init --check` gates.
- **Also rewrite:** about 56 exact-SDL-text assertions, as checks against the parsed schema (QRY-T-4), and the 479 rendered-substring assertions in `collector_test.go` (IDX-T-4).

**5. Infrastructure.**

- **No shared testkit:** there are 72 temp-vault builders across 25 packages, and `testNoteMetadataIndexer` is copied into 10 packages.
- **Serial tests:** 156 overrides of three global variables block `t.Parallel`. `pkg/ontology` runs 4 of 432 tests in parallel, and `pkg/app/indexing` runs almost entirely serially (28.7 s).
- **Proposal:** `internal/testkit` (Vault, Env, Store, Index, NoteMetadataIndexer, and canonical fakes). It is specified in [cross-cutting.md](cross-cutting.md#proposed-shared-testkit).

## 4. Refactor roadmap

Phases are ordered so that each step either removes something the next would otherwise have to carry, or adds the safety net the next one needs. Items inside a phase are independent PRs.

### Phase 0: Stop the damage (S each, test-first, can start now)

1. SRCH-1 read-only store open, with SRCH-T-8. Ship alone.
2. EDIT-1 and EDIT-4 (narrative ranges), with EDIT-T-8.
3. EDIT-2 shared BOM-aware frontmatter locator, with EDIT-T-7. Have edits fail closed when the frontmatter is not at line 1.
4. EDIT-3: route `setFrontmatter`/`appendAlias` through the span patcher, with EDIT-T-10.
5. VAL-1 / FND-5: span-based block-ID rewrite with code masking.
6. FND-2, FND-3, FND-10, FND-12, NR-3, NR-4, EDIT-6 (quarantine instead of failing startup), and the IDX-3 ignore-path fix.
7. VAL-4a: add `json:"-"` on whole-file repair payloads so agent JSON stays bounded.

### Phase 1: Safety nets and gates (S–M)

1. Differential harnesses: ONT-T-1, QRY-T-1 (red on QRY-1, -2 and -3 until Phase 3), FND-T-1, VAL-T-4, and EDIT-T-1.
2. Batching contract tests: QRY-T-3, NR-T-7.
3. Run `golangci-lint` (`unused` and staticcheck SA) in `check-fast` and CI, and add a file-size ratchet listing current offenders (ARCH-7).
4. Start `internal/testkit` with the Vault, NoteMetadataIndexer, and fakes items, and migrate the 10 indexer copies.
5. Fuzz targets for paths and parsers (FND-T-2), in report-only mode at first.

### Phase 2: Delete what was replaced (S–M, low risk)

Delete IDX-1, IDX-11, the EDIT-5 receipt plumbing, VAL-6, VAL-10, SRCH-4, SRCH-6, SRCH-7, NR-8, QRY-11, the DEAD-1 to DEAD-5 clusters, and the 31 deprecated `pkg/paths` shim uses, each with the tests listed in §3.3. Fold `searchengine` into `planner` so search tests run the production wiring (SRCH-8, first part).

### Phase 3: One owner per concept (M–L; this is where the correctness value lands)

Run the following in this order:

1. **Markdown syntax layer** (theme A, FND-4). Migrate the 12 frontmatter detectors, 6 code-region detectors, and 3+ link scanners. Validation then uses the snapshot's provider facts (VAL-2).
2. **Value and scalar semantics** (QRY-1, QRY-2, QRY-3, ONT-6). One normalizer and one scalar parser, with a materialization version bump. Phase 1's QRY-T-1 turns green.
3. **One ontology build pipeline** (ONT-1, ONT-3, ONT-4, then ONT-5 single writer). ONT-T-1 is the gate.
4. **Predicate IR and a single evaluator** (QRY-7, QRY-8). Deletes `root_filters.go`, the overlay matcher copies, and the views copies.
5. **Link resolution and paths** (FND-1, FND-11), after the case-sensitivity decision in §5.
6. **One transaction engine** (EDIT-5, VAL-3), with EDIT-T-1 as the gate. This deletes `edit_transaction*.go` and `edit_write_lock.go`, and moves the engine out of `pkg/validate`.
7. **One validation orchestration** (VAL-5), then the check contract (VAL-7, VAL-9).
8. **Indexing writer:** a kind table and controls (IDX-4, IDX-5), a mandatory writer (IDX-6), one coalescing post-write hand-off (IDX-2), and a `RunUnifiedCore` restructure (IDX-7, IDX-8).
9. **Search:** one lane executor with fair concurrent budgets (SRCH-2, evaluated with `tools/searchperf` and `tools/searchquality` within one embedding-cache family), then one scorer, with rerank pinning as the final tier (SRCH-3, SRCH-5).

### Phase 4: Hot-path performance (M)

- **Noderead:** fix the locator path (NR-1), remove the embedded-index fallback (NR-2), and collapse the profiles and scope inventory (NR-6, NR-7).
- **Query:** batch-hydrate typed roots and `nodes(refs:)` (QRY-4), add an indexed identifier lookup (QRY-5), and build section neighbors from persisted edges (QRY-6). Read through `Scope` and delete `Deps.Service` (QRY-9).
- **Foundation:** persist unresolved link inputs so adding or deleting a note costs only the affected notes; move MCP facts onto persisted rows (FND-6); take the integrity check off the open path (FND-13).
- **Search:** add a request-scoped corpus snapshot and skip zero-weight graph adjusters (SRCH-9).
- **Consumers:** migrate the web type page and note search onto the Scope summary API, and delete the hand-written overlay patches (NR-11, ARCH-6).

### Phase 5: Package structure (L, mostly mechanical once Phases 2–3 shrink the code)

1. Cut the upward imports (ARCH-3): a `filterexpr` leaf, `perfworkload` moved to `tools/`, and `bootstrap` helpers moved to `codeintel`.
2. Config and env injection (DUP-2, TEST-3). This unblocks `t.Parallel`.
3. A single store opener (DUP-1), then split `pkg/anchors/sqlite` into `pkg/store` plus per-domain sub-stores (ARCH-1).
4. Split `obsidian` into primitives, config, and the store opener, and add an import-boundary test (ARCH-2, FND-7).
5. Split `pkg/ontology` into `schema`, projection, `index`, `workspace`, and `schemadoc`, and move `viewconfig` to `app/views` (ontology-core.md, "Proposed package split"). Start with `schema`.
6. Split `execute.go` (QRY-10) and `edit_session.go` (EDIT-9), and deepen the `EditSession` API.
7. Search package moves: separate the semantic query and indexing code, and add `pkg/graph` and `pkg/embeddings` (search.md, "Package map"). Unify the embedding stores (DUP-4).
8. App-layer merges: `semanticops`, `searchengine`, `agent`, `agentstart`, the two `mcpserve` packages, `runtimestop`, and `indexcore` with `indexingpipe`. Make `cmd` thin with per-command option structs (ARCH-5).

**Documentation:** each owning subsystem note must be updated in the same change as the code it describes. Several notes already contradict the code: ontology.md (the browser-save journal), indexing.md (cancellation), noderead.md (`Budget`, `SharedCache`), validate.md, and search.md. `pkg/app/web` needs an owning note (ARCH-6).

## 5. Decisions for Drew

These change observable behavior or scope, so the reviewers did not resolve them.

1. **Wikilink case and Unicode folding (FND-1).** Obsidian resolves links case-insensitively.
   - **Recommendation:** match it, folding both case and NFC.
   - **Impact:** the graph changes for existing vaults and the indexer version must be bumped.
2. **Frozen-story edges (ONT-2).** Either express "plain-ID references in a heading" as a schema directive, or drop the feature.
   - **Recommendation:** a directive, so core sync carries no starter vocabulary.
3. **`rzm code index` / `code index-anchors` (IDX-3).**
   - **Recommendation:** retire them, or make them thin fronts over the unified pipeline. Today they are a second writer.
4. **Which journal engine survives (EDIT-5, VAL-3).**
   - **Recommendation:** extract validate's engine, since browser saves already use it, into a neutral package, and retire the ontology engine.
5. **Stuck-journal policy (EDIT-6).**
   - **Recommendation:** quarantine the journal and warn, as validate already does, plus a documented `rzm` command to clear it.
6. **Search lane concurrency (SRCH-2).** This changes latency and ranking inputs.
   - **Recommendation:** go ahead, gated by a before-and-after search-quality and performance comparison.
7. **Prose-pinning tests (TEST-4).**
   - **Recommendation:** accept that template wording is guarded by `rzm init --check` and validation rather than by unit tests.
