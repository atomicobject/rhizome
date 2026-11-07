# SPEC-0133 Batch 5 delivery

Lane S5, branch `t3/ao-kb-batch5-proof-docs`. It merges the coordinator branch through `03a4b3c71`, which includes Lane Z (`a61a22725`, materialization 25) and `origin/main`, plus worker T's test branch (`t3/test-ao-kb-batch5`, `6fb1cb2cc`) and worker P's screenshot branch (`t3/ao-kb-fixture-dogfood`, `4b37f9088`, which also brings the integration head `cb800ccd0`). Every proof below ran on that merged build. All commits are local; nothing was pushed, merged to `main`, installed, or deployed.

Batch 5 proves the installation promise on a real AO KB and on the product-model fixtures, finishes the starter documentation, and adds automated coverage for the parts the suite lacked. Runs used disposable copies only. Real-copy screens and logs stay outside the repository because they show internal knowledge; this report describes that run by counts and outcomes.

Logs: `$S5LOGS` is `/private/tmp/claude-501/-Users-colthorp--t3-worktrees-rhizome-t3-2d8573f2/302e492a-bb40-4df2-b04e-1c158aa13c14/scratchpad/s5-logs`; the final real-copy pass is under `$S5LOGS/final/`. Worker logs are in `/private/tmp/aokb-s5t-logs/` (T) and `/private/tmp/aokb-s5p-logs/` (P).

## Real knowledge-only AO KB

The input is an internal Atomic Object AO KB: schemaVersion 8, single product, 825 tracked files, 190 assertions, 48 concepts, 59 source notes (29 with AO ids), 119 artifacts, and no product model. It never enabled AO's site engine, so it has no `.kb/config/manifest.json`. It already carried an older Rhizome setup (a v0.48.0 pin) and its own hand-written Rhizome types for assertions, concepts, sources, and artifacts. Its own site is an Astro build into a tracked `www/`.

Each copy was a `git clone` of the original with the remote removed (no `.env`, `node_modules`, or local index copied). Before Rhizome ran, the copy's tracked `.rhizome/config.yml` was adjusted: it pinned `noteEmbeddings.indexpath` to an absolute path inside the original checkout, embeddings were turned off so no text left the machine, and the binary was marked externally managed because the pinned v0.48.0 release now returns 404. The original repository and the AO toolkit checkout have the same `HEAD` and `git status` before and after (`$S5LOGS/originals-before.txt`).

