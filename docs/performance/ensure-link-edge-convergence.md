---
summary: "B17 contract and evidence for durable edge convergence after explicit ensure-link apply."
reference-kind: analysis
last-verified: 2026-09-05
---

# Ensure-link edge convergence

## Contract before implementation

B17 repairs D13: adding a block ID through node resolution currently refreshes the catalog but leaves persisted structural edges on the provisional node identity. The reproduced source changes from a heading locator to a block locator; a canonical metadata publication and ontology sync repairs the edge. Catalog IDs remain hashes (`OntologyNodeID(ref)`); structural edge source IDs remain projection `ref.NodeID` locators.

Explicit apply is complete only after source metadata and ontology rows converge through their existing writer authority. Never/plan stay read-only. Source-only `NodeLinkService` remains available to validation, whose repair engine already owns mutation and post-apply refresh.

The app indexing layer will own one bounded apply coordinator. It validates the explicit metadata indexer, canonical vault and paths, and expected schema; acquires the canonical index lock; opens and retains the live writer before source mutation; applies block IDs; then reuses exact-path queued metadata publication and `SyncPublishedPaths` with their existing flush barriers. It will not use the validation projection's read-only `BeforeMutation` hook for an edit. No SQL or edge replacement is added to noderead.

`noderead.Service` receives an explicit whole-apply capability. Missing authority fails before mutation. Every explicit apply with resolved embedded targets runs convergence, even when a previous attempt already inserted the block and no fix remains. Apply success and source-committed/index-incomplete errors are distinct. The same Scope clears all source-derived caches after an attempt; shared cache invalidation covers dependent data. Successful results resolve again with ensure=never.

Local graph file-context and read-write MCP file_context/node_link compose the coordinator using live source and store authority. B13 confirms these consumers remain live; its retained HTTP read bundles cannot receive this capability. This scoped repair never publishes a new B13 generation or claims semantic/graph-score convergence.

Independent formative review supports this ownership boundary. Validation must cover warm and fresh scope edges, source idempotence, dependent-note convergence, missing authority and canceled preflight without mutation, truthful postcommit failure and retry, and both app entry points. Full engineering gates and an independent final review precede acceptance. No performance improvement is claimed for this correctness repair.

## Evidence

The desired-contract regression fails with the old heading-based edge after source and catalog update. A diagnostic canonical metadata sync followed by ontology sync makes that same assertion pass. The implemented coordinator now converges through that same authority.

## Capability composition and migration

`noderead.Service.ApplyLinkTargets` owns a whole apply operation. `actions.FileContextTextParams.ApplyLinkTargets` and `mcp.Config.ApplyLinkTargets` accept a function with signature `func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error)`; the string is the read scope's expected schema hash. The command layer composes the actual indexing implementation because importing indexing from CLI/MCP would create a dependency cycle through indexing's bootstrap dependencies.

Local graph file-context binds `cmd.liveNodeLinkApplier`. A writable MCP embedder must bind the capability alongside its explicit metadata indexer and canonical live vault/store:

```go
cfg.ApplyLinkTargets = func(ctx context.Context, schemaHash string, req ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
    return indexing.ApplyNodeLinkTargets(ctx, indexing.NodeLinkApplyRequest{
        VaultDef: cfg.VaultDef, NoteMetadata: cfg.NoteMetadata,
        NoteReader: &obsidian.Note{}, SchemaHash: schemaHash, LinkTarget: req,
    })
}
```

The embedder owns consistency between this vault and the live store supplied to file-context. Do not bind this capability to a retained published read bundle or preview. `ReadWrite: true` alone no longer authorizes source-only apply. Missing capability returns an error before inserting a block ID. Note-root and unsupported section targets do not enter the embedded mutation coordinator.

`LinkTargetResult.Applied` records an actual source commit; it remains true if the subsequent target reread or publication fails. A publication error says `source applied; index convergence incomplete`. CLI and MCP file-context propagate that error rather than rendering a successful context packet. Retry with the same explicit Apply repairs the exact paths without inserting another block ID, even if source metadata was already published. A failure does not roll back the authored block ID. Reverting the code also does not remove committed IDs or reverse index changes; canonical indexing can reconcile retained source.

## Verification status

Focused scope regressions, indexing coordinator regressions, CLI/MCP caller tests, and the real graph file-context command pass. They cover the corrected source-locator edge identity, warm/fresh scope reads, dependent-note revisit, idempotence, prewrite failures, and publication-error recovery. The complete production source at `30d93d4e`, including the cache-generation correction, passed full `make check` (race unit and integration tests, lint/vet, and web checks), full `make build`, and default documentation validation (433 identifiers, zero issues/errors). The generation regressions failed before the correction and passed with race detection afterward. Retained local evidence is `/tmp/b17-cache-full-check.txt`, `/tmp/b17-cache-build.txt`, `/tmp/b17-cache-docs.txt`, and `/tmp/b17-cache-generation-green.txt`. Independent review accepted the corrected source with no remaining findings, and Greptile reviewed that exact head at 5/5. Hosted CI acceptance remains pending while an intermittent full-index fixture failure is investigated; passing local checks are not being treated as hosted acceptance.

