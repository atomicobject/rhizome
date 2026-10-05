---
summary: "B11 investigation of full browser note and graph-node opening latency."
last-verified: 2026-09-05
---

# Full note and node opening latency

User report: opening notes or graph nodes in the Rhizome repository web app took roughly ten seconds. The objective is effectively immediate useful content. Controlled reproduction found two backend costs and unnecessary browser graph reconstruction. The original ten-second report was not reproduced exactly; the warm delays below were measured on disposable copies.

## Reproduction context

The likely real vault is the maintainer's Rhizome checkout, confirmed by the maintainer. On inspection no Rhizome process/listener or tab was available to attach to. Main is `af100a90`; its 625 MB index is Intel v59, with 489 notes, 1,670 files, 3,819 catalog nodes and 1,126 ontology edges. Read-only immutable SQLite inspection preserved the database. These counts and version do not by themselves establish the cause of the reported delay.

The preserved baseline CLI binary is `/tmp/rhizome-b11-fixtures/evidence/baseline-cli-preserved-rzm`, built from `af100a9059fe1e34a88a59d70af26cf75e5e1368`, SHA-256 `a0f950bc5a5874317de92ec3704ed816c250feb124c09827d9d282a8ae2d5316`. Reproduction must use disposable copies for migration, reindexing or other startup writes. The final matched comparison uses integration `df56c3b9`, including B01–B10, as its base.

## Critical path and hypotheses

- `web/src/api/graphql/operations.ts` requests full `PublicNodeDetail`: locator, bodies, fields, source links, assessment, structure, relation groups and local graph. The pane waits for the complete response and adaptation before showing its content. Opening a new pane is uncached; refocusing an existing pane is reused.
- `workspace.sourceLinks` with at least one authored link calls `notePathCacheForSections`, which calls `loaders.snapshot`. The web route supplies a raw `obsidian.Note`, so this reads and parses every current Markdown note before building a path/alias cache. The snapshot cache lasts one query execution. This remains beyond B04's bounded root lookup fix; test it first by removing only the sourceLinks selection from an otherwise identical request.
- Root projection currently loads broad host metadata/types/assessments. B04 addresses that work. Workspace body traversal projects descendants; B05's catalog resolver change must not be assumed to optimize this different traversal without measurement.
- Detail includes a 50-node/100-edge local graph; after detail resolves, a separate 500-node/1,000-edge `PublicLocalGraph` query populates the sidebar. Removing the detail graph is not yet proven safe: graph-only nodes also supplement parent/sibling context and adapter fallback when canonical bodies are missing.
- The note route also starts summary, global graph, views and validation requests. A type query follows the summary. These can overlap opening a URL-selected note and must be included in first-page queueing measurements.
- The adapter repeatedly copies growing child arrays; local graph layout runs synchronous ForceAtlas2 iterations. Neither is yet a measured explanation for the delay.
- The readiness gate wraps the GraphQL handler before existing `RZM_TRACE_GRAPHQL` timing starts. Trace totals alone omit startup gate waiting. The full detail POST has no client retry delay; the ten-second retry cap belongs to GET/HEAD handling of index-initializing responses.

## Measurement plan

Capture actual browser click-to-content and operation resource timing, including first launch, first note, repeated close/reopen and embedded-node navigation. Record readiness state and startup/index logs. Replay the exact frontend operation and compare full selection with sourceLinks, bodies, relationGroups and localGraph independently omitted. Use ordinary and descendant-heavy linked notes. Pair baseline with the combined integration, preserve payload/result parity, and profile the residual slow path before proposing a fix.

Existing GraphQL tracing separates context, preparation, execution and JSON writing. Add outer readiness and workspace-domain attribution in a disposable research harness if required; no production pprof listener or new cache is assumed. Timed work waits for the coordinator's shared-host measurement slot.

## Baseline API measurements

On the preserved repository copy after startup, three serial samples per variant gave these medians (milliseconds). Every retained result had a non-null node and no GraphQL errors. These are complete HTTP response times for the actual frontend operation; they do not include browser rendering.