| Step | Outcome | Evidence |
| --- | --- | --- |
| AO baseline before init | `kb-site lint` exit 0 (18 headline warnings). `verify_kb.py` exit 1 on existing findings (144 broken wikilinks, 416 schema findings, 54 typed-link findings). `locators.py check` exit 0: 190 checked, 0 located. AO's index builder exit 0: 386 nodes, 1,071 edges, 84 unresolved declarations. The Astro site build exit 0, and a second build is byte-identical, so its output needs no volatile-field allowance. | `final/ao-before/`, `final/site-build-before.log`, `final/site-build-repeat.log` |
| Install | `init --add-addon ao-kb` and `init --check` pass. The check's `knowledge bindings` row is an error naming the four local types whose query roots collide with the starter's (a fix in this batch). The adopter retires them; `hub`, `reference`, and `system` stay and point at the starter types. | `final/init.log`, `final/check-1.json` |
| Bindings | `refresh-bindings --single` succeeds without a site-engine manifest (a fix in this batch, noted `ao_site_engine_absent`): knowledge admission plus 67 model types over 42 declared model files, none present. | `final/refresh-1.json` |
| Includes and index | The check advises a first include list starting with `**/*.md` (a fix in this batch). The index wrapper verifies the index. `rzm init` reruns because includes change the managed guidance block. Final state: starter installed, bindings ready, index ready; `init --check` exit 0. | `final/check-3.json`, `final/index-1.json`, `final/check-5.json`, `final/init-check-3.log` |
| Files after install, setup, and reads | No AO-authored file changed. Changes are Rhizome-managed: `.rhizome/*`, agent skills, the managed `AGENTS.md`/`CLAUDE.md` block, harness settings (`.claude/settings.json`, `.codex/config.toml`), the shipped guides, and the adopter's edit to the local ontology. Reads changed nothing outside `.rhizome/`. | `final/H1b-H2.diff.txt`, `final/H2-after-setup.sha256`, `final/H3-after-reads.sha256` |
| Typed records against AO's index | By AO id: assertions 190/190, concepts 48/48, sources 29/29 (Rhizome also types the 30 id-less source notes). Artifacts by path: 118/119; the other is a `type: reference` note that the retained local `reference` type claims. Citations: 177/180 assertion-to-source pairs; the 3 AO-only pairs cite artifacts, which the typed `sources` field excludes. | `final/typed-vs-ao-1.txt`, `final/artifacts-vs-ao-1.txt`, `final/evidence-1.txt` |
| Browse, query, search | Claim-to-source and reverse-impact recipes, lexical search, typed GraphQL, the Assertions and Attention views, and the assertion presentation all work. Attention's 16 missing-Sources rows match the 16 the repository's own ontology flagged. | `final/recipe-*.json`, `final/search-1.txt`, screens below |
| AO after setup | Lint and locators identical. The AO index is identical except `meta.generatedAt`. The verifier differs only in its import-budget warning (4 → 3), because init's managed block shrank `CLAUDE.md` from 15,070 to 12,734 characters. The site build is byte-identical. | `final/ao-after-setup/`, `final/site-build-after-setup.log` |
| AO-workflow edits | Edit a headline and body, add an assertion with a citation and a `supports` link, delete an assertion seven others support, and reorder an assertion's two citations, as AO's process and curate skills write them. The running runtime reflects every edit; bindings stay ready and the index receipt expires. | `final/after-edits-runtime.json`, `final/check-6-after-edits.json` |
| Metamodel override | Adding a project override that removes a kind makes model bindings stale. A malformed override then fails refresh: the previous model SDL stays (its hash matches the manifest, now marked stale) and the check shows the YAML parse error. Fixing it and refreshing gives 66 types over 41 files, removes that kind's views, and returns every check to ready. | `final/check-7-override-stale.json`, `final/refresh-failed.json`, `final/refresh-recover.json`, `final/check-9-recovered.json` |
| Convergence | After the edits, and again after restoring the deleted assertion, incremental results equal a fresh `rzm index --rebuild` for typed records, ordered citations, and every outbound relation. The restored assertion keeps its AO id. | `final/conv-*-*.canon.json` |
| AO after edits | The AO index differs from the baseline exactly by the edits: one added node, one added `supports` edge, the edited claim's title and excerpt, and locators checked 190 → 191. Lint is identical. The site build is byte-identical. Across the whole run the only AO-authored changes are the four edits and the added override. | `final/ao-after-edits/`, `final/H1b-H4.diff.txt` |

### Refusals on the real copy

The sweep ran with bindings stale, again after a failed refresh, and covered a typed assertion, a typed source, an assertion with malformed frontmatter, an untyped raw source under `sources/_raw/`, a concept whose frontmatter is not a mapping, link repair from an unowned note that 13 owned notes link to, and an edit staged on an unowned note before its path was declared owned. All 35 attempts left every file outside `.rhizome/` byte-identical (`final/refusals-*.json`).

| Surface | Result |
| --- | --- |
| HTTP edit sessions | 9/9 refused with "is read-only (AO KB)" and the declared reason: `setSource` and `setField` at staging, and the staged edit at commit ("revalidate edits against the current ontology"). |
| GraphQL | 6/6 report `canEdit: false` with the declared reason, including the malformed, untyped, and invalid-frontmatter files and both binding states. GraphQL has no mutations. |
| CLI | 16/16 refused. `note move`, `note rename`, `note rename-heading --apply`, and `note properties set` give the owner's reason; link repair names the owned file it would rewrite. `note properties set` on the two malformed-frontmatter files fails on YAML parsing before the ownership check. |
| MCP and agent surfaces | No path could write. `rzm mcp serve` `note_rename_heading` fails for every note with "note format runtime is required" (pre-existing on `main`); stdio `node_link` apply needs a live writer; the agent CLI and code mode keep these operations plan-only by design. Worker T's in-process test proves the MCP handlers refuse owned targets when a writer exists. |

