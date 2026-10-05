---
summary: "B19 operation-local resolver reuse in embedded validation, parity evidence, and controlled scaling measurements."
reference-kind: guide
---

# Embedded validation resolver reuse

B19, delivered 2026-09-05. `validateEmbeddedProjectionFields` in `pkg/ontology/index.go` previously built a fresh projection resolver for every field child, collection item, and global-source ref it visited. Constructing a resolver is O(document): it re-extracts inline properties across the whole note, re-resolves the note's type assessment, and re-indexes every `@source` span. A note with N embedded nodes therefore cost O(N x document). The traversal now builds one resolver per call and projects every child through it, matching the B05 catalog pattern. Nothing is cached across operations; the resolver is dropped when the function returns.

The duplicate-ref check moved to the read side: a ref already in `seen` is skipped before it is projected rather than after. Refs fed into this traversal are canonical — `resolver.project(ref)` resolves by `NodeID` and returns the same ref string — so the earlier check is equivalent. Everything else is unchanged: depth-first recursion, the cycle guard, global-source enumeration after the root walk in snapshot `SourceSpans` order, skipping (and not marking seen) a child whose projection fails, and the issue emission order. Map iteration over `Fields` and `Collections` was already unordered and stays that way. This is a computation change; no materialization version, indexed row shape, or exported API changes.

## Parity evidence

`pkg/ontology/embedded_validation_scaling_test.go` keeps a frozen copy of the previous traversal as `validateEmbeddedProjectionFieldsReference` and compares against it as a live regression guard.

- `TestValidateEmbeddedProjectionFields_ParityAcrossScales` — 1, 10, 100, 500, and 2,000 embedded nodes. Asserts equal issue counts, exact slice equality after a full-key sort (pinning `NodeRef`, `NodeID`, `Structural`, `Line`, `Code`, `FieldName`, `TypeName`), `equalValidationIssues`, and an identical unsorted per-field code/ref sequence.
- `TestValidateEmbeddedProjectionFields_SkipsDuplicateRefsWithoutProjecting` — a `@contains` collection and a global `@source` matching the same spans. Parity holds and no `(NodeRef, Code, FieldName)` is emitted twice.
- `TestValidateEmbeddedProjectionFields_PartialProjectionErrorSkipsChild` — an unresolvable ref and a foreign-note ref injected into the root collection. Parity holds, valid siblings still emit, neither bad ref reaches the output.
- `TestValidateEmbeddedProjectionFields_ResolverConstructedOnce` — allocation guard at 500 nodes plus a 500-to-2,000 allocation growth ratio, which is the structural check that per-child resolver construction has not returned.

## Controlled measurements

Apple M4 Pro, Go 1.24.2, `GOMAXPROCS=4`, median of five samples at 200 ms benchtime, no race instrumentation. Both test binaries were compiled before timing and run back to back. The host is a shared laptop, so treat wall times as indicative.

`BenchmarkEmbeddedValidationResolverScaling`:

| Nodes | Before | After | Speedup | Before bytes/op | After bytes/op | Before allocs/op | After allocs/op |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 4.00 ms | 0.46 ms | 8.8x | 7,391,441 | 577,344 | 49,511 | 9,082 |
| 500 | 108.77 ms | 4.02 ms | 27.1x | 206,557,360 | 3,037,011 | 928,999 | 45,627 |
| 2000 | 1,677.08 ms | 41.71 ms | 40.2x | 3,659,774,664 | 12,344,353 | 13,832,380 | 185,724 |

Allocation growth from 500 to 2,000 nodes falls from 14.9x (quadratic) to 4.07x (linear in node count). Bytes/op at 2,000 nodes drops about 296-fold.

Time still grows about 10x rather than 4x across that same range. The remaining superlinearity is `lineNumberAt` (`pkg/ontology/index.go`), which counts newlines from byte zero for every emitted issue and allocates nothing. That is a separate O(issues x document) cost, not resolver construction, and is left for a follow-up.

Workload, the two view tests named in the handoff, run without race instrumentation from compiled binaries:

| Test | Before | After |
| --- | ---: | ---: |
| `TestOntologySourceFiltersAndSortsIndexedRowsBeforePagination` | 1.04 s | 0.38 s |
| `TestOntologySourcePushesNotePathFilterBeforeSourceCap` | 5.63 s | 0.73 s |