| Note | Full detail | Without source links | Without bodies | Without relation groups | Without local graph |
| --- | ---: | ---: | ---: | ---: | ---: |
| Linkable embedded node identifiers | 1851.6 | 903.7 | 1837.2 | 1275.4 | 1856.9 |
| Pizza party 2026 | 1576.6 | 584.5 | 1542.0 | 1267.0 | 1550.4 |

The source-links selection adds roughly one second in this baseline. Relations also contribute, while body and local-graph omission have little effect in these requests. Selection costs need not be additive because loaders share work within a request. These observations justify comparing the same operation after B04/B05 integration before selecting the residual B11 fix.

Initial requests during startup returned null and were discarded. Bare `README.md` is ambiguous in `node(ref:)` because many files share that basename; use `./README.md` for the explicit root path. The original baseline binary was built without web assets, so its 404 browser probe is not a latency measurement. Its API samples and original binary remain preserved. A separate full frontend build of the same source revision will supply browser evidence.

Raw query variants, response payloads and samples are retained under `/tmp/rhizome-b11-fixtures/evidence/`, including `queries.json`, `baseline-warm-results.json` and per-note payload JSON. The original CLI binary is `baseline-cli-preserved-rzm`, with the SHA-256 above. All index writes occurred in disposable copies. Embeddings were initially disabled, but a later explicit semantic-index command overrode that setting; the historical provider audit below supersedes the earlier assumption that no external calls occurred. The retained timings measure warm reads, not embedding performance.

## Combined read comparison

Integration `2242706a` includes B04/B05 and the integrated write improvements. Full UI build SHA-256: `af6d33ab962a68a9fe5ff10683e379475d639f1f3970b21536d1ca5258254a0e`. After metadata readiness converged, three valid samples per variant gave full/no-source-links medians of 1948.0/929.4 ms for the linked specification and 1606.9/597.0 ms for pizza party. The residual source-links cost remains about one second. Raw data: `combined-stable-results.json`.

Actual headless Chromium navigation to specification content took 3175 ms on the combined build; clicking its related Usage-driven block-id policy note took 3653 ms. Baseline UI samples were roughly 2.8–3.2 seconds for navigation and 2914 ms for that click. These single click samples establish the visible delay, not a statistically significant frontend regression. Browser screenshots confirm rendered pane contents. CUA could not connect, so repository Playwright supplied the browser evidence.

## Startup readiness finding

Both legacy-copy startups returned null nodes while indexing. On the combined build an explicit semantic index restored a ready metadata row and valid details; background indexing then temporarily removed it again. Status still reported ready while `note_metadata_state` was empty. Null responses were excluded from warm latency samples, and their logs are retained separately.

Source attribution: ownership transitions invalidate the global metadata state before code ingestion (`ApplyOwnershipTransitions`). Unified indexing prepares its replacement delta before code ingestion but publishes and flushes it afterward. Normal completion restores readiness before ontology synchronization; cancellation or failure before publication can leave it unready. The server opens its sticky index gate from existing ontology rows, so that gate does not track this invalidation interval. Identical ownership observations are filtered: a clean restart does not necessarily invalidate metadata. The precise changed path triggering this fixture's transition has not yet been isolated.

This is a separate correctness finding, not proof that it caused the user's reported ten-second delay. The original index remains untouched.

## Source-links change and residual profile

The approved inventory change avoids reading/parsing unrelated note bodies. It retains current Markdown metadata eligibility, the live reader inventory, persisted alias authority, canonical filename/alias resolution and request-local lifetime. Persisted aliases are intentionally filtered to admitted paths: an alias cannot re-admit an excluded, stale, deleted or non-Markdown target. A resolved link means an eligible listed target; it does not promise a later unrelated target-body read or parse will succeed. Target opening retains ownership of those errors.

The regression exercises full sourceLinks selection, checks no unrelated content reads, refreshes paths/aliases between executions, and excludes ineligible targets. The existing authored-order/resolution test also passes. Independent read-lane review found no actionable defect under this documented authority. `Execute` already rejects missing store/reader dependencies before constructing loaders.

