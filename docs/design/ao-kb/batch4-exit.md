# SPEC-0133 Batch 4 delivery

Lane L4, branch `t3/ao-kb-model-bindings`, built on the coordinator branch `t3/evaluate-ao-prompt-toolkit` through `5e7a47e95` (Lanes P, Y3 and R merged). Materialization version is **24**: Lane Y3 took 23, and 24 adds this lane's reference, matcher, membership and admission contracts. All commits are local; nothing was pushed, merged to main, or installed.

Batch 4 makes one selected AO product context and its evidence usable in Rhizome after the starter's install, refresh and index sequence. Implementation ran as three workers in separate worktrees (W1 core contracts and W2 compiler/setup on GPT-6.1 Sol; W3 views on Opus 5.5), with an independent Sol review of the integrated diff. The lead merged, reviewed, fixed the review findings, and ran the gates.

## Shared core contracts

Each is a narrow `shared:` commit with its deficiency named.

| Contract | Deficiency it closes | Commit |
| --- | --- | --- |
| `@references(..., targetKey: "aoId")` | RECORD references reached Markdown only by exact note path (`record_reference_index.go`), so AO `supported_by: [a-…]` could not reach assertions without making `aoId` an alias-backed identifier. Exact projected key, ambiguity yields no edge. | `1c2f02558` |
| `has:<property>`, quoted literals (`products:"*"`) | Matchers could not express AO `note_in_product`: absent/null/empty counts as all, `[]` names none, `*` is literal. | `db60c4a87`, `de2b86064` |
| Membership `wildcard: "*"` | Account rows tagged `products: ["*"]` failed with `ao_membership_wildcard_unsupported`. | `a14bd87ee`, `f3aa1b572` |
| `@admit(matches:)`, including from `extend type` | Static knowledge types must exist before refresh (views validate against them), but only the compiler knows the selected slug. A generated extension narrows the static types. | `eaee92631` |

## Requirement evidence

| Batch 4 exit item | Result | Evidence |
| --- | --- | --- |
| Setup: detect root, version, product; ownership before refresh; refresh; index; separate readiness | Done | Preflight in `bindings/knowledge.mjs` (root, `schemaVersion` 8, toolkit pin, registry, markers, explicit product). Refresh refuses unowned knowledge roots or model sources before publishing. `check` reports `starterInstalled`, `bindingsReady` (knowledge and model separately), `indexReady`; installation alone reports incomplete with the next command. Lead lifecycle below. |
| Model records connected to the R7 journey | Done | `views/knowledge/model.tsx`, assertion "Cited by", Attention per-file sync observations, Trace `structure` hops keyed by native identity, recipe `ao-kb-model-to-evidence`. E2E `web/tests/e2e-ao-kb/model.spec.ts` walks Attention/Assertions → assertion → recorded passage → model record → Back, plus direct links, Back/Forward, identity agreement, and two rows of one YAML file. |
| Model fields targeting knowledge resolve natively | Done for `kb:` targets | `supported_by`/`rests_on` compile to `targetKey: "aoId"` references with AO's `evidenced-by` direction. Markdown logical-ID fields (`extractedAssertionIds`, `provenance.source`) still have no native path and keep the bounded exact-`aoId` resolver. `aoId` stays off `@identifier`. |
| Product-aware knowledge | Done | Generated `ao-knowledge.generated.graphql` adds `@admit` to Assertion, Source, Concept, Artifact. AO keeps product knowledge at account `<type>/<slug>/` and `artifacts/<phase>/<slug>/`, not under `products/<slug>/` (`build-kb-graph.js:332`). |
| Fixtures: project override, selected product with shared account evidence, unselected product with colliding IDs | Done | `testdata/ao-kb/booking` (standard-kind model, override) and `testdata/ao-kb-product/account` (alpha/beta collisions, `*`/`[alpha]`/absent/`[]` tags, wildcard and untagged account rows, missing and ambiguous evidence). |
| Compare against AO's own reader | Done | Checked intermediates captured from pinned AO `e9dce951` with scratch-only Python (rows, structural edges, unresolved edges, AO-selected knowledge, and `supported_by` resolved against AO-selected assertion IDs, since AO `edges()` omits claim targets). Offline Go parity: booking 10 model records / 42 knowledge nodes / 11 evidence occurrences; alpha 3 / 10 / 10; original 15-record harness. Knowledge parity unchanged (42 nodes, 112 of 125 edges). |
| Lifecycle in disposable repositories | Done | Go/Node tests for init, re-init, upgrade, edited-file preservation, ejection, missing `uv`/`rzm`, stale configuration, failed refresh retention, product switch failure. Real runs below. |
| `init --check` and starter check pass after setup | Done | Both pass on booking and alpha after real refresh and index. |