`rzm note create` was not run against a real-path copy because it dispatches through an `obsidian://` URI; Lane B's operations test asserts no URI is built for an owned path.

### Real-copy screens

All inspected at full size, kept in `$S5LOGS/final/screens/`:

- An unowned ordinary note, before the add-on (plain copy) and after setup, at 1280 and 728 in light and dark. The note body region is byte-identical at 1280 in both appearances (`screens/crop/`); only shell chrome differs (vault name, the added Knowledge, People, and Product model groups, header counts).
- One assertion before the add-on (the repository's own type) and in the starter presentation, which shows the owner banner and no Edit button (Lane Z).
- The same assertion before and after the AO edit, and the added assertion, in all four variants.
- Assertions and Attention views in all four variants; the saved-assessment section showing "Unavailable" after this batch's fix.

## Product-model fixtures (worker P)

Worker P ran the same cycle on AO's two-product fixture, composed by the toolkit's own test helper with product alpha selected and the engine pinned with `builder: local`, and on `testdata/ao-kb-product/account` with alpha selected. Both runs used the merged build and a scratch clone of the toolkit at `e9dce951`. Evidence is in `/private/tmp/aokb-s5p-logs/`.

| | Two-product (alpha) | Account (alpha) |
| --- | --- | --- |
| Authored bytes through install, refresh, index, reads, and reverted edits | Unchanged; only AO's rebuilt `graph.json` and `kb-trace.json` differ | Unchanged |
| Starter installed / bindings ready / index ready | Yes / yes / **no**: the index completeness collision below | Yes / yes / yes |
| AO validation and site build, before and after (`kb-site validate`, `build`, `lint`, `metamodel`, `trace check`, verifier) | Identical exit codes, output, resolved metamodel, and 84 build files | Identical; 11 build files |

The comparison excludes only `meta.generatedAt` in `graph.json` and `kb-trace.json` (wall-clock build time), uv's "Installed N packages" line, and the `$ ` command echoes that name temporary paths. AO's own first run, before Rhizome was installed, rewrote kit stamps in each product manifest, the account fixture's alpha `metamodel.yaml` (see the bugs), and its build output. To get past two starter limits on the two-product copy, P rewrote its registry as a flow list (fixed since by `dea7e925b`) and indexed without the product `services.yaml` include; AO's outputs were identical after each change.

Account fixture convergence through the index wrapper:

| Step | Model records | Evidence edges | Check |
| --- | ---: | ---: | --- |
| Baseline | 3 | 8 | |
| Change a value | 3 | 8 | Same record identity |
| Add a row | 4 | 10 | |
| Reorder keyed rows | 4 | 10 | No identity changed |
| Delete a row | 3 | 8 | |
| Edit a cited assertion | 3 | 8 | New headline shown |
| Delete that assertion | 3 | 5 | |
| Restore it | 3 | 8 | |

A project-override change reports stale, refresh recovers, and the new field appears. A duplicate ID fails refresh, keeps the previous bindings, and withholds both rows rather than choosing one. A full rebuild matches the incremental records, field values, edges, and knowledge. On the two-product copy, a component value edit and its revert converge with stable identity.

Refusals: 129 attempts across four states (ready on the pre-merge and merged builds, stale bindings, failed refresh) left every byte identical. HTTP edit sessions refused Markdown edits with the AO KB reason. Record-targeted YAML edits were refused by request-shape checks ("Cannot find note in vault", `record_locator_unresolved`, "source edit changes a heading…") before ownership; fix `52d5902eb` now refuses every one of those shapes with the owner's reason. CLI move, rename, rename-heading, properties, tags, and delete refuse with the owner's reason, including a move whose backlink rewrite reaches an owned artifact. GraphQL rejects mutation operations. Repair-review apply offered only advisory actions (409), so it remains untested against owned files. Table: `refusal-table.md`.

Lane Z, re-checked on the merged build: inbound evidence endpoints carry `fragment` and `typeName`; `supported_by` publishes one link per value; owned notes show no Edit; the record breadcrumb is present; the sidebar shows one "Model records 117" entry under "Product model"; quoted matchers admit exactly AO's set. **Positional record with an edge:** it opens as its own record from a direct link and by following the inbound edge from its user, labeled as a revision-bound positional record (`testdata/ao-kb-model/account`, because neither product fixture has one; screen `pos-positional-job-opened-from-inbound-edge-1280-light.png`).

Screens in `docs/design/ao-kb/batch5-screens/` (synthetic fixtures; 1280 and 728, light and dark unless noted; P inspected every one against `comp-v1.html` and the design notes):

- Account feature record before and after a value edit, with its evidence: `acct-feature-record-{before,after}-edit-*`.
- Assertion listing the model records that cite it: `acct-assertion-cited-by-model-*`; Trace focused on a model record: `acct-trace-focus-feature-record-*`.
- Two-product component record: `two-component-record-*`; a feature whose AO file-name citations show as unresolved: `two-feature-record-slug-evidence-unresolved-*`.
- Positional job record: `pos-positional-job-record-*`, and opened from its inbound edge; sidebar: `two-sidebar-model-records-1280-light.png`.
- `acct-assertion-saved-assessment-error-*` shows the saved-assessment error before fix `88180b887`.

## Automated coverage added (worker T)

| Test | Covers |
| --- | --- |
| `TestAOProductExternalEditsConvergeWithFullIndex` (`pkg/ontology`) | Product fixture with compiled bindings and static knowledge SDL. Row value change, added row, keyed reorder, deleted row, assertion headline/body edit, cited assertion deleted and restored, an undeclared value, a project-override SDL variant, and restart. After each step, incremental equals full on sources, records, field values, edges, occurrences, assessments, and diagnostics; exact evidence edges; GraphQL traversal agrees; authored bytes unchanged after rich reads. |
| `TestAOPositionalRowsConvergeWithFullIndex` (`pkg/ontology`) | Positional rows added, reordered, and deleted with account-first numbering. |
| `TestMoveRefusesOwnedStructuredHostsWithOwnerReason` (`pkg/app/operations`) | Moving an owned YAML file, or YAML into an owned path, names the owner (fix `095147016`). |
| `TestMCPWriteToolsRefuseAOOwnedTargetsWithoutChangingBytes` (`pkg/app/mcp`) | `node_link` apply and heading rename on owned booking targets, which succeed once the ownership file is removed. |
| `TestAOKBOwnedSourcesRefuseCLIHTTPAndGraphQLWrites` (`tests/integration/aokb`) | The real binary and the starter's real ownership file, with compiled and failed bindings: typed, unselected-product, malformed, YAML record, and stale cases through CLI writes, link repair, HTTP staging and staged-then-owned commits, and GraphQL capabilities. |

The convergence harness fails if the incremental sync is skipped. All five pass on the merged branch (`$S5LOGS/merged-T-*.log`).

## Bugs found

Fixed in this batch, each with a test that failed first:

| Commit | Bug | Test |
| --- | --- | --- |
| `07c92cbcb` | A knowledge-only single-product KB has no site-engine manifest, so refresh failed with `ao_toolkit_pin` and setup could never report ready. | `a knowledge-only single-product KB without a site engine pin publishes knowledge admission`, `a selected product still requires its site engine pin` (`compiler.test.mjs`); `test_knowledge_only_single_kb_without_site_engine_reads_no_model` (live adapter, `$S5LOGS/adapter-knowledge-only-{red,green}.log`) |
| `07c92cbcb` | With no `notes.includes`, the check warned that knowledge paths were uncovered; following it literally would replace all-Markdown discovery with the AO paths alone. | `absent notes.includes is default all-Markdown discovery, and a first include list must keep it` (`check.test.mjs`) |
| `c4d65aa88` | A repository's own Rhizome types for AO notes broke the combined schema after a clean init; refresh failed with an unattributed CLI error and the local types claimed every note. | `local schema types whose query roots collide with the starter knowledge types are named` |
| `88180b887` | Without `.kb/index/kb-weights.json`, every assertion showed a red "data is undefined" error instead of "unavailable". | `missing and malformed snapshots stay unavailable` (`saved-analysis.test.mjs`) |
| `095147016` (T) | Moving an owned YAML file was refused with a format error that did not name the owner. | `TestMoveRefusesOwnedStructuredHostsWithOwnerReason` |
| `52d5902eb` | HTTP edit sessions reached record projection, source witnesses, and Markdown-only source validation before ownership, so owned YAML records were refused with planner errors instead of the owner's reason (P). Every operation entering a session, whether created, staged, or restored from a client snapshot, now passes the shared source-ownership policy on its host first. | `TestOwnedStructuredHostEditsAreRefusedWithOwnerReasonForEveryRequestShape` (P's shapes: record-fragment, `nodeId`, and record-pointer `setField`; witnessed and unwitnessed `setSource`; create-with-ops; commit from a client snapshot; five failed first) |
| `dea7e925b` | A product registry written as a block list that ends the frontmatter lost its last product, so refresh failed with `ao_product_unregistered` (P). | `a block-list product registry keeps its last product like AO does` |