A standalone full-query CPU profile after this change found directory enumeration dominating: 85.7% cumulative CPU under `Note.GetNotesList → DiscoverFiles → discoverByGlobs`, with 30 listings across five executions. `NodeLinkService.LinkTargets` accounts for much of this repeated enumeration. A disposable per-request listing experiment reduced roughly 1.7 seconds to 294–308 ms; it is evidence, not a new production cache.

The server already constructs a watcher-backed `NoteAdapter`, but GraphQL's default service/dependencies bypassed it with a raw reader. B11 routes those defaults through the existing server reader, preserving supplied readers and raw fallback when no cache exists. Freshness follows the existing watcher/refresh contract; it does not claim to observe an undelivered filesystem event immediately. No new persistent cache is introduced. A later correction keeps that contract while removing the crawl coupling: a stale-triggered recrawl reconciles the live cache index in place, so a reader observes every delivered watcher event and never waits on the crawl; only the first crawl at process start gates reads (`pkg/vault/cache/CONTEXT.md`).


The first web wiring pass still took about 1.4 seconds in real `serve`: server construction captured a nil `LiveRuntime.Cache()` before asynchronous search initialization and retained its raw fallback. Matched complete response payloads for four representative notes remained identical. The approved lifetime correction exposes the currently published note reader in the existing nonblocking runtime snapshot and resolves it for each web call. Snapshot lookup creates no cache and performs no initialization or refresh; normal reader operations retain the existing freshness contract. A late-publication regression covers this startup ordering.


## Matched backend result

The late runtime-reader correction reduced complete HTTP response medians as follows. Before has three samples per note; after has five, on the same disposable v62 index and integration base `df56c3b9`; the baseline overlap caveat below limits which samples represent quiet reads.

| Note | Before (ms) | After (ms) |
| --- | ---: | ---: |
| Root README (baseline startup overlap) | 1653.96 | 20.45 |
| Indexing subsystem (first baseline sample overlaps) | 1702.76 | 9.79 |
| Linkable embedded node identifiers | 1802.46 | 32.86 |
| Pizza party 2026 | 1491.77 | 8.03 |

All four complete JSON payloads matched exactly, with non-null nodes and no GraphQL errors. Evidence: `final-before-results.json`, `final-live-results.json` and their full payload files. Backend-only build SHA-256: `2eee494b04062eec2ed25d19f4e125c39c17757312e177b5bbf264551afa6e34`.

## Browser renderer lifetime

Fast responses did not remove the entire interaction delay. Native in-page pointer-to-requested-title observation took 984–1060 ms, with the following animation frame at 996–1072 ms, despite a roughly 23 ms detail response. Playwright wall time added about another second, so it is excluded from these native measurements. An animation-frame callback is a rendering opportunity, not proof of pixels reaching the display.

Instrumenting WebGL canvas creation showed four additional global renderer constructions (12 contexts) during one related-note click, while global graph data stayed unchanged. A separate new local renderer (three contexts) was expected when changing the focused note. A CPU profile showed repeated native context creation and destruction with little JavaScript CPU work.

The construction effect in `useSigmaGraph` depends on `onNodeClick`; global and local graph callers supply changing callbacks on pane updates. The approved correction keeps the latest callback in a ref updated after React commits and removes callback identity from renderer lifetime. Actual graph-data dependencies and cleanup remain. Regression coverage must retain the renderer for callback-only changes, invoke the latest handler, rebuild for new data and clean up on unmount. The lifecycle test fails against the original hook (two instances after a callback-only update) and passes with the fix. Independent source review found no actionable issue.

The startup metadata/readiness mismatch remains a separately owned design follow-up. B11 does not change indexing publication or gate semantics.


