---
type: ReferenceDoc
reference-kind: analysis
summary: "Intermediate review findings for shared search assessment, source identity, facet aggregation, and profile routing."
---

# Search application integration review

This is an intermediate review of the in-progress implementation for [[search-engine-quality]]. Findings describe the inspected working-tree checkpoint, not completed behavior. The implementation worker received these findings for correction and must attach subsequent resolution evidence before claiming acceptance.

## Reproduced assessment defects

Two temporary package-level tests in `pkg/app/unifiedsearch/application` failed with `go test ./pkg/app/unifiedsearch/application -run TestParentReview -count=1`. The temporary file was removed after sharing the reproduction with the worker.

- A strong ordinary note with `knowledge.NoteHandle("docs/test.md")`, no NodeRef, and no FQN produced result identity `handle\x00note:docs/test.md` but answer identity `path\x00docs/test.md`. This incorrectly fails the all-selected-sources-strong check. Both projections must preserve the same identity for every supported source kind, not only embedded nodes.
- A result with semantic and lexical raw scores both equal to 0.0001 was classified as strong. `Assess` checked only positive channel presence. Multiple weak observations do not establish strong relevance. Calibrate strength using independently judged development cases, including plausible no-answer cases and low-signal counterexamples; a threshold chosen solely to pass this reproduction is insufficient.

## Adapter and facet review

The inspected `mergeRankedResults` in `pkg/app/unifiedsearch/run.go` appends results in facet order, merges duplicates, and truncates the combined slice. Removing its previous additive score bonus does not by itself implement balanced multi-facet retrieval: a full first facet can exclude every unique result from later facets. Its `rankedKey` also collapses all note results by path, including distinct embedded nodes. Verify independent-facet coverage under query permutation and preserve canonical source identity through aggregation. Shared adapters need the same aggregation policy.

The inspected MCP path resolves `ProfileAgent` explicitly at both policy calls. The web handler calls that path through `SemanticQueryUnifiedWithOptions`; profile selection was not present in the inspected controls. Complete the interactive-profile route and verify actual web, MCP, and CLI output equivalence for equal effective requests and profiles. Shared helper usage alone does not prove transport parity.

## Resolution evidence required

### Continuation follow-up

The inspected `unifiedsearch.Execute` reruns retrieval before decoding a supplied continuation. Its `ContinuationCursor` stores request, generation, window digest, bound, and offset, but no query-vector snapshot. Neither `Execute` nor `Run` installs the query embedding memo used by the MCP continuation path. The worker previously measured changing query vectors from the live provider for repeated identical text. Without equivalent reuse, application pagination can reject an unchanged request as stale even when the index is unchanged. Verify continuation across separate CLI invocations with a deliberately varying query embedder, retaining the first page's query vectors. Reject malformed cursors before unnecessary provider work. Shared transport policy must cover this behavior as well as ranking.

Provide permanent behavior regressions for ordinary and embedded identities, judged confidence results including absent topics, multi-facet source coverage and identity retention, and actual adapter tests for both profiles. Run the relevant existing tests after integration. Keep confidence, transport parity, and corpus acceptance open until this evidence is available.

## Verified partial resolution

An independent `go test ./pkg/app/unifiedsearch/application -count=1` passed after the worker's corrections. The inspected permanent regressions now check ordinary-note identity equality, distinct embedded identities, low confidence with no must-read sources for near-zero lexical/semantic evidence, query-order-independent facet output, and retention of distinct embedded nodes during aggregation. Duplicate source strength is combined with OR rather than overwritten.

This establishes those focused behaviors only. The facet permutation example has three unique sources and a limit of three, so it does not establish quality under a restrictive result budget. Confidence cutoffs remain provisional; fixture scores do not establish relevance calibration. Actual adapter/profile parity, continuation-vector reuse, judged task coverage, and performance acceptance remain open.