Reported, not fixed. The first three block or break real product-model use:

- **Index completeness fails on paired inverse relations** (P). The standard metamodel's `service.owns` and `collection.owner` both declare `governs`. The inbound side swaps endpoints, so the row produced from `collections.yaml` has the same `ontology_edges` key (source path and node, relation, target, provenance) as the row from `services.yaml`, with different occurrences. `INSERT OR REPLACE` and the readiness witness (`pkg/ontology/catalog_completeness.go`, `ontologyOutputWitnesses`) each keep whichever row comes last, so readiness fails ("ontology readiness completeness validation failed": 3 of 3 times on the merged build, and intermittently before). `dedupeEdges` merges duplicates only within one host's projection. A fix belongs in the indexing subsystem: merge same-key occurrences deterministically across hosts, or have the compiler emit one edge for a paired inverse declaration. Evidence: `bug-readiness-witness-collision.txt`.
- **AO file-name evidence never links** (P). AO resolves `supported_by` values that name an assertion's file, which AO's own tooling writes (`apply.py:696`); the two-product fixture has 45 such evidence edges in AO's graph and none in Rhizome, because the binding matches `aoId` only. Evidence: `bug-slug-evidence-unresolved.txt`.
- **Typed connection lists fail whole** (real KB). 31 assertions' `supports` field returns a GraphQL error and no list: 28 have a concept target, 1 an artifact, and 2 point nowhere. AO accepts concept and artifact targets. A deleted assertion produces the same error on each supporter. The guide documents the limit.