The corrected build creates zero additional global renderers during warm note and graph clicks. Software-rendered Chromium still measured roughly 433–476 ms to content. Native profiles attributed most remaining time to rendering, and WebGL diagnostics identified SwiftShader. Matched hardware-backed runs (`--use-angle=metal`, ANGLE Metal Renderer: Apple M4 Pro) measured the requested exact-summary DOM observation at 198.6 ms median before the renderer fix and 93.0 ms after (three samples each, same backend and fixture). Following animation-frame medians were 210.3 ms before and 107.2 ms after. An actual global graph-node click measured requested body-paragraph DOM availability at 113.1 ms, with its next frame at 127.2 ms and a 42.8 ms detail response. The global graph contained 1,393 nodes and 3,349 edges. Zero additional global renderer contexts were created during the fixed graph click.

The related-note condition observes an exact summary text match in the DOM, “Retire eager standalone block-id insertion, clean up historical noise, and pivot embedded nodes toward identifier-backed block IDs with validation/autofix link migration.” This field comes from the fetched workspace; it is not a navigation label. Screenshots verify the completed structured pane.


Final combined build SHA-256: `f989d7cbdb4631e3a593597f092ccdc2e55129a1b15b98eb210761041befc93d`. Raw hardware samples: `body-metal-before*.log`, `body-metal-after*.log`; probes: `browser-body-metal.cjs`, `graph-metal.cjs`. Software-rendering comparisons are retained separately and are not mixed with hardware medians. These are warm interactions after the initial global graph loads; cold startup and external embedding latency are not included. The rendering opportunity measured by `requestAnimationFrame` should not be described as a physical display timestamp.

A source audit also identified redundant inactive-search refreshes in covered graphs. Since the approved lifetime fix meets the warm hardware target, this remains an optimization candidate rather than an additional B11 change.


The timing observer matches summary text anywhere in the DOM rather than testing visibility within a specific pane. Final screenshots confirm visible fetched content, but the reported timing is DOM availability plus the next animation frame, not a timed pixel or visibility assertion. The independent review verified all six hardware samples and this limitation.

React Doctor 0.9.13 reports 61/100 on both the unchanged pre-B11 hook and the fixed scan. Its `effect-needs-cleanup` diagnostic points to the existing renderer effect despite that effect returning cleanup which unregisters camera/mouse handlers, cancels hover work and kills Sigma. The focused lifecycle regression verifies replacement/unmount cleanup; this is an unchanged static-analysis diagnostic, not a new score regression. No suppression or tool installation was added.

## Compatibility, rollback and verification

B11 changes no public GraphQL shape or persisted schema and performs no data migration. Its path-inventory eligibility and alias filtering boundary is documented above. Roll back this change as one unit, rebuild the frontend and Go binary, then restart the server so runtime reader wiring and embedded assets agree. This restores the former read costs; it is separate from downgrading an Intel v62 database, which B11 neither requires nor implements.

Verification on the final source: full `make check` passed (364 frontend tests plus Go race-enabled unit/integration checks), `make web-e2e` passed all ten tests, and `./scripts/rzm validate` reported zero issues/errors. Focused source-link and renderer-lifetime red/green logs, complete HTTP payload equality, independent runtime/frontend reviews and rendered browser evidence are retained with the measurements. React Doctor's unchanged diagnostic is classified above.

## Reproduction from a checkout

The retained scripts in `scripts/perf/node_open_http.py` and `scripts/perf/node_open_browser.cjs` avoid private fixture paths and read no user index. The HTTP probe extracts the current frontend operation directly from `web/src/api/graphql/operations.ts`. The browser probe retains the original exact-summary DOM condition and context counting. Its Metal flag targets macOS; verify the renderer when comparing another platform. Neither script measures startup or embeddings.

The matched full-backend comparison is `df56c3b9` → `7d9550de`; both use Intel v62 and can open the same disposable index. Build those revisions in separate checkouts with `make build` and preserve both binaries. Set `RZM_TASK_BEFORE_BIN` and `RZM_TASK_AFTER_BIN` to their absolute paths before starting. The 198.6 → 93.0 ms browser result is a different, renderer-only comparison: both binaries already contain the fast B11 backend. To reconstruct that isolation, use two disposable checkouts at `7d9550de`; in the before checkout only, restore `web/src/components/useSigmaGraph.ts` from `df56c3b9` before building. The resulting before binary is intentionally an experimental working-tree build, not the whole `df56c3b9` revision.