## Review correction: in-flight cache publication

Greptile identified a concurrency gap in the initial reset: Graph, GraphFacts, and note-path inventory builders release the scope mutex while loading, then could republish old results after apply invalidated the maps. A deterministic channel-barrier regression failed for all three builders. Each now captures the scope cache generation at its miss and publishes only if that generation still matches. Invalidation increments it under the mutex. Other cache builders already hold the mutex through publication. An overlapping read may finish from its original inputs; later reads rebuild instead of reusing its stale entry.

## CI correction: ontology body publication ordering

Linux CI exposed an existing full-index fixture race before any ensure-link behavior ran. An initial body worker could reuse a matching chunk, wait for its embedding provider, and then publish embedding state after the final catalog refresh deleted that chunk. A deterministic real-store/queued-writer reproduction failed with `apply ontology node embedding states: FOREIGN KEY constraint failed`. Twenty isolated race runs and five full web-package race runs had passed, so retries alone did not establish correctness.

Full indexing now finishes initial streaming producers and flushes their queued writes before starting the final ontology catalog refresh. This removes overlap between initial streaming and the final catalog/body pass; it preserves the earlier provider/writeback concurrency and adds no indexing pass. No timing improvement is claimed. The explicit barrier prevents catalog replacement from removing a chunk while an earlier body worker can still publish state for it.

The barrier regression uses the real streaming coordinator, ontology body syncer, queued writer, and SQLite store. It verifies initial state durability before catalog removal, complete final chunks/embeddings/state after the current catalog is installed, one provider call in each phase, and bounded failure/cancellation propagation. Full `make check` passed on the combined source after integration merge `4a49fce8` plus this correction; raw evidence: `/tmp/b17-publication-full-check.txt`. Focused red/green evidence: `/tmp/b17-ontology-publication-order-{red,green}.txt`.

### Matched barrier timing

Five alternating before/after pairs indexed fresh copies of the Python integration fixture with 250 additional typed Decision notes, using enabled deterministic 256-dimensional note and code embedding providers. Both binaries used the same build options (`NO_WEB=1`); the baseline was `4a49fce8`, and the current binary added only the publication barrier to that production source. Each sample used `rzm index --rebuild`; setup and database inspection were outside wall timing. The run held the exclusive heavy-work slot.

| Measure | Before barrier | With barrier |
| --- | ---: | ---: |
| Median wall time | 622.496 ms | 655.951 ms |
| Catalog nodes | 296 | 296 |
| Chunks / ontology chunks | 342 / 294 | 342 / 294 |
| Embeddings / ontology embedding states | 342 / 294 | 342 / 294 |
| Foreign-key violations | 0 | 0 |

The median wall increase was 33.455 ms (+5.37%); the median paired increase was 37.369 ms (+6.01%). The first current sample had a 553.687 ms paired increase and remains in the evidence; no samples were excluded. All ten commands succeeded with exact count parity. Evidence, binary hashes, source-diff hash, and individual logs are retained in `/tmp/b17-publication-timing-20260905-paired/`. This quantifies the correctness tradeoff for this fixture with local deterministic providers. It does not establish network-provider latency or preserve prior B12 whole-index timing claims.

Retained timing provenance:

- Baseline source: `4a49fce8ad27a37eb482e19e90fa4d4a2dd1169e`; binary SHA-256: `19e84e3b3853e3a50013296975a6cdbcaa4587c152ac4c050ba91efc51ea198b`.
- Current source: that same commit plus `unified.go` diff SHA-256 `322deaa591f985b8f09fee715d7279b469d2fcba8e772a74c97ac578bf09d9fd`; measured binary SHA-256: `11fb7264102fe5c5c203e7f593e125e08306d9e5e5ec5ba5681e695d0eeff109`.
- Fixture: `testdata/integration/python-app/vault` from that source, plus 250 Decision notes generated by runner SHA-256 `5cff40d54d26cd7a6857ab557f1923be3ec071478856d0f95ba99bf5fc90bc0c`.
- Ordered pairs, before/after milliseconds: `624.311/1177.997`, `626.089/648.370`, `622.496/653.755`, `616.285/655.951`, `621.661/659.030`.

The final full build and documentation validation also passed (`/tmp/b17-publication-final-build.txt`, `/tmp/b17-publication-docs.txt`). Independent review accepted the barrier and confirmed final ontology completion precedes sync/freshness publication and indexing completion. Hosted acceptance is tracked in PR211 and remains required for integration merge.