CPU profiles over the same pair: before, `validateEmbeddedProjectionFields` held 2.39 s of 7.84 s samples (30.5%), with `newProjectionResolver` at 2.31 s and `indexGlobalSourceTypes` at 1.96 s beneath it. After, the function holds 110 ms of 1.12 s samples (9.8%) and neither `newProjectionResolver` nor `indexGlobalSourceTypes` appears in the top forty cumulative entries.

## Reconstruct the comparison

Run from a checkout containing both commits with the same Go/CGO toolchain. Detached worktrees keep the live vault and index untouched; the retained benchmark is carried into the baseline so no historical production overlay is needed. Compile both binaries before the quiet timing window.

```sh
set -eu
comparison=$(mktemp -d /tmp/rzm-b19-compare.XXXXXX)
git worktree add --detach "$comparison/before" 87b76ede
git worktree add --detach "$comparison/after" codex/engine-b19-resolver-reuse
git show codex/engine-b19-resolver-reuse:pkg/ontology/embedded_validation_scaling_test.go \
  > "$comparison/before/pkg/ontology/embedded_validation_scaling_test.go"
for side in before after; do
  (cd "$comparison/$side" && GOMAXPROCS=4 go test -mod=vendor -tags fts5 -c -o "$comparison/$side.test" ./pkg/ontology)
done
# Enter the quiet timing window only after both builds complete.
for side in before after; do
  GOMAXPROCS=4 "$comparison/$side.test" -test.run '^$' \
    -test.bench '^BenchmarkEmbeddedValidationResolverScaling$' \
    -test.benchmem -test.benchtime=200ms -test.count=5 > "$comparison/$side.txt" 2>&1
done
```

## Follow-ups not taken here

- `lineNumberAt` rescanned note content per issue. Fixed in the follow-up below.
- `walkStoryIDProjections` (`pkg/ontology/sync.go`) and `pkg/ontology/node_link.go` still project children one resolver at a time.
- Field and collection iteration in validation remains map-ordered. Making it deterministic is a behavior change and belongs to its own decision.

## Follow-up: line-start index and sibling-ordinal memo

Delivered 2026-09-05 on top of the resolver reuse above. The remaining 10x growth from 500 to 2,000 nodes had two sources, not one:

- `lineNumberAt` counted newlines from byte zero for every emitted issue, O(issues x document). `validateEmbeddedProjectionFields` now builds a `lineIndex` (`pkg/ontology/line_index.go`, a slice of line-start offsets) once per traversal and resolves each issue by `sort.Search`. `validationIssuesFromBlockIDs` and `ontologyProjectionLinkFacts` had the same per-loop shape and take the same index. `TestLineIndex_LineAt` pins offset 0, offsets on a newline byte, the final byte, `offset == len`, offsets past the end, negative offsets, a trailing newline, and CRLF content; the table was first run green against the old `lineNumberAt` and then switched to the new API.
- `sourceSpanSiblingOrdinal` scanned every span in the snapshot to find one span's ordinal among its siblings, and `stableSourceSpanFingerprint` called it for every `sourceSpanNodeRef`, so global-source enumeration plus per-child projection was O(spans^2). The line index alone only brought the ratio to 7.8x; the profile then showed `sourceSpanSiblingOrdinal` at 34% of samples. The resolver now memoizes ordinals in one pass on first fingerprint (`projectionResolver.siblingOrdinal`), which stays inside the operation-local resolver and is dropped with it. `TestProjectionResolver_SiblingOrdinalMatchesLinearScan` compares the memo against the linear scan for every span in a nested, mixed-shape fixture plus a span absent from the snapshot.

Same host, toolchain, and benchmark settings as above (`GOMAXPROCS=4`, `-benchtime=200ms -count=5`, no race), medians, indicative:

| Nodes | Before (B19) | Line index only | Line index + ordinal memo | Speedup |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 0.52 ms | 0.43 ms | 0.44 ms | 1.2x |
| 500 | 4.47 ms | 3.03 ms | 2.38 ms | 1.9x |
| 2000 | 44.5 ms | 23.7 ms | 9.34 ms | 4.8x |

The 500-to-2,000 wall-time ratio drops from 9.96x to 3.92x, matching the 4x node-count ratio. Allocations per run are unchanged within a handful (one line-start slice and one ordinal map per traversal); the existing parity tests in `embedded_validation_scaling_test.go` still pass against the frozen reference traversal, so issue counts, fields, and `Structural` hashes are byte-identical.