Do not open a candidate-created v62 index with original `af100a90` (v61): that downgrade is unsupported. For an original-main comparison, use separate fresh disposable indexes per revision, or run the original binary first and then let the candidate migrate a copy of its disposable index. Never alternate those versions against a candidate-upgraded database. This does not require or permit changing a user's index.

For the compatible v62 pair, create a fresh fixture from the candidate checkout:

```sh
RZM_TASK_SOURCE="$PWD"
RZM_TASK_BIN="$PWD/bin/darwin/rzm"
RZM_TASK_FIXTURE=$(mktemp -d /tmp/rzm-node-open.XXXXXX)
git archive af100a9059fe1e34a88a59d70af26cf75e5e1368 | tar -x -C "$RZM_TASK_FIXTURE"
python3 - "$RZM_TASK_FIXTURE/.rhizome/config.yml" <<'PY'
from pathlib import Path
import re
import sys
path = Path(sys.argv[1])
text = path.read_text()
for section in ("noteEmbeddings", "codeEmbeddings"):
    text = re.sub(rf"({section}:\n\s+enabled:) true", r"\1 false", text)
path.write_text(text)
PY
cd "$RZM_TASK_FIXTURE"
RZM_SKIP_REPO_DELEGATE=1 "$RZM_TASK_BIN" index
python3 - .rhizome/config.yml <<'PYCONFIG'
from pathlib import Path
import re
import sys
text = Path(sys.argv[1]).read_text()
for section in ("noteEmbeddings", "codeEmbeddings"):
    block = re.search(rf"(?m)^{section}:\n((?:[ \t]+.*\n)*)", text)
    if block and re.search(r"(?m)^\s+enabled:\s*true\s*$", block[1]):
        raise SystemExit(f"{section} unexpectedly enabled; do not start the server")
print("Embedding flags remain disabled (omitted false values are allowed).")
PYCONFIG
# Indexing has exited; leave this seed stopped. If you started a seed server,
# stop it before copying. Create BOTH copies before any measurements.
RZM_TASK_BEFORE_COPY=$(mktemp -d /tmp/rzm-node-before.XXXXXX)
RZM_TASK_AFTER_COPY=$(mktemp -d /tmp/rzm-node-after.XXXXXX)
cp -R "$RZM_TASK_FIXTURE/." "$RZM_TASK_BEFORE_COPY"
cp -R "$RZM_TASK_FIXTURE/." "$RZM_TASK_AFTER_COPY"
cd "$RZM_TASK_BEFORE_COPY"
RZM_SKIP_REPO_DELEGATE=1 "$RZM_TASK_BEFORE_BIN" serve --host 127.0.0.1 --port 4374 --open=false --leader-follower=false
```

Wait for indexing and metadata readiness to converge before retaining samples. A fresh index reproduces the workflow and dataset, not the historical migrated-v59 state or its exact timings. In another terminal, set `RZM_TASK_SOURCE` to the checkout and use an output directory outside it:

```sh
RZM_TASK_RESULTS=$(mktemp -d /tmp/rzm-node-open-results.XXXXXX)
python3 "$RZM_TASK_SOURCE/scripts/perf/node_open_http.py" \
  --port 4374 --output "$RZM_TASK_RESULTS/http" --samples 5 \
  --ref ./README.md --ref docs/reference/subsystems/indexing.md \
  --ref docs/specs/technical/linkable-embedded-node-identifiers.md \
  --ref docs/playground/pizza-party-2026.md
node "$RZM_TASK_SOURCE/scripts/perf/node_open_browser.cjs" \
  'http://127.0.0.1:4374/notes?note=docs%2Fspecs%2Ftechnical%2Flinkable-embedded-node-identifiers.md' \
  "$RZM_TASK_RESULTS/browser" \
  '2026-05-01-12-00-usage-driven-block-id-policy' \
  'Retire eager standalone block-id insertion, clean up historical noise, and pivot embedded nodes toward identifier-backed block IDs with validation/autofix link migration.'
```