Other findings:

- **Search misses YAML model records with embeddings off** (P): CLI, HTTP, and GraphQL search find none by title, id, field value, or type filter. R4 asks for configured search.
- **The starter is stricter or staler than AO in three places** (P): it rejects a missing `schemaVersion`, which AO accepts and AO's own two-product fixture omits; it reads a product's mirrored `metamodel.yaml`, so an account metamodel edit is invisible until an AO command mirrors it; and `testdata/ao-kb-product/account`'s alpha override sits in a file AO overwrites (its `product.yaml` lacks `owns: [metamodel.yaml]`), so AO's first command drops `feature.project_detail`. Correcting the fixture means regenerating its checked intermediates.
- **Check labels** (P): an offline uv dependency failure, and a duplicate-id failure with an empty message, both show only "stale"; an index failure reports bindings not ready.
- **One invalid UTF-8 note stops the whole vault** (pre-existing on `main` `bb746610c`): indexing and every typed query fail with "build root document snapshot: source is not valid UTF-8", without the path, and a running runtime retries every few seconds indefinitely (P saw the same loop after a failed index).
- **A copied vault directory attaches to the original's runtime** (P), so commands in the copy can reach the original. Copies here were clones without `.rhizome/` runtime files.
- **MCP:** `rzm mcp serve` `note_rename_heading` fails for every note with "note format runtime is required" (pre-existing on `main`), and `node_link` apply reports `exists: true` for missing files and creates nothing (P).
- **`node(ref: "README.md")` returns null** when another `README.md` exists deeper in the vault, so the reader cannot open the root README (pre-existing on `main`).
- **An upgrade leaves retired starter files** (P): `views/knowledge/lib/refs.ts`, removed by Lane Z, stays installed.
- **Layout:** a knowledge-only KB keeps an empty "Product model" sidebar group (the `AOKBModelRecord` interface member has 0 records); at 728 px the note toolbar cuts off its third view option, and the Assertions table is wider than the pane with its later columns cut at the edge (scrolling not verified).
- **Legacy configuration hazard:** an older init wrote an absolute `noteEmbeddings.indexpath` into the tracked config, pointing every clone at the original checkout's database. Current init no longer writes it, and re-init does not remove an existing one.