## Lead lifecycle with real refresh

Scratch `uv` 0.13.7 in a throwaway venv, pinned toolkit `--toolkit /Users/colthorp/Developer/atomic-ai-prompt-toolkit/packages/kb-site`, disposable copies of both fixtures:

1. `rzm init --addons ao-kb`, `init --check` (pass), check reports incomplete setup with the refresh command.
2. Add knowledge includes; `refresh-bindings --single` (booking) or `--product alpha`; check's `note includes` row names the compiled model sources to include; append exactly those.
3. Index wrapper: `indexReady: true` in one pass, although `rzm index` re-serializes `.rhizome/config.yml`.
4. Edit the project override: check reports stale; refresh and index recover. Edit an assertion headline: bindings stay ready, only the index receipt expires; index recovers.
5. Alpha typed knowledge equals AO's `note_in_product` set (folder wins over tags; `*`, `[alpha]`, untagged admitted; `[beta]` and `[]` excluded). `a-collision` resolves to `assertions/alpha/collision.md`; `a-missing` and `a-ambiguous` produce `record_reference_unresolved` and `record_reference_ambiguous` with no edge.

Two lead fixes came from this run: config bytes left the binding fingerprint (`b9300a6c6`; first-run setup previously needed a second refresh and index), and the CLI tolerates non-blocking stdin from agent shells (`52060b142`).

## Independent review

GPT-6.1 Sol (high) reviewed the diff against the coordinator branch. Findings and dispositions:

| Finding | Disposition |
| --- | --- |
| P1: a failed product switch left the new product's admission beside the previous product's model | Fixed `cad6c2db0`: a model-stage failure during a switch restores the previous admission, selection and manifest. Failing-first test. |
| P1: admission matchers read inline `products::` and compare case-insensitively; AO reads frontmatter exactly | Accepted as a documented limit; the check's `product tags` row names affected notes (`95da56563`). Closing it needs another core matcher contract. |
| P2: capped 200-edge evidence reads reported unread IDs as unresolved | Fixed `f19616539`: truncated reads mark IDs not checked and disclose incompleteness. |

## Known limits and open items

- **Platform (escalated, not fixed here):** INBOUND RECORD neighborhood endpoints come back without `fragment`/`typeName` and are dropped from `nodes`, from any focus; outbound endpoints were fixed by Lane R. `noderead/hydrate_source_node_ids.go` skips refs with an empty `TypeName`. Views derive `record:v1:<d>` from `record-v1-<d>` in one helper (`views/knowledge/lib/refs.ts`, marked for removal); positional records are shown honestly and only their file opens.
- `workspace.fields[].links` is empty for RECORD reference fields; views read reference declarations from `/api/v1/ontology/types/<type>` annotations.
- Prefix matchers cannot express `placed()`'s direct-child restriction; artifact phase folders are enumerated at refresh and a new phase makes admission stale (check names it).
- Model Source content shows the record's JSON-pointer address, not YAML line numbers; the API exposes no record line range.
- Every metamodel kind gets a concrete model type, including kinds whose files are absent, so Explorer's Product model group lists empty types.
- Starter views are not typechecked by `make check-fast`; the lead typechecked them separately (zero errors after `f19616539`).
- Before the first refresh in a multi-product account, static knowledge typing is unscoped; check reports incomplete setup.

## Screens

`docs/design/ao-kb/batch4-screens/`: model feature, flow and component records; assertion model section; Trace focused on an assertion and on a model record; Attention sync group. Each at 1280 and 728 wide, light and dark, inspected against `comp-v1.html` and the design notes. No horizontal page scroll; record titles ink, teal only for actions, red only for unresolved evidence.