Repeat browser runs three times with separate output prefixes. After measuring the before copy, stop that server, change to `$RZM_TASK_AFTER_COPY`, and start `$RZM_TASK_AFTER_BIN` with the same serve arguments. Wait for its startup to converge, then repeat with new output prefixes. Both copies were made before either measurement; never measure against or restart the seed. Never alternate binaries against the same mutable copy. Use separate original-created indexes or original-first migration copies for `af100a90`, as described above. Compare parsed full payloads, per-note medians, DOM/frame observations and global/local context deltas separately. For an actual graph click, open `/notes`, inspect the rendered graph screenshot, and click the desired node using its observed coordinates; the reported run observed the specification's rendered summary paragraph beginning “Rhizome should make ontology nodes addressable without polluting source markdown.” Do not reuse coordinates without inspecting that run's layout.


## Historical provider audit correction

The initial baseline/combined server logs report `embeddings not enabled`. However, `combined-explicit-index.log` records an explicit semantic pass (with no changed notes to embed), and the retained integration configuration later has `noteEmbeddings.enabled: true` with Voyage configured. Explicit `index --semantic` forces enablement; it is intentionally absent from the safe recipe above.

Read-only comparison with the preserved legacy database found no new note/code embedding-cache entries, but all 54 existing intent exemplar vectors changed and received current-run timestamps in the integration copy. The semantic intent synchronization path submits missing exemplars to the provider; the shared node invokes `EmbedTexts`. This evidence indicates provider-backed intent embedding work occurred despite the initial disabled flag. No outbound HTTP trace was retained, so the precise request count and external-call extent cannot be established. The earlier blanket statement that external calls were avoided is withdrawn. The measurement server was stopped after this audit; the revised recipe keeps providers disabled.

Retained audit: `embedding-state-audit.json`, initial server logs, `combined-explicit-index.log`, and the disposable configuration. Warm read timings and full payload equality remain the measured results; they are not evidence of provider isolation or embedding latency.


## Warm-sample audit qualification

`final-before-server.log` interleaves all three README requests with startup indexing. `Index complete` appears during the first indexing-note request, so its other two samples and all three linked-spec/pizza samples follow completion. Thus the README and indexing medians above remain historical observations with startup overlap, not controlled quiet-read comparisons. The linked-spec 1802.46 → 32.86 ms and pizza 1491.77 → 8.03 ms comparisons retain quiet baseline evidence. All full payloads were valid and equal, which does not itself prove an idle server.

`final-live-server.log` contains readiness followed by query traces without indexing stages. The hardware renderer-pair server logs likewise contain no indexing stages. This supports converged read timing for those runs, not a guarantee of absent network traffic: no outbound trace was collected. No historical samples were silently relabeled or remeasured during this audit.


## Final plain-index browser smoke

After B12, the retained HTTP and browser helpers were executed against full-UI binary `35394d21` (SHA256 `81ac096f1460e75a8ea0cfbf8f4e05dc6ce6cf3e54dfe7d317927aa8ec419085`) on a fresh disposable copy of the original repository sources. Plain `rzm index` and both helpers exited zero. All four HTTP responses were non-null and error-free. The browser fetched the requested pane, satisfied its exact-summary DOM condition, and a screenshot independently confirmed visible content. The fixture server was stopped afterward.

Note and code embeddings remained disabled; both persisted Intel and intent embedding tables had zero rows. Semantic readiness consequently remained blocked, while the note HTTP and browser paths worked. This verifies the retained recipe under disabled providers; it does not replace the historical provider audit or add a statistical before/after timing claim. The later four diagnostic-string changes at `9cd25887` leave these runtime and frontend paths unchanged.

Commands, fixture details, helper hashes, results and the screenshot are retained in `/tmp/rhizome-final-browser-smoke.vLdSA7/evidence/smoke-result.json` and `browser.png`. HTTP helper SHA256: `8ea32bf85039c27303d935a496f7baefca54470eac9abc946f2b4fca3338231d`; browser helper: `f1c5bd9471fc8c5cbe48773a201790ec0462a093382d7196a63f53fd60f47bbe`.