## Acceptance fixtures and checks

| Row | Batch 5 evidence |
| --- | --- |
| Existing Markdown sections, lists, checkboxes, root-only HTML | Real copy: the unowned ordinary note's body is byte-identical with and without the add-on at 1280 in both appearances. The shared suite in `make check` and the Batch 1 and Batch 3 goldens cover structure and edits. |
| Synthetic structural provider without edits | Batches 1 and 3 (`TestRecordQueriesReadNativeContentWithoutMutationSupport` and the foundation contract tests); unchanged here. |
| YAML selectors and source values | Batch 3 provider and oracle tests; P's product runs read real fixture YAML end to end. |
| Explicit, mapping-key, composite, positional IDs | `TestAOProductExternalEditsConvergeWithFullIndex` (keyed reorder stability), `TestAOPositionalRowsConvergeWithFullIndex`; P's positional checks; guide section "Positional records and links". |
| YAML/JSON/Markdown relationships | Batch 4 parity; T's convergence test checks exact evidence edges after each edit; P's runs. Real copy: citations 177/180 against AO, with the typed-connection gap reported. |
| Product plus account | P's account run reaches all three ready states and converges; the two-product run keeps AO identical but cannot reach index ready (completeness collision, reported). T's convergence test on the product fixture; Batch 4 collision fixtures. |
| Invalid/unsupported source | Real copy: a malformed override fails with its YAML error and retains the previous model; malformed and non-mapping frontmatter stay owned and readable. Pre-existing UTF-8 vault failure reported. |
| AO custom metamodel | Real copy: override stales bindings, a failed refresh keeps the previous SDL, refresh recovers (67 → 66 types). T's override SDL variant converges. P's project override. |
| Init lifecycle | Real copy: init, `init --check`, the starter check's three states, managed re-init upgrade, rerun after includes. Batch 4 lifecycle tests. Three starter fixes above. |
| External AO edits and build | Real copy: four edits plus delete and restore reflected and converged; AO lint, verifier, locators, and index rerun with only edit-caused differences; site build byte-identical before, after setup, and after edits. P's product runs with `kb-site` validate and build. |
| Mutation refusal | Real-copy sweep (35 attempts) and P's 129 attempts, all with bytes unchanged; `TestOwnedStructuredHostEditsAreRefusedWithOwnerReasonForEveryRequestShape`; `TestAOKBOwnedSourcesRefuseCLIHTTPAndGraphQLWrites`; `TestMCPWriteToolsRefuseAOOwnedTargetsWithoutChangingBytes`; `TestMoveRefusesOwnedStructuredHostsWithOwnerReason`; Lane B's admission tests. Repair-review apply against owned files remains untested. |
| Scale and cache behavior | Batches 1 and 3 operation-count tests; no performance claim. The real-copy index wrapper ran in about 13 s on 456 notes. |
| Knowledge/assertion journey | Real copy: claim-to-source and reverse-impact recipes and the assertion presentation; Batch 2 and 4 E2E journeys in the AO KB suite. |
| Knowledge conventions | Real copy: AO-id parity for assertions, concepts, and sources; ordered citations converge; the typed-connection gap is reported and documented. |
| Passage locators | The real KB records no passage locators (AO's own check: 0 of 190 located), so every citation reads as document-only. Line and section parity stays with the Batch 2 goldens. |
| Attention filters and saved judgments | Real copy: Attention shows 16 missing-Sources rows matching the repository's own check and labels saved analysis unavailable. Fix `88180b887` makes the assertion presentation say the same. |
| View edge cases | Real copy: missing saved analysis, dangling references after a delete, an empty model, and owned read-only handoffs. The empty Product model group and the 728 clipping are reported. |

## Documentation

- `pkg/app/cli/init/templates/starters/ao-kb/repo/docs/reference/guides/ao-kb.md`: supported versions (schemaVersion 8, toolkit `e9dce951`, knowledge-only KBs without a site engine), syntax limits (including the typed-connection gap), positional links, read-only behavior on every surface, which changes need a binding refresh, recovery, colliding local types, and the setup steps proven above.
- `pkg/app/cli/init/templates/starters/ao-kb/agents/skills/ao-kb-setup/SKILL.md`: stop and ask before retiring colliding local types, start a first include list with `**/*.md`, the knowledge-only note, and rerun init after includes change.
- `pkg/app/cli/init/templates/starters/ao-kb/module/bindings/README.md`: the knowledge-only pin rule and the corrected statement that includes do not stale bindings.
- `docs/reference/subsystems/cli.md`: the starter check and init constraints above.
- `docs/reference/subsystems/web.md`: edit-session admission applies source ownership before projection, witnesses, and Markdown-only validation.

## Gates

| Gate | Result | Log |
| --- | --- | --- |
| Starter Node tests: `check.test.mjs`, `compiler.test.mjs`, `saved-analysis.test.mjs` | 15, 25, and 2 pass, each new case red first | `$S5LOGS/check-includes-*.log`, `check-schema-collision-*.log`, `knowledge-only-*.log`, `registry-block-*.log`, `saved-analysis-*.log` |
| Live adapter tests (scratch uv, PyYAML 6.0.3, toolkit `e9dce951`) | 13 pass; the new case failed first against the previous adapter | `$S5LOGS/adapter-knowledge-only-{red,green}.log` |
| Worker T's tests, rerun on the merged branch (including the real-binary integration test) | Pass | `$S5LOGS/merged-T-*.log` |
| Ownership-first fix: the new web test, `pkg/app/web`, `pkg/sourceownership`, `pkg/app/operations`, ontology ownership tests, and T's real-binary integration test | Pass; the new test failed first on five shapes | `$S5LOGS/owned-shapes-{red,green}.log`, `owned-shapes-affected-packages.log`, `owned-shapes-ontology-ownership.log`, `owned-shapes-integration.log` |
| `make check-fast` | Pass | `$S5LOGS/check-fast-4.log` |
| `make check` | Pass: Go packages, content checks, lint and typechecks, 248 web files / 2,013 tests | `$S5LOGS/make-check-final-2.log` |
| `make build` | Pass | `$S5LOGS/make-build-final-2.log` |
| Real copy with the final binary | Managed re-init, `init --check` exit 0, refresh, index wrapper, all three states ready; 228 views validate | `$S5LOGS/final/init-check-6.log`, `final/check-final.json`, `final/view-validate.log` |
| `./scripts/rzm validate` | Exit 1 on the five broken links that predate this batch in `docs/reviews/2026-10-17-subsystem-review/`; none in changed files | `$S5LOGS/rzm-validate-3.log` |

`make check-full` and `make web-e2e` were not run in this lane: this batch changes starter scripts, documentation, and tests, not indexing or shared web code, and the coordinator's gate list for Batch 5 does not include them. The coordinator's closing gates should include both, because the merged branch carries Lane Z and `origin/main`.

Cleanup: every scratch copy, scratch toolkit clone, scratch uv and cache, and server was removed or stopped; logs remain. The originals are unchanged.
